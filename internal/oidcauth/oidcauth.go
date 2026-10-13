// Package oidcauth implements optional single sign-on with OpenID Connect (authorization code + PKCE).
// Token verification is delegated to go-oidc; this package only maps claims to users and roles.
package oidcauth

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"pvemanager/internal/config"
)

// PasswordMarker is stored as the password hash of SSO users: it never verifies, so they cannot use password login.
const PasswordMarker = "!oidc"

var reName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._@+-]{0,63}$`)

type Provider struct {
	cfg      config.OIDC
	secret   string
	redirect string

	mu       sync.Mutex
	oauth    *oauth2.Config
	verifier *oidc.IDTokenVerifier
	lastTry  time.Time
	lastErr  error
}

func New(cfg config.OIDC, clientSecret, redirectURL string) *Provider {
	return &Provider{cfg: cfg, secret: clientSecret, redirect: redirectURL}
}

// RedirectHost is the host[:port] the browser must use: the state cookie only travels to that host.
func (p *Provider) RedirectHost() string {
	if u, err := url.Parse(p.redirect); err == nil {
		return u.Host
	}
	return ""
}

func (p *Provider) Label() string {
	if p.cfg.ButtonLabel != "" {
		return p.cfg.ButtonLabel
	}
	return "SSO"
}

// ready performs discovery lazily so an unreachable IdP never blocks startup.
func (p *Provider) ready(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.verifier != nil {
		return nil
	}
	if p.lastErr != nil && time.Since(p.lastTry) < 10*time.Second {
		return p.lastErr
	}
	p.lastTry = time.Now()
	dctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	prov, err := oidc.NewProvider(dctx, p.cfg.Issuer)
	if err != nil {
		p.lastErr = fmt.Errorf("OIDC discovery failed: %w", err)
		return p.lastErr
	}
	scopes := p.cfg.Scopes
	if len(scopes) == 0 {
		scopes = []string{oidc.ScopeOpenID, "profile", "email", "groups"}
	}
	p.oauth = &oauth2.Config{ClientID: p.cfg.ClientID, ClientSecret: p.secret, Endpoint: prov.Endpoint(), RedirectURL: p.redirect, Scopes: scopes}
	p.verifier = prov.Verifier(&oidc.Config{ClientID: p.cfg.ClientID}) // checks signature, issuer, audience, expiry
	p.lastErr = nil
	return nil
}

// AuthURL returns the IdP URL to send the browser to.
func (p *Provider) AuthURL(ctx context.Context, state, nonce, verifier string) (string, error) {
	if err := p.ready(ctx); err != nil {
		return "", err
	}
	return p.oauth.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), nil
}

type Identity struct {
	Username string
	Groups   []string
}

// Exchange trades the code for tokens and returns the verified identity.
func (p *Provider) Exchange(ctx context.Context, code, verifier, nonce string) (Identity, error) {
	var id Identity
	if err := p.ready(ctx); err != nil {
		return id, err
	}
	tok, err := p.oauth.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return id, fmt.Errorf("code exchange failed: %w", err)
	}
	raw, _ := tok.Extra("id_token").(string)
	if raw == "" {
		return id, errors.New("the IdP returned no id_token")
	}
	idt, err := p.verifier.Verify(ctx, raw)
	if err != nil {
		return id, fmt.Errorf("invalid id_token: %w", err)
	}
	if idt.Nonce != nonce {
		return id, errors.New("nonce mismatch")
	}
	var claims map[string]any
	if err := idt.Claims(&claims); err != nil {
		return id, err
	}
	uc := p.cfg.UsernameClaim
	if uc == "" {
		uc = "preferred_username"
	}
	name, _ := claims[uc].(string)
	if !reName.MatchString(name) {
		return id, fmt.Errorf("claim %q is missing or not a valid user name", uc)
	}
	gc := p.cfg.GroupsClaim
	if gc == "" {
		gc = "groups"
	}
	switch g := claims[gc].(type) {
	case []any:
		for _, x := range g {
			if s, ok := x.(string); ok {
				id.Groups = append(id.Groups, s)
			}
		}
	case string:
		id.Groups = strings.Fields(strings.ReplaceAll(g, ",", " "))
	}
	id.Username = name
	return id, nil
}

// RoleFor maps groups to the highest matching role; ok is false when the user must be denied.
func (p *Provider) RoleFor(groups []string) (string, bool) {
	has := func(g string) bool {
		if g == "" {
			return false
		}
		for _, x := range groups {
			if x == g {
				return true
			}
		}
		return false
	}
	switch {
	case has(p.cfg.AdminGroup):
		return "admin", true
	case has(p.cfg.OperatorGroup):
		return "operator", true
	case has(p.cfg.ViewerGroup):
		return "viewer", true
	}
	switch p.cfg.DefaultRole {
	case "viewer", "operator", "admin":
		return p.cfg.DefaultRole, true
	}
	return "", false
}
