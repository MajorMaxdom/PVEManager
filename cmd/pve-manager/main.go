// Command pve-manager: web UI to manage several Proxmox VE hosts.
package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"pvemanager/internal/api"
	"pvemanager/internal/auth"
	"pvemanager/internal/backup"
	"pvemanager/internal/config"
	"pvemanager/internal/crypto"
	"pvemanager/internal/monitor"
	"pvemanager/internal/oidcauth"
	"pvemanager/internal/poller"
	"pvemanager/internal/store"
	"pvemanager/internal/tlsutil"
	"pvemanager/web"
)

var version = "dev"

const defaultConfig = "/etc/pve-manager/config.yaml"

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	cmd, args := os.Args[1], os.Args[2:]
	var err error
	switch cmd {
	case "serve":
		err = serve(args)
	case "user":
		err = userCmd(args)
	case "server":
		err = serverCmd(args)
	case "backup":
		err = backupCmd(args)
	case "restore":
		err = restoreCmd(args)
	case "version":
		fmt.Println(version)
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage: pve-manager <command> [-config FILE]
  serve                              run the web UI
  user add NAME [-role admin|operator|viewer] [-password-file F]
  user list
  server add -file servers.d/x.yaml  import a server definition
  server list | server rm NAME
  backup [-out F] [-passphrase-file F]   encrypted archive of database, keys and config
  restore -in F [-force] [-dry-run]      restore it (service must be stopped)
  version`)
	os.Exit(2)
}

func open(cfgPath string) (config.Config, *store.Store, error) {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return cfg, nil, err
	}
	if err := os.MkdirAll(cfg.DataDir, 0o750); err != nil {
		return cfg, nil, err
	}
	key, err := crypto.LoadOrCreateKey(cfg.SecretFile)
	if err != nil {
		return cfg, nil, err
	}
	box, err := crypto.New(key)
	if err != nil {
		return cfg, nil, err
	}
	st, err := store.Open(cfg.DataDir, box)
	return cfg, st, err
}

func importServers(st *store.Store, dir string) error {
	list, err := config.LoadServers(dir)
	if err != nil {
		return err
	}
	for _, s := range list {
		if err := putServer(st, s); err != nil {
			return err
		}
	}
	return nil
}

func putServer(st *store.Store, s config.Server) error {
	v := store.Server{Name: s.Name, URL: s.URL, Node: s.Node, Fingerprint: s.Fingerprint, Token: s.Token}
	if s.SSH != nil {
		v.SSHUser, v.SSHHost, v.SSHCmd, v.SSHHostKey = s.SSH.User, s.SSH.Host, s.SSH.UpgradeCommand, s.SSH.HostKey
	}
	return st.PutServer(v)
}

func serve(args []string) error {
	fl := flag.NewFlagSet("serve", flag.ExitOnError)
	cfgPath := fl.String("config", defaultConfig, "config file")
	_ = fl.Parse(args)
	cfg, st, err := open(*cfgPath)
	if err != nil {
		return err
	}
	defer st.Close()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := importServers(st, cfg.ServersDir); err != nil {
		return err
	}
	servers, err := st.Servers()
	if err != nil {
		return err
	}
	if n, _ := st.CountUsers(); n == 0 {
		log.Warn("no users yet - create one: pve-manager user add NAME -role admin")
	}
	p := poller.New()
	p.SetServers(servers)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go p.Run(ctx)
	mon := monitor.New(st, p, log)
	go mon.Run(ctx)
	go func() {
		for range time.Tick(time.Hour) {
			st.PurgeSessions()
		}
	}()

	static, _ := fs.Sub(web.Dist, "dist")
	apiSrv := api.New(st, p, static, cfg.TLS.Mode != "off", cfg.SSHKeyFile, log)
	apiSrv.Notify = mon.Emit
	apiSrv.Version = version
	apiSrv.BackupFn = func(pass string, w io.Writer) error {
		return backup.Create(w, pass, backup.Layout{ConfigPath: *cfgPath, Cfg: cfg}, version, st.Snapshot)
	}
	if cfg.OIDC.Issuer != "" {
		sec, err := os.ReadFile(cfg.OIDC.ClientSecretFile)
		if err != nil {
			return fmt.Errorf("oidc.client_secret_file: %w", err)
		}
		redirect := cfg.OIDC.RedirectURL
		if redirect == "" {
			scheme := "https"
			if cfg.TLS.Mode == "off" {
				scheme = "http"
			}
			host := cfg.Hostname
			if host == "" {
				host = "localhost"
			}
			_, port, _ := strings.Cut(cfg.Listen, ":")
			redirect = fmt.Sprintf("%s://%s:%s/api/oidc/callback", scheme, host, port)
		}
		apiSrv.OIDC = oidcauth.New(cfg.OIDC, strings.TrimSpace(string(sec)), redirect)
		log.Info("single sign-on enabled", "issuer", cfg.OIDC.Issuer, "redirect", redirect)
	}
	if cfg.MetricsTokenFile != "" {
		b, err := os.ReadFile(cfg.MetricsTokenFile)
		if err != nil {
			return fmt.Errorf("metrics_token_file: %w", err)
		}
		if apiSrv.MetricsToken = strings.TrimSpace(string(b)); len(apiSrv.MetricsToken) < 24 {
			return fmt.Errorf("metrics token in %s is too short (min 24 characters)", cfg.MetricsTokenFile)
		}
		log.Info("metrics endpoint enabled", "path", "/metrics")
	}
	srv := &http.Server{Addr: cfg.Listen, Handler: apiSrv.Handler(),
		ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute}
	go func() {
		<-ctx.Done()
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(c)
	}()

	log.Info("listening", "addr", cfg.Listen, "tls", cfg.TLS.Mode, "servers", len(servers), "version", version)
	switch cfg.TLS.Mode {
	case "off":
		err = srv.ListenAndServe()
	case "files":
		srv.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
		err = srv.ListenAndServeTLS(cfg.TLS.Cert, cfg.TLS.Key)
	default:
		var c tls.Certificate
		if c, err = tlsutil.SelfSigned(cfg.DataDir, cfg.Hostname); err != nil {
			return err
		}
		srv.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{c}}
		err = srv.ListenAndServeTLS("", "")
	}
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func userCmd(args []string) error {
	if len(args) >= 1 && args[0] == "list" {
		fl := flag.NewFlagSet("user list", flag.ExitOnError)
		cfgPath := fl.String("config", defaultConfig, "config file")
		_ = fl.Parse(args[1:])
		_, st, err := open(*cfgPath)
		if err != nil {
			return err
		}
		defer st.Close()
		us, err := st.Users()
		if err != nil {
			return err
		}
		for _, u := range us {
			fmt.Printf("%s\t%s\n", u.Name, u.Role)
		}
		return nil
	}
	if len(args) < 2 || args[0] != "add" {
		usage()
	}
	name := args[1]
	fl := flag.NewFlagSet("user add", flag.ExitOnError)
	cfgPath := fl.String("config", defaultConfig, "config file")
	role := fl.String("role", "admin", "admin|operator|viewer")
	pwFile := fl.String("password-file", "", "read password from file (non-interactive)")
	_ = fl.Parse(args[2:])
	if !auth.Allows(*role, "viewer") {
		return fmt.Errorf("invalid role %q", *role)
	}
	var pw string
	switch {
	case *pwFile != "":
		b, err := os.ReadFile(*pwFile)
		if err != nil {
			return err
		}
		pw = strings.TrimRight(string(b), "\r\n")
	case term.IsTerminal(int(os.Stdin.Fd())):
		fmt.Fprint(os.Stderr, "Password: ")
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return err
		}
		pw = string(b)
	default:
		l, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		pw = strings.TrimRight(l, "\r\n")
	}
	h, err := auth.HashPassword(pw)
	if err != nil {
		return err
	}
	_, st, err := open(*cfgPath)
	if err != nil {
		return err
	}
	defer st.Close()
	if err := st.PutUser(store.User{Name: name, Hash: h, Role: *role}); err != nil {
		return err
	}
	fmt.Printf("user %s (%s) saved\n", name, *role)
	return nil
}

func serverCmd(args []string) error {
	if len(args) < 1 {
		usage()
	}
	fl := flag.NewFlagSet("server", flag.ExitOnError)
	cfgPath := fl.String("config", defaultConfig, "config file")
	file := fl.String("file", "", "server definition (add)")
	rest := args[1:]
	_ = fl.Parse(rest)
	_, st, err := open(*cfgPath)
	if err != nil {
		return err
	}
	defer st.Close()
	switch args[0] {
	case "add":
		s, err := config.LoadServer(*file)
		if err != nil {
			return err
		}
		if err := putServer(st, s); err != nil {
			return err
		}
		fmt.Println("server", s.Name, "saved")
	case "list":
		l, err := st.Servers()
		if err != nil {
			return err
		}
		for _, s := range l {
			fmt.Printf("%-12s %s  ssh=%v\n", s.Name, s.URL, s.SSHUser != "")
		}
	case "rm":
		if len(rest) == 0 {
			usage()
		}
		return st.DeleteServer(rest[0])
	default:
		usage()
	}
	return nil
}
