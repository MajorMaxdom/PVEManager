package api

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"regexp"
	"strconv"

	"pvemanager/internal/auth"
	"pvemanager/internal/oidcauth"
	"pvemanager/internal/store"
)

var reUser = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,31}$`)

func validRole(r string) bool { return r == "admin" || r == "operator" || r == "viewer" }

func (s *Server) sessionID(r *http.Request) string {
	if c, err := r.Cookie(cookieName); err == nil {
		return auth.SessionID(c.Value)
	}
	return ""
}

// lastAdmin reports whether changing user `name` away from admin (or deleting it) would leave no admin.
func (s *Server) lastAdmin(name string) bool {
	u, err := s.St.User(name)
	if err != nil || u.Role != "admin" {
		return false
	}
	n, _ := s.St.CountAdmins()
	return n <= 1
}

func (s *Server) registerAccounts(m *http.ServeMux) {
	m.Handle("GET /api/users", s.needSession("admin", func(w http.ResponseWriter, r *http.Request) {
		us, err := s.St.Users()
		if err != nil {
			jerr(w, 500, "internal error")
			return
		}
		out := []map[string]any{}
		for _, u := range us {
			out = append(out, map[string]any{"name": u.Name, "role": u.Role, "sso": u.Hash == oidcauth.PasswordMarker})
		}
		jsonOut(w, 200, out)
	}))

	m.Handle("POST /api/users", s.needSession("admin", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Name, Password, Role string }
		if !decode(w, r, &in) {
			return
		}
		if !reUser.MatchString(in.Name) || !validRole(in.Role) {
			jerr(w, 400, "invalid name or role")
			return
		}
		if _, err := s.St.User(in.Name); err == nil {
			jerr(w, 409, "user already exists")
			return
		}
		h, err := auth.HashPassword(in.Password)
		if err != nil {
			jerr(w, 400, err.Error())
			return
		}
		err = s.St.PutUser(store.User{Name: in.Name, Hash: h, Role: in.Role})
		s.St.Audit(userFrom(r).Name, "user.create", in.Name, in.Role, err == nil)
		if err != nil {
			jerr(w, 500, "internal error")
			return
		}
		jsonOut(w, 201, map[string]bool{"ok": true})
	}))

	m.Handle("PUT /api/users/{name}", s.needSession("admin", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		var in struct{ Role, Password string }
		if !decode(w, r, &in) {
			return
		}
		u, err := s.St.User(name)
		if err != nil {
			jerr(w, 404, "unknown user")
			return
		}
		if u.Hash == oidcauth.PasswordMarker {
			jerr(w, 409, "this account is managed by single sign-on")
			return
		}
		if in.Role != "" {
			if !validRole(in.Role) {
				jerr(w, 400, "invalid role")
				return
			}
			if in.Role != "admin" && s.lastAdmin(name) {
				jerr(w, 409, "this is the last admin")
				return
			}
			u.Role = in.Role
		}
		if in.Password != "" {
			if u.Hash, err = auth.HashPassword(in.Password); err != nil {
				jerr(w, 400, err.Error())
				return
			}
		}
		if err := s.St.PutUser(u); err != nil {
			jerr(w, 500, "internal error")
			return
		}
		if in.Password != "" {
			keep := ""
			if name == userFrom(r).Name {
				keep = s.sessionID(r)
			}
			s.St.DeleteUserSessions(name, keep) // a reset password ends the old logins
		}
		s.St.Audit(userFrom(r).Name, "user.update", name, in.Role, true)
		jsonOut(w, 200, map[string]bool{"ok": true})
	}))

	m.Handle("DELETE /api/users/{name}", s.needSession("admin", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		switch {
		case name == userFrom(r).Name:
			jerr(w, 409, "you cannot delete your own account")
			return
		case s.lastAdmin(name):
			jerr(w, 409, "this is the last admin")
			return
		}
		err := s.St.DeleteUser(name)
		s.St.Audit(userFrom(r).Name, "user.delete", name, "", err == nil)
		jsonOut(w, 200, map[string]bool{"ok": err == nil})
	}))

	m.Handle("POST /api/me/password", s.needSession("viewer", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Current, New string }
		if !decode(w, r, &in) {
			return
		}
		u, err := s.St.User(userFrom(r).Name)
		if err != nil || !auth.CheckPassword(in.Current, u.Hash) {
			s.St.Audit(userFrom(r).Name, "password.change", "", "", false)
			jerr(w, 403, "current password is wrong")
			return
		}
		if u.Hash, err = auth.HashPassword(in.New); err != nil {
			jerr(w, 400, err.Error())
			return
		}
		if err := s.St.PutUser(u); err != nil {
			jerr(w, 500, "internal error")
			return
		}
		s.St.DeleteUserSessions(u.Name, s.sessionID(r)) // all other devices are signed out
		s.St.Audit(u.Name, "password.change", "", "", true)
		jsonOut(w, 200, map[string]bool{"ok": true})
	}))

	m.Handle("GET /api/keys", s.needSession("admin", func(w http.ResponseWriter, r *http.Request) {
		k, err := s.St.APIKeys()
		if err != nil {
			jerr(w, 500, "internal error")
			return
		}
		jsonOut(w, 200, k)
	}))

	m.Handle("POST /api/keys", s.needSession("admin", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Name, Role string }
		if !decode(w, r, &in) {
			return
		}
		if !reLabel.MatchString(in.Name) || !validRole(in.Role) {
			jerr(w, 400, "invalid name or role")
			return
		}
		b := make([]byte, 30)
		_, _ = rand.Read(b)
		token := keyPrefix + base64.RawURLEncoding.EncodeToString(b)
		id, err := s.St.PutAPIKey(in.Name, auth.SessionID(token), in.Role, token[:keyPrefixLen], userFrom(r).Name)
		s.St.Audit(userFrom(r).Name, "key.create", in.Name, in.Role, err == nil)
		if err != nil {
			jerr(w, 409, "a key with this name already exists")
			return
		}
		jsonOut(w, 201, map[string]any{"id": id, "token": token}) // the only time the secret is shown
	}))

	m.Handle("DELETE /api/keys/{id}", s.needSession("admin", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		err := s.St.DeleteAPIKey(id)
		s.St.Audit(userFrom(r).Name, "key.delete", r.PathValue("id"), "", err == nil)
		jsonOut(w, 200, map[string]bool{"ok": err == nil})
	}))
}
