package api

import (
	"context"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"pvemanager/internal/cron"
	"pvemanager/internal/notify"
	"pvemanager/internal/store"
)

type channelView struct {
	ID      int64    `json:"id"`
	Name    string   `json:"name"`
	Kind    string   `json:"kind"`
	Events  []string `json:"events"`
	Enabled bool     `json:"enabled"`
	Summary string   `json:"summary"`
}

var reLabel = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N} ._-]{0,59}$`)

func (s *Server) registerExtras(m *http.ServeMux) {
	m.Handle("GET /api/channels", s.need("admin", func(w http.ResponseWriter, r *http.Request) {
		list, err := s.St.Channels()
		if err != nil {
			jerr(w, 500, "internal error")
			return
		}
		out := []channelView{}
		for _, c := range list {
			out = append(out, channelView{c.ID, c.Name, c.Kind, c.Events, c.Enabled, notify.Summary(c.Kind, c.Config)})
		}
		jsonOut(w, 200, map[string]any{"channels": out, "events": notify.Events})
	}))

	m.Handle("POST /api/channels", s.need("admin", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Name   string
			Kind   string
			Config map[string]string
			Events []string
		}
		if !decode(w, r, &in) {
			return
		}
		if !reLabel.MatchString(in.Name) {
			jerr(w, 400, "invalid name")
			return
		}
		if err := notify.Validate(in.Kind, in.Config); err != nil {
			jerr(w, 400, err.Error())
			return
		}
		if len(in.Events) == 0 {
			jerr(w, 400, "choose at least one event")
			return
		}
		for _, e := range in.Events {
			if !notify.ValidEvent(e) {
				jerr(w, 400, "unknown event "+e)
				return
			}
		}
		id, err := s.St.PutChannel(store.Channel{Name: in.Name, Kind: in.Kind, Config: in.Config, Events: in.Events, Enabled: true})
		s.St.Audit(userFrom(r).Name, "channel.create", in.Name, in.Kind, err == nil)
		if err != nil {
			jerr(w, 409, "a channel with this name already exists")
			return
		}
		jsonOut(w, 201, map[string]int64{"id": id})
	}))

	m.Handle("DELETE /api/channels/{id}", s.need("admin", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		err := s.St.DeleteChannel(id)
		s.St.Audit(userFrom(r).Name, "channel.delete", r.PathValue("id"), "", err == nil)
		jsonOut(w, 200, map[string]bool{"ok": err == nil})
	}))

	m.Handle("POST /api/channels/{id}/test", s.need("admin", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		list, _ := s.St.Channels()
		for _, c := range list {
			if c.ID != id {
				continue
			}
			ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
			defer cancel()
			err := notify.Send(ctx, c.Kind, c.Config, "pve-manager test", "This is a test message from pve-manager.")
			s.St.Audit(userFrom(r).Name, "channel.test", c.Name, "", err == nil)
			if err != nil {
				jerr(w, 502, err.Error())
				return
			}
			jsonOut(w, 200, map[string]bool{"ok": true})
			return
		}
		jerr(w, 404, "unknown channel")
	}))

	m.Handle("GET /api/schedules", s.need("viewer", func(w http.ResponseWriter, r *http.Request) {
		l, err := s.St.Schedules()
		if err != nil {
			jerr(w, 500, "internal error")
			return
		}
		jsonOut(w, 200, l)
	}))

	m.Handle("POST /api/schedules", s.need("admin", func(w http.ResponseWriter, r *http.Request) {
		var in store.Schedule
		if !decode(w, r, &in) {
			return
		}
		if !reLabel.MatchString(in.Name) {
			jerr(w, 400, "invalid name")
			return
		}
		if _, err := cron.Parse(in.Cron); err != nil {
			jerr(w, 400, "cron: "+err.Error())
			return
		}
		if _, _, ok := s.Poll.Client(in.Server); !ok {
			jerr(w, 400, "unknown server")
			return
		}
		switch in.Action {
		case "apt-refresh":
			in.GType, in.VMID = "", 0
		case "start", "shutdown", "reboot", "backup":
			if _, ok := s.Poll.FindGuest(in.Server, in.GType, in.VMID); !ok {
				jerr(w, 400, "guest not found on that server")
				return
			}
			if in.Action == "backup" {
				if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`).MatchString(in.Params["storage"]) {
					jerr(w, 400, "backup needs a storage")
					return
				}
				if m := in.Params["mode"]; m != "" && m != "snapshot" && m != "suspend" && m != "stop" {
					jerr(w, 400, "invalid backup mode")
					return
				}
			}
		default:
			jerr(w, 400, "unknown action")
			return
		}
		keep := map[string]string{}
		for _, k := range []string{"storage", "mode"} {
			if in.Params[k] != "" {
				keep[k] = in.Params[k]
			}
		}
		in.Params, in.Enabled = keep, true
		id, err := s.St.PutSchedule(in)
		s.St.Audit(userFrom(r).Name, "schedule.create", in.Name, in.Cron+" "+in.Action, err == nil)
		if err != nil {
			jerr(w, 500, "internal error")
			return
		}
		jsonOut(w, 201, map[string]int64{"id": id})
	}))

	m.Handle("POST /api/schedules/{id}/enabled", s.need("admin", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		var in struct{ Enabled bool }
		if !decode(w, r, &in) {
			return
		}
		if err := s.St.SetScheduleEnabled(id, in.Enabled); err != nil {
			jerr(w, 404, "unknown schedule")
			return
		}
		s.St.Audit(userFrom(r).Name, "schedule.toggle", r.PathValue("id"), strconv.FormatBool(in.Enabled), true)
		jsonOut(w, 200, map[string]bool{"ok": true})
	}))

	m.Handle("DELETE /api/schedules/{id}", s.need("admin", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		err := s.St.DeleteSchedule(id)
		s.St.Audit(userFrom(r).Name, "schedule.delete", r.PathValue("id"), "", err == nil)
		jsonOut(w, 200, map[string]bool{"ok": err == nil})
	}))
}
