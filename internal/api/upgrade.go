package api

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"pvemanager/internal/sshx"
)

// upgrade runs the restricted upgrade wrapper on the host over SSH and streams its output into a job.
func (s *Server) upgrade(w http.ResponseWriter, r *http.Request) {
	u, server := userFrom(r), r.PathValue("server")
	_, meta, ok := s.Poll.Client(server)
	if !ok {
		jerr(w, 404, "unknown server")
		return
	}
	if meta.SSHUser == "" || meta.SSHHostKey == "" {
		jerr(w, 409, "SSH upgrade is not configured for this server (onboard with --with-updates)")
		return
	}
	j, started := s.Jobs.Start(server, "upgrade", u.Name)
	if !started {
		jsonOut(w, 409, map[string]any{"error": "an upgrade is already running", "job": j.ID})
		return
	}
	s.St.Audit(u.Name, "apt.upgrade", server, j.ID, true)
	host := meta.SSHHost
	if host == "" {
		if pu, err := url.Parse(meta.URL); err == nil {
			host = pu.Hostname()
		}
	}
	t := sshx.Target{Host: host, User: meta.SSHUser, KeyFile: s.SSHKey, Fingerprint: meta.SSHHostKey}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
		defer cancel()
		j.Append("$ sudo /usr/local/sbin/pve-manager-upgrade upgrade")
		err := sshx.Run(ctx, t, "sudo -n /usr/local/sbin/pve-manager-upgrade upgrade", j.Append)
		j.Finish(err)
		if err != nil && s.Notify != nil {
			s.Notify("job.failed", server+": upgrade failed", err.Error())
		}
		s.St.Audit(u.Name, "apt.upgrade.done", server, j.Status, err == nil)
		s.Poll.RefreshNow(context.Background(), server)
	}()
	jsonOut(w, 202, map[string]string{"job": j.ID})
}

func (s *Server) job(w http.ResponseWriter, r *http.Request) {
	j, ok := s.Jobs.Get(r.PathValue("id"))
	if !ok {
		jerr(w, 404, "unknown job")
		return
	}
	from, _ := strconv.Atoi(r.URL.Query().Get("from"))
	jsonOut(w, 200, j.View(from))
}
