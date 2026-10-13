// Package pve is a small client for the Proxmox VE REST API using API tokens.
package pve

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client talks to one PVE host. TLS is verified by pinning the leaf certificate's
// SHA-256 fingerprint (PVE hosts normally use a self-signed certificate).
type Client struct {
	BaseURL     string // https://host:8006
	Token       string // user@realm!id=secret
	Fingerprint string // aa:bb:... (sha256); empty = system roots
	HTTP        *http.Client
}

func New(baseURL, token, fingerprint string) *Client {
	c := &Client{BaseURL: strings.TrimRight(baseURL, "/"), Token: token, Fingerprint: normFP(fingerprint)}
	c.HTTP = &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{TLSClientConfig: c.TLSConfig()}}
	return c
}

func normFP(s string) string {
	return strings.ToLower(strings.NewReplacer(":", "", " ", "").Replace(s))
}

// TLSConfig returns a config that pins Fingerprint (or uses system roots when empty).
func (c *Client) TLSConfig() *tls.Config {
	if c.Fingerprint == "" {
		return &tls.Config{MinVersion: tls.VersionTLS12}
	}
	want := c.Fingerprint
	return &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: true, // verification is replaced by the pin below
		VerifyPeerCertificate: func(raw [][]byte, _ [][]*x509Cert) error {
			if len(raw) == 0 {
				return errors.New("no peer certificate")
			}
			sum := sha256.Sum256(raw[0])
			if hex.EncodeToString(sum[:]) != want {
				return fmt.Errorf("certificate fingerprint mismatch (got %s)", hex.EncodeToString(sum[:]))
			}
			return nil
		},
	}
}

// APIError is returned for non-2xx answers.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string { return fmt.Sprintf("pve: %d %s", e.Status, e.Message) }

func (c *Client) do(ctx context.Context, method, path string, form url.Values, out any) error {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+"/api2/json"+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "PVEAPIToken="+c.Token)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		var e struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(raw, &e)
		msg := strings.TrimSpace(e.Message)
		if msg == "" {
			msg = resp.Status
		}
		return &APIError{Status: resp.StatusCode, Message: msg}
	}
	if out == nil {
		return nil
	}
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return err
	}
	return json.Unmarshal(env.Data, out)
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	return c.do(ctx, http.MethodGet, path, nil, out)
}
func (c *Client) post(ctx context.Context, path string, form url.Values, out any) error {
	if form == nil {
		form = url.Values{}
	}
	return c.do(ctx, http.MethodPost, path, form, out)
}

// Version returns the PVE version string, e.g. "9.2.21". Doubles as a connectivity/auth test.
func (c *Client) Version(ctx context.Context) (string, error) {
	var v struct {
		Version string `json:"version"`
	}
	return v.Version, c.get(ctx, "/version", &v)
}
