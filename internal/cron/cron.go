// Package cron parses 5-field cron expressions (minute hour day-of-month month day-of-week).
package cron

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type Spec struct {
	min, hour, dom, mon, dow uint64
	domStar, dowStar         bool
}

func field(s string, lo, hi int) (uint64, bool, error) {
	var bits uint64
	star := s == "*"
	for _, part := range strings.Split(s, ",") {
		step := 1
		if i := strings.Index(part, "/"); i >= 0 {
			n, err := strconv.Atoi(part[i+1:])
			if err != nil || n < 1 {
				return 0, false, fmt.Errorf("bad step in %q", s)
			}
			step, part = n, part[:i]
		}
		a, b := lo, hi
		if part != "*" {
			r := strings.SplitN(part, "-", 2)
			x, err := strconv.Atoi(r[0])
			if err != nil {
				return 0, false, fmt.Errorf("bad value %q", part)
			}
			a, b = x, x
			if len(r) == 2 {
				if b, err = strconv.Atoi(r[1]); err != nil {
					return 0, false, fmt.Errorf("bad range %q", part)
				}
			} else if step > 1 {
				b = hi
			}
		}
		if a < lo || b > hi || a > b {
			return 0, false, fmt.Errorf("%q out of range %d-%d", part, lo, hi)
		}
		for v := a; v <= b; v += step {
			bits |= 1 << uint(v)
		}
	}
	return bits, star, nil
}

func Parse(expr string) (*Spec, error) {
	f := strings.Fields(expr)
	if len(f) != 5 {
		return nil, fmt.Errorf("need 5 fields (min hour dom month dow), got %d", len(f))
	}
	var s Spec
	var err error
	if s.min, _, err = field(f[0], 0, 59); err != nil {
		return nil, err
	}
	if s.hour, _, err = field(f[1], 0, 23); err != nil {
		return nil, err
	}
	if s.dom, s.domStar, err = field(f[2], 1, 31); err != nil {
		return nil, err
	}
	if s.mon, _, err = field(f[3], 1, 12); err != nil {
		return nil, err
	}
	if s.dow, s.dowStar, err = field(f[4], 0, 7); err != nil {
		return nil, err
	}
	if s.dow&(1<<7) != 0 { // 7 == Sunday
		s.dow |= 1
	}
	return &s, nil
}

// Matches reports whether t (minute resolution) satisfies the spec.
func (s *Spec) Matches(t time.Time) bool {
	if s.min&(1<<uint(t.Minute())) == 0 || s.hour&(1<<uint(t.Hour())) == 0 || s.mon&(1<<uint(t.Month())) == 0 {
		return false
	}
	dom := s.dom&(1<<uint(t.Day())) != 0
	dow := s.dow&(1<<uint(t.Weekday())) != 0
	switch { // classic cron: if both restricted, either may match
	case s.domStar && s.dowStar:
		return true
	case s.domStar:
		return dow
	case s.dowStar:
		return dom
	}
	return dom || dow
}
