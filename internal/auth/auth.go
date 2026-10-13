// Package auth: argon2id password hashing, session ids, roles, login throttling.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"
)

const (
	memKiB  = 64 * 1024
	iters   = 3
	threads = 2
	keyLen  = 32
)

func HashPassword(pw string) (string, error) {
	if len(pw) < 10 {
		return "", errors.New("password must be at least 10 characters")
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	k := argon2.IDKey([]byte(pw), salt, iters, memKiB, threads, keyLen)
	return fmt.Sprintf("argon2id$%d$%d$%d$%s$%s", memKiB, iters, threads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(k)), nil
}

func CheckPassword(pw, enc string) bool {
	p := strings.Split(enc, "$")
	if len(p) != 6 || p[0] != "argon2id" {
		return false
	}
	var m, t uint32
	var th uint8
	if _, err := fmt.Sscan(p[1], &m); err != nil {
		return false
	}
	if _, err := fmt.Sscan(p[2], &t); err != nil {
		return false
	}
	if _, err := fmt.Sscan(p[3], &th); err != nil {
		return false
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(p[4])
	want, err2 := base64.RawStdEncoding.DecodeString(p[5])
	if err1 != nil || err2 != nil || len(want) == 0 {
		return false
	}
	got := argon2.IDKey([]byte(pw), salt, t, m, th, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// NewSessionToken returns the secret cookie value and the id stored server-side (its hash).
func NewSessionToken() (token, id string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return
	}
	token = base64.RawURLEncoding.EncodeToString(b)
	return token, SessionID(token), nil
}

func SessionID(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// Role ordering: viewer < operator < admin.
var rank = map[string]int{"viewer": 1, "operator": 2, "admin": 3}

func Allows(have, need string) bool { return rank[have] >= rank[need] && rank[need] > 0 }

// Limiter throttles failed logins per key (e.g. client IP).
type Limiter struct {
	mu    sync.Mutex
	fails map[string][]time.Time
	Max   int
	Win   time.Duration
}

func NewLimiter(max int, win time.Duration) *Limiter {
	return &Limiter{fails: map[string][]time.Time{}, Max: max, Win: win}
}

func (l *Limiter) prune(k string, now time.Time) []time.Time {
	var keep []time.Time
	for _, t := range l.fails[k] {
		if now.Sub(t) < l.Win {
			keep = append(keep, t)
		}
	}
	l.fails[k] = keep
	return keep
}

func (l *Limiter) Blocked(k string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.prune(k, time.Now())) >= l.Max
}

func (l *Limiter) Fail(k string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	l.fails[k] = append(l.prune(k, now), now)
}

func (l *Limiter) Reset(k string) {
	l.mu.Lock()
	delete(l.fails, k)
	l.mu.Unlock()
}
