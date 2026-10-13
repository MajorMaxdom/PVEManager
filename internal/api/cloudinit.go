package api

import (
	"net/http"

	"pvemanager/internal/pve"
	"pvemanager/internal/store"
)

func (s *Server) registerCloudInit(m *http.ServeMux) {
	base := "/api/servers/{server}/guests/{type}/{vmid}"

	m.Handle("POST "+base+"/template", s.onGuest("admin", func(w http.ResponseWriter, r *http.Request, c *pve.Client, g pve.Guest, u store.User) {
		if g.Template != 0 {
			jerr(w, 409, "already a template")
			return
		}
		if g.Status != "stopped" {
			jerr(w, 409, "stop the guest before converting it to a template")
			return
		}
		upid, err := c.ConvertToTemplate(r.Context(), g.Node, g.Type, g.VMID)
		go s.Poll.RefreshNow(r.Context(), r.PathValue("server"))
		s.result(w, u, "template.convert", r, g, upid, err)
	}))

	m.Handle("PUT "+base+"/cloudinit", s.onGuest("operator", func(w http.ResponseWriter, r *http.Request, c *pve.Client, g pve.Guest, u store.User) {
		if g.Type != "qemu" {
			jerr(w, 400, "cloud-init is only available for virtual machines")
			return
		}
		var in pve.CloudInit
		if !decode(w, r, &in) {
			return
		}
		err := c.SetCloudInit(r.Context(), g.Node, g.VMID, in)
		s.result(w, u, "config.cloudinit", r, g, "", err)
	}))
}
