package metrics

import (
	"bytes"
	"strings"
	"testing"

	"pvemanager/internal/poller"
	"pvemanager/internal/pve"
)

func TestOutputFormatAndEscaping(t *testing.T) {
	var b bytes.Buffer
	Write(&b, "1.0", []poller.ServerState{
		{Name: "pve2", Online: true, Updated: 5,
			Nodes:    []pve.NodeInfo{{Node: "pve2", CPU: 0.25, MaxCPU: 8, Mem: 1, MaxMem: 2, Disk: 3, MaxDisk: 4, Uptime: 9}},
			Guests:   []pve.Guest{{VMID: 100, Name: "evil\"name\nx\\", Type: "qemu", Node: "pve2", Status: "running", CPU: 0.5}, {VMID: 9, Name: "tpl", Template: 1}},
			Storages: []pve.StorageRes{{Storage: "local", Node: "pve2", Type: "dir", Status: "available", Used: 10, Total: 100}}},
		{Name: "pve1", Online: false, Updated: 7},
		{Name: "fresh"}, // never polled: must not be reported as down
	})
	out := b.String()
	for _, want := range []string{
		`pve_manager_info{version="1.0"} 1`, `pve_server_up{server="pve1"} 0`, `pve_server_up{server="pve2"} 1`,
		`pve_node_cpu_usage_ratio{server="pve2",node="pve2"} 0.25`, `pve_guest_up{server="pve2",node="pve2",type="qemu",vmid="100",name="evil\"name\nx\\"} 1`,
		`pve_storage_used_bytes{server="pve2",node="pve2",storage="local",type="dir"} 10`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, `server="fresh"`) || strings.Contains(out, `vmid="9"`) {
		t.Error("unpolled server or template leaked into output")
	}
	if strings.Count(out, "# TYPE pve_server_up") != 1 {
		t.Error("TYPE line must appear exactly once per metric")
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" || strings.Contains(line, "\r") {
			t.Errorf("malformed line %q", line)
		}
	}
}
