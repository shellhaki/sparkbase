// Package cronexpr parses 5-field cron expressions (minute hour day-of-month
// month day-of-week, UTC) and finds the next run time.
package cronexpr

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type Schedule struct {
	minute, hour, dom, month, dow [64]bool
	domStar, dowStar              bool
}

var aliases = map[string]string{
	"@yearly": "0 0 1 1 *", "@annually": "0 0 1 1 *", "@monthly": "0 0 1 * *",
	"@weekly": "0 0 * * 0", "@daily": "0 0 * * *", "@midnight": "0 0 * * *", "@hourly": "0 * * * *",
}

// Parse reads an expression such as "*/5 * * * *" or "@daily".
func Parse(expr string) (*Schedule, error) {
	expr = strings.TrimSpace(expr)
	if a, ok := aliases[strings.ToLower(expr)]; ok {
		expr = a
	}
	f := strings.Fields(expr)
	if len(f) != 5 {
		return nil, fmt.Errorf("a cron expression has 5 fields (minute hour day month weekday), got %d", len(f))
	}
	s := &Schedule{}
	parts := []struct {
		set      *[64]bool
		min, max int
	}{{&s.minute, 0, 59}, {&s.hour, 0, 23}, {&s.dom, 1, 31}, {&s.month, 1, 12}, {&s.dow, 0, 7}}
	for i, p := range parts {
		if err := parseField(f[i], p.min, p.max, p.set); err != nil {
			return nil, fmt.Errorf("field %d (%q): %w", i+1, f[i], err)
		}
	}
	if s.dow[7] {
		s.dow[0] = true // 7 is Sunday too
	}
	s.domStar, s.dowStar = f[2] == "*", f[4] == "*"
	return s, nil
}

func parseField(field string, min, max int, set *[64]bool) error {
	for _, part := range strings.Split(field, ",") {
		step := 1
		if base, st, ok := strings.Cut(part, "/"); ok {
			n, err := strconv.Atoi(st)
			if err != nil || n <= 0 {
				return fmt.Errorf("bad step %q", st)
			}
			step, part = n, base
		}
		lo, hi := min, max
		switch {
		case part == "*":
		case strings.Contains(part, "-"):
			a, b, _ := strings.Cut(part, "-")
			var err1, err2 error
			lo, err1 = strconv.Atoi(a)
			hi, err2 = strconv.Atoi(b)
			if err1 != nil || err2 != nil {
				return fmt.Errorf("bad range %q", part)
			}
		default:
			n, err := strconv.Atoi(part)
			if err != nil {
				return fmt.Errorf("bad value %q", part)
			}
			lo, hi = n, n
			if step > 1 {
				hi = max
			}
		}
		if lo < min || hi > max || lo > hi {
			return fmt.Errorf("%d-%d is outside %d-%d", lo, hi, min, max)
		}
		for v := lo; v <= hi; v += step {
			set[v] = true
		}
	}
	return nil
}

// Next returns the first run time strictly after t, in UTC.
func (s *Schedule) Next(t time.Time) time.Time {
	t = t.UTC().Truncate(time.Minute).Add(time.Minute)
	limit := t.AddDate(5, 0, 0)
	for t.Before(limit) {
		if !s.month[int(t.Month())] {
			t = time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, time.UTC)
			continue
		}
		if !s.dayMatches(t) {
			t = time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, 0, time.UTC)
			continue
		}
		if !s.hour[t.Hour()] {
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour()+1, 0, 0, 0, time.UTC)
			continue
		}
		if !s.minute[t.Minute()] {
			t = t.Add(time.Minute)
			continue
		}
		return t
	}
	return limit
}

// Standard cron: when both day fields are restricted, either may match.
func (s *Schedule) dayMatches(t time.Time) bool {
	dom, dow := s.dom[t.Day()], s.dow[int(t.Weekday())]
	switch {
	case s.domStar && s.dowStar:
		return true
	case s.domStar:
		return dow
	case s.dowStar:
		return dom
	}
	return dom || dow
}

// MinInterval is the shortest gap between two runs over the next day.
func (s *Schedule) MinInterval(from time.Time) time.Duration {
	a := s.Next(from)
	shortest := 24 * time.Hour
	for i := 0; i < 50; i++ {
		b := s.Next(a)
		if d := b.Sub(a); d < shortest {
			shortest = d
		}
		a = b
	}
	return shortest
}
