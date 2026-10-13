package main

import (
	"fmt"
	"hash/fnv"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// demoSet is a made-up host used for documentation screenshots and for trying the UI without Proxmox.
// All names, sizes and addresses are invented; addresses come from the documentation range 192.0.2.0/24.
type demoSet struct {
	version             string
	cores               int
	memTotal, memUsed   int64
	diskTotal, diskUsed int64
	cpu                 float64
	uptime              int64
	ip, gw, port        string
	packages            []string
	disks               []map[string]any
	storages            []map[string]any
	guests              []*guest
	cpuBase             map[int]float64 // typical CPU load per guest, drives the graphs
	failedBackupGuest   int             // a vzdump task for this guest failed recently
	runningTask         string          // a task type that is still running
}

func gib(n int64) int64 { return n << 30 }

func demoFor(n int) *demoSet {
	nas := map[string]any{"storage": "nas-backup", "type": "cifs", "content": "backup", "active": 1, "enabled": 1, "shared": 1, "total": gib(3100), "used": gib(980), "avail": gib(2120), "used_fraction": 0.316}
	local := func(used int64) map[string]any {
		return map[string]any{"storage": "local", "type": "dir", "content": "iso,vztmpl,backup", "active": 1, "enabled": 1, "total": gib(94), "used": gib(used), "avail": gib(94 - used), "used_fraction": float64(used) / 94}
	}
	lvm := func(total, used int64) map[string]any {
		return map[string]any{"storage": "local-lvm", "type": "lvmthin", "content": "images,rootdir", "active": 1, "enabled": 1, "total": gib(total), "used": gib(used), "avail": gib(total - used), "used_fraction": float64(used) / float64(total)}
	}
	switch n {
	case 1:
		return &demoSet{version: "9.0.11", cores: 8, memTotal: gib(32), memUsed: gib(14), diskTotal: gib(94), diskUsed: gib(8), cpu: 0.17, uptime: 41 * 86400, ip: "192.0.2.11", gw: "192.0.2.1", port: "nic0",
			packages: []string{"libpve-storage-perl", "qemu-server", "pve-firmware"},
			disks:    []map[string]any{{"devpath": "/dev/sda", "model": "ACME_SSD_480GB", "size": gib(447), "type": "ssd", "health": "PASSED", "wearout": 97, "serial": "DEMO0001"}},
			storages: []map[string]any{nas, local(8), lvm(320, 41)},
			guests: []*guest{
				{VMID: 100, Name: "web-01", Type: "qemu", Status: "running", Mem: gib(3), MaxMem: gib(4), Cores: 2, Uptime: 12 * 86400},
				{VMID: 101, Name: "db-01", Type: "qemu", Status: "running", Mem: gib(5), MaxMem: gib(8), Cores: 4, Uptime: 12 * 86400},
				{VMID: 102, Name: "dev-sandbox", Type: "qemu", Status: "stopped", MaxMem: gib(4), Cores: 2},
				{VMID: 200, Name: "cache-01", Type: "lxc", Status: "running", Mem: 310 << 20, MaxMem: gib(1), Cores: 1, Uptime: 30 * 86400},
				{VMID: 201, Name: "monitoring", Type: "lxc", Status: "running", Mem: gib(1) + 400<<20, MaxMem: gib(2), Cores: 2, Uptime: 30 * 86400},
			},
			cpuBase: map[int]float64{100: 0.12, 101: 0.28, 200: 0.03, 201: 0.09}, failedBackupGuest: 102}
	case 2:
		return &demoSet{version: "9.0.11", cores: 12, memTotal: gib(64), memUsed: gib(41), diskTotal: gib(94), diskUsed: gib(6), cpu: 0.31, uptime: 26 * 86400, ip: "192.0.2.12", gw: "192.0.2.1", port: "nic0",
			packages: nil,
			disks: []map[string]any{
				{"devpath": "/dev/nvme0n1", "model": "ACME_NVME_256GB", "size": gib(238), "type": "nvme", "health": "PASSED", "wearout": 88, "serial": "DEMO0002"},
				{"devpath": "/dev/sda", "model": "ACME_SSD_480GB", "size": gib(447), "type": "ssd", "health": "PASSED", "wearout": 99, "serial": "DEMO0003"}},
			storages: []map[string]any{nas, local(6), lvm(320, 238)},
			guests: []*guest{
				{VMID: 100, Name: "nextcloud", Type: "qemu", Status: "running", Mem: gib(9), MaxMem: gib(12), Cores: 4, Uptime: 26 * 86400},
				{VMID: 101, Name: "home-automation", Type: "qemu", Status: "running", Mem: gib(2), MaxMem: gib(4), Cores: 2, Uptime: 26 * 86400},
				{VMID: 110, Name: "backup-server", Type: "qemu", Status: "running", Mem: gib(5), MaxMem: gib(8), Cores: 2, Uptime: 9 * 86400},
				{VMID: 150, Name: "test-vm", Type: "qemu", Status: "stopped", MaxMem: gib(2), Cores: 1},
				{VMID: 200, Name: "git", Type: "lxc", Status: "running", Mem: gib(1) + 100<<20, MaxMem: gib(2), Cores: 2, Uptime: 20 * 86400},
			},
			cpuBase: map[int]float64{100: 0.22, 101: 0.07, 110: 0.15, 200: 0.05}, runningTask: "vzdump"}
	default:
		return &demoSet{version: "9.0.11", cores: 6, memTotal: gib(16), memUsed: gib(14) + 700<<20, diskTotal: gib(94), diskUsed: gib(7), cpu: 0.08, uptime: 14 * 86400, ip: "192.0.2.13", gw: "192.0.2.1", port: "nic1",
			packages: []string{"proxmox-kernel-6.14"},
			disks: []map[string]any{
				{"devpath": "/dev/nvme0n1", "model": "ACME_NVME_256GB", "size": gib(238), "type": "nvme", "health": "PASSED", "wearout": 65, "serial": "DEMO0004"},
				{"devpath": "/dev/sda", "model": "ACME_SSD_500GB", "size": gib(466), "type": "ssd", "health": "PASSED", "wearout": 96, "serial": "DEMO0005"},
				{"devpath": "/dev/sdb", "model": "ACME_HDD_4TB", "size": gib(3726), "type": "hdd", "health": "PASSED", "wearout": "N/A", "serial": "DEMO0006"},
				{"devpath": "/dev/sdc", "model": "ACME_HDD_4TB", "size": gib(3726), "type": "hdd", "health": "PASSED", "wearout": "N/A", "serial": "DEMO0007"}},
			storages: []map[string]any{nas, local(7), lvm(338, 9)},
			guests: []*guest{
				{VMID: 100, Name: "media-server", Type: "qemu", Status: "running", Mem: gib(11), MaxMem: gib(12), Cores: 4, Uptime: 14 * 86400},
				{VMID: 120, Name: "build-runner", Type: "qemu", Status: "stopped", MaxMem: gib(4), Cores: 4},
				{VMID: 200, Name: "dns-01", Type: "lxc", Status: "running", Mem: 90 << 20, MaxMem: 512 << 20, Cores: 1, Uptime: 14 * 86400},
				{VMID: 201, Name: "vpn-gateway", Type: "lxc", Status: "running", Mem: 150 << 20, MaxMem: 512 << 20, Cores: 1, Uptime: 14 * 86400},
			},
			cpuBase: map[int]float64{100: 0.06, 200: 0.01, 201: 0.02}}
	}
}

// noise returns a deterministic pseudo-random number in [-1, 1) for a seed.
func noise(seed string, i int) float64 {
	h := fnv.New32a()
	fmt.Fprintf(h, "%s/%d", seed, i)
	return float64(h.Sum32()%2000)/1000 - 1
}

func series(base, amp float64, seed string, i, period int) float64 {
	v := base + amp*math.Sin(2*math.Pi*float64(i)/float64(period)+float64(len(seed))) + amp*0.4*noise(seed, i)
	if v < 0 {
		v = 0
	}
	return v
}

func (m *mock) demoBackups() {
	now := time.Now().Unix()
	m.backups = nil
	add := func(typ string, id int, ago int64, size int64) {
		t := now - ago
		m.backups = append(m.backups, backupFile{VolID: fmt.Sprintf("nas-backup:backup/vzdump-%s-%d-%s.zst", typ, id, time.Unix(t, 0).UTC().Format("2006_01_02-15_04_05")), VMID: id, CTime: t, Size: size})
	}
	for _, g := range m.demo.guests {
		if g.VMID == 102 || g.VMID == 150 || g.VMID == 120 { // test guests: never backed up, to show the warning
			continue
		}
		for d := int64(0); d < 7; d++ {
			add(g.Type, g.VMID, 3*3600+d*86400, gib(2)+int64(g.VMID)*1<<20)
		}
	}
	if m.node == "pve2" {
		add("qemu", 300, 3*86400, gib(6)) // a backup of a guest that no longer exists
	}
	if m.node == "pve1" {
		m.backups = m.backups[:0]
		for _, g := range m.demo.guests {
			if g.VMID == 102 {
				continue
			}
			ago := int64(3 * 3600)
			if g.VMID == 201 {
				ago = 4 * 86400 // stale: older than the default two days
			}
			for d := int64(0); d < 6; d++ {
				add(g.Type, g.VMID, ago+d*86400, gib(2)+int64(g.VMID)*1<<20)
			}
		}
	}
}

// demoHandle answers the endpoints the demo dataset overrides. It reports whether it handled the request.
func (m *mock) demoHandle(w http.ResponseWriter, r *http.Request, p string, seg []string) bool {
	d := m.demo
	isNode := len(seg) >= 3 && seg[0] == "nodes" && seg[1] == m.node
	switch {
	case p == "/version":
		out(w, map[string]string{"version": d.version})
	case p == "/cluster/resources":
		rows := []map[string]any{{"type": "node", "node": m.node, "status": "online", "cpu": d.cpu, "maxcpu": d.cores, "mem": d.memUsed, "maxmem": d.memTotal, "disk": gib(d.diskUsed >> 30), "maxdisk": d.diskTotal, "uptime": d.uptime}}
		for _, g := range m.guests {
			cpu := 0.0
			if g.Status == "running" {
				cpu = d.cpuBase[g.VMID] * (0.8 + 0.4*math.Abs(noise(m.node, int(time.Now().Unix()/5)+g.VMID)))
			}
			rows = append(rows, map[string]any{"type": g.Type, "node": m.node, "vmid": g.VMID, "name": g.Name, "status": g.Status, "cpu": cpu, "maxcpu": g.Cores, "mem": g.Mem, "maxmem": g.MaxMem, "disk": 0, "maxdisk": gib(32), "uptime": g.Uptime})
		}
		for _, s := range d.storages {
			rows = append(rows, map[string]any{"type": "storage", "node": m.node, "storage": s["storage"], "plugintype": s["type"], "status": "available", "disk": s["used"], "maxdisk": s["total"], "shared": s["shared"]})
		}
		out(w, rows)
	case isNode && len(seg) == 3 && seg[2] == "storage":
		out(w, d.storages)
	case isNode && len(seg) == 4 && seg[2] == "disks" && seg[3] == "list":
		out(w, d.disks)
	case isNode && len(seg) == 3 && seg[2] == "network":
		out(w, []map[string]any{
			{"iface": "nic0", "type": "eth", "active": 1}, {"iface": "nic1", "type": "eth", "active": 1},
			{"iface": "vmbr0", "type": "bridge", "active": 1, "cidr": d.ip + "/24", "gateway": d.gw, "bridge_ports": d.port}})
	case isNode && len(seg) == 4 && seg[2] == "apt" && seg[3] == "update" && r.Method == http.MethodGet:
		l := []map[string]any{}
		for i, name := range d.packages {
			l = append(l, map[string]any{"Package": name, "OldVersion": "9.0." + strconv.Itoa(i+1), "Version": "9.0." + strconv.Itoa(i+2), "Origin": "Proxmox", "Title": name})
		}
		out(w, l)
	case isNode && len(seg) == 5 && seg[4] == "rrddata":
		m.demoRRD(w, r, seg)
	case p == "/cluster/tasks":
		m.demoTasks(w)
	default:
		return false
	}
	return true
}

func (m *mock) demoRRD(w http.ResponseWriter, r *http.Request, seg []string) {
	vmid, _ := strconv.Atoi(seg[3])
	g := m.find(vmid)
	n, step := 60, int64(60)
	switch r.URL.Query().Get("timeframe") {
	case "day":
		n, step = 96, 900
	case "week":
		n, step = 84, 7200
	}
	now := time.Now().Unix()
	pts := []map[string]any{}
	seed := m.node + seg[3]
	for i := 0; i < n; i++ {
		row := map[string]any{"time": now - int64(n-i)*step}
		if g == nil || g.Status != "running" {
			pts = append(pts, row) // no data for a stopped guest, like PVE
			continue
		}
		base := m.demo.cpuBase[vmid]
		row["cpu"] = math.Min(series(base, base*0.6, seed, i, n/2+7), 0.95)
		row["maxmem"] = g.MaxMem
		row["mem"] = int64(float64(g.Mem) * (0.96 + 0.04*math.Sin(float64(i)/9)))
		row["netin"] = series(40e3, 30e3, seed+"in", i, n/3+5)
		row["netout"] = series(18e3, 14e3, seed+"out", i, n/3+9)
		row["diskread"] = series(60e3, 90e3, seed+"r", i, n/4+3)
		row["diskwrite"] = series(120e3, 160e3, seed+"w", i, n/5+4)
		pts = append(pts, row)
	}
	out(w, pts)
}

func (m *mock) demoTasks(w http.ResponseWriter) {
	now := time.Now().Unix()
	d := m.demo
	mk := func(i int, typ, id, status string, start, dur int64, user string) map[string]any {
		upid := fmt.Sprintf("UPID:%s:%08X:%08X:%08X:%s:%s:%s:", m.node, 0x1000+i, 0x2000+i, start, typ, id, user)
		if status != "OK" && status != "" && !strings.HasPrefix(status, "WARNINGS") {
			upid = fmt.Sprintf("UPID:%s:0000BBBB:%08X:%08X:%s:%s:%s:", m.node, 0x2000+i, start, typ, id, user) // marks a failed task for the mock's status route
		}
		row := map[string]any{"upid": upid, "type": typ, "node": m.node, "id": id, "starttime": start, "user": user}
		if status != "" {
			row["status"] = status
			row["endtime"] = start + dur
		} else {
			row["endtime"] = 0
		}
		return row
	}
	var tasks []map[string]any
	i := 0
	for _, g := range d.guests[:min(3, len(d.guests))] {
		tasks = append(tasks, mk(i, "vzdump", strconv.Itoa(g.VMID), "OK", now-3*3600+int64(i)*220, 200, "root@pam"))
		i++
	}
	tasks = append(tasks,
		mk(i+1, "qmstart", strconv.Itoa(d.guests[0].VMID), "OK", now-26*3600, 9, "admin@pve"),
		mk(i+2, "qmsnapshot", strconv.Itoa(d.guests[0].VMID), "OK", now-31*3600, 4, "admin@pve"),
		mk(i+3, "aptupdate", "", "OK", now-5*3600, 12, "root@pam"),
		mk(i+4, "vncproxy", strconv.Itoa(d.guests[0].VMID), "OK", now-50*60, 2, "admin@pve"))
	if d.failedBackupGuest != 0 {
		tasks = append(tasks, mk(i+5, "vzdump", strconv.Itoa(d.failedBackupGuest), "job errors", now-2*3600, 6, "root@pam"))
	}
	if d.runningTask != "" {
		tasks = append(tasks, mk(i+6, d.runningTask, strconv.Itoa(d.guests[0].VMID), "", now-95, 0, "root@pam"))
	}
	out(w, tasks)
}

// demoTaskLog returns a realistic log for the task type found in the UPID.
func demoTaskLog(upid string) []map[string]any {
	f := strings.Split(upid, ":")
	typ, id, when := "", "", time.Now()
	if len(f) > 7 {
		typ, id = f[5], f[6]
		if ts, err := strconv.ParseInt(f[4], 16, 64); err == nil {
			when = time.Unix(ts, 0)
		}
	}
	var lines []string
	switch {
	case typ == "vzdump" && strings.Contains(upid, "0000BBBB"):
		lines = []string{"INFO: starting new backup job: vzdump " + id + " --storage nas-backup --mode snapshot --compress zstd",
			"INFO: Starting Backup of VM " + id + " (qemu)", "INFO: status = stopped", "ERROR: Backup of VM " + id + " failed - storage 'nas-backup' is not online", "INFO: Failed at " + when.Format("2006-01-02 15:04:05"), "INFO: Backup job finished with errors", "TASK ERROR: job errors"}
	case typ == "vzdump":
		lines = []string{"INFO: starting new backup job: vzdump " + id + " --storage nas-backup --mode snapshot --compress zstd",
			"INFO: Starting Backup of VM " + id + " (qemu)", "INFO: status = running", "INFO: VM Name: demo", "INFO: include disk 'scsi0' 'local-lvm:vm-" + id + "-disk-0' 32G",
			"INFO: creating vzdump archive 'nas-backup:backup/vzdump-qemu-" + id + ".vma.zst'", "INFO: started backup task", "INFO: 100% (32.0 GiB of 32.0 GiB) in 3m 4s, read: 178.1 MiB/s, write: 151.3 MiB/s",
			"INFO: backup is sparse: 11.20 GiB (35%) total zero data", "INFO: transferred 32.00 GiB in 184 seconds (178.1 MiB/s)", "INFO: archive file size: 4.21GB", "INFO: Finished Backup of VM " + id + " (00:03:08)", "INFO: Backup job finished successfully", "TASK OK"}
	case typ == "aptupdate":
		lines = []string{"starting apt-get update", "Hit:1 http://deb.debian.org/debian trixie InRelease", "Get:2 http://download.proxmox.com/debian/pve trixie InRelease [2,771 B]", "Fetched 2,771 B in 1s (4,208 B/s)", "Reading package lists...", "TASK OK"}
	default:
		lines = []string{"starting task", "task done", "TASK OK"}
	}
	out := make([]map[string]any, len(lines))
	for i, l := range lines {
		out[i] = map[string]any{"n": i + 1, "t": l}
	}
	return out
}
