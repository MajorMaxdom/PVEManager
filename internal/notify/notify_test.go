package notify

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	if Validate("webhook", map[string]string{"url": "https://x/y"}) != nil {
		t.Error("valid webhook rejected")
	}
	for _, c := range []struct {
		k string
		m map[string]string
	}{
		{"webhook", map[string]string{}}, {"webhook", map[string]string{"url": "file:///etc/passwd"}},
		{"webhook", map[string]string{"url": "ftp://x"}}, {"telegram", map[string]string{"token": "t"}},
		{"smtp", map[string]string{"host": "h", "from": "a\r\nBcc: x", "to": "b"}}, {"nope", nil},
	} {
		if Validate(c.k, c.m) == nil {
			t.Errorf("%s %v accepted", c.k, c.m)
		}
	}
}

func TestWebhookAndNtfy(t *testing.T) {
	var gotBody, gotAuth, gotTitle string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody, gotAuth, gotTitle = string(b), r.Header.Get("Authorization"), r.Header.Get("Title")
	}))
	defer srv.Close()
	if err := Send(context.Background(), "webhook", map[string]string{"url": srv.URL, "token": "tok"}, "T", "B"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotBody, `"title":"T"`) || gotAuth != "Bearer tok" {
		t.Fatalf("webhook: %q %q", gotBody, gotAuth)
	}
	if err := Send(context.Background(), "ntfy", map[string]string{"url": srv.URL}, "Hello", "body text"); err != nil {
		t.Fatal(err)
	}
	if gotBody != "body text" || gotTitle != "Hello" {
		t.Fatalf("ntfy: %q %q", gotBody, gotTitle)
	}
}

func TestErrorDoesNotLeakURL(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL + "/secret-path-token"
	srv.Close() // connection refused
	err := Send(context.Background(), "webhook", map[string]string{"url": url}, "T", "B")
	if err == nil || strings.Contains(err.Error(), "secret-path-token") {
		t.Fatalf("error leaks the secret URL: %v", err)
	}
	if got := Summary("webhook", map[string]string{"url": "https://hooks.example/abc/SECRET"}); strings.Contains(got, "SECRET") {
		t.Fatalf("summary leaks secret: %q", got)
	}
}
