package crypto

import (
	"bytes"
	"path/filepath"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "k")
	k, err := LoadOrCreateKey(p)
	if err != nil {
		t.Fatal(err)
	}
	k2, _ := LoadOrCreateKey(p)
	if !bytes.Equal(k, k2) {
		t.Fatal("key not persisted")
	}
	b, _ := New(k)
	s, _ := b.Seal([]byte("secret"))
	if bytes.Contains(s, []byte("secret")) {
		t.Fatal("plaintext leaked")
	}
	o, err := b.Open(s)
	if err != nil || string(o) != "secret" {
		t.Fatalf("got %q %v", o, err)
	}
	s[len(s)-1] ^= 1
	if _, err := b.Open(s); err == nil {
		t.Fatal("tamper not detected")
	}
}
