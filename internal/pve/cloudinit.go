package pve

import (
	"context"
	"errors"
	"net"
	"net/url"
	"regexp"
	"strings"
)

var (
	reCIUser   = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)
	reDomain   = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]{0,251}[A-Za-z0-9])?$`)
	reCIDR     = regexp.MustCompile(`^[0-9]{1,3}(\.[0-9]{1,3}){3}/[0-9]{1,2}$`)
	reIPv4     = regexp.MustCompile(`^[0-9]{1,3}(\.[0-9]{1,3}){3}$`)
	reCIDrive  = regexp.MustCompile(`^(ide|sata|scsi)[0-9]{1,2}$`)
	reCIConfig = regexp.MustCompile(`cloudinit`)
)

// CloudInit describes the cloud-init settings of a VM. Empty fields are left unchanged.
type CloudInit struct {
	User       string `json:"user"`
	Password   string `json:"password"`
	SSHKeys    string `json:"sshKeys"` // one key per line
	IP         string `json:"ip"`      // "" (unchanged), "dhcp" or CIDR
	Gateway    string `json:"gateway"`
	Nameserver string `json:"nameserver"` // space separated IPs
	SearchDom  string `json:"searchdomain"`
	DriveStore string `json:"driveStorage"` // add a cloud-init drive on this storage when the VM has none
}

func validIP(s string) bool { return reIPv4.MatchString(s) && net.ParseIP(s) != nil }

func (c CloudInit) Validate() error {
	if c.User != "" && !reCIUser.MatchString(c.User) {
		return errors.New("invalid cloud-init user (lowercase letters, digits, _ and -)")
	}
	if c.Password != "" && (len(c.Password) < 5 || len(c.Password) > 128 || strings.ContainsAny(c.Password, "\r\n")) {
		return errors.New("password must have 5-128 characters and no line breaks")
	}
	for _, k := range c.keys() {
		if !reSSHKey.MatchString(k) {
			return errors.New("invalid SSH public key (one key per line)")
		}
	}
	switch {
	case c.IP == "" && c.Gateway != "":
		return errors.New("a gateway needs an IP")
	case c.IP != "" && c.IP != "dhcp":
		if !reCIDR.MatchString(c.IP) {
			return errors.New("IP must be 'dhcp' or CIDR like 192.168.1.20/24")
		}
		if ip, _, err := net.ParseCIDR(c.IP); err != nil || ip.To4() == nil {
			return errors.New("invalid IP address")
		}
		if c.Gateway != "" && !validIP(c.Gateway) {
			return errors.New("invalid gateway")
		}
	case c.IP == "dhcp" && c.Gateway != "":
		return errors.New("DHCP takes no gateway")
	}
	for _, ns := range strings.Fields(c.Nameserver) {
		if !validIP(ns) {
			return errors.New("invalid nameserver address")
		}
	}
	if c.SearchDom != "" && !reDomain.MatchString(c.SearchDom) {
		return errors.New("invalid search domain")
	}
	if c.DriveStore != "" && !reStorage.MatchString(c.DriveStore) {
		return errors.New("invalid storage")
	}
	return nil
}

func (c CloudInit) keys() []string {
	var out []string
	for _, l := range strings.Split(c.SSHKeys, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// Form builds the PVE parameters. sshkeys must be percent-encoded for PVE (it decodes it itself).
func (c CloudInit) Form() url.Values {
	f := url.Values{}
	if c.User != "" {
		f.Set("ciuser", c.User)
	}
	if c.Password != "" {
		f.Set("cipassword", c.Password)
	}
	if ks := c.keys(); len(ks) > 0 {
		f.Set("sshkeys", strings.ReplaceAll(url.QueryEscape(strings.Join(ks, "\n")+"\n"), "+", "%20"))
	}
	if c.IP != "" {
		ic := "ip=" + c.IP
		if c.Gateway != "" {
			ic += ",gw=" + c.Gateway
		}
		f.Set("ipconfig0", ic)
	}
	if c.Nameserver != "" {
		f.Set("nameserver", strings.Join(strings.Fields(c.Nameserver), " "))
	}
	if c.SearchDom != "" {
		f.Set("searchdomain", c.SearchDom)
	}
	return f
}

// SetCloudInit applies the settings, adding a cloud-init drive first when requested and missing.
func (c *Client) SetCloudInit(ctx context.Context, node string, vmid int, ci CloudInit) error {
	if err := ci.Validate(); err != nil {
		return err
	}
	cfg, err := c.Config(ctx, node, "qemu", vmid)
	if err != nil {
		return err
	}
	f := ci.Form()
	if !cfg.HasCloudInit {
		if ci.DriveStore == "" {
			return errors.New("this VM has no cloud-init drive; choose a storage to add one")
		}
		f.Set(freeSlot(cfg), ci.DriveStore+":cloudinit")
	}
	if len(f) == 0 {
		return errors.New("nothing to change")
	}
	p, err := gpath(node, "qemu", vmid, "/config")
	if err != nil {
		return err
	}
	return c.do(ctx, "PUT", p, f, nil)
}

func freeSlot(cfg GuestConfig) string {
	used := map[string]bool{}
	for _, d := range cfg.AllKeys {
		used[d] = true
	}
	for _, k := range []string{"ide2", "ide3", "sata5", "sata4"} {
		if !used[k] {
			return k
		}
	}
	return "ide2"
}

// ConvertToTemplate turns a stopped guest into a template (irreversible).
func (c *Client) ConvertToTemplate(ctx context.Context, node, typ string, vmid int) (string, error) {
	p, err := gpath(node, typ, vmid, "/template")
	if err != nil {
		return "", err
	}
	var upid any
	err = c.post(ctx, p, nil, &upid)
	s, _ := upid.(string)
	if err == nil && s == "" {
		return "", nil
	}
	return s, err
}
