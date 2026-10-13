package main

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"pvemanager/internal/backup"
	"pvemanager/internal/config"
	"pvemanager/internal/crypto"
	"pvemanager/internal/store"
)

func readPassphrase(file string, confirm bool) (string, error) {
	switch {
	case file != "":
		b, err := os.ReadFile(file)
		return strings.TrimRight(string(b), "\r\n"), err
	case term.IsTerminal(int(os.Stdin.Fd())):
		fmt.Fprint(os.Stderr, "Passphrase: ")
		a, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", err
		}
		if confirm {
			fmt.Fprint(os.Stderr, "Repeat passphrase: ")
			b, err := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Fprintln(os.Stderr)
			if err != nil {
				return "", err
			}
			if !bytes.Equal(a, b) {
				return "", errors.New("the passphrases do not match")
			}
		}
		return string(a), nil
	default:
		l, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		return strings.TrimRight(l, "\r\n"), nil
	}
}

// dropToOwnerOf makes a root process continue as the owner of dir, so SQLite never creates root-owned
// WAL/SHM files next to the service's database.
func dropToOwnerOf(dir string) error {
	if os.Geteuid() != 0 {
		return nil
	}
	fi, err := os.Stat(dir)
	if err != nil {
		return nil
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok || st.Uid == 0 {
		return nil
	}
	if err := syscall.Setgroups([]int{int(st.Gid)}); err != nil {
		return err
	}
	if err := syscall.Setgid(int(st.Gid)); err != nil {
		return err
	}
	return syscall.Setuid(int(st.Uid))
}

func backupCmd(args []string) error {
	fl := flag.NewFlagSet("backup", flag.ExitOnError)
	cfgPath := fl.String("config", defaultConfig, "config file")
	out := fl.String("out", "", "output file (will not overwrite)")
	pwFile := fl.String("passphrase-file", "", "read the passphrase from a file")
	_ = fl.Parse(args)
	if *out == "" {
		*out = fmt.Sprintf("pve-manager-backup-%s.pvmbak", time.Now().Format("20060102-1504"))
	}
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	// Everything that needs the caller's identity comes first: the passphrase file and the output file.
	pass, err := readPassphrase(*pwFile, true)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(*out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	// Then continue as the owner of the data directory, so SQLite never leaves root-owned WAL/SHM files behind.
	if err := dropToOwnerOf(cfg.DataDir); err != nil {
		f.Close()
		os.Remove(*out)
		return err
	}
	key, err := os.ReadFile(cfg.SecretFile)
	if err != nil {
		f.Close()
		os.Remove(*out)
		return err
	}
	box, err := crypto.New(key)
	if err != nil {
		f.Close()
		os.Remove(*out)
		return err
	}
	st, err := store.Open(cfg.DataDir, box)
	if err != nil {
		f.Close()
		os.Remove(*out)
		return err
	}
	defer st.Close()
	if err := backup.Create(f, pass, backup.Layout{ConfigPath: *cfgPath, Cfg: cfg}, version, st.Snapshot); err != nil {
		f.Close()
		os.Remove(*out)
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	fmt.Printf("backup written to %s (encrypted, mode 0600)\nKeep the passphrase safe: without it the backup cannot be opened.\n", *out)
	return nil
}

func restoreCmd(args []string) error {
	fl := flag.NewFlagSet("restore", flag.ExitOnError)
	cfgPath := fl.String("config", defaultConfig, "config file location on this host")
	in := fl.String("in", "", "backup file")
	pwFile := fl.String("passphrase-file", "", "read the passphrase from a file")
	force := fl.Bool("force", false, "replace data of an existing installation")
	dry := fl.Bool("dry-run", false, "show what would be written, change nothing")
	_ = fl.Parse(args)
	if *in == "" {
		return errors.New("-in FILE is required")
	}
	if !*dry && exec.Command("systemctl", "is-active", "--quiet", "pve-manager").Run() == nil {
		return errors.New("the pve-manager service is running; stop it first: sudo systemctl stop pve-manager")
	}
	pass, err := readPassphrase(*pwFile, false)
	if err != nil {
		return err
	}
	f, err := os.Open(*in)
	if err != nil {
		return err
	}
	defer f.Close()
	t := backup.DefaultTarget(*cfgPath)
	if c, err := config.Load(*cfgPath); err == nil {
		t.DataDir = c.DataDir
	}
	rep, err := backup.Restore(f, pass, t, *force, *dry)
	if err != nil {
		return err
	}
	verb := "restored"
	if *dry {
		verb = "would restore"
	}
	fmt.Printf("backup of %s from %s (pve-manager %s): %s\n", rep.Manifest.Host, rep.Manifest.Created.Format(time.RFC3339), rep.Manifest.Version, verb)
	for _, p := range rep.Written {
		fmt.Println("  ", p)
	}
	for _, p := range rep.Conflicts {
		fmt.Println("   exists, a real restore needs -force:", p)
	}
	for _, p := range rep.Skipped {
		fmt.Println("   skipped unexpected entry:", p)
	}
	if !*dry {
		if err := backup.FixOwnership(rep, "pvemanager"); err != nil {
			return err
		}
		fmt.Println("Done. Start the service: sudo systemctl start pve-manager")
	}
	return nil
}
