package pve

import (
	"context"
	"errors"
	"sort"
)

type StorageInfo struct {
	Storage string  `json:"storage"`
	Type    string  `json:"type"`
	Content string  `json:"content"`
	Active  int     `json:"active"`
	Enabled int     `json:"enabled"`
	Shared  int     `json:"shared"`
	Total   int64   `json:"total"`
	Used    int64   `json:"used"`
	Avail   int64   `json:"avail"`
	Frac    float64 `json:"used_fraction"`
}

func (c *Client) StorageList(ctx context.Context, node string) ([]StorageInfo, error) {
	if !validNode.MatchString(node) {
		return nil, errors.New("invalid node")
	}
	var l []StorageInfo
	if err := c.get(ctx, "/nodes/"+node+"/storage", &l); err != nil {
		return nil, err
	}
	sort.Slice(l, func(i, j int) bool { return l[i].Storage < l[j].Storage })
	return l, nil
}

type DiskInfo struct {
	DevPath string `json:"devpath"`
	Model   string `json:"model"`
	Vendor  string `json:"vendor"`
	Serial  string `json:"serial"`
	Size    int64  `json:"size"`
	Type    string `json:"type"` // ssd|hdd|nvme|usb
	Health  string `json:"health"`
	Wearout any    `json:"wearout"` // number or "N/A"
	Used    string `json:"used"`
}

func (c *Client) Disks(ctx context.Context, node string) ([]DiskInfo, error) {
	if !validNode.MatchString(node) {
		return nil, errors.New("invalid node")
	}
	var l []DiskInfo
	if err := c.get(ctx, "/nodes/"+node+"/disks/list", &l); err != nil {
		return nil, err
	}
	sort.Slice(l, func(i, j int) bool { return l[i].DevPath < l[j].DevPath })
	return l, nil
}

type NetIface struct {
	Iface       string   `json:"iface"`
	Type        string   `json:"type"`
	Active      int      `json:"active"`
	Autostart   int      `json:"autostart"`
	CIDR        string   `json:"cidr"`
	Address     string   `json:"address"`
	Gateway     string   `json:"gateway"`
	BridgePorts string   `json:"bridge_ports"`
	Slaves      string   `json:"slaves"`
	VLANID      any      `json:"vlan-id"`
	VLANRaw     string   `json:"vlan-raw-device"`
	Comments    string   `json:"comments"`
	Families    []string `json:"families"`
}

func (c *Client) Network(ctx context.Context, node string) ([]NetIface, error) {
	if !validNode.MatchString(node) {
		return nil, errors.New("invalid node")
	}
	var l []NetIface
	if err := c.get(ctx, "/nodes/"+node+"/network", &l); err != nil {
		return nil, err
	}
	sort.Slice(l, func(i, j int) bool { return l[i].Iface < l[j].Iface })
	return l, nil
}
