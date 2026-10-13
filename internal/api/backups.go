package api

import (
	"fmt"
	"net/http"
	"sort"

	"pvemanager/internal/pve"
)

type backupGuest struct {
	VMID      int    `json:"vmid"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	Node      string `json:"node"`
	Count     int    `json:"count"`
	LastTime  int64  `json:"lastTime"`
	LastSize  int64  `json:"lastSize"`
	LastVolID string `json:"lastVolid"`
	Storage   string `json:"storage"`
}

type backupOverview struct {
	Node     string            `json:"node"`
	Storages []pve.StorageInfo `json:"storages"`
	Guests   []backupGuest     `json:"guests"`
	Orphans  []backupGuest     `json:"orphans"` // backups of guests that no longer exist
}

func (s *Server) backupOverview(w http.ResponseWriter, r *http.Request) {
	server, node := r.PathValue("server"), r.URL.Query().Get("node")
	c, _, ok := s.Poll.Client(server)
	if !ok || !s.nodeOf(server, node) {
		jerr(w, 404, "unknown server or node")
		return
	}
	files, stores, err := c.AllBackups(r.Context(), node)
	if err != nil {
		jerr(w, 502, err.Error())
		return
	}
	byID := map[int]*backupGuest{}
	for _, f := range files {
		typ, id, ok := pve.BackupOrigin(f.VolID)
		if !ok {
			continue
		}
		g := byID[id]
		if g == nil {
			g = &backupGuest{VMID: id, Type: typ}
			byID[id] = g
		}
		g.Count++
		if f.CTime >= g.LastTime {
			g.LastTime, g.LastSize, g.LastVolID = f.CTime, f.Size, f.VolID
			g.Storage = f.VolID[:indexByte(f.VolID, ':')]
		}
	}
	out := backupOverview{Node: node, Storages: stores, Guests: []backupGuest{}, Orphans: []backupGuest{}}
	if out.Storages == nil {
		out.Storages = []pve.StorageInfo{}
	}
	known := map[int]bool{}
	for _, st := range s.Poll.Snapshot() {
		if st.Name != server {
			continue
		}
		for _, g := range st.Guests {
			if g.Template != 0 || g.Node != node {
				continue
			}
			known[g.VMID] = true
			bg := backupGuest{VMID: g.VMID, Name: g.Name, Type: g.Type, Node: g.Node}
			if b := byID[g.VMID]; b != nil {
				bg.Count, bg.LastTime, bg.LastSize, bg.LastVolID, bg.Storage = b.Count, b.LastTime, b.LastSize, b.LastVolID, b.Storage
			}
			out.Guests = append(out.Guests, bg)
		}
	}
	for id, b := range byID {
		if !known[id] {
			out.Orphans = append(out.Orphans, *b)
		}
	}
	sort.Slice(out.Guests, func(i, j int) bool { return out.Guests[i].VMID < out.Guests[j].VMID })
	sort.Slice(out.Orphans, func(i, j int) bool { return out.Orphans[i].VMID < out.Orphans[j].VMID })
	jsonOut(w, 200, out)
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return 0
}

// restoreBackup restores any backup to a new guest id (or over an existing, stopped guest with force).
func (s *Server) restoreBackup(w http.ResponseWriter, r *http.Request) {
	server, node, u := r.PathValue("server"), r.URL.Query().Get("node"), userFrom(r)
	c, _, ok := s.Poll.Client(server)
	if !ok || !s.nodeOf(server, node) {
		jerr(w, 404, "unknown server or node")
		return
	}
	var in struct {
		VolID string
		VMID  int
		Force bool
	}
	if !decode(w, r, &in) {
		return
	}
	typ, origin, ok := pve.BackupOrigin(in.VolID)
	if !ok {
		jerr(w, 400, "this is not a vzdump backup")
		return
	}
	if in.VMID == 0 {
		in.VMID = origin
	}
	var existing *pve.Guest
	for _, st := range s.Poll.Snapshot() {
		if st.Name == server {
			for _, g := range st.Guests {
				if g.VMID == in.VMID {
					g := g
					existing = &g
				}
			}
		}
	}
	switch {
	case existing != nil && !in.Force:
		jerr(w, 409, fmt.Sprintf("ID %d is already used by %q; choose another ID or confirm overwriting", in.VMID, existing.Name))
		return
	case existing != nil && existing.Type != typ:
		jerr(w, 409, "the existing guest is of a different type than the backup")
		return
	case existing != nil && existing.Status != "stopped":
		jerr(w, 409, "stop the guest before restoring over it")
		return
	case existing == nil && in.Force:
		in.Force = false // nothing to overwrite
	}
	upid, err := c.Restore(r.Context(), node, typ, in.VMID, in.VolID, in.Force)
	s.St.Audit(u.Name, "backup.restore", fmt.Sprintf("%s/%s/%d", server, node, in.VMID), in.VolID, err == nil)
	if err != nil {
		code := 502
		if _, isAPI := err.(*pve.APIError); !isAPI {
			code = 400
		}
		jerr(w, code, err.Error())
		return
	}
	go s.Poll.RefreshNow(r.Context(), server)
	jsonOut(w, 202, map[string]any{"task": upid, "node": node, "vmid": in.VMID})
}

func (s *Server) registerBackups(m *http.ServeMux) {
	m.Handle("GET /api/servers/{server}/backup-overview", s.need("viewer", s.backupOverview))
	m.Handle("POST /api/servers/{server}/restore", s.need("admin", s.restoreBackup))
}
