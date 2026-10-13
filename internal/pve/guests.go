package pve

import (
	"context"
	"crypto/x509"
	"fmt"
	"net/url"
	"regexp"
	"sort"
)

type x509Cert = x509.Certificate

// Guest is a VM (qemu) or container (lxc).
type Guest struct {
	VMID     int     `json:"vmid"`
	Name     string  `json:"name"`
	Type     string  `json:"type"` // qemu | lxc
	Node     string  `json:"node"`
	Status   string  `json:"status"` // running | stopped | paused
	CPU      float64 `json:"cpu"`
	CPUs     float64 `json:"maxcpu"`
	Mem      int64   `json:"mem"`
	MaxMem   int64   `json:"maxmem"`
	Disk     int64   `json:"disk"`
	MaxDisk  int64   `json:"maxdisk"`
	Uptime   int64   `json:"uptime"`
	Template int     `json:"template"`
	Tags     string  `json:"tags"`
}

// NodeInfo is one PVE node's current load.
type NodeInfo struct {
	Node    string  `json:"node"`
	Status  string  `json:"status"` // online | offline
	CPU     float64 `json:"cpu"`
	MaxCPU  int     `json:"maxcpu"`
	Mem     int64   `json:"mem"`
	MaxMem  int64   `json:"maxmem"`
	Disk    int64   `json:"disk"`
	MaxDisk int64   `json:"maxdisk"`
	Uptime  int64   `json:"uptime"`
}

// StorageRes is a storage row of cluster/resources.
type StorageRes struct {
	Storage string `json:"storage"`
	Node    string `json:"node"`
	Type    string `json:"plugintype"`
	Status  string `json:"status"`
	Used    int64  `json:"disk"`
	Total   int64  `json:"maxdisk"`
	Shared  int    `json:"shared"`
}

// Resources returns all guests and nodes in one call (cluster/resources).
func (c *Client) Resources(ctx context.Context) ([]Guest, []NodeInfo, error) {
	g, n, _, err := c.ResourcesFull(ctx)
	return g, n, err
}

// ResourcesFull also returns the storages.
func (c *Client) ResourcesFull(ctx context.Context) ([]Guest, []NodeInfo, []StorageRes, error) {
	var rows []struct {
		Kind     string  `json:"type"`
		Node     string  `json:"node"`
		Name     string  `json:"name"`
		VMID     int     `json:"vmid"`
		Status   string  `json:"status"`
		CPU      float64 `json:"cpu"`
		MaxCPU   float64 `json:"maxcpu"`
		Mem      int64   `json:"mem"`
		MaxMem   int64   `json:"maxmem"`
		Disk     int64   `json:"disk"`
		MaxDisk  int64   `json:"maxdisk"`
		Uptime   int64   `json:"uptime"`
		Template int     `json:"template"`
		Tags     string  `json:"tags"`
		Storage  string  `json:"storage"`
		Plugin   string  `json:"plugintype"`
		Shared   int     `json:"shared"`
	}
	if err := c.get(ctx, "/cluster/resources", &rows); err != nil {
		return nil, nil, nil, err
	}
	var guests []Guest
	var nodes []NodeInfo
	var stores []StorageRes
	for _, r := range rows {
		switch r.Kind {
		case "qemu", "lxc":
			guests = append(guests, Guest{VMID: r.VMID, Name: r.Name, Type: r.Kind, Node: r.Node, Status: r.Status,
				CPU: r.CPU, CPUs: r.MaxCPU, Mem: r.Mem, MaxMem: r.MaxMem, Disk: r.Disk, MaxDisk: r.MaxDisk,
				Uptime: r.Uptime, Template: r.Template, Tags: r.Tags})
		case "storage":
			stores = append(stores, StorageRes{Storage: r.Storage, Node: r.Node, Type: r.Plugin, Status: r.Status, Used: r.Disk, Total: r.MaxDisk, Shared: r.Shared})
		case "node":
			nodes = append(nodes, NodeInfo{Node: r.Node, Status: r.Status, CPU: r.CPU, MaxCPU: int(r.MaxCPU),
				Mem: r.Mem, MaxMem: r.MaxMem, Disk: r.Disk, MaxDisk: r.MaxDisk, Uptime: r.Uptime})
		}
	}
	sort.Slice(guests, func(i, j int) bool { return guests[i].VMID < guests[j].VMID })
	return guests, nodes, stores, nil
}

var (
	validType   = regexp.MustCompile(`^(qemu|lxc)$`)
	validNode   = regexp.MustCompile(`^[A-Za-z0-9-]+$`)
	powerAction = map[string]bool{"start": true, "stop": true, "shutdown": true, "reboot": true, "reset": true, "suspend": true, "resume": true}
)

// IsPowerAction reports whether action is a supported power action.
func IsPowerAction(a string) bool { return powerAction[a] }

// Power runs a status action and returns the task UPID.
func (c *Client) Power(ctx context.Context, node, typ string, vmid int, action string) (string, error) {
	if !validNode.MatchString(node) || !validType.MatchString(typ) || !powerAction[action] {
		return "", fmt.Errorf("invalid power request")
	}
	var upid string
	err := c.post(ctx, fmt.Sprintf("/nodes/%s/%s/%d/status/%s", node, typ, vmid, action), nil, &upid)
	return upid, err
}

// Task describes a PVE task status.
type Task struct {
	Status     string `json:"status"` // running | stopped
	ExitStatus string `json:"exitstatus"`
}

func (c *Client) TaskStatus(ctx context.Context, node, upid string) (Task, error) {
	var t Task
	err := c.get(ctx, fmt.Sprintf("/nodes/%s/tasks/%s/status", node, url.PathEscape(upid)), &t)
	return t, err
}

// Package is a pending apt update.
type Package struct {
	Package    string `json:"Package"`
	OldVersion string `json:"OldVersion"`
	Version    string `json:"Version"`
	Origin     string `json:"Origin"`
	Title      string `json:"Title"`
}

func (c *Client) AptUpdates(ctx context.Context, node string) ([]Package, error) {
	if !validNode.MatchString(node) {
		return nil, fmt.Errorf("invalid node")
	}
	var p []Package
	err := c.get(ctx, fmt.Sprintf("/nodes/%s/apt/update", node), &p)
	return p, err
}

// AptRefresh runs "apt update" on the node and returns the task UPID.
func (c *Client) AptRefresh(ctx context.Context, node string) (string, error) {
	if !validNode.MatchString(node) {
		return "", fmt.Errorf("invalid node")
	}
	var upid string
	err := c.post(ctx, fmt.Sprintf("/nodes/%s/apt/update", node), nil, &upid)
	return upid, err
}

// VNCProxy is the answer of a vncproxy/termproxy call.
type VNCProxy struct {
	Port   string
	Ticket string
	WSPath string // path+query for the websocket upgrade
}

// OpenConsole asks PVE for a VNC console session (works for qemu and lxc).
func (c *Client) OpenConsole(ctx context.Context, node, typ string, vmid int) (*VNCProxy, error) {
	if !validNode.MatchString(node) || !validType.MatchString(typ) {
		return nil, fmt.Errorf("invalid console request")
	}
	form := url.Values{"websocket": {"1"}}
	if typ == "qemu" {
		form.Set("generate-password", "1")
	}
	var r struct {
		Port   any    `json:"port"`
		Ticket string `json:"ticket"`
	}
	if err := c.post(ctx, fmt.Sprintf("/nodes/%s/%s/%d/vncproxy", node, typ, vmid), form, &r); err != nil {
		return nil, err
	}
	port := fmt.Sprint(r.Port)
	if f, ok := r.Port.(float64); ok {
		port = fmt.Sprint(int(f))
	}
	q := url.Values{"port": {port}, "vncticket": {r.Ticket}}
	return &VNCProxy{Port: port, Ticket: r.Ticket,
		WSPath: fmt.Sprintf("/api2/json/nodes/%s/%s/%d/vncwebsocket?%s", node, typ, vmid, q.Encode())}, nil
}
