package api

import (
	"net/http"
	"sort"
	"strconv"

	"pvemanager/internal/pve"
)

func (s *Server) registerTasks(m *http.ServeMux) {
	// recent tasks of one server, newest first
	m.Handle("GET /api/servers/{server}/task-list", s.need("viewer", func(w http.ResponseWriter, r *http.Request) {
		c, _, ok := s.Poll.Client(r.PathValue("server"))
		if !ok {
			jerr(w, 404, "unknown server")
			return
		}
		l, err := c.ClusterTasks(r.Context())
		if err != nil {
			jerr(w, 502, err.Error())
			return
		}
		sort.Slice(l, func(i, j int) bool { return l[i].StartTime > l[j].StartTime })
		limit := 150
		if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 && n <= 500 {
			limit = n
		}
		if len(l) > limit {
			l = l[:limit]
		}
		if l == nil {
			l = []pve.ClusterTask{}
		}
		jsonOut(w, 200, l)
	}))

	m.Handle("GET /api/servers/{server}/tasks/{node}/log", s.need("viewer", func(w http.ResponseWriter, r *http.Request) {
		server, node, upid := r.PathValue("server"), r.PathValue("node"), r.URL.Query().Get("upid")
		c, _, ok := s.Poll.Client(server)
		if !ok || !s.nodeOf(server, node) || !reUPID.MatchString(upid) {
			jerr(w, 404, "unknown task")
			return
		}
		start, _ := strconv.Atoi(r.URL.Query().Get("start"))
		lines, err := c.TaskLog(r.Context(), node, upid, start, 500)
		if err != nil {
			jerr(w, 502, err.Error())
			return
		}
		if lines == nil {
			lines = []pve.TaskLine{}
		}
		st, _ := c.TaskStatus(r.Context(), node, upid)
		jsonOut(w, 200, map[string]any{"lines": lines, "status": st.Status, "exitstatus": st.ExitStatus})
	}))
}
