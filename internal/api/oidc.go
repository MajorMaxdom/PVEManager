package api

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"pvemanager/internal/oidcauth"
	"pvemanager/internal/store"

	"golang.org/x/oauth2"
)

const ssoCookie = "pvm_sso"

func (s *Server) authOptions(w http.ResponseWriter, r *http.Request) {
	o := map[string]any{"enabled": s.OIDC != nil}
	if s.OIDC != nil {
		o["label"] = s.OIDC.Label()
	}
	jsonOut(w, 200, map[string]any{"local": true, "oidc": o})
}

func rnd(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func (s *Server) sign(payload string) string {
	m := hmac.New(sha256.New, s.oidcKey)
	m.Write([]byte(payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func (s *Server) unsign(v string) (string, bool) {
	i := strings.LastIndexByte(v, '.')
	if i < 0 {
		return "", false
	}
	want := s.sign(v[:i])
	return v[:i], subtle.ConstantTimeCompare([]byte(want), []byte(v)) == 1
}

func (s *Server) ssoFail(w http.ResponseWriter, r *http.Request, code string, err error, extra ...string) {
	if err != nil {
		s.Log.Warn("sso login failed", "reason", code, "err", err)
	}
	s.St.Audit("sso", "login", clientIP(r), code, false)
	q := "sso_error=" + url.QueryEscape(code)
	for i := 0; i+1 < len(extra); i += 2 {
		q += "&" + extra[i] + "=" + url.QueryEscape(extra[i+1])
	}
	http.Redirect(w, r, "/?"+q, http.StatusFound)
}

func (s *Server) oidcLogin(w http.ResponseWriter, r *http.Request) {
	if s.OIDC == nil {
		http.NotFound(w, r)
		return
	}
	if want := s.OIDC.RedirectHost(); want != "" && !strings.EqualFold(want, r.Host) {
		// the state cookie is host-bound: starting on another host would lose it at the callback
		s.Log.Warn("sso login started on a different host than the redirect URL", "got", r.Host, "want", want)
		s.ssoFail(w, r, "host", nil, "sso_host", want)
		return
	}
	state, nonce, verifier := rnd(24), rnd(24), oauth2.GenerateVerifier()
	u, err := s.OIDC.AuthURL(r.Context(), state, nonce, verifier)
	if err != nil {
		s.ssoFail(w, r, "unavailable", err)
		return
	}
	exp := time.Now().Add(10 * time.Minute)
	// SameSite=Lax: the IdP redirects back with a top-level GET, which Strict would not carry the cookie on.
	http.SetCookie(w, &http.Cookie{Name: ssoCookie, Value: s.sign(strings.Join([]string{state, nonce, verifier, strconv.FormatInt(exp.Unix(), 10)}, ".")),
		Path: "/api/oidc", Expires: exp, HttpOnly: true, Secure: s.Secure, SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, u, http.StatusFound)
}

func (s *Server) oidcCallback(w http.ResponseWriter, r *http.Request) {
	if s.OIDC == nil {
		http.NotFound(w, r)
		return
	}
	ip := clientIP(r)
	if s.limiter.Blocked(ip) {
		http.Error(w, "too many attempts", http.StatusTooManyRequests)
		return
	}
	// the state cookie is single-use whatever happens next
	http.SetCookie(w, &http.Cookie{Name: ssoCookie, Value: "", Path: "/api/oidc", MaxAge: -1, HttpOnly: true, Secure: s.Secure, SameSite: http.SameSiteLaxMode})
	c, err := r.Cookie(ssoCookie)
	if err != nil {
		s.limiter.Fail(ip)
		s.ssoFail(w, r, "failed", err)
		return
	}
	payload, ok := s.unsign(c.Value)
	parts := strings.Split(payload, ".")
	if !ok || len(parts) != 4 {
		s.limiter.Fail(ip)
		s.ssoFail(w, r, "failed", nil)
		return
	}
	if exp, _ := strconv.ParseInt(parts[3], 10, 64); time.Now().Unix() > exp {
		s.ssoFail(w, r, "failed", nil)
		return
	}
	if subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("state")), []byte(parts[0])) != 1 {
		s.limiter.Fail(ip)
		s.ssoFail(w, r, "failed", nil)
		return
	}
	if e := r.URL.Query().Get("error"); e != "" {
		s.ssoFail(w, r, "denied", nil)
		return
	}
	id, err := s.OIDC.Exchange(r.Context(), r.URL.Query().Get("code"), parts[2], parts[1])
	if err != nil {
		s.limiter.Fail(ip)
		s.ssoFail(w, r, "failed", err)
		return
	}
	role, ok := s.OIDC.RoleFor(id.Groups)
	if !ok {
		s.ssoFail(w, r, "denied", nil)
		return
	}
	u := store.User{Name: id.Username, Hash: oidcauth.PasswordMarker, Role: role}
	if ex, err := s.St.User(id.Username); err == nil && ex.Hash != oidcauth.PasswordMarker {
		// a local account of that name exists: never let an IdP identity take it over
		s.ssoFail(w, r, "local-user", nil)
		return
	}
	if err := s.St.PutUser(u); err != nil {
		s.ssoFail(w, r, "failed", err)
		return
	}
	s.limiter.Reset(ip)
	if !s.startSession(w, u) {
		return
	}
	s.St.Audit(u.Name, "login.sso", ip, role, true)
	http.Redirect(w, r, "/", http.StatusFound)
}
