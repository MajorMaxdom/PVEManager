package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"

	"pvemanager/internal/pve"
)

var reUPID = regexp.MustCompile(`^UPID:[A-Za-z0-9._:@!=-]+$`)

// nodeOf checks that the node belongs to the server (from the polled snapshot).
func (s *Server) nodeOf(server, node string) bool {
	for _, st := range s.Poll.Snapshot() {
		if st.Name == server {
			for _, n := range st.Nodes {
				if n.Node == node {
					return true
				}
			}
		}
	}
	return false
}

func (s *Server) createOptions(w http.ResponseWriter, r *http.Request) {
	server, node := r.PathValue("server"), r.URL.Query().Get("node")
	c, _, ok := s.Poll.Client(server)
	if !ok || !s.nodeOf(server, node) {
		jerr(w, 404, "unknown server or node")
		return
	}
	o, err := c.CreateOptions(r.Context(), node)
	if err != nil {
		jerr(w, 502, err.Error())
		return
	}
	jsonOut(w, 200, o)
}

func (s *Server) createGuest(typ string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, server, node := userFrom(r), r.PathValue("server"), r.URL.Query().Get("node")
		c, _, ok := s.Poll.Client(server)
		if !ok || !s.nodeOf(server, node) {
			jerr(w, 404, "unknown server or node")
			return
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
		dec.DisallowUnknownFields()
		var upid, name string
		var vmid int
		var err error
		if typ == "qemu" {
			var spec pve.VMSpec
			if err = dec.Decode(&spec); err == nil {
				name, vmid = spec.Name, spec.VMID
				upid, err = c.CreateVM(r.Context(), node, spec)
			}
		} else {
			var spec pve.CTSpec
			if err = dec.Decode(&spec); err == nil {
				name, vmid = spec.Hostname, spec.VMID
				upid, err = c.CreateCT(r.Context(), node, spec)
			}
		}
		s.St.Audit(u.Name, "create."+typ, fmt.Sprintf("%s/%s/%d", server, node, vmid), name, err == nil)
		if err != nil {
			code := 502
			if _, isAPI := err.(*pve.APIError); !isAPI {
				code = 400 // decode or validation error
			}
			jerr(w, code, err.Error())
			return
		}
		jsonOut(w, 202, map[string]any{"task": upid, "node": node, "vmid": vmid})
	}
}

func (s *Server) deleteGuest(w http.ResponseWriter, r *http.Request) {
	u, server, typ, vmid := userFrom(r), r.PathValue("server"), r.PathValue("type"), r.PathValue("vmid")
	c, g, err := s.resolveGuest(server, typ, vmid)
	if err != nil {
		jerr(w, 404, err.Error())
		return
	}
	if g.Status != "stopped" {
		jerr(w, 409, "only stopped guests can be deleted")
		return
	}
	if r.URL.Query().Get("confirm") != g.Name {
		jerr(w, 400, "confirm must equal the guest name")
		return
	}
	upid, err := c.Delete(r.Context(), g.Node, typ, g.VMID)
	s.St.Audit(u.Name, "delete."+typ, fmt.Sprintf("%s/%s/%s", server, typ, vmid), g.Name, err == nil)
	if err != nil {
		jerr(w, 502, err.Error())
		return
	}
	jsonOut(w, 202, map[string]string{"task": upid, "node": g.Node})
}

func (s *Server) taskStatus(w http.ResponseWriter, r *http.Request) {
	server, node, upid := r.PathValue("server"), r.PathValue("node"), r.URL.Query().Get("upid")
	c, _, ok := s.Poll.Client(server)
	if !ok || !s.nodeOf(server, node) || !reUPID.MatchString(upid) {
		jerr(w, 404, "unknown task")
		return
	}
	t, err := c.TaskStatus(r.Context(), node, upid)
	if err != nil {
		jerr(w, 502, err.Error())
		return
	}
	go s.Poll.RefreshNow(r.Context(), server)
	jsonOut(w, 200, t)
}
