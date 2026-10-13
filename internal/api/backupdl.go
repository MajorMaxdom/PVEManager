package api

import (
	"bytes"
	"net/http"
	"time"
)

func (s *Server) registerBackupDownload(m *http.ServeMux) {
	// A backup contains the master key and all secrets: browser session of an admin only, never an API key.
	m.Handle("POST /api/backup", s.needSession("admin", func(w http.ResponseWriter, r *http.Request) {
		if s.BackupFn == nil {
			jerr(w, 501, "backups are not available")
			return
		}
		var in struct{ Passphrase string }
		if !decode(w, r, &in) {
			return
		}
		var buf bytes.Buffer
		err := s.BackupFn(in.Passphrase, &buf)
		s.St.Audit(userFrom(r).Name, "manager.backup", clientIP(r), "", err == nil)
		if err != nil {
			jerr(w, 400, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="pve-manager-backup-`+time.Now().Format("20060102-1504")+`.pvmbak"`)
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(buf.Bytes())
	}))
}
