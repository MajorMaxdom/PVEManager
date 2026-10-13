package api

import (
	"fmt"
	"net/http"

	"pvemanager/internal/pve"
)

const maxUpload = 20 << 30 // 20 GiB

func (s *Server) mediaClient(w http.ResponseWriter, r *http.Request) (*pve.Client, string, bool) {
	server, node := r.PathValue("server"), r.URL.Query().Get("node")
	c, _, ok := s.Poll.Client(server)
	if !ok || !s.nodeOf(server, node) {
		jerr(w, 404, "unknown server or node")
		return nil, "", false
	}
	return c, node, true
}

func (s *Server) registerMedia(m *http.ServeMux) {
	const base = "/api/servers/{server}/media"

	m.Handle("GET "+base, s.need("viewer", func(w http.ResponseWriter, r *http.Request) {
		c, node, ok := s.mediaClient(w, r)
		if !ok {
			return
		}
		md, err := c.Media(r.Context(), node)
		if err != nil {
			jerr(w, 502, err.Error())
			return
		}
		jsonOut(w, 200, md)
	}))

	m.Handle("GET "+base+"/appliances", s.need("viewer", func(w http.ResponseWriter, r *http.Request) {
		c, node, ok := s.mediaClient(w, r)
		if !ok {
			return
		}
		l, err := c.Appliances(r.Context(), node)
		if err != nil {
			jerr(w, 502, err.Error())
			return
		}
		if l == nil {
			l = []pve.Appliance{}
		}
		jsonOut(w, 200, l)
	}))

	m.Handle("POST "+base+"/upload", s.need("operator", func(w http.ResponseWriter, r *http.Request) {
		c, node, ok := s.mediaClient(w, r)
		if !ok {
			return
		}
		q, u, server := r.URL.Query(), userFrom(r), r.PathValue("server")
		if r.ContentLength <= 0 || r.ContentLength > maxUpload {
			jerr(w, 413, fmt.Sprintf("upload must declare a size up to %d GiB", maxUpload>>30))
			return
		}
		upid, err := c.Upload(r.Context(), node, q.Get("storage"), q.Get("content"), q.Get("filename"), r.ContentLength, http.MaxBytesReader(w, r.Body, maxUpload))
		s.St.Audit(u.Name, "media.upload", fmt.Sprintf("%s/%s/%s", server, node, q.Get("storage")), q.Get("filename"), err == nil)
		if err != nil {
			code := 502
			if _, isAPI := err.(*pve.APIError); !isAPI {
				code = 400
			}
			jerr(w, code, err.Error())
			return
		}
		jsonOut(w, 202, map[string]string{"task": upid, "node": node})
	}))

	m.Handle("POST "+base+"/download", s.need("operator", func(w http.ResponseWriter, r *http.Request) {
		c, node, ok := s.mediaClient(w, r)
		if !ok {
			return
		}
		var in struct{ Storage, Content, URL, Filename, Checksum, Algo string }
		if !decode(w, r, &in) {
			return
		}
		upid, err := c.DownloadURL(r.Context(), node, in.Storage, in.Content, in.URL, in.Filename, in.Checksum, in.Algo)
		s.St.Audit(userFrom(r).Name, "media.download", fmt.Sprintf("%s/%s/%s", r.PathValue("server"), node, in.Storage), in.Filename, err == nil)
		s.mediaResult(w, node, upid, err)
	}))

	m.Handle("POST "+base+"/appliance", s.need("operator", func(w http.ResponseWriter, r *http.Request) {
		c, node, ok := s.mediaClient(w, r)
		if !ok {
			return
		}
		var in struct{ Storage, Template string }
		if !decode(w, r, &in) {
			return
		}
		upid, err := c.DownloadAppliance(r.Context(), node, in.Storage, in.Template)
		s.St.Audit(userFrom(r).Name, "media.appliance", fmt.Sprintf("%s/%s/%s", r.PathValue("server"), node, in.Storage), in.Template, err == nil)
		s.mediaResult(w, node, upid, err)
	}))
}

func (s *Server) mediaResult(w http.ResponseWriter, node, upid string, err error) {
	if err != nil {
		code := 502
		if _, isAPI := err.(*pve.APIError); !isAPI {
			code = 400
		}
		jerr(w, code, err.Error())
		return
	}
	jsonOut(w, 202, map[string]string{"task": upid, "node": node})
}
