// Package config loads /etc/pve-manager/config.yaml and servers.d/*.yaml.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

type TLS struct {
	Mode string `yaml:"mode"` // selfsigned | files | off
	Cert string `yaml:"cert"`
	Key  string `yaml:"key"`
}

type Config struct {
	Listen           string `yaml:"listen"`             // host:port
	DataDir          string `yaml:"data_dir"`           // sqlite lives here
	SecretFile       string `yaml:"secret_file"`        // 32-byte master key
	ServersDir       string `yaml:"servers_dir"`        // *.yaml server definitions (imported at start)
	MetricsTokenFile string `yaml:"metrics_token_file"` // empty = /metrics disabled
	SSHKeyFile       string `yaml:"ssh_key_file"`       // private key used for upgrades over SSH
	Hostname         string `yaml:"hostname"`           // used for the self-signed certificate
	TLS              TLS    `yaml:"tls"`
	OIDC             OIDC   `yaml:"oidc"`
}

// SSH holds the optional upgrade-over-SSH settings of a server.
type SSH struct {
	User           string `yaml:"user"`
	Host           string `yaml:"host"`
	UpgradeCommand string `yaml:"upgrade_command"`
	HostKey        string `yaml:"host_key"` // SHA256:... fingerprint of the SSH host key
}

// Server is one PVE host definition as written by onboard-pve-host.sh.
type Server struct {
	Name        string `yaml:"name"`
	URL         string `yaml:"url"`
	Node        string `yaml:"node"`
	PVEVersion  string `yaml:"pve_version"`
	Token       string `yaml:"token"`
	Fingerprint string `yaml:"fingerprint"`
	SSH         *SSH   `yaml:"ssh"`
}

func Default() Config {
	return Config{
		Listen:     "0.0.0.0:8443",
		DataDir:    "/var/lib/pve-manager",
		SecretFile: "/etc/pve-manager/secret.key",
		ServersDir: "/etc/pve-manager/servers.d",
		SSHKeyFile: "/etc/pve-manager/ssh/id_ed25519",
		TLS:        TLS{Mode: "selfsigned"},
	}
}

func Load(path string) (Config, error) {
	c := Default()
	b, err := os.ReadFile(path)
	if err != nil {
		return c, err
	}
	if err := yaml.Unmarshal(b, &c); err != nil {
		return c, fmt.Errorf("%s: %w", path, err)
	}
	switch c.TLS.Mode {
	case "selfsigned", "off":
	case "files":
		if c.TLS.Cert == "" || c.TLS.Key == "" {
			return c, fmt.Errorf("tls.mode=files needs tls.cert and tls.key")
		}
	default:
		return c, fmt.Errorf("unknown tls.mode %q", c.TLS.Mode)
	}
	if c.OIDC.Issuer != "" {
		if c.OIDC.ClientID == "" || c.OIDC.ClientSecretFile == "" {
			return c, fmt.Errorf("oidc needs client_id and client_secret_file")
		}
		switch c.OIDC.DefaultRole {
		case "", "none", "viewer", "operator", "admin":
		default:
			return c, fmt.Errorf("oidc.default_role must be none, viewer, operator or admin")
		}
	}
	return c, nil
}

func LoadServer(path string) (Server, error) {
	var s Server
	b, err := os.ReadFile(path)
	if err != nil {
		return s, err
	}
	if err := yaml.Unmarshal(b, &s); err != nil {
		return s, fmt.Errorf("%s: %w", path, err)
	}
	if s.Name == "" || s.URL == "" || s.Token == "" {
		return s, fmt.Errorf("%s: name, url and token are required", path)
	}
	return s, nil
}

func LoadServers(dir string) ([]Server, error) {
	files, _ := filepath.Glob(filepath.Join(dir, "*.yaml"))
	sort.Strings(files)
	var out []Server
	for _, f := range files {
		s, err := LoadServer(f)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

// OIDC configures optional single sign-on. Empty Issuer disables it.
type OIDC struct {
	Issuer           string   `yaml:"issuer"`
	ClientID         string   `yaml:"client_id"`
	ClientSecretFile string   `yaml:"client_secret_file"`
	RedirectURL      string   `yaml:"redirect_url"` // default: https://<hostname>:<port>/api/oidc/callback
	Scopes           []string `yaml:"scopes"`
	UsernameClaim    string   `yaml:"username_claim"` // default preferred_username
	GroupsClaim      string   `yaml:"groups_claim"`   // default groups
	AdminGroup       string   `yaml:"admin_group"`
	OperatorGroup    string   `yaml:"operator_group"`
	ViewerGroup      string   `yaml:"viewer_group"`
	DefaultRole      string   `yaml:"default_role"` // role for users in no mapped group: viewer|operator|admin|none (default none)
	ButtonLabel      string   `yaml:"button_label"`
}
