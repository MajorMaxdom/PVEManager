package pve

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	reSnap    = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,39}$`)
	reDiskKey = regexp.MustCompile(`^((scsi|virtio|sata|ide)[0-9]{1,2}|rootfs|mp[0-9]{1,2})$`)
	reBackup  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*:backup/[A-Za-z0-9._:+-]+$`)
	reFrame   = regexp.MustCompile(`^(hour|day|week|month|year)$`)
)

func gpath(node, typ string, vmid int, rest string) (string, error) {
	if !validNode.MatchString(node) || !validType.MatchString(typ) || vmid < 1 {
		return "", errors.New("invalid guest reference")
	}
	return fmt.Sprintf("/nodes/%s/%s/%d%s", node, typ, vmid, rest), nil
}

// ---- metrics

type Point struct {
	Time      int64    `json:"time"`
	CPU       *float64 `json:"cpu"`
	Mem       *float64 `json:"mem"`
	MaxMem    *float64 `json:"maxmem"`
	NetIn     *float64 `json:"netin"`
	NetOut    *float64 `json:"netout"`
	DiskRead  *float64 `json:"diskread"`
	DiskWrite *float64 `json:"diskwrite"`
}

func (c *Client) RRD(ctx context.Context, node, typ string, vmid int, frame string) ([]Point, error) {
	if !reFrame.MatchString(frame) {
		return nil, errors.New("invalid timeframe")
	}
	p, err := gpath(node, typ, vmid, "/rrddata?cf=AVERAGE&timeframe="+frame)
	if err != nil {
		return nil, err
	}
	var pts []Point
	return pts, c.get(ctx, p, &pts)
}

// ---- snapshots

type Snapshot struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	SnapTime    int64  `json:"snaptime"`
	VMState     int    `json:"vmstate"`
	Parent      string `json:"parent"`
}

func (c *Client) Snapshots(ctx context.Context, node, typ string, vmid int) ([]Snapshot, error) {
	p, err := gpath(node, typ, vmid, "/snapshot")
	if err != nil {
		return nil, err
	}
	var all []Snapshot
	if err := c.get(ctx, p, &all); err != nil {
		return nil, err
	}
	out := all[:0]
	for _, s := range all {
		if s.Name != "current" {
			out = append(out, s)
		}
	}
	return out, nil
}

func (c *Client) CreateSnapshot(ctx context.Context, node, typ string, vmid int, name, desc string, vmstate bool) (string, error) {
	if !reSnap.MatchString(name) {
		return "", errors.New("invalid snapshot name (letter first; letters, digits, _ and -)")
	}
	if len(desc) > 200 || strings.ContainsAny(desc, "\r\n") {
		return "", errors.New("description too long or multi-line")
	}
	p, err := gpath(node, typ, vmid, "/snapshot")
	if err != nil {
		return "", err
	}
	f := url.Values{"snapname": {name}}
	if desc != "" {
		f.Set("description", desc)
	}
	if vmstate && typ == "qemu" {
		f.Set("vmstate", "1")
	}
	var upid string
	return upid, c.post(ctx, p, f, &upid)
}

func (c *Client) RollbackSnapshot(ctx context.Context, node, typ string, vmid int, name string) (string, error) {
	if !reSnap.MatchString(name) {
		return "", errors.New("invalid snapshot name")
	}
	p, err := gpath(node, typ, vmid, "/snapshot/"+name+"/rollback")
	if err != nil {
		return "", err
	}
	var upid string
	return upid, c.post(ctx, p, nil, &upid)
}

func (c *Client) DeleteSnapshot(ctx context.Context, node, typ string, vmid int, name string) (string, error) {
	if !reSnap.MatchString(name) {
		return "", errors.New("invalid snapshot name")
	}
	p, err := gpath(node, typ, vmid, "/snapshot/"+name)
	if err != nil {
		return "", err
	}
	var upid string
	return upid, c.do(ctx, "DELETE", p, nil, &upid)
}

// ---- clone

func (c *Client) Clone(ctx context.Context, node, typ string, vmid, newid int, name string, full bool) (string, error) {
	if newid < 100 || !reName.MatchString(name) {
		return "", errors.New("invalid new id or name")
	}
	p, err := gpath(node, typ, vmid, "/clone")
	if err != nil {
		return "", err
	}
	f := url.Values{"newid": {strconv.Itoa(newid)}, "full": {b2i(full)}}
	if typ == "qemu" {
		f.Set("name", name)
	} else {
		f.Set("hostname", name)
	}
	var upid string
	return upid, c.post(ctx, p, f, &upid)
}

// ---- configuration

type Disk struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}
type GuestConfig struct {
	Cores  int    `json:"cores"`
	Memory int    `json:"memory"`
	Swap   int    `json:"swap"`
	Disks  []Disk `json:"disks"`
	Nets   []Disk `json:"nets"`
	Digest string `json:"digest"`
	Name   string `json:"name"`
	// HasCloudInit is true when a cloud-init drive is attached; Template when this is a template.
	HasCloudInit bool     `json:"hasCloudInit"`
	Template     bool     `json:"template"`
	CIUser       string   `json:"ciuser"`
	IPConfig     string   `json:"ipconfig0"`
	Nameserver   string   `json:"nameserver"`
	SearchDomain string   `json:"searchdomain"`
	AllKeys      []string `json:"-"`
}

var reNetKey = regexp.MustCompile(`^net[0-9]+$`)

func (c *Client) Config(ctx context.Context, node, typ string, vmid int) (GuestConfig, error) {
	var gc GuestConfig
	p, err := gpath(node, typ, vmid, "/config")
	if err != nil {
		return gc, err
	}
	var raw map[string]any
	if err := c.get(ctx, p, &raw); err != nil {
		return gc, err
	}
	num := func(k string) int {
		switch v := raw[k].(type) {
		case float64:
			return int(v)
		case string:
			n, _ := strconv.Atoi(v)
			return n
		}
		return 0
	}
	gc.Cores, gc.Memory, gc.Swap = num("cores"), num("memory"), num("swap")
	gc.Digest, _ = raw["digest"].(string)
	if n, ok := raw["name"].(string); ok {
		gc.Name = n
	} else if n, ok := raw["hostname"].(string); ok {
		gc.Name = n
	}
	gc.Template = num("template") == 1
	gc.CIUser, _ = raw["ciuser"].(string)
	gc.IPConfig, _ = raw["ipconfig0"].(string)
	gc.Nameserver, _ = raw["nameserver"].(string)
	gc.SearchDomain, _ = raw["searchdomain"].(string)
	for k, v := range raw {
		s, _ := v.(string)
		gc.AllKeys = append(gc.AllKeys, k)
		if reCIConfig.MatchString(s) && strings.Contains(s, "media=cdrom") {
			gc.HasCloudInit = true
		}
		switch {
		case reDiskKey.MatchString(k) && !strings.Contains(s, "media=cdrom"):
			gc.Disks = append(gc.Disks, Disk{k, s})
		case reNetKey.MatchString(k):
			gc.Nets = append(gc.Nets, Disk{k, s})
		}
	}
	sort.Slice(gc.Disks, func(i, j int) bool { return gc.Disks[i].Key < gc.Disks[j].Key })
	sort.Slice(gc.Nets, func(i, j int) bool { return gc.Nets[i].Key < gc.Nets[j].Key })
	return gc, nil
}

// SetResources changes cores/memory (and swap for containers). Zero values are left alone.
func (c *Client) SetResources(ctx context.Context, node, typ string, vmid, cores, mem, swap int) error {
	if err := between(cores, 1, 128, "cores"); err != nil {
		return err
	}
	if err := between(mem, 16, 4<<20, "memory"); err != nil {
		return err
	}
	f := url.Values{"cores": {strconv.Itoa(cores)}, "memory": {strconv.Itoa(mem)}}
	if typ == "lxc" {
		if err := between(swap, 0, 4<<20, "swap"); err != nil {
			return err
		}
		f.Set("swap", strconv.Itoa(swap))
	}
	p, err := gpath(node, typ, vmid, "/config")
	if err != nil {
		return err
	}
	return c.do(ctx, "PUT", p, f, nil)
}

// GrowDisk enlarges a disk by addGiB (shrinking is not supported by PVE).
func (c *Client) GrowDisk(ctx context.Context, node, typ string, vmid int, disk string, addGiB int) error {
	if !reDiskKey.MatchString(disk) {
		return errors.New("invalid disk")
	}
	if err := between(addGiB, 1, 65536, "size"); err != nil {
		return err
	}
	p, err := gpath(node, typ, vmid, "/resize")
	if err != nil {
		return err
	}
	return c.do(ctx, "PUT", p, url.Values{"disk": {disk}, "size": {"+" + strconv.Itoa(addGiB) + "G"}}, nil)
}

// ---- backups

type BackupFile struct {
	VolID string `json:"volid"`
	Size  int64  `json:"size"`
	CTime int64  `json:"ctime"`
	Notes string `json:"notes"`
	VMID  int    `json:"vmid"`
}

type BackupInfo struct {
	Storages []string     `json:"storages"`
	Files    []BackupFile `json:"files"`
}

func (c *Client) Backups(ctx context.Context, node string, vmid int) (BackupInfo, error) {
	var bi BackupInfo
	if !validNode.MatchString(node) {
		return bi, errors.New("invalid node")
	}
	var st []struct {
		Storage string `json:"storage"`
		Content string `json:"content"`
		Active  int    `json:"active"`
	}
	if err := c.get(ctx, "/nodes/"+node+"/storage?content=backup", &st); err != nil {
		return bi, err
	}
	for _, s := range st {
		if s.Active == 0 || !reStorage.MatchString(s.Storage) {
			continue
		}
		bi.Storages = append(bi.Storages, s.Storage)
		var files []BackupFile
		if err := c.get(ctx, fmt.Sprintf("/nodes/%s/storage/%s/content?content=backup&vmid=%d", node, s.Storage, vmid), &files); err == nil {
			bi.Files = append(bi.Files, files...)
		}
	}
	sort.Slice(bi.Files, func(i, j int) bool { return bi.Files[i].CTime > bi.Files[j].CTime })
	return bi, nil
}

func (c *Client) BackupNow(ctx context.Context, node string, vmid int, storage, mode string) (string, error) {
	if !validNode.MatchString(node) || !reStorage.MatchString(storage) || (mode != "snapshot" && mode != "suspend" && mode != "stop") {
		return "", errors.New("invalid backup request")
	}
	f := url.Values{"vmid": {strconv.Itoa(vmid)}, "storage": {storage}, "mode": {mode}, "compress": {"zstd"}}
	var upid string
	return upid, c.post(ctx, "/nodes/"+node+"/vzdump", f, &upid)
}

var reVzdump = regexp.MustCompile(`vzdump-(qemu|lxc)-([0-9]+)-`)

// BackupOrigin extracts guest type and original id from a backup volume id.
func BackupOrigin(volid string) (typ string, vmid int, ok bool) {
	m := reVzdump.FindStringSubmatch(volid)
	if m == nil {
		return "", 0, false
	}
	id, _ := strconv.Atoi(m[2])
	return m[1], id, true
}

// Restore recreates a guest from a backup archive. force overwrites an existing guest with that id.
// A restore to a new id (force=false) gets fresh MAC addresses.
func (c *Client) Restore(ctx context.Context, node, typ string, vmid int, volid string, force bool) (string, error) {
	if !validNode.MatchString(node) || !validType.MatchString(typ) || vmid < 100 || !reBackup.MatchString(volid) {
		return "", errors.New("invalid restore request")
	}
	f := url.Values{"vmid": {strconv.Itoa(vmid)}, "force": {b2i(force)}}
	if !force {
		f.Set("unique", "1")
	}
	if typ == "qemu" {
		f.Set("archive", volid)
	} else {
		f.Set("ostemplate", volid)
		f.Set("restore", "1")
	}
	var upid string
	return upid, c.post(ctx, "/nodes/"+node+"/"+typ, f, &upid)
}

type BackupJob struct {
	ID       string `json:"id"`
	Schedule string `json:"schedule"`
	Storage  string `json:"storage"`
	VMID     string `json:"vmid"`
	All      int    `json:"all"`
	Mode     string `json:"mode"`
	Enabled  int    `json:"enabled"`
}

func (c *Client) BackupJobs(ctx context.Context) ([]BackupJob, error) {
	var j []BackupJob
	return j, c.get(ctx, "/cluster/backup", &j)
}

// ClusterTask is one entry of the recent task list.
type ClusterTask struct {
	UPID      string `json:"upid"`
	Type      string `json:"type"`
	Node      string `json:"node"`
	ID        string `json:"id"`
	Status    string `json:"status"`
	EndTime   int64  `json:"endtime"`
	StartTime int64  `json:"starttime"`
	User      string `json:"user"`
}

func (c *Client) ClusterTasks(ctx context.Context) ([]ClusterTask, error) {
	var t []ClusterTask
	return t, c.get(ctx, "/cluster/tasks", &t)
}

// AllBackups lists every backup on every active backup storage of the node.
func (c *Client) AllBackups(ctx context.Context, node string) ([]BackupFile, []StorageInfo, error) {
	if !validNode.MatchString(node) {
		return nil, nil, errors.New("invalid node")
	}
	stores, err := c.StorageList(ctx, node)
	if err != nil {
		return nil, nil, err
	}
	var files []BackupFile
	var used []StorageInfo
	for _, s := range stores {
		if s.Active == 0 || !strings.Contains(s.Content, "backup") || !reStorage.MatchString(s.Storage) {
			continue
		}
		used = append(used, s)
		var l []BackupFile
		if err := c.get(ctx, fmt.Sprintf("/nodes/%s/storage/%s/content?content=backup", node, s.Storage), &l); err == nil {
			files = append(files, l...)
		}
	}
	return files, used, nil
}

type TaskLine struct {
	N int    `json:"n"`
	T string `json:"t"`
}

// TaskLog returns up to limit log lines of a task, starting at line `start`.
func (c *Client) TaskLog(ctx context.Context, node, upid string, start, limit int) ([]TaskLine, error) {
	if !validNode.MatchString(node) || !strings.HasPrefix(upid, "UPID:") || start < 0 || limit < 1 || limit > 2000 {
		return nil, errors.New("invalid task log request")
	}
	var l []TaskLine
	err := c.get(ctx, fmt.Sprintf("/nodes/%s/tasks/%s/log?start=%d&limit=%d", node, url.PathEscape(upid), start, limit), &l)
	return l, err
}
