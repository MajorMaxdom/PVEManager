// Package metrics renders the poller snapshot in the Prometheus text exposition format.
package metrics

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"pvemanager/internal/poller"
)

var esc = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)

type writer struct {
	w     io.Writer
	typed map[string]bool
}

func (m *writer) sample(name, help, kind string, labels [][2]string, v float64) {
	if !m.typed[name] {
		m.typed[name] = true
		fmt.Fprintf(m.w, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, kind)
	}
	var b strings.Builder
	for i, l := range labels {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `%s="%s"`, l[0], esc.Replace(l[1]))
	}
	if b.Len() > 0 {
		fmt.Fprintf(m.w, "%s{%s} %g\n", name, b.String(), v)
	} else {
		fmt.Fprintf(m.w, "%s %g\n", name, v)
	}
}

func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// Write emits every metric. Output is grouped by metric name as the format requires.
func Write(w io.Writer, version string, servers []poller.ServerState) {
	m := &writer{w: w, typed: map[string]bool{}}
	m.sample("pve_manager_info", "pve-manager build information", "gauge", [][2]string{{"version", version}}, 1)
	sort.Slice(servers, func(i, j int) bool { return servers[i].Name < servers[j].Name })

	each := func(f func(s poller.ServerState)) {
		for _, s := range servers {
			f(s)
		}
	}
	each(func(s poller.ServerState) {
		if s.Updated > 0 {
			m.sample("pve_server_up", "1 if the PVE API of the server answered the last poll", "gauge", [][2]string{{"server", s.Name}}, b2f(s.Online))
		}
	})
	each(func(s poller.ServerState) {
		if s.Updated > 0 {
			m.sample("pve_server_last_poll_timestamp_seconds", "Unix time of the last poll", "gauge", [][2]string{{"server", s.Name}}, float64(s.Updated))
		}
	})
	nodeMetric := func(name, help string, val func(n poller.ServerState, i int) float64) {
		each(func(s poller.ServerState) {
			if !s.Online {
				return
			}
			for i, n := range s.Nodes {
				m.sample(name, help, "gauge", [][2]string{{"server", s.Name}, {"node", n.Node}}, val(s, i))
			}
		})
	}
	nodeMetric("pve_node_cpu_usage_ratio", "Node CPU usage, 0..1", func(s poller.ServerState, i int) float64 { return s.Nodes[i].CPU })
	nodeMetric("pve_node_cpu_cores", "Node CPU threads", func(s poller.ServerState, i int) float64 { return float64(s.Nodes[i].MaxCPU) })
	nodeMetric("pve_node_memory_used_bytes", "Node memory in use", func(s poller.ServerState, i int) float64 { return float64(s.Nodes[i].Mem) })
	nodeMetric("pve_node_memory_total_bytes", "Node memory installed", func(s poller.ServerState, i int) float64 { return float64(s.Nodes[i].MaxMem) })
	nodeMetric("pve_node_rootfs_used_bytes", "Node root filesystem used", func(s poller.ServerState, i int) float64 { return float64(s.Nodes[i].Disk) })
	nodeMetric("pve_node_rootfs_total_bytes", "Node root filesystem size", func(s poller.ServerState, i int) float64 { return float64(s.Nodes[i].MaxDisk) })
	nodeMetric("pve_node_uptime_seconds", "Node uptime", func(s poller.ServerState, i int) float64 { return float64(s.Nodes[i].Uptime) })

	guestMetric := func(name, help string, val func(g int, s poller.ServerState) float64) {
		each(func(s poller.ServerState) {
			if !s.Online {
				return
			}
			for i, g := range s.Guests {
				if g.Template != 0 {
					continue
				}
				m.sample(name, help, "gauge", [][2]string{{"server", s.Name}, {"node", g.Node}, {"type", g.Type}, {"vmid", fmt.Sprint(g.VMID)}, {"name", g.Name}}, val(i, s))
			}
		})
	}
	guestMetric("pve_guest_up", "1 if the guest is running", func(i int, s poller.ServerState) float64 { return b2f(s.Guests[i].Status == "running") })
	guestMetric("pve_guest_cpu_usage_ratio", "Guest CPU usage relative to its cores, 0..1", func(i int, s poller.ServerState) float64 { return s.Guests[i].CPU })
	guestMetric("pve_guest_cpu_cores", "Guest CPU cores", func(i int, s poller.ServerState) float64 { return s.Guests[i].CPUs })
	guestMetric("pve_guest_memory_used_bytes", "Guest memory in use", func(i int, s poller.ServerState) float64 { return float64(s.Guests[i].Mem) })
	guestMetric("pve_guest_memory_max_bytes", "Guest memory limit", func(i int, s poller.ServerState) float64 { return float64(s.Guests[i].MaxMem) })
	guestMetric("pve_guest_uptime_seconds", "Guest uptime", func(i int, s poller.ServerState) float64 { return float64(s.Guests[i].Uptime) })

	storeMetric := func(name, help string, val func(st int, s poller.ServerState) float64) {
		each(func(s poller.ServerState) {
			if !s.Online {
				return
			}
			for i, st := range s.Storages {
				m.sample(name, help, "gauge", [][2]string{{"server", s.Name}, {"node", st.Node}, {"storage", st.Storage}, {"type", st.Type}}, val(i, s))
			}
		})
	}
	storeMetric("pve_storage_up", "1 if the storage is available", func(i int, s poller.ServerState) float64 { return b2f(s.Storages[i].Status == "available") })
	storeMetric("pve_storage_used_bytes", "Storage space used", func(i int, s poller.ServerState) float64 { return float64(s.Storages[i].Used) })
	storeMetric("pve_storage_total_bytes", "Storage size", func(i int, s poller.ServerState) float64 { return float64(s.Storages[i].Total) })
}
