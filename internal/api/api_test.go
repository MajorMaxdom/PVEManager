package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"pvemanager/internal/auth"
	"pvemanager/internal/crypto"
	"pvemanager/internal/poller"
	"pvemanager/internal/store"
)

type env struct {
	t   *testing.T
	srv *httptest.Server
	st  *store.Store
}

func newEnv(t *testing.T) *env {
	t.Helper()
	box, _ := crypto.New(bytes.Repeat([]byte{3}, 32))
	st, err := store.Open(t.TempDir(), box)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	for _, u := range []struct{ n, r string }{{"root", "admin"}, {"op", "operator"}, {"look", "viewer"}} {
		h, _ := auth.HashPassword("password-" + u.n + "-123")
		_ = st.PutUser(store.User{Name: u.n, Hash: h, Role: u.r})
	}
	a := New(st, poller.New(), fstest.MapFS{"index.html": {Data: []byte("x")}}, false, "", slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv := httptest.NewServer(a.Handler())
	t.Cleanup(srv.Close)
	return &env{t, srv, st}
}

type client struct {
	e      *env
	cookie string
	bearer string
	csrf   bool
}

func (c *client) do(method, path string, body any) (int, map[string]any) {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.e.srv.URL+path, rd)
	if c.cookie != "" {
		req.AddCookie(&http.Cookie{Name: cookieName, Value: c.cookie})
	}
	if c.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+c.bearer)
	}
	if c.csrf {
		req.Header.Set(csrfHeader, "1")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.e.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func (e *env) login(user string) *client {
	b, _ := json.Marshal(map[string]string{"user": user, "password": "password-" + user + "-123"})
	req, _ := http.NewRequest("POST", e.srv.URL+"/api/login", bytes.NewReader(b))
	req.Header.Set(csrfHeader, "1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		e.t.Fatalf("login %s failed: %v %v", user, err, resp)
	}
	for _, c := range resp.Cookies() {
		if c.Name == cookieName {
			return &client{e: e, cookie: c.Value, csrf: true}
		}
	}
	e.t.Fatal("no session cookie")
	return nil
}

func TestCSRFAndRoles(t *testing.T) {
	e := newEnv(t)
	root, look := e.login("root"), e.login("look")
	if code, _ := (&client{e: e}).do("GET", "/api/state", nil); code != 401 {
		t.Fatalf("anonymous: %d", code)
	}
	if code, _ := look.do("GET", "/api/state", nil); code != 200 {
		t.Fatalf("viewer GET: %d", code)
	}
	if code, _ := look.do("POST", "/api/servers/x/guests/qemu/100/start", nil); code != 403 {
		t.Fatalf("viewer power must be forbidden, got %d", code)
	}
	if code, _ := look.do("GET", "/api/users", nil); code != 403 {
		t.Fatalf("viewer lists users: %d", code)
	}
	noCSRF := &client{e: e, cookie: root.cookie}
	if code, _ := noCSRF.do("POST", "/api/users", map[string]string{"name": "x"}); code != 403 {
		t.Fatalf("cookie POST without CSRF header must fail, got %d", code)
	}
}

func TestAPIKeys(t *testing.T) {
	e := newEnv(t)
	root := e.login("root")
	code, out := root.do("POST", "/api/keys", map[string]string{"name": "ci", "role": "viewer"})
	tok, _ := out["token"].(string)
	if code != 201 || !strings.HasPrefix(tok, "pvm_") {
		t.Fatalf("create key: %d %v", code, out)
	}
	if _, listed := root.do("GET", "/api/keys", nil); strings.Contains(toJSON(listed), tok) {
		t.Fatal("secret visible after creation")
	}
	k := &client{e: e, bearer: tok} // note: no CSRF header, no cookie
	if code, _ := k.do("GET", "/api/state", nil); code != 200 {
		t.Fatalf("viewer key GET: %d", code)
	}
	if code, _ := k.do("POST", "/api/servers/x/guests/qemu/100/start", nil); code != 403 {
		t.Fatalf("viewer key must not control guests: %d", code)
	}
	if code, _ := k.do("GET", "/api/users", nil); code != 401 && code != 403 {
		t.Fatalf("API keys must not reach account administration: %d", code)
	}
	if code, _ := k.do("POST", "/api/keys", map[string]string{"name": "evil", "role": "admin"}); code == 201 {
		t.Fatal("key created a new admin key")
	}
	// even an ADMIN key must stay out of account administration: that needs a browser session
	_, ao := root.do("POST", "/api/keys", map[string]string{"name": "adm", "role": "admin"})
	adm := &client{e: e, bearer: ao["token"].(string)}
	if code, _ := adm.do("GET", "/api/state", nil); code != 200 {
		t.Fatalf("admin key GET state: %d", code)
	}
	for _, rt := range []struct{ m, p string }{{"GET", "/api/users"}, {"GET", "/api/keys"}, {"POST", "/api/keys"}, {"POST", "/api/users"}, {"DELETE", "/api/users/op"}} {
		if code, _ := adm.do(rt.m, rt.p, map[string]string{"name": "x", "role": "admin", "password": "long-enough-pw1"}); code != 401 {
			t.Fatalf("admin API key reached %s %s (status %d), must be 401", rt.m, rt.p, code)
		}
	}
	if code, _ := (&client{e: e, bearer: "pvm_wrongwrongwrongwrong"}).do("GET", "/api/state", nil); code != 401 {
		t.Fatalf("bad key: %d", code)
	}
	keys, _ := e.st.APIKeys()
	for _, key := range keys {
		if key.Name == "ci" {
			root.do("DELETE", "/api/keys/"+jsonNum(key.ID), nil)
		}
	}
	if code, _ := k.do("GET", "/api/state", nil); code != 401 {
		t.Fatalf("revoked key still works: %d", code)
	}
}

func TestUserAdministrationGuards(t *testing.T) {
	e := newEnv(t)
	root := e.login("root")
	if code, _ := root.do("DELETE", "/api/users/root", nil); code != 409 {
		t.Fatalf("self delete: %d", code)
	}
	if code, _ := root.do("PUT", "/api/users/root", map[string]string{"role": "viewer"}); code != 409 {
		t.Fatalf("demoting the last admin: %d", code)
	}
	if code, _ := root.do("POST", "/api/users", map[string]string{"name": "new", "password": "short", "role": "viewer"}); code != 400 {
		t.Fatalf("weak password accepted: %d", code)
	}
	if code, _ := root.do("POST", "/api/users", map[string]string{"name": "bad name", "password": "long-enough-pw1", "role": "viewer"}); code != 400 {
		t.Fatalf("bad user name accepted: %d", code)
	}
	if code, _ := root.do("POST", "/api/users", map[string]string{"name": "second", "password": "long-enough-pw1", "role": "admin"}); code != 201 {
		t.Fatalf("create: %d", code)
	}
	// with two admins the first may be demoted
	if code, _ := root.do("PUT", "/api/users/second", map[string]string{"role": "operator"}); code != 200 {
		t.Fatalf("demote with another admin around: %d", code)
	}
	// role change takes effect on the live session immediately
	op := e.login("op")
	root.do("PUT", "/api/users/op", map[string]string{"role": "viewer"})
	if code, _ := op.do("GET", "/api/audit", nil); code != 403 {
		t.Fatalf("audit for viewer: %d", code)
	}
}

func TestPasswordChangeEndsOtherSessions(t *testing.T) {
	e := newEnv(t)
	a, b := e.login("op"), e.login("op")
	if code, _ := a.do("POST", "/api/me/password", map[string]string{"current": "wrong", "new": "another-long-pw-1"}); code != 403 {
		t.Fatalf("wrong current password accepted: %d", code)
	}
	if code, _ := a.do("POST", "/api/me/password", map[string]string{"current": "password-op-123", "new": "another-long-pw-1"}); code != 200 {
		t.Fatalf("change: %d", code)
	}
	if code, _ := a.do("GET", "/api/me", nil); code != 200 {
		t.Fatal("the session that changed the password must stay logged in")
	}
	if code, _ := b.do("GET", "/api/me", nil); code != 401 {
		t.Fatalf("other session survived a password change: %d", code)
	}
}

func TestLoginThrottle(t *testing.T) {
	e := newEnv(t)
	for i := 0; i < 8; i++ {
		b, _ := json.Marshal(map[string]string{"user": "root", "password": "nope"})
		req, _ := http.NewRequest("POST", e.srv.URL+"/api/login", bytes.NewReader(b))
		req.Header.Set(csrfHeader, "1")
		resp, _ := http.DefaultClient.Do(req)
		resp.Body.Close()
	}
	b, _ := json.Marshal(map[string]string{"user": "root", "password": "password-root-123"})
	req, _ := http.NewRequest("POST", e.srv.URL+"/api/login", bytes.NewReader(b))
	req.Header.Set(csrfHeader, "1")
	resp, _ := http.DefaultClient.Do(req)
	if resp.StatusCode != 429 {
		t.Fatalf("correct password must still be throttled after 8 failures, got %d", resp.StatusCode)
	}
}

func toJSON(v any) string { b, _ := json.Marshal(v); return string(b) }
func jsonNum(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func TestManagerBackupDownloadRules(t *testing.T) {
	box, _ := crypto.New(bytes.Repeat([]byte{4}, 32))
	st, _ := store.Open(t.TempDir(), box)
	t.Cleanup(func() { st.Close() })
	for _, u := range []struct{ n, r string }{{"root", "admin"}, {"op", "operator"}} {
		h, _ := auth.HashPassword("password-" + u.n + "-123")
		_ = st.PutUser(store.User{Name: u.n, Hash: h, Role: u.r})
	}
	a := New(st, poller.New(), fstest.MapFS{"index.html": {Data: []byte("x")}}, false, "", slog.New(slog.NewTextHandler(io.Discard, nil)))
	var gotPass string
	a.BackupFn = func(pass string, w io.Writer) error {
		gotPass = pass
		if len(pass) < 12 {
			return errors.New("the passphrase must have at least 12 characters")
		}
		_, err := w.Write([]byte("ARCHIVE-BYTES"))
		return err
	}
	srv := httptest.NewServer(a.Handler())
	defer srv.Close()
	e := &env{t, srv, st}
	root, op := e.login("root"), e.login("op")

	if code, _ := op.do("POST", "/api/backup", map[string]string{"passphrase": "long-enough-passphrase"}); code != 403 {
		t.Fatalf("operator may not download a backup: %d", code)
	}
	if code, _ := root.do("POST", "/api/backup", map[string]string{"passphrase": "short"}); code != 400 {
		t.Fatalf("weak passphrase must give a clear error: %d", code)
	}
	// an admin API key must not be able to pull the master key out
	_, ko := root.do("POST", "/api/keys", map[string]string{"name": "adm", "role": "admin"})
	if code, _ := (&client{e: e, bearer: ko["token"].(string)}).do("POST", "/api/backup", map[string]string{"passphrase": "long-enough-passphrase"}); code != 401 {
		t.Fatalf("API keys must not reach the backup download: %d", code)
	}
	req, _ := http.NewRequest("POST", srv.URL+"/api/backup", strings.NewReader(`{"passphrase":"long-enough-passphrase"}`))
	req.AddCookie(&http.Cookie{Name: cookieName, Value: root.cookie})
	req.Header.Set(csrfHeader, "1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || string(body) != "ARCHIVE-BYTES" || !strings.Contains(resp.Header.Get("Content-Disposition"), ".pvmbak") || resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("download: %d %q %v", resp.StatusCode, body, resp.Header)
	}
	if gotPass != "long-enough-passphrase" {
		t.Fatalf("passphrase not passed through: %q", gotPass)
	}
	if code, _ := (&client{e: e}).do("POST", "/api/backup", map[string]string{"passphrase": "long-enough-passphrase"}); code != 401 {
		t.Fatalf("anonymous download: %d", code)
	}
}
