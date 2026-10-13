package pve

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var (
	reName    = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)
	reStorage = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	reBridge  = regexp.MustCompile(`^[A-Za-z0-9._-]{1,15}$`)
	reVolID   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*:(iso|vztmpl)/[A-Za-z0-9._+ -]+$`)
	reSSHKey  = regexp.MustCompile(`^(ssh-ed25519|ssh-rsa|ecdsa-sha2-[a-z0-9]+) [A-Za-z0-9+/=]+( [^\r\n]*)?$`)
	reVLAN    = regexp.MustCompile(`^[0-9]{1,4}$`)
)

// VMSpec is the user-facing description of a new VM.
type VMSpec struct {
	VMID    int    `json:"vmid"`
	Name    string `json:"name"`
	Cores   int    `json:"cores"`
	MemMiB  int    `json:"memory"`
	DiskGiB int    `json:"disk"`
	Storage string `json:"storage"`
	ISO     string `json:"iso"`    // volid or empty
	Bridge  string `json:"bridge"` // e.g. vmbr0
	VLAN    string `json:"vlan"`
	UEFI    bool   `json:"uefi"`
	Agent   bool   `json:"agent"`
	Start   bool   `json:"start"`
}

// CTSpec describes a new LXC container.
type CTSpec struct {
	VMID         int    `json:"vmid"`
	Hostname     string `json:"hostname"`
	Template     string `json:"template"` // volid
	Cores        int    `json:"cores"`
	MemMiB       int    `json:"memory"`
	SwapMiB      int    `json:"swap"`
	DiskGiB      int    `json:"disk"`
	Storage      string `json:"storage"`
	Bridge       string `json:"bridge"`
	VLAN         string `json:"vlan"`
	IP           string `json:"ip"` // "dhcp" or CIDR
	Gateway      string `json:"gateway"`
	Password     string `json:"password"`
	SSHKey       string `json:"sshKey"`
	Unprivileged bool   `json:"unprivileged"`
	Nesting      bool   `json:"nesting"`
	Start        bool   `json:"start"`
}

func between(n, lo, hi int, what string) error {
	if n < lo || n > hi {
		return fmt.Errorf("%s must be between %d and %d", what, lo, hi)
	}
	return nil
}

func netOK(bridge, vlan string) error {
	if !reBridge.MatchString(bridge) {
		return errors.New("invalid bridge")
	}
	if vlan != "" {
		if !reVLAN.MatchString(vlan) {
			return errors.New("invalid VLAN")
		}
		if v, _ := strconv.Atoi(vlan); v < 1 || v > 4094 {
			return errors.New("VLAN must be 1-4094")
		}
	}
	return nil
}

// Validate checks every field that ends up in a PVE parameter string.
func (v VMSpec) Validate() error {
	switch {
	case v.VMID < 100 || v.VMID > 999999999:
		return errors.New("vmid must be 100 or higher")
	case !reName.MatchString(v.Name):
		return errors.New("invalid name (letters, digits, hyphen)")
	case !reStorage.MatchString(v.Storage):
		return errors.New("invalid storage")
	case v.ISO != "" && !reVolID.MatchString(v.ISO):
		return errors.New("invalid ISO")
	}
	for _, e := range []error{between(v.Cores, 1, 128, "cores"), between(v.MemMiB, 16, 4<<20, "memory"), between(v.DiskGiB, 1, 65536, "disk"), netOK(v.Bridge, v.VLAN)} {
		if e != nil {
			return e
		}
	}
	return nil
}

func (v CTSpec) Validate() error {
	switch {
	case v.VMID < 100 || v.VMID > 999999999:
		return errors.New("vmid must be 100 or higher")
	case !reName.MatchString(v.Hostname):
		return errors.New("invalid hostname")
	case !reVolID.MatchString(v.Template) || !strings.Contains(v.Template, ":vztmpl/"):
		return errors.New("invalid template")
	case !reStorage.MatchString(v.Storage):
		return errors.New("invalid storage")
	case v.Password == "" && v.SSHKey == "":
		return errors.New("set a root password or an SSH public key")
	case v.Password != "" && len(v.Password) < 8:
		return errors.New("password must have at least 8 characters")
	case v.SSHKey != "" && !reSSHKey.MatchString(strings.TrimSpace(v.SSHKey)):
		return errors.New("invalid SSH public key")
	}
	for _, e := range []error{between(v.Cores, 1, 128, "cores"), between(v.MemMiB, 16, 4<<20, "memory"), between(v.SwapMiB, 0, 4<<20, "swap"), between(v.DiskGiB, 1, 65536, "disk"), netOK(v.Bridge, v.VLAN)} {
		if e != nil {
			return e
		}
	}
	if v.IP != "dhcp" {
		if !regexp.MustCompile(`^[0-9]{1,3}(\.[0-9]{1,3}){3}/[0-9]{1,2}$`).MatchString(v.IP) {
			return errors.New("IP must be 'dhcp' or CIDR like 192.168.1.20/24")
		}
		if v.Gateway != "" && !regexp.MustCompile(`^[0-9]{1,3}(\.[0-9]{1,3}){3}$`).MatchString(v.Gateway) {
			return errors.New("invalid gateway")
		}
	}
	return nil
}

func b2i(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

func (v VMSpec) Form() url.Values {
	f := url.Values{}
	f.Set("vmid", strconv.Itoa(v.VMID))
	f.Set("name", v.Name)
	f.Set("cores", strconv.Itoa(v.Cores))
	f.Set("memory", strconv.Itoa(v.MemMiB))
	f.Set("cpu", "host")
	f.Set("ostype", "l26")
	f.Set("machine", "q35")
	f.Set("scsihw", "virtio-scsi-single")
	f.Set("scsi0", fmt.Sprintf("%s:%d", v.Storage, v.DiskGiB))
	net := "virtio,bridge=" + v.Bridge
	if v.VLAN != "" {
		net += ",tag=" + v.VLAN
	}
	f.Set("net0", net)
	if v.ISO != "" {
		f.Set("ide2", v.ISO+",media=cdrom")
		f.Set("boot", "order=ide2;scsi0")
	} else {
		f.Set("boot", "order=scsi0")
	}
	if v.UEFI {
		f.Set("bios", "ovmf")
		f.Set("efidisk0", v.Storage+":1,efitype=4m,pre-enrolled-keys=0")
	}
	f.Set("agent", b2i(v.Agent))
	f.Set("start", b2i(v.Start))
	return f
}

func (v CTSpec) Form() url.Values {
	f := url.Values{}
	f.Set("vmid", strconv.Itoa(v.VMID))
	f.Set("hostname", v.Hostname)
	f.Set("ostemplate", v.Template)
	f.Set("cores", strconv.Itoa(v.Cores))
	f.Set("memory", strconv.Itoa(v.MemMiB))
	f.Set("swap", strconv.Itoa(v.SwapMiB))
	f.Set("storage", v.Storage)
	f.Set("rootfs", fmt.Sprintf("%s:%d", v.Storage, v.DiskGiB))
	net := "name=eth0,bridge=" + v.Bridge + ",ip=" + v.IP
	if v.IP != "dhcp" && v.Gateway != "" {
		net += ",gw=" + v.Gateway
	}
	if v.VLAN != "" {
		net += ",tag=" + v.VLAN
	}
	f.Set("net0", net)
	if v.Password != "" {
		f.Set("password", v.Password)
	}
	if v.SSHKey != "" {
		f.Set("ssh-public-keys", strings.TrimSpace(v.SSHKey))
	}
	f.Set("unprivileged", b2i(v.Unprivileged))
	if v.Nesting {
		f.Set("features", "nesting=1")
	}
	f.Set("start", b2i(v.Start))
	return f
}

func (c *Client) CreateVM(ctx context.Context, node string, v VMSpec) (string, error) {
	if !validNode.MatchString(node) {
		return "", errors.New("invalid node")
	}
	if err := v.Validate(); err != nil {
		return "", err
	}
	var upid string
	return upid, c.post(ctx, "/nodes/"+node+"/qemu", v.Form(), &upid)
}

func (c *Client) CreateCT(ctx context.Context, node string, v CTSpec) (string, error) {
	if !validNode.MatchString(node) {
		return "", errors.New("invalid node")
	}
	if err := v.Validate(); err != nil {
		return "", err
	}
	var upid string
	return upid, c.post(ctx, "/nodes/"+node+"/lxc", v.Form(), &upid)
}

// Delete removes a stopped guest and its disks.
func (c *Client) Delete(ctx context.Context, node, typ string, vmid int) (string, error) {
	if !validNode.MatchString(node) || !validType.MatchString(typ) {
		return "", errors.New("invalid request")
	}
	var upid string
	err := c.do(ctx, "DELETE", fmt.Sprintf("/nodes/%s/%s/%d?purge=1&destroy-unreferenced-disks=1", node, typ, vmid), nil, &upid)
	return upid, err
}

// Options are the choices the create dialogs need.
type Options struct {
	NextID    int           `json:"nextid"`
	Storages  Storages      `json:"storages"`
	ISOs      []ContentItem `json:"isos"`
	Templates []ContentItem `json:"templates"`
	Bridges   []string      `json:"bridges"`
}
type Storages struct {
	Images  []Storage `json:"images"`  // VM disks
	Rootdir []Storage `json:"rootdir"` // container volumes
}
type Storage struct {
	Name  string `json:"storage"`
	Type  string `json:"type"`
	Avail int64  `json:"avail"`
}
type ContentItem struct {
	VolID string `json:"volid"`
	Size  int64  `json:"size"`
}

func (c *Client) CreateOptions(ctx context.Context, node string) (Options, error) {
	var o Options
	if !validNode.MatchString(node) {
		return o, errors.New("invalid node")
	}
	var id any
	if err := c.get(ctx, "/cluster/nextid", &id); err != nil {
		return o, err
	}
	o.NextID, _ = strconv.Atoi(fmt.Sprint(id))
	if f, ok := id.(float64); ok {
		o.NextID = int(f)
	}
	var st []struct {
		Storage string `json:"storage"`
		Type    string `json:"type"`
		Content string `json:"content"`
		Avail   int64  `json:"avail"`
		Active  int    `json:"active"`
	}
	if err := c.get(ctx, "/nodes/"+node+"/storage", &st); err != nil {
		return o, err
	}
	for _, s := range st {
		if s.Active == 0 {
			continue
		}
		has := func(k string) bool {
			for _, p := range strings.Split(s.Content, ",") {
				if p == k {
					return true
				}
			}
			return false
		}
		item := Storage{Name: s.Storage, Type: s.Type, Avail: s.Avail}
		if has("images") {
			o.Storages.Images = append(o.Storages.Images, item)
		}
		if has("rootdir") {
			o.Storages.Rootdir = append(o.Storages.Rootdir, item)
		}
		for kind, dst := range map[string]*[]ContentItem{"iso": &o.ISOs, "vztmpl": &o.Templates} {
			if !has(kind) {
				continue
			}
			var items []ContentItem
			if err := c.get(ctx, fmt.Sprintf("/nodes/%s/storage/%s/content?content=%s", node, s.Storage, kind), &items); err == nil {
				*dst = append(*dst, items...)
			}
		}
	}
	var nets []struct {
		Iface string `json:"iface"`
		Type  string `json:"type"`
	}
	if err := c.get(ctx, "/nodes/"+node+"/network", &nets); err == nil {
		for _, n := range nets {
			if n.Type == "bridge" {
				o.Bridges = append(o.Bridges, n.Iface)
			}
		}
	}
	return o, nil
}
