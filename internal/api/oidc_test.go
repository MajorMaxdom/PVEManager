package api

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"

	"pvemanager/internal/config"
	"pvemanager/internal/oidcauth"
	"pvemanager/internal/poller"
)

// fakeIdP is a minimal OpenID provider that auto-approves and can be told to misbehave.
type fakeIdP struct {
	srv   *httptest.Server
	key   *rsa.PrivateKey
	other *rsa.PrivateKey // for tokens signed with a key that is NOT in the JWKS
	mu    sync.Mutex
	codes map[string]pending
	// per-test behaviour
	claims    map[string]any
	badNonce  bool
	audience  string
	expired   bool
	wrongSign bool
	denyAuth  bool
}

type pending struct {
	nonce, challenge string
}

func newIdP(t *testing.T) *fakeIdP {
	t.Helper()
	k, _ := rsa.GenerateKey(rand.Reader, 2048)
	o, _ := rsa.GenerateKey(rand.Reader, 2048)
	f := &fakeIdP{key: k, other: o, codes: map[string]pending{}, audience: "pvm-client",
		claims: map[string]any{"preferred_username": "alice", "groups": []string{"pve-admins"}}}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"issuer": f.srv.URL, "authorization_endpoint": f.srv.URL + "/authorize", "token_endpoint": f.srv.URL + "/token",
			"jwks_uri": f.srv.URL + "/jwks", "id_token_signing_alg_values_supported": []string{"RS256"}, "response_types_supported": []string{"code"}, "subject_types_supported": []string{"public"}})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &k.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"}}})
	})
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		redir, _ := url.Parse(q.Get("redirect_uri"))
		rq := redir.Query()
		rq.Set("state", q.Get("state"))
		if f.denyAuth {
			rq.Set("error", "access_denied")
		} else {
			code := rnd(12)
			f.mu.Lock()
			f.codes[code] = pending{nonce: q.Get("nonce"), challenge: q.Get("code_challenge")}
			f.mu.Unlock()
			rq.Set("code", code)
		}
		redir.RawQuery = rq.Encode()
		http.Redirect(w, r, redir.String(), http.StatusFound)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		f.mu.Lock()
		p, ok := f.codes[r.FormValue("code")]
		delete(f.codes, r.FormValue("code"))
		f.mu.Unlock()
		if !ok || r.FormValue("code_verifier") == "" || p.challenge == "" { // PKCE must be in use
			http.Error(w, `{"error":"invalid_grant"}`, 400)
			return
		}
		signer := k
		if f.wrongSign {
			signer = o
		}
		sg, _ := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: signer}, (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "k1"))
		nonce := p.nonce
		if f.badNonce {
			nonce = "forged"
		}
		exp := time.Now().Add(time.Hour)
		if f.expired {
			exp = time.Now().Add(-time.Hour)
		}
		std := jwt.Claims{Issuer: f.srv.URL, Subject: "sub-1", Audience: jwt.Audience{f.audience}, Expiry: jwt.NewNumericDate(exp), IssuedAt: jwt.NewNumericDate(time.Now().Add(-2 * time.Hour))}
		raw, _ := jwt.Signed(sg).Claims(std).Claims(map[string]any{"nonce": nonce}).Claims(f.claims).Serialize()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"access_token": "at", "token_type": "Bearer", "id_token": raw})
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

type ssoEnv struct {
	*env
	idp *fakeIdP
	url string
}

func newSSO(t *testing.T, def string) *ssoEnv {
	t.Helper()
	e := newEnv(t)
	idp := newIdP(t)
	a := New(e.st, poller.New(), fstest.MapFS{"index.html": {Data: []byte("x")}}, false, "", slog.New(slog.NewTextHandler(testWriter{t}, nil)))
	front := httptest.NewServer(a.Handler())
	t.Cleanup(front.Close)
	a.OIDC = oidcauth.New(config.OIDC{Issuer: idp.srv.URL, ClientID: "pvm-client", AdminGroup: "pve-admins", OperatorGroup: "pve-ops", ViewerGroup: "pve-view", DefaultRole: def, ButtonLabel: "Login with Test"},
		"secret", front.URL+"/api/oidc/callback")
	return &ssoEnv{e, idp, front.URL}
}

// browse walks the SSO flow like a browser: cookie jar, follows redirects, returns the final URL and the jar.
func (s *ssoEnv) browse(t *testing.T) (final *url.URL, jar http.CookieJar) {
	t.Helper()
	jar, _ = cookiejar.New(nil)
	c := &http.Client{Jar: jar}
	resp, err := c.Get(s.url + "/api/oidc/login")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.Request.URL, jar
}

func (s *ssoEnv) me(jar http.CookieJar) (int, map[string]any) {
	c := &http.Client{Jar: jar}
	resp, err := c.Get(s.url + "/api/me")
	if err != nil {
		return 0, nil
	}
	defer resp.Body.Close()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestSSOHappyPathCreatesUserWithRoleFromGroup(t *testing.T) {
	s := newSSO(t, "none")
	final, jar := s.browse(t)
	if final.Path != "/" || final.Query().Get("sso_error") != "" {
		t.Fatalf("flow ended at %s", final)
	}
	code, me := s.me(jar)
	if code != 200 || me["user"] != "alice" || me["role"] != "admin" {
		t.Fatalf("me: %d %v", code, me)
	}
	u, _ := s.st.User("alice")
	if u.Hash != oidcauth.PasswordMarker {
		t.Fatal("SSO user must not get a usable password")
	}
	// the user cannot log in with any password
	b, _ := json.Marshal(map[string]string{"user": "alice", "password": "anything-at-all-123"})
	req, _ := http.NewRequest("POST", s.url+"/api/login", strings.NewReader(string(b)))
	req.Header.Set(csrfHeader, "1")
	resp, _ := http.DefaultClient.Do(req)
	if resp.StatusCode != 401 {
		t.Fatalf("password login for SSO user: %d", resp.StatusCode)
	}
}

func TestSSORoleIsResyncedAtEveryLogin(t *testing.T) {
	s := newSSO(t, "none")
	s.browse(t)
	s.idp.claims["groups"] = []string{"pve-view"}
	_, jar := s.browse(t)
	if _, me := s.me(jar); me["role"] != "viewer" {
		t.Fatalf("role not synced down: %v", me)
	}
}

func TestSSODeniesWithoutMappedGroupAndDefaultNone(t *testing.T) {
	s := newSSO(t, "none")
	s.idp.claims["groups"] = []string{"somebody-else"}
	final, jar := s.browse(t)
	if final.Query().Get("sso_error") != "denied" {
		t.Fatalf("ended at %s", final)
	}
	if code, _ := s.me(jar); code != 401 {
		t.Fatal("denied user got a session")
	}
	if _, err := s.st.User("alice"); err == nil {
		t.Fatal("denied user was created")
	}
	s2 := newSSO(t, "viewer") // with a default role the same user gets in as viewer
	s2.idp.claims["groups"] = []string{"somebody-else"}
	if _, jar2 := s2.browse(t); func() bool { _, me := s2.me(jar2); return me["role"] != "viewer" }() {
		t.Fatal("default_role viewer not applied")
	}
}

func TestSSORejectsForgedTokens(t *testing.T) {
	cases := map[string]func(f *fakeIdP){
		"wrong nonce":         func(f *fakeIdP) { f.badNonce = true },
		"wrong audience":      func(f *fakeIdP) { f.audience = "someone-elses-client" },
		"expired":             func(f *fakeIdP) { f.expired = true },
		"signed by wrong key": func(f *fakeIdP) { f.wrongSign = true },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			s := newSSO(t, "none")
			mutate(s.idp)
			final, jar := s.browse(t)
			if final.Query().Get("sso_error") != "failed" {
				t.Fatalf("%s accepted, ended at %s", name, final)
			}
			if code, _ := s.me(jar); code != 401 {
				t.Fatalf("%s produced a session", name)
			}
			if _, err := s.st.User("alice"); err == nil {
				t.Fatalf("%s created a user", name)
			}
		})
	}
}

func TestSSORefusesToTakeOverLocalAccount(t *testing.T) {
	s := newSSO(t, "none")
	s.idp.claims["preferred_username"] = "root" // a local admin created by newEnv
	final, jar := s.browse(t)
	if final.Query().Get("sso_error") != "local-user" {
		t.Fatalf("ended at %s", final)
	}
	if code, _ := s.me(jar); code != 401 {
		t.Fatal("IdP identity got a session for a local account")
	}
	if u, _ := s.st.User("root"); u.Hash == oidcauth.PasswordMarker || u.Role != "admin" {
		t.Fatal("local account was modified")
	}
}

func TestSSOStateAndCookieChecks(t *testing.T) {
	s := newSSO(t, "none")
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	// callback without ever starting the flow
	resp, _ := c.Get(s.url + "/api/oidc/callback?code=x&state=y")
	if loc := resp.Header.Get("Location"); !strings.Contains(loc, "sso_error=failed") {
		t.Fatalf("callback without state cookie: %q", loc)
	}
	// start the flow, then come back with a different state
	resp, _ = c.Get(s.url + "/api/oidc/login")
	resp.Body.Close()
	resp, _ = c.Get(s.url + "/api/oidc/callback?code=x&state=attacker-state")
	if loc := resp.Header.Get("Location"); !strings.Contains(loc, "sso_error=failed") {
		t.Fatalf("state mismatch accepted: %q", loc)
	}
	// the state cookie is single-use: replaying the correct callback afterwards must fail
	u, _ := url.Parse(s.url)
	if len(jar.Cookies(u)) != 0 {
		for _, ck := range jar.Cookies(u) {
			if ck.Name == ssoCookie {
				t.Fatal("state cookie survived the callback")
			}
		}
	}
	// a tampered cookie is rejected
	req, _ := http.NewRequest("GET", s.url+"/api/oidc/callback?code=x&state=y", nil)
	req.AddCookie(&http.Cookie{Name: ssoCookie, Value: "a.b.c.1.sig"})
	resp, _ = c.Do(req)
	if loc := resp.Header.Get("Location"); !strings.Contains(loc, "sso_error=failed") {
		t.Fatalf("forged cookie accepted: %q", loc)
	}
	// IdP-side denial maps to "denied"
	s.idp.denyAuth = true
	if final, _ := s.browse(t); final.Query().Get("sso_error") != "denied" {
		t.Fatalf("IdP denial ended at %s", final)
	}
}

// A valid code from the IdP must still be refused when the state does not match the browser's cookie
// (this is the login-CSRF case: an attacker feeds a victim a callback URL for the attacker's own login).
func TestSSOValidCodeWithForeignStateIsRefused(t *testing.T) {
	s := newSSO(t, "none")
	jar, _ := cookiejar.New(nil)
	noFollow := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, _ := noFollow.Get(s.url + "/api/oidc/login")
	idpURL := resp.Header.Get("Location")
	resp, _ = noFollow.Get(idpURL) // the IdP approves and points back to the callback
	cb, _ := url.Parse(resp.Header.Get("Location"))
	q := cb.Query()
	if q.Get("code") == "" {
		t.Fatal("test setup: no code")
	}
	q.Set("state", "state-from-the-attackers-flow")
	cb.RawQuery = q.Encode()
	resp, _ = noFollow.Get(cb.String())
	if loc := resp.Header.Get("Location"); !strings.Contains(loc, "sso_error=failed") {
		t.Fatalf("callback with a valid code but foreign state was accepted: %q", loc)
	}
	if _, err := s.st.User("alice"); err == nil {
		t.Fatal("a user was created from a state-mismatched callback")
	}
}

// An attacker who can plant a cookie must not be able to log a victim into the attacker's account:
// the state cookie is HMAC-signed, so a copy with a valid payload but a wrong signature is refused.
func TestSSOUnsignedStateCookieIsRefused(t *testing.T) {
	s := newSSO(t, "none")
	ajar, _ := cookiejar.New(nil)
	noFollow := func(j http.CookieJar) *http.Client {
		return &http.Client{Jar: j, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	attacker := noFollow(ajar)
	resp, _ := attacker.Get(s.url + "/api/oidc/login")
	resp, _ = attacker.Get(resp.Header.Get("Location"))
	callback := resp.Header.Get("Location")            // carries the attacker's code and state
	base, _ := url.Parse(s.url + "/api/oidc/callback") // the cookie is scoped to /api/oidc
	var payload string
	for _, c := range ajar.Cookies(base) {
		if c.Name == ssoCookie {
			payload = c.Value[:strings.LastIndexByte(c.Value, '.')]
		}
	}
	if payload == "" {
		t.Fatal("test setup: no state cookie")
	}
	vjar, _ := cookiejar.New(nil)
	vjar.SetCookies(base, []*http.Cookie{{Name: ssoCookie, Value: payload + ".AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", Path: "/api/oidc"}})
	resp, _ = noFollow(vjar).Get(callback)
	if loc := resp.Header.Get("Location"); !strings.Contains(loc, "sso_error=failed") {
		t.Fatalf("callback with an unsigned state cookie was accepted: %q", loc)
	}
	if _, err := s.st.User("alice"); err == nil {
		t.Fatal("a user was created from a forged state cookie")
	}
}

func TestSSODisabledAndOptions(t *testing.T) {
	e := newEnv(t)
	for _, p := range []string{"/api/oidc/login", "/api/oidc/callback"} {
		resp, _ := http.Get(e.srv.URL + p)
		if resp.StatusCode != 404 {
			t.Fatalf("%s without OIDC: %d", p, resp.StatusCode)
		}
	}
	resp, _ := http.Get(e.srv.URL + "/api/auth-options")
	var o map[string]map[string]any
	var raw map[string]any
	json.NewDecoder(resp.Body).Decode(&raw)
	b, _ := json.Marshal(raw)
	json.Unmarshal(b, &o)
	if o["oidc"]["enabled"] != false {
		t.Fatalf("options: %v", raw)
	}
	s := newSSO(t, "none")
	resp, _ = http.Get(s.url + "/api/auth-options")
	json.NewDecoder(resp.Body).Decode(&raw)
	if raw["oidc"].(map[string]any)["label"] != "Login with Test" {
		t.Fatalf("options: %v", raw)
	}
}

func TestSSOUsersCannotBeEditedLocally(t *testing.T) {
	s := newSSO(t, "none")
	s.browse(t)
	root := s.env.login("root")
	if code, _ := root.do("PUT", "/api/users/alice", map[string]string{"password": "a-new-local-password-1"}); code != 409 {
		t.Fatalf("password set on SSO user: %d", code)
	}
	if code, _ := root.do("PUT", "/api/users/alice", map[string]string{"role": "viewer"}); code != 409 {
		t.Fatalf("role set on SSO user: %d", code)
	}
}

type testWriter struct{ t *testing.T }

func (w testWriter) Write(b []byte) (int, error) {
	w.t.Log(strings.TrimSpace(string(b)))
	return len(b), nil
}

// Starting the flow on another host than the redirect URL would lose the host-bound state cookie;
// the user must get an explicit hint instead of a cryptic failure.
func TestSSOStartedOnWrongHostGivesHint(t *testing.T) {
	s := newSSO(t, "none")
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, _ := http.NewRequest("GET", s.url+"/api/oidc/login", nil)
	req.Host = "some-other-name.example"
	resp, err := noFollow.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	loc := resp.Header.Get("Location")
	if !strings.Contains(loc, "sso_error=host") || !strings.Contains(loc, "sso_host=") {
		t.Fatalf("expected a host hint, got %q", loc)
	}
	for _, c := range resp.Cookies() {
		if c.Name == ssoCookie {
			t.Fatal("no state cookie must be issued for the wrong host")
		}
	}
}
