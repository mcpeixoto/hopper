// Package cron parses standard 5-field cron expressions and computes the next
// fire time. Fields: minute(0-59) hour(0-23) day-of-month(1-31) month(1-12)
// day-of-week(0-6, 0=Sunday; 7 also accepted as Sunday). Each field supports
// "*", lists "a,b", ranges "a-b", and steps "*/n" or "a-b/n".
//
// Day-of-month and day-of-week use the usual cron "or" semantics: when both are
// restricted, a time matches if it matches *either*; when one is "*", both must
// match.
package cron

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Schedule is a parsed cron expression.
type Schedule struct {
	min, hour, dom, month, dow uint64
	domStar, dowStar           bool
}

// Parse parses a 5-field cron expression.
func Parse(expr string) (Schedule, error) {
	f := strings.Fields(expr)
	if len(f) != 5 {
		return Schedule{}, fmt.Errorf("cron: expected 5 fields, got %d", len(f))
	}
	var s Schedule
	var err error
	if s.min, err = parseField(f[0], 0, 59); err != nil {
		return Schedule{}, err
	}
	if s.hour, err = parseField(f[1], 0, 23); err != nil {
		return Schedule{}, err
	}
	if s.dom, err = parseField(f[2], 1, 31); err != nil {
		return Schedule{}, err
	}
	if s.month, err = parseField(f[3], 1, 12); err != nil {
		return Schedule{}, err
	}
	if s.dow, err = parseField(normalizeDOW(f[4]), 0, 6); err != nil {
		return Schedule{}, err
	}
	s.domStar = f[2] == "*"
	s.dowStar = f[4] == "*"
	return s, nil
}

// Next returns the first time strictly after `after` that matches the schedule.
// It searches up to ~5 years before giving up (returns zero time).
func (s Schedule) Next(after time.Time) time.Time {
	t := after.Truncate(time.Minute).Add(time.Minute)
	limit := t.AddDate(5, 0, 0)
	for t.Before(limit) {
		if s.matches(t) {
			return t
		}
		t = t.Add(time.Minute)
	}
	return time.Time{}
}

func (s Schedule) matches(t time.Time) bool {
	if s.min&(1<<uint(t.Minute())) == 0 {
		return false
	}
	if s.hour&(1<<uint(t.Hour())) == 0 {
		return false
	}
	if s.month&(1<<uint(t.Month())) == 0 {
		return false
	}
	domOK := s.dom&(1<<uint(t.Day())) != 0
	dowOK := s.dow&(1<<uint(int(t.Weekday()))) != 0
	switch {
	case s.domStar && s.dowStar:
		return true
	case s.domStar:
		return dowOK
	case s.dowStar:
		return domOK
	default:
		return domOK || dowOK
	}
}

// normalizeDOW maps a "7" token (Sunday) to "0".
func normalizeDOW(f string) string {
	if f == "7" {
		return "0"
	}
	return strings.ReplaceAll(f, "7", "0") // handles lists/ranges containing 7
}

// parseField turns one cron field into a bitmask over [min,max].
func parseField(field string, min, max int) (uint64, error) {
	var mask uint64
	for _, part := range strings.Split(field, ",") {
		step := 1
		rng := part
		if i := strings.Index(part, "/"); i >= 0 {
			s, err := strconv.Atoi(part[i+1:])
			if err != nil || s < 1 {
				return 0, fmt.Errorf("cron: bad step %q", part)
			}
			step = s
			rng = part[:i]
		}
		lo, hi := min, max
		switch {
		case rng == "*":
			// full range
		case strings.Contains(rng, "-"):
			ab := strings.SplitN(rng, "-", 2)
			a, err1 := strconv.Atoi(ab[0])
			b, err2 := strconv.Atoi(ab[1])
			if err1 != nil || err2 != nil {
				return 0, fmt.Errorf("cron: bad range %q", rng)
			}
			lo, hi = a, b
		default:
			v, err := strconv.Atoi(rng)
			if err != nil {
				return 0, fmt.Errorf("cron: bad value %q", rng)
			}
			lo, hi = v, v
		}
		if lo < min || hi > max || lo > hi {
			return 0, fmt.Errorf("cron: value out of range in %q (allowed %d-%d)", part, min, max)
		}
		for v := lo; v <= hi; v += step {
			mask |= 1 << uint(v)
		}
	}
	return mask, nil
}
