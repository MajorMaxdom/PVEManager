// Package notify delivers event messages through webhook, ntfy, Telegram or SMTP channels.
package notify

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"strings"
	"time"
)

// Events a channel can subscribe to.
var Events = []string{"server.offline", "server.online", "node.disk", "backup.failed", "updates.available", "job.failed"}

func ValidEvent(e string) bool {
	for _, x := range Events {
		if x == e {
			return true
		}
	}
	return false
}

// Required config keys per channel kind.
var required = map[string][]string{
	"webhook":  {"url"},
	"ntfy":     {"url"},
	"telegram": {"token", "chat"},
	"smtp":     {"host", "from", "to"},
}

// SecretKeys must never be shown again after creation.
var SecretKeys = map[string]bool{"url": true, "token": true, "password": true}

var Client = &http.Client{Timeout: 15 * time.Second}

func Validate(kind string, cfg map[string]string) error {
	req, ok := required[kind]
	if !ok {
		return fmt.Errorf("unknown channel kind %q", kind)
	}
	for _, k := range req {
		if strings.TrimSpace(cfg[k]) == "" {
			return fmt.Errorf("%s is required", k)
		}
	}
	if u := cfg["url"]; u != "" {
		pu, err := url.Parse(u)
		if err != nil || (pu.Scheme != "http" && pu.Scheme != "https") || pu.Host == "" {
			return errors.New("url must be http(s)://…")
		}
	}
	if kind == "smtp" {
		if strings.ContainsAny(cfg["from"]+cfg["to"], "\r\n") {
			return errors.New("invalid address")
		}
	}
	return nil
}

// Summary is a safe one-line description of a channel (no secrets).
func Summary(kind string, cfg map[string]string) string {
	switch kind {
	case "webhook", "ntfy":
		if u, err := url.Parse(cfg["url"]); err == nil {
			return u.Host
		}
	case "telegram":
		return "chat " + cfg["chat"]
	case "smtp":
		return cfg["host"] + " → " + cfg["to"]
	}
	return ""
}

// Send delivers one message.
func Send(ctx context.Context, kind string, cfg map[string]string, title, body string) error {
	switch kind {
	case "webhook":
		b, _ := json.Marshal(map[string]string{"title": title, "message": body, "text": title + "\n" + body})
		return post(ctx, cfg["url"], "application/json", b, cfg["token"], nil)
	case "ntfy":
		return post(ctx, cfg["url"], "text/plain", []byte(body), cfg["token"], map[string]string{"Title": title, "Tags": "computer"})
	case "telegram":
		b, _ := json.Marshal(map[string]string{"chat_id": cfg["chat"], "text": title + "\n" + body})
		return post(ctx, "https://api.telegram.org/bot"+cfg["token"]+"/sendMessage", "application/json", b, "", nil)
	case "smtp":
		return sendMail(ctx, cfg, title, body)
	}
	return fmt.Errorf("unknown channel kind %q", kind)
}

func post(ctx context.Context, u, ctype string, body []byte, bearer string, hdr map[string]string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", ctype)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := Client.Do(req)
	if err != nil {
		return errors.New(strings.ReplaceAll(err.Error(), u, "<url>")) // never leak secret URLs into logs/UI
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("endpoint answered %s", resp.Status)
	}
	return nil
}

func sendMail(ctx context.Context, cfg map[string]string, title, body string) error {
	port := cfg["port"]
	if port == "" {
		port = "587"
	}
	addr := net.JoinHostPort(cfg["host"], port)
	d := net.Dialer{Timeout: 15 * time.Second}
	var conn net.Conn
	var err error
	if cfg["tls"] == "tls" {
		conn, err = tls.DialWithDialer(&d, "tcp", addr, &tls.Config{ServerName: cfg["host"], MinVersion: tls.VersionTLS12})
	} else {
		conn, err = d.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return err
	}
	c, err := smtp.NewClient(conn, cfg["host"])
	if err != nil {
		return err
	}
	defer c.Close()
	if cfg["tls"] != "tls" && cfg["tls"] != "none" {
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err := c.StartTLS(&tls.Config{ServerName: cfg["host"], MinVersion: tls.VersionTLS12}); err != nil {
				return err
			}
		}
	}
	if cfg["user"] != "" {
		if err := c.Auth(smtp.PlainAuth("", cfg["user"], cfg["password"], cfg["host"])); err != nil {
			return err
		}
	}
	if err := c.Mail(cfg["from"]); err != nil {
		return err
	}
	for _, to := range strings.Split(cfg["to"], ",") {
		if err := c.Rcpt(strings.TrimSpace(to)); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s\r\n",
		cfg["from"], cfg["to"], strings.NewReplacer("\r", " ", "\n", " ").Replace("[pve-manager] "+title), body)
	if _, err := w.Write([]byte(msg)); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}
