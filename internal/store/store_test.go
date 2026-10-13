package store

import (
	"bytes"
	"database/sql"
	"testing"
	"time"

	"pvemanager/internal/crypto"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	box, _ := crypto.New(bytes.Repeat([]byte{7}, 32))
	s, err := Open(t.TempDir(), box)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestServerTokenEncryptedAtRest(t *testing.T) {
	s := newStore(t)
	if err := s.PutServer(Server{Name: "a", URL: "https://x:8006", Token: "u@pve!t=SECRET"}); err != nil {
		t.Fatal(err)
	}
	var raw []byte
	if err := s.db.QueryRow(`SELECT token FROM servers WHERE name='a'`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("SECRET")) {
		t.Fatal("token stored in plaintext")
	}
	got, _ := s.Servers()
	if len(got) != 1 || got[0].Token != "u@pve!t=SECRET" {
		t.Fatalf("roundtrip failed: %+v", got)
	}
}

func TestSessionExpiryAndUser(t *testing.T) {
	s := newStore(t)
	if err := s.PutUser(User{Name: "j", Hash: "h", Role: "admin"}); err != nil {
		t.Fatal(err)
	}
	_ = s.PutSession("live", "j", time.Now().Add(time.Hour))
	_ = s.PutSession("dead", "j", time.Now().Add(-time.Hour))
	if u, err := s.Session("live"); err != nil || u.Role != "admin" {
		t.Fatalf("live: %v %v", u, err)
	}
	if _, err := s.Session("dead"); err != ErrNotFound {
		t.Fatalf("expired session accepted: %v", err)
	}
	var _ = sql.ErrNoRows
}

func TestUserDeletionCascadesSessionsAndKeys(t *testing.T) {
	s := newStore(t)
	_ = s.PutUser(User{Name: "a", Hash: "h", Role: "admin"})
	_ = s.PutUser(User{Name: "b", Hash: "h", Role: "operator"})
	_ = s.PutSession("sa", "a", time.Now().Add(time.Hour))
	_ = s.PutSession("sb1", "b", time.Now().Add(time.Hour))
	_ = s.PutSession("sb2", "b", time.Now().Add(time.Hour))
	s.DeleteUserSessions("b", "sb1")
	if _, err := s.Session("sb2"); err != ErrNotFound {
		t.Fatal("other session of the user survived")
	}
	if _, err := s.Session("sb1"); err != nil {
		t.Fatal("kept session was removed")
	}
	if n, _ := s.CountAdmins(); n != 1 {
		t.Fatalf("admins %d", n)
	}
	_ = s.DeleteUser("b")
	if _, err := s.Session("sb1"); err != ErrNotFound {
		t.Fatal("session outlived its user")
	}
	id, err := s.PutAPIKey("ci", "hash1", "viewer", "pvm_abcd", "a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.PutAPIKey("ci", "hash2", "viewer", "pvm_x", "a"); err == nil {
		t.Fatal("duplicate key name accepted")
	}
	k, err := s.APIKeyByHash("hash1")
	if err != nil || k.Role != "viewer" || k.ID != id {
		t.Fatalf("lookup %v %v", k, err)
	}
	_ = s.DeleteAPIKey(id)
	if _, err := s.APIKeyByHash("hash1"); err != ErrNotFound {
		t.Fatal("deleted key still valid")
	}
}
