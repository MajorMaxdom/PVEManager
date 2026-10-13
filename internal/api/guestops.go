package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"pvemanager/internal/pve"
	"pvemanager/internal/store"
)

type guestFn func(w http.ResponseWriter, r *http.Request, c *pve.Client, g pve.Guest, u store.User)

// onGuest resolves {server}/{type}/{vmid} to a client and the guest's current node.
func (s *Server) onGuest(role string, fn guestFn) http.Handler {
	return s.need(role, func(w http.ResponseWriter, r *http.Request) {
		c, g, err := s.resolveGuest(r.PathValue("server"), r.PathValue("type"), r.PathValue("vmid"))
		if err != nil {
			jerr(w, 404, err.Error())
			return
		}
		fn(w, r, c, g, userFrom(r))
	})
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		jerr(w, 400, err.Error())
		return false
	}
	return true
}

// result writes the outcome of a PVE call and audits it. A UPID becomes {"task","node"}.
func (s *Server) result(w http.ResponseWriter, u store.User, action string, r *http.Request, g pve.Guest, upid string, err error) {
	target := fmt.Sprintf("%s/%s/%d", r.PathValue("server"), g.Type, g.VMID)
	s.St.Audit(u.Name, action, target, g.Name, err == nil)
	if err != nil {
		code := 502
		if _, isAPI := err.(*pve.APIError); !isAPI {
			code = 400
		}
		jerr(w, code, err.Error())
		return
	}
	if upid == "" {
		jsonOut(w, 200, map[string]bool{"ok": true})
		return
	}
	jsonOut(w, 202, map[string]string{"task": upid, "node": g.Node})
}

func (s *Server) registerGuestOps(m *http.ServeMux) {
	base := "/api/servers/{server}/guests/{type}/{vmid}"

	m.Handle("GET "+base+"/rrd", s.onGuest("viewer", func(w http.ResponseWriter, r *http.Request, c *pve.Client, g pve.Guest, _ store.User) {
		frame := r.URL.Query().Get("frame")
		if frame == "" {
			frame = "hour"
		}
		pts, err := c.RRD(r.Context(), g.Node, g.Type, g.VMID, frame)
		if err != nil {
			jerr(w, 502, err.Error())
			return
		}
		jsonOut(w, 200, pts)
	}))

	m.Handle("GET "+base+"/snapshots", s.onGuest("viewer", func(w http.ResponseWriter, r *http.Request, c *pve.Client, g pve.Guest, _ store.User) {
		sn, err := c.Snapshots(r.Context(), g.Node, g.Type, g.VMID)
		if err != nil {
			jerr(w, 502, err.Error())
			return
		}
		if sn == nil {
			sn = []pve.Snapshot{}
		}
		jsonOut(w, 200, sn)
	}))
	m.Handle("POST "+base+"/snapshots", s.onGuest("operator", func(w http.ResponseWriter, r *http.Request, c *pve.Client, g pve.Guest, u store.User) {
		var in struct {
			Name, Description string
			VMState           bool
		}
		if !decode(w, r, &in) {
			return
		}
		upid, err := c.CreateSnapshot(r.Context(), g.Node, g.Type, g.VMID, in.Name, in.Description, in.VMState)
		s.result(w, u, "snapshot.create", r, g, upid, err)
	}))
	m.Handle("POST "+base+"/snapshots/{name}/rollback", s.onGuest("operator", func(w http.ResponseWriter, r *http.Request, c *pve.Client, g pve.Guest, u store.User) {
		upid, err := c.RollbackSnapshot(r.Context(), g.Node, g.Type, g.VMID, r.PathValue("name"))
		s.result(w, u, "snapshot.rollback", r, g, upid, err)
	}))
	m.Handle("DELETE "+base+"/snapshots/{name}", s.onGuest("operator", func(w http.ResponseWriter, r *http.Request, c *pve.Client, g pve.Guest, u store.User) {
		upid, err := c.DeleteSnapshot(r.Context(), g.Node, g.Type, g.VMID, r.PathValue("name"))
		s.result(w, u, "snapshot.delete", r, g, upid, err)
	}))

	m.Handle("POST "+base+"/clone", s.onGuest("operator", func(w http.ResponseWriter, r *http.Request, c *pve.Client, g pve.Guest, u store.User) {
		var in struct {
			NewID int
			Name  string
			Full  bool
		}
		if !decode(w, r, &in) {
			return
		}
		upid, err := c.Clone(r.Context(), g.Node, g.Type, g.VMID, in.NewID, in.Name, in.Full)
		s.result(w, u, "clone", r, g, upid, err)
	}))

	m.Handle("GET "+base+"/config", s.onGuest("viewer", func(w http.ResponseWriter, r *http.Request, c *pve.Client, g pve.Guest, _ store.User) {
		cfg, err := c.Config(r.Context(), g.Node, g.Type, g.VMID)
		if err != nil {
			jerr(w, 502, err.Error())
			return
		}
		jsonOut(w, 200, cfg)
	}))
	m.Handle("PUT "+base+"/config", s.onGuest("admin", func(w http.ResponseWriter, r *http.Request, c *pve.Client, g pve.Guest, u store.User) {
		var in struct{ Cores, Memory, Swap int }
		if !decode(w, r, &in) {
			return
		}
		err := c.SetResources(r.Context(), g.Node, g.Type, g.VMID, in.Cores, in.Memory, in.Swap)
		s.result(w, u, "config.resources", r, g, "", err)
	}))
	m.Handle("POST "+base+"/disks/{disk}/grow", s.onGuest("admin", func(w http.ResponseWriter, r *http.Request, c *pve.Client, g pve.Guest, u store.User) {
		var in struct{ AddGiB int }
		if !decode(w, r, &in) {
			return
		}
		err := c.GrowDisk(r.Context(), g.Node, g.Type, g.VMID, r.PathValue("disk"), in.AddGiB)
		s.result(w, u, "config.disk-grow", r, g, "", err)
	}))

	m.Handle("GET "+base+"/backups", s.onGuest("viewer", func(w http.ResponseWriter, r *http.Request, c *pve.Client, g pve.Guest, _ store.User) {
		bi, err := c.Backups(r.Context(), g.Node, g.VMID)
		if err != nil {
			jerr(w, 502, err.Error())
			return
		}
		if bi.Files == nil {
			bi.Files = []pve.BackupFile{}
		}
		jsonOut(w, 200, bi)
	}))
	m.Handle("POST "+base+"/backups", s.onGuest("operator", func(w http.ResponseWriter, r *http.Request, c *pve.Client, g pve.Guest, u store.User) {
		var in struct{ Storage, Mode string }
		if !decode(w, r, &in) {
			return
		}
		upid, err := c.BackupNow(r.Context(), g.Node, g.VMID, in.Storage, in.Mode)
		s.result(w, u, "backup.run", r, g, upid, err)
	}))
	m.Handle("POST "+base+"/restore", s.onGuest("admin", func(w http.ResponseWriter, r *http.Request, c *pve.Client, g pve.Guest, u store.User) {
		var in struct {
			VolID string
			Force bool
		}
		if !decode(w, r, &in) {
			return
		}
		upid, err := c.Restore(r.Context(), g.Node, g.Type, g.VMID, in.VolID, in.Force)
		s.result(w, u, "backup.restore", r, g, upid, err)
	}))

	m.Handle("GET /api/servers/{server}/backup-jobs", s.need("viewer", func(w http.ResponseWriter, r *http.Request) {
		c, _, ok := s.Poll.Client(r.PathValue("server"))
		if !ok {
			jerr(w, 404, "unknown server")
			return
		}
		j, err := c.BackupJobs(r.Context())
		if err != nil {
			jerr(w, 502, err.Error())
			return
		}
		if j == nil {
			j = []pve.BackupJob{}
		}
		jsonOut(w, 200, j)
	}))
}
