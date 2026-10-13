package cron

import (
	"testing"
	"time"
)

func at(s string) time.Time { t, _ := time.Parse("2006-01-02 15:04", s); return t }

func TestMatches(t *testing.T) {
	cases := []struct {
		expr, when string
		want       bool
	}{
		{"0 3 * * *", "2026-10-13 03:00", true},
		{"0 3 * * *", "2026-10-13 03:01", false},
		{"*/15 * * * *", "2026-10-13 10:45", true},
		{"*/15 * * * *", "2026-10-13 10:46", false},
		{"30 22 * * 1-5", "2026-10-13 22:30", true},  // Tuesday
		{"30 22 * * 1-5", "2026-10-18 22:30", false}, // Sunday
		{"0 0 * * 7", "2026-10-18 00:00", true},      // 7 = Sunday
		{"0 0 1 * *", "2026-11-01 00:00", true},
		{"0 0 1,15 * *", "2026-10-15 00:00", true},
		{"0 8 1 * 1", "2026-10-13 08:00", true}, // dom OR dow: Tuesday is not Monday, 13 is not 1 -> false below
		{"5-10/2 * * * *", "2026-10-13 10:07", true},
		{"5-10/2 * * * *", "2026-10-13 10:08", false},
	}
	cases[9].want = false
	for _, c := range cases {
		s, err := Parse(c.expr)
		if err != nil {
			t.Fatalf("%q: %v", c.expr, err)
		}
		if got := s.Matches(at(c.when)); got != c.want {
			t.Errorf("%q at %s: got %v want %v", c.expr, c.when, got, c.want)
		}
	}
}

func TestParseErrors(t *testing.T) {
	for _, e := range []string{"", "* * * *", "60 * * * *", "* 24 * * *", "* * 0 * *", "* * * 13 *", "a * * * *", "*/0 * * * *", "5-2 * * * *", "* * * * * *"} {
		if _, err := Parse(e); err == nil {
			t.Errorf("%q accepted", e)
		}
	}
}
