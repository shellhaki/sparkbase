package cronexpr

import (
	"testing"
	"time"
)

func TestNext(t *testing.T) {
	base := time.Date(2026, 10, 6, 10, 7, 30, 0, time.UTC) // Tuesday
	cases := map[string]string{
		"* * * * *":     "2026-10-06T10:08:00Z",
		"*/15 * * * *":  "2026-10-06T10:15:00Z",
		"0 * * * *":     "2026-10-06T11:00:00Z",
		"30 3 * * *":    "2026-10-07T03:30:00Z",
		"0 9 * * 1-5":   "2026-10-07T09:00:00Z",
		"0 0 1 * *":     "2026-11-01T00:00:00Z",
		"0 0 * * 0":     "2026-10-11T00:00:00Z",
		"0 0 * * 7":     "2026-10-11T00:00:00Z",
		"@daily":        "2026-10-07T00:00:00Z",
		"5,10 10 * * *": "2026-10-06T10:10:00Z",
		"0 12 29 2 *":   "2028-02-29T12:00:00Z",
	}
	for expr, want := range cases {
		s, err := Parse(expr)
		if err != nil {
			t.Fatalf("%s: %v", expr, err)
		}
		if got := s.Next(base).Format(time.RFC3339); got != want {
			t.Errorf("%s: got %s want %s", expr, got, want)
		}
	}
	for _, bad := range []string{"", "* * * *", "61 * * * *", "*/0 * * * *", "a * * * *", "5-1 * * * *"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	s, _ := Parse("*/5 * * * *")
	if s.MinInterval(base) != 5*time.Minute {
		t.Fatal("min interval")
	}
}
