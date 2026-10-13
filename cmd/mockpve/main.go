// Command mockpve is a tiny stateful fake of the Proxmox VE API for end-to-end tests.
// It prints its TLS fingerprint on the first line of stdout and then serves until killed.
package main

import (
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"pvemanager/internal/tlsutil"
)

type guest struct {
	VMID   int    `json:"vmid"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	Status string `json:"status"`
	Mem    int64  `json:"mem"`
	MaxMem int64  `json:"maxmem"`
	Cores  int    `json:"maxcpu"`
	Uptime int64  `json:"uptime"`
}

type backupFile struct {
	VolID string `json:"volid"`
	VMID  int    `json:"vmid"`
	CTime int64  `json:"ctime"`
	Size  int64  `json:"size"`
}

type mock struct {
	demo    *demoSet // nil: the small fixed dataset the browser tests rely on
	backups []backupFile
	mu      sync.Mutex
	node    string
	token   string
	guests  []*guest
	snaps   map[int][]map[string]any
	calls   []string
	upidSeq int
}

func main() {
	addr := flag.String("addr", "127.0.0.1:0", "listen address")
	node := flag.String("node", "mock1", "node name")
	token := flag.String("token", "e2e@pve!t=secret", "accepted API token")
	dir := flag.String("dir", os.TempDir(), "directory for the self-signed certificate")
	demo := flag.Int("demo", 0, "1-3: serve a made-up demo host (names, sizes, graphs) instead of the test dataset")
	flag.Parse()

	now := time.Now().Unix()
	bk := func(typ string, id int, ago int64) backupFile {
		t := now - ago
		return backupFile{VolID: fmt.Sprintf("local:backup/vzdump-%s-%d-%s.vma.zst", typ, id, time.Unix(t, 0).UTC().Format("2006_01_02-15_04_05")), VMID: id, CTime: t, Size: 3 << 30}
	}
	m := &mock{node: *node, backups: []backupFile{bk("qemu", 100, 3600), bk("qemu", 100, 90000), bk("qemu", 101, 5*86400), bk("qemu", 300, 2*86400)}, token: *token, snaps: map[int][]map[string]any{}, guests: []*guest{
		{VMID: 100, Name: "web-01", Type: "qemu", Status: "running", Mem: 1 << 30, MaxMem: 2 << 30, Cores: 2, Uptime: 3600},
		{VMID: 101, Name: "db-01", Type: "qemu", Status: "stopped", MaxMem: 4 << 30, Cores: 4},
		{VMID: 200, Name: "cache", Type: "lxc", Status: "running", Mem: 128 << 20, MaxMem: 512 << 20, Cores: 1, Uptime: 7200},
	}}
	if *demo > 0 {
		m.demo = demoFor(*demo)
		m.guests = m.demo.guests
		m.demoBackups()
	}
	cert, err := tlsutil.SelfSigned(*dir, "localhost")
	if err != nil {
		log.Fatal(err)
	}
	sum := sha256.Sum256(cert.Certificate[0])
	ln, err := tls.Listen("tcp", *addr, &tls.Config{Certificates: []tls.Certificate{cert}})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%s\n%s\n", hex.EncodeToString(sum[:]), ln.Addr().String())
	os.Stdout.Sync()
	log.Fatal((&http.Server{Handler: m}).Serve(ln))
}

func out(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
}

func (m *mock) upid(kind string, vmid int) string {
	m.upidSeq++
	return fmt.Sprintf("UPID:%s:%08X:00000000:%08X:%s:%d:e2e@pve!t:", m.node, m.upidSeq, time.Now().Unix(), kind, vmid)
}

func (m *mock) find(id int) *guest {
	for _, g := range m.guests {
		if g.VMID == id {
			return g
		}
	}
	return nil
}

func (m *mock) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "PVEAPIToken="+m.token {
		w.WriteHeader(401)
		out(w, nil)
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	p := strings.TrimPrefix(r.URL.Path, "/api2/json")
	m.calls = append(m.calls, r.Method+" "+p)
	seg := strings.Split(strings.Trim(p, "/"), "/") // nodes/{n}/{type}/{vmid}/status/{action} has 6 segments
	if m.demo != nil && m.demoHandle(w, r, p, seg) {
		return
	}
	isNode := len(seg) >= 3 && seg[0] == "nodes" && seg[1] == m.node
	switch {
	case p == "/version":
		out(w, map[string]string{"version": "9.9.9-mock"})
	case p == "/cluster/resources":
		rows := []map[string]any{{"type": "node", "node": m.node, "status": "online", "cpu": 0.12, "maxcpu": 8, "mem": 4 << 30, "maxmem": 16 << 30, "disk": 10 << 30, "maxdisk": 100 << 30, "uptime": 86400}}
		for _, g := range m.guests {
			rows = append(rows, map[string]any{"type": g.Type, "node": m.node, "vmid": g.VMID, "name": g.Name, "status": g.Status, "cpu": 0.05, "maxcpu": g.Cores, "mem": g.Mem, "maxmem": g.MaxMem, "disk": 0, "maxdisk": 32 << 30, "uptime": g.Uptime})
		}
		rows = append(rows, map[string]any{"type": "storage", "node": m.node, "storage": "local", "plugintype": "dir", "status": "available", "disk": 20 << 30, "maxdisk": 100 << 30})
		out(w, rows)
	case p == "/cluster/nextid":
		out(w, "102")
	case p == "/cluster/backup":
		out(w, []any{})
	case p == "/cluster/tasks":
		t := time.Now().Unix()
		out(w, []map[string]any{
			{"upid": "UPID:mock1:0000AAAA:00000001:00000001:qmstart:100:e2e@pve!t:", "type": "qmstart", "node": m.node, "id": "100", "status": "OK", "starttime": t - 600, "endtime": t - 595, "user": "e2e@pve!t"},
			{"upid": "UPID:mock1:0000BBBB:00000002:00000002:vzdump:101:root@pam:", "type": "vzdump", "node": m.node, "id": "101", "status": "storage 'x' is not online", "starttime": t - 300, "endtime": t - 290, "user": "root@pam"},
			{"upid": "UPID:mock1:0000CCCC:00000003:00000003:aptupdate::root@pam:", "type": "aptupdate", "node": m.node, "id": "", "starttime": t - 5, "endtime": 0, "user": "root@pam"},
		})
	case isNode && len(seg) == 6 && seg[4] == "status" && r.Method == http.MethodPost:
		m.power(w, seg)
	case isNode && len(seg) == 5 && seg[2] == "tasks" && seg[4] == "status":
		exit := "OK"
		if strings.Contains(seg[3], "0000BBBB") { // the failed backup of the task list
			exit = "storage 'x' is not online"
			if m.demo != nil {
				exit = "storage 'nas-backup' is not online"
			}
		}
		out(w, map[string]string{"status": "stopped", "exitstatus": exit})
	case isNode && len(seg) == 3 && seg[2] == "storage":
		out(w, []map[string]any{{"storage": "local", "type": "dir", "content": "iso,vztmpl,backup,images,rootdir", "active": 1, "enabled": 1, "total": 100 << 30, "used": 20 << 30, "avail": 80 << 30, "used_fraction": 0.2}})
	case isNode && len(seg) == 4 && seg[2] == "disks" && seg[3] == "list":
		out(w, []map[string]any{{"devpath": "/dev/sda", "model": "MOCK_SSD", "size": 500 << 30, "type": "ssd", "health": "PASSED", "wearout": 98}})
	case isNode && len(seg) == 5 && (seg[2] == "qemu" || seg[2] == "lxc") && seg[4] == "config" && r.Method == http.MethodGet:
		id, _ := strconv.Atoi(seg[3])
		g := m.find(id)
		if g == nil {
			w.WriteHeader(500)
			out(w, nil)
			return
		}
		cfg := map[string]any{"cores": g.Cores, "memory": g.MaxMem >> 20, "name": g.Name, "digest": "0123456789abcdef", "net0": "virtio=BC:24:11:00:00:01,bridge=vmbr0"}
		if g.Type == "qemu" {
			cfg["scsi0"] = fmt.Sprintf("local-lvm:vm-%d-disk-0,size=32G", g.VMID)
		} else {
			cfg["rootfs"] = fmt.Sprintf("local-lvm:vm-%d-disk-0,size=8G", g.VMID)
			cfg["swap"] = 512
		}
		out(w, cfg)
	case isNode && len(seg) == 5 && seg[2] == "storage" && seg[4] == "content":
		list := []backupFile{}
		if r.URL.Query().Get("content") == "backup" {
			for _, b := range m.backups {
				if !strings.HasPrefix(b.VolID, seg[3]+":") { // only the backups that live on this storage
					continue
				}
				if v := r.URL.Query().Get("vmid"); v == "" || v == strconv.Itoa(b.VMID) {
					list = append(list, b)
				}
			}
		}
		out(w, list)
	case isNode && len(seg) == 3 && (seg[2] == "qemu" || seg[2] == "lxc") && r.Method == http.MethodPost: // create or restore
		_ = r.ParseForm()
		id, _ := strconv.Atoi(r.FormValue("vmid"))
		name := r.FormValue("name")
		if name == "" {
			name = r.FormValue("hostname")
		}
		if name == "" {
			name = fmt.Sprintf("restored-%d", id)
		}
		if g := m.find(id); g != nil {
			g.Name, g.Status = name, "stopped"
		} else {
			m.guests = append(m.guests, &guest{VMID: id, Name: name, Type: seg[2], Status: "stopped", MaxMem: 1 << 30, Cores: 1})
		}
		out(w, m.upid("qmrestore", id))
	case isNode && len(seg) == 3 && seg[2] == "vzdump" && r.Method == http.MethodPost:
		_ = r.ParseForm()
		id, _ := strconv.Atoi(r.FormValue("vmid"))
		m.backups = append(m.backups, backupFile{VolID: fmt.Sprintf("local:backup/vzdump-qemu-%d-%s.vma.zst", id, time.Now().UTC().Format("2006_01_02-15_04_05")), VMID: id, CTime: time.Now().Unix(), Size: 1 << 30})
		out(w, m.upid("vzdump", id))
	case isNode && len(seg) == 5 && seg[2] == "tasks" && seg[4] == "log":
		if m.demo != nil {
			out(w, demoTaskLog(seg[3]))
			return
		}
		out(w, []map[string]any{{"n": 1, "t": "INFO: starting task"}, {"n": 2, "t": "INFO: mock work done"}, {"n": 3, "t": "TASK OK"}})
	case isNode && len(seg) == 3 && seg[2] == "aplinfo":
		out(w, []map[string]any{{"template": "debian-13-standard_13.6-1_amd64.tar.zst", "package": "debian-13-standard", "version": "13.6-1", "section": "system", "headline": "Debian 13 (mock)", "os": "debian-13"}})
	case isNode && len(seg) == 3 && seg[2] == "network":
		out(w, []map[string]any{{"iface": "vmbr0", "type": "bridge", "active": 1, "cidr": "10.0.0.2/24", "gateway": "10.0.0.1", "bridge_ports": "nic0"}})
	case isNode && len(seg) == 4 && seg[2] == "apt" && seg[3] == "update":
		if r.Method == http.MethodPost {
			out(w, m.upid("aptupdate", 0))
		} else {
			out(w, []map[string]any{{"Package": "pve-mock-pkg", "OldVersion": "1.0", "Version": "1.1", "Origin": "Proxmox", "Title": "mock"}})
		}
	case isNode && len(seg) == 5 && seg[4] == "rrddata":
		pts := []map[string]any{}
		now := time.Now().Unix()
		for i := 0; i < 30; i++ {
			pts = append(pts, map[string]any{"time": now - int64(30-i)*60, "cpu": 0.1 + float64(i%5)/50, "mem": 1 << 30, "maxmem": 2 << 30, "netin": 1000 * i, "netout": 500 * i, "diskread": 0, "diskwrite": 100 * i})
		}
		out(w, pts)
	case isNode && len(seg) == 5 && seg[4] == "snapshot":
		m.snapshot(w, r, seg)
	case p == "/__calls": // test hook: what did the manager call?
		out(w, m.calls)
	default:
		w.WriteHeader(501)
		out(w, nil)
	}
}

// power handles /nodes/{n}/{type}/{vmid}/status/{action}
func (m *mock) power(w http.ResponseWriter, seg []string) {
	var vmid int
	var action string
	if len(seg) == 6 {
		vmid, _ = strconv.Atoi(seg[3])
		action = seg[5]
	}
	g := m.find(vmid)
	if g == nil {
		w.WriteHeader(500)
		out(w, nil)
		return
	}
	switch action {
	case "start":
		if g.Status == "running" {
			w.WriteHeader(500)
			_ = json.NewEncoder(w).Encode(map[string]any{"data": nil, "message": "VM already running"})
			return
		}
		g.Status, g.Uptime = "running", 1
	case "stop", "shutdown":
		g.Status, g.Uptime = "stopped", 0
	}
	out(w, m.upid("q"+action, vmid))
}

func (m *mock) snapshot(w http.ResponseWriter, r *http.Request, seg []string) {
	vmid, _ := strconv.Atoi(seg[3])
	if r.Method == http.MethodPost && len(seg) == 5 {
		_ = r.ParseForm()
		m.snaps[vmid] = append(m.snaps[vmid], map[string]any{"name": r.FormValue("snapname"), "description": r.FormValue("description"), "snaptime": time.Now().Unix()})
		out(w, m.upid("qmsnapshot", vmid))
		return
	}
	list := append([]map[string]any{{"name": "current"}}, m.snaps[vmid]...)
	out(w, list)
}
