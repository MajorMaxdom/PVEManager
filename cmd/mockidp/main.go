// Command mockidp is a throw-away OpenID provider for end-to-end tests. It approves every login
// automatically as the identity set with -user/-groups, which a test can change via /__set.
// The first stdout line is its issuer URL.
package main

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:0", "listen address")
	user := flag.String("user", "sso-alice", "initial user name")
	groups := flag.String("groups", "pve-admins", "initial comma separated groups")
	clientID := flag.String("client-id", "pvm-e2e", "expected audience")
	flag.Parse()

	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	issuer := "http://" + ln.Addr().String()
	var mu sync.Mutex
	codes := map[string]string{} // code -> nonce
	cur := struct{ user, groups string }{*user, *groups}
	mux := http.NewServeMux()
	jw := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(v)
	}
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		jw(w, map[string]any{"issuer": issuer, "authorization_endpoint": issuer + "/authorize", "token_endpoint": issuer + "/token", "jwks_uri": issuer + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"}, "response_types_supported": []string{"code"}, "subject_types_supported": []string{"public"}})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		jw(w, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"}}})
	})
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		b := make([]byte, 12)
		rand.Read(b)
		code := base64.RawURLEncoding.EncodeToString(b)
		mu.Lock()
		codes[code] = q.Get("nonce")
		mu.Unlock()
		back, _ := url.Parse(q.Get("redirect_uri"))
		rq := back.Query()
		rq.Set("code", code)
		rq.Set("state", q.Get("state"))
		back.RawQuery = rq.Encode()
		http.Redirect(w, r, back.String(), http.StatusFound)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		mu.Lock()
		nonce, ok := codes[r.FormValue("code")]
		delete(codes, r.FormValue("code"))
		u, g := cur.user, cur.groups
		mu.Unlock()
		if !ok || r.FormValue("code_verifier") == "" {
			http.Error(w, `{"error":"invalid_grant"}`, 400)
			return
		}
		sg, _ := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "k1"))
		std := jwt.Claims{Issuer: issuer, Subject: "sub-" + u, Audience: jwt.Audience{*clientID}, Expiry: jwt.NewNumericDate(time.Now().Add(time.Hour)), IssuedAt: jwt.NewNumericDate(time.Now())}
		raw, _ := jwt.Signed(sg).Claims(std).Claims(map[string]any{"nonce": nonce, "preferred_username": u, "groups": strings.FieldsFunc(g, func(r rune) bool { return r == ',' })}).Serialize()
		jw(w, map[string]any{"access_token": "at", "token_type": "Bearer", "id_token": raw})
	})
	mux.HandleFunc("/__set", func(w http.ResponseWriter, r *http.Request) { // /__set?user=x&groups=a,b
		mu.Lock()
		cur.user, cur.groups = r.URL.Query().Get("user"), r.URL.Query().Get("groups")
		mu.Unlock()
		fmt.Fprint(w, "ok")
	})
	fmt.Println(issuer)
	os.Stdout.Sync()
	log.Fatal(http.Serve(ln, mux))
}
