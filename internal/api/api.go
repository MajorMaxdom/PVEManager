// Package api wires the HTTP surface: session auth, state, power actions, updates, consoles.
package api

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"pvemanager/internal/auth"
	"pvemanager/internal/jobs"
	"pvemanager/internal/metrics"
	"pvemanager/internal/oidcauth"
	"pvemanager/internal/poller"
	"pvemanager/internal/pve"
	"pvemanager/internal/store"
)

const (
	cookieName   = "pvm_session"
	csrfHeader   = "X-PVM" // any value; browsers cannot add it cross-site without CORS preflight
	sessionTTL   = 12 * time.Hour
	keyPrefix    = "pvm_"
	keyPrefixLen = 9 // "pvm_" + 5 characters, shown in the key list
)

type Server struct {
	St           *store.Store
	Poll         *poller.Poller
	Static       fs.FS
	Secure       bool // set Secure on the cookie (TLS enabled / behind TLS proxy)
	Log          *slog.Logger
	SSHKey       string
	Notify       func(event, title, body string) // set by main; may be nil
	MetricsToken string                          // empty: /metrics is disabled
	Version      string
	Jobs         *jobs.Registry
	limiter      *auth.Limiter
	consoles     sync.Map                                   // id -> *consoleSession
	OIDC         *oidcauth.Provider                         // nil: single sign-on is disabled
	oidcKey      []byte                                     // signs the short-lived SSO state cookie
	BackupFn     func(passphrase string, w io.Writer) error // set by main
}

func New(st *store.Store, p *poller.Poller, static fs.FS, secure bool, sshKey string, log *slog.Logger) *Server {
	return &Server{St: st, Poll: p, Static: static, Secure: secure, SSHKey: sshKey, Jobs: jobs.New(), Log: log, limiter: auth.NewLimiter(8, 10*time.Minute), oidcKey: []byte(rnd(32))}
}

type ctxKey struct{}

func userFrom(r *http.Request) store.User { u, _ := r.Context().Value(ctxKey{}).(store.User); return u }

func (s *Server) Handler() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("POST /api/login", s.login)
	m.HandleFunc("POST /api/logout", s.logout)
	m.Handle("GET /api/me", s.need("viewer", s.me))
	s.registerAccounts(m)
	m.Handle("GET /api/state", s.need("viewer", s.state))
	m.Handle("GET /api/events", s.need("viewer", s.events))
	m.Handle("POST /api/servers/{server}/guests/{type}/{vmid}/{action}", s.need("operator", s.power))
	m.Handle("GET /api/servers/{server}/updates", s.need("viewer", s.updates))
	m.Handle("POST /api/servers/{server}/updates/refresh", s.need("operator", s.updatesRefresh))
	m.Handle("POST /api/servers/{server}/guests/{type}/{vmid}/console", s.need("operator", s.consoleOpen))
	m.Handle("GET /api/console/{id}", s.need("operator", s.consoleWS))
	m.Handle("POST /api/servers/{server}/updates/upgrade", s.need("admin", s.upgrade))
	m.Handle("GET /api/servers/{server}/infra", s.need("viewer", s.infra))
	m.Handle("GET /api/servers/{server}/create-options", s.need("operator", s.createOptions))
	m.Handle("POST /api/servers/{server}/vms", s.need("operator", s.createGuest("qemu")))
	m.Handle("POST /api/servers/{server}/containers", s.need("operator", s.createGuest("lxc")))
	m.Handle("DELETE /api/servers/{server}/guests/{type}/{vmid}", s.need("admin", s.deleteGuest))
	m.Handle("GET /api/servers/{server}/tasks/{node}", s.need("viewer", s.taskStatus))
	m.Handle("GET /api/jobs/{id}", s.need("viewer", s.job))
	m.Handle("GET /api/audit", s.need("admin", s.audit))
	s.registerGuestOps(m)
	s.registerExtras(m)
	s.registerMedia(m)
	s.registerBackups(m)
	s.registerTasks(m)
	s.registerBackupDownload(m)
	s.registerCloudInit(m)
	m.HandleFunc("GET /metrics", s.metrics)
	m.HandleFunc("GET /api/auth-options", s.authOptions)
	m.HandleFunc("GET /api/oidc/login", s.oidcLogin)
	m.HandleFunc("GET /api/oidc/callback", s.oidcCallback)
	m.Handle("/", s.spa())
	return secureHeaders(m)
}

func secureHeaders(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hd := w.Header()
		hd.Set("X-Content-Type-Options", "nosniff")
		hd.Set("X-Frame-Options", "DENY")
		hd.Set("Referrer-Policy", "no-referrer")
		hd.Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self' wss: ws:; frame-ancestors 'none'")
		h.ServeHTTP(w, r)
	})
}

func (s *Server) spa() http.Handler {
	files := http.FileServerFS(s.Static)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := fs.Stat(s.Static, "index.html"); err != nil {
			http.Error(w, "The web UI is not built into this binary. Run 'make web', then build again.", http.StatusServiceUnavailable)
			return
		}
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			p = "index.html"
		}
		if _, err := fs.Stat(s.Static, p); err != nil { // client-side routes fall back to the app shell
			r.URL.Path = "/"
		}
		files.ServeHTTP(w, r)
	})
}

// need requires a logged-in user (browser session) or an API key with at least the given role.
func (s *Server) need(role string, h http.HandlerFunc) http.Handler { return s.guard(role, false, h) }

// needSession is for account administration: only a browser session counts, never an API key.
func (s *Server) needSession(role string, h http.HandlerFunc) http.Handler {
	return s.guard(role, true, h)
}

func (s *Server) guard(role string, sessionOnly bool, h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var u store.User
		if c, err := r.Cookie(cookieName); err == nil {
			var e error
			if u, e = s.St.Session(auth.SessionID(c.Value)); e != nil {
				jerr(w, 401, "session expired")
				return
			}
			if r.Method != http.MethodGet && r.Header.Get(csrfHeader) == "" {
				jerr(w, 403, "missing "+csrfHeader+" header")
				return
			}
		} else if tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok && strings.HasPrefix(tok, keyPrefix) && !sessionOnly {
			ip := clientIP(r)
			if s.limiter.Blocked(ip) {
				jerr(w, 429, "too many attempts")
				return
			}
			k, e := s.St.APIKeyByHash(auth.SessionID(tok)) // same SHA-256 hashing as session ids
			if e != nil {
				s.limiter.Fail(ip)
				jerr(w, 401, "invalid API key")
				return
			}
			u = store.User{Name: "key:" + k.Name, Role: k.Role} // bearer auth is not ambient, so no CSRF header is needed
		} else {
			jerr(w, 401, "not logged in")
			return
		}
		if !auth.Allows(u.Role, role) {
			jerr(w, 403, "insufficient role")
			return
		}
		h(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, u)))
	})
}

func jerr(w http.ResponseWriter, code int, msg string) {
	jsonOut(w, code, map[string]string{"error": msg})
}

func jsonOut(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func clientIP(r *http.Request) string {
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return h
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get(csrfHeader) == "" {
		jerr(w, 403, "missing "+csrfHeader+" header")
		return
	}
	ip := clientIP(r)
	if s.limiter.Blocked(ip) {
		jerr(w, 429, "too many attempts, try again later")
		return
	}
	var in struct{ User, Password string }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in); err != nil {
		jerr(w, 400, "bad request")
		return
	}
	u, err := s.St.User(in.User)
	hash := "argon2id$65536$3$2$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA" // constant work for unknown users
	if err == nil {
		hash = u.Hash
	}
	if !auth.CheckPassword(in.Password, hash) || err != nil {
		s.limiter.Fail(ip)
		s.St.Audit(in.User, "login", ip, "", false)
		jerr(w, 401, "invalid credentials")
		return
	}
	s.limiter.Reset(ip)
	if !s.startSession(w, u) {
		return
	}
	s.St.Audit(u.Name, "login", ip, "", true)
	jsonOut(w, 200, map[string]string{"user": u.Name, "role": u.Role})
}

// startSession creates a session and sets the cookie. It reports false after writing an error.
func (s *Server) startSession(w http.ResponseWriter, u store.User) bool {
	tok, id, _ := auth.NewSessionToken()
	exp := time.Now().Add(sessionTTL)
	if err := s.St.PutSession(id, u.Name, exp); err != nil {
		jerr(w, 500, "internal error")
		return false
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: tok, Path: "/", Expires: exp, HttpOnly: true, Secure: s.Secure, SameSite: http.SameSiteStrictMode})
	return true
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		_ = s.St.DeleteSession(auth.SessionID(c.Value))
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.Secure, SameSite: http.SameSiteStrictMode})
	jsonOut(w, 200, map[string]bool{"ok": true})
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	jsonOut(w, 200, map[string]string{"user": u.Name, "role": u.Role})
}

func (s *Server) state(w http.ResponseWriter, r *http.Request) { jsonOut(w, 200, s.Poll.Snapshot()) }

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		jerr(w, 500, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	ch, cancel := s.Poll.Subscribe()
	defer cancel()
	first, _ := json.Marshal(s.Poll.Snapshot())
	fmt.Fprintf(w, "event: state\ndata: %s\n\n", first)
	fl.Flush()
	ka := time.NewTicker(25 * time.Second)
	defer ka.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case b := <-ch:
			fmt.Fprintf(w, "event: state\ndata: %s\n\n", b)
		case <-ka.C:
			fmt.Fprint(w, ": ping\n\n")
		}
		fl.Flush()
	}
}

// resolveGuest finds the node a guest currently lives on from the polled snapshot.
func (s *Server) resolveGuest(server, typ, vmidStr string) (c *pve.Client, g pve.Guest, err error) {
	c, _, ok := s.Poll.Client(server)
	if !ok {
		return nil, g, fmt.Errorf("unknown server")
	}
	vmid, e := strconv.Atoi(vmidStr)
	if e != nil || (typ != "qemu" && typ != "lxc") {
		return nil, g, fmt.Errorf("bad guest reference")
	}
	for _, st := range s.Poll.Snapshot() {
		if st.Name != server {
			continue
		}
		for _, x := range st.Guests {
			if x.VMID == vmid && x.Type == typ {
				return c, x, nil
			}
		}
	}
	return nil, g, fmt.Errorf("guest not found")
}

func (s *Server) power(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	server, typ, vmid, action := r.PathValue("server"), r.PathValue("type"), r.PathValue("vmid"), r.PathValue("action")
	target := fmt.Sprintf("%s/%s/%s", server, typ, vmid)
	if !pve.IsPowerAction(action) {
		jerr(w, 400, "unknown action")
		return
	}
	c, g, err := s.resolveGuest(server, typ, vmid)
	if err != nil {
		jerr(w, 404, err.Error())
		return
	}
	upid, err := c.Power(r.Context(), g.Node, typ, g.VMID, action)
	s.St.Audit(u.Name, "power."+action, target, g.Name, err == nil)
	if err != nil {
		jerr(w, 502, err.Error())
		return
	}
	go func() { time.Sleep(2 * time.Second); s.Poll.RefreshNow(context.Background(), server) }()
	jsonOut(w, 202, map[string]string{"task": upid})
}

func (s *Server) updates(w http.ResponseWriter, r *http.Request) {
	server := r.PathValue("server")
	c, meta, ok := s.Poll.Client(server)
	if !ok {
		jerr(w, 404, "unknown server")
		return
	}
	out := []map[string]any{}
	for _, st := range s.Poll.Snapshot() {
		if st.Name != server {
			continue
		}
		for _, n := range st.Nodes {
			pk, err := c.AptUpdates(r.Context(), n.Node)
			e := ""
			if err != nil {
				e = err.Error()
			}
			out = append(out, map[string]any{"node": n.Node, "packages": pk, "error": e})
		}
	}
	jsonOut(w, 200, map[string]any{"nodes": out, "canUpgrade": meta.SSHUser != ""})
}

func (s *Server) updatesRefresh(w http.ResponseWriter, r *http.Request) {
	u, server := userFrom(r), r.PathValue("server")
	c, _, ok := s.Poll.Client(server)
	if !ok {
		jerr(w, 404, "unknown server")
		return
	}
	var tasks []string
	for _, st := range s.Poll.Snapshot() {
		if st.Name == server {
			for _, n := range st.Nodes {
				upid, err := c.AptRefresh(r.Context(), n.Node)
				s.St.Audit(u.Name, "apt.update", server+"/"+n.Node, "", err == nil)
				if err != nil {
					jerr(w, 502, err.Error())
					return
				}
				tasks = append(tasks, upid)
			}
		}
	}
	jsonOut(w, 202, map[string]any{"tasks": tasks})
}

func (s *Server) audit(w http.ResponseWriter, r *http.Request) {
	l, err := s.St.AuditList(200)
	if err != nil {
		jerr(w, 500, "internal error")
		return
	}
	jsonOut(w, 200, l)
}

func randID() string {
	b := make([]byte, 18)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// metrics serves the Prometheus endpoint; it authenticates with a static bearer token, not a session.
func (s *Server) metrics(w http.ResponseWriter, r *http.Request) {
	if s.MetricsToken == "" {
		http.NotFound(w, r)
		return
	}
	ip := clientIP(r)
	if s.limiter.Blocked(ip) {
		http.Error(w, "too many attempts", http.StatusTooManyRequests)
		return
	}
	got := r.Header.Get("Authorization")
	if subtle.ConstantTimeCompare([]byte(got), []byte("Bearer "+s.MetricsToken)) != 1 {
		s.limiter.Fail(ip)
		w.Header().Set("WWW-Authenticate", `Bearer realm="pve-manager"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	metrics.Write(w, s.Version, s.Poll.Snapshot())
}
