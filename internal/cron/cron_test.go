package cron

import (
	"testing"
	"time"
)

func mustParse(t *testing.T, expr string) Schedule {
	t.Helper()
	s, err := Parse(expr)
	if err != nil {
		t.Fatalf("parse %q: %v", expr, err)
	}
	return s
}

func TestNextEveryMinute(t *testing.T) {
	s := mustParse(t, "* * * * *")
	from := time.Date(2026, 1, 1, 10, 30, 15, 0, time.UTC)
	next := s.Next(from)
	want := time.Date(2026, 1, 1, 10, 31, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("next=%v want %v", next, want)
	}
}

func TestNextHourly(t *testing.T) {
	s := mustParse(t, "0 * * * *")
	from := time.Date(2026, 1, 1, 10, 30, 0, 0, time.UTC)
	if got := s.Next(from); !got.Equal(time.Date(2026, 1, 1, 11, 0, 0, 0, time.UTC)) {
		t.Fatalf("hourly next=%v", got)
	}
}

func TestNextDailyAt(t *testing.T) {
	s := mustParse(t, "30 2 * * *") // 02:30 daily
	from := time.Date(2026, 1, 1, 5, 0, 0, 0, time.UTC)
	if got := s.Next(from); !got.Equal(time.Date(2026, 1, 2, 2, 30, 0, 0, time.UTC)) {
		t.Fatalf("daily next=%v", got)
	}
}

func TestStepAndRange(t *testing.T) {
	s := mustParse(t, "*/15 9-17 * * *") // every 15 min, 9am-5pm
	from := time.Date(2026, 1, 1, 9, 7, 0, 0, time.UTC)
	if got := s.Next(from); !got.Equal(time.Date(2026, 1, 1, 9, 15, 0, 0, time.UTC)) {
		t.Fatalf("step next=%v", got)
	}
}

func TestDayOfWeek(t *testing.T) {
	s := mustParse(t, "0 0 * * 1")                      // midnight Mondays
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) // Thu Jan 1 2026
	got := s.Next(from)
	if got.Weekday() != time.Monday || got.Hour() != 0 {
		t.Fatalf("dow next=%v (weekday %v)", got, got.Weekday())
	}
}

func TestParseErrors(t *testing.T) {
	for _, bad := range []string{"* * * *", "60 * * * *", "* 24 * * *", "* * 0 * *", "a * * * *", "*/0 * * * *"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("expected error for %q", bad)
		}
	}
}

func TestSundayBothForms(t *testing.T) {
	a := mustParse(t, "0 0 * * 0")
	b := mustParse(t, "0 0 * * 7")
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if !a.Next(from).Equal(b.Next(from)) {
		t.Fatal("0 and 7 should both mean Sunday")
	}
}
