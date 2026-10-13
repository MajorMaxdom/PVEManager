package api

import (
	"context"
	"net/http"
	"sync"

	"pvemanager/internal/pve"
)

type nodeInfra struct {
	Node     string            `json:"node"`
	Storages []pve.StorageInfo `json:"storages"`
	Disks    []pve.DiskInfo    `json:"disks"`
	Networks []pve.NetIface    `json:"networks"`
	Errors   map[string]string `json:"errors,omitempty"` // per section, e.g. missing permission
}

func (s *Server) infra(w http.ResponseWriter, r *http.Request) {
	server := r.PathValue("server")
	c, _, ok := s.Poll.Client(server)
	if !ok {
		jerr(w, 404, "unknown server")
		return
	}
	var nodes []string
	for _, st := range s.Poll.Snapshot() {
		if st.Name == server {
			for _, n := range st.Nodes {
				nodes = append(nodes, n.Node)
			}
		}
	}
	out := make([]nodeInfra, len(nodes))
	var wg sync.WaitGroup
	for i, n := range nodes {
		wg.Add(1)
		go func(i int, n string) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(r.Context(), 20*1e9)
			defer cancel()
			ni := nodeInfra{Node: n, Storages: []pve.StorageInfo{}, Disks: []pve.DiskInfo{}, Networks: []pve.NetIface{}, Errors: map[string]string{}}
			if v, err := c.StorageList(ctx, n); err != nil {
				ni.Errors["storage"] = err.Error()
			} else {
				ni.Storages = v
			}
			if v, err := c.Disks(ctx, n); err != nil {
				ni.Errors["disks"] = err.Error()
			} else {
				ni.Disks = v
			}
			if v, err := c.Network(ctx, n); err != nil {
				ni.Errors["network"] = err.Error()
			} else {
				ni.Networks = v
			}
			out[i] = ni
		}(i, n)
	}
	wg.Wait()
	jsonOut(w, 200, out)
}
