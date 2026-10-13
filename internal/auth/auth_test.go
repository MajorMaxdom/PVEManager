package auth

import (
	"testing"
	"time"
)

func TestPassword(t *testing.T) {
	h, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword("correct horse battery", h) || CheckPassword("wrong password!!", h) {
		t.Fatal("password check wrong")
	}
	if _, err := HashPassword("short"); err == nil {
		t.Fatal("short password accepted")
	}
	if CheckPassword("x", "garbage") {
		t.Fatal("garbage hash accepted")
	}
}

func TestRolesAndLimiter(t *testing.T) {
	if !Allows("admin", "operator") || Allows("viewer", "operator") || Allows("admin", "bogus") {
		t.Fatal("role ranking wrong")
	}
	l := NewLimiter(3, time.Minute)
	for i := 0; i < 3; i++ {
		l.Fail("ip")
	}
	if !l.Blocked("ip") || l.Blocked("other") {
		t.Fatal("limiter wrong")
	}
	l.Reset("ip")
	if l.Blocked("ip") {
		t.Fatal("reset failed")
	}
}
