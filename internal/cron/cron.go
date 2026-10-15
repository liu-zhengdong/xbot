// Package cron matches a 5-field cron expression. Both day-of-month and
// day-of-week restricted means either one may match, which is the usual cron rule.
package cron

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// MinInterval is the smallest gap xbot will schedule.
const MinInterval = 5 * time.Minute

// Schedule is a parsed cron expression in UTC.
type Schedule struct {
	min, hour, dom, mon, dow field
}

type field struct {
	any  bool
	vals map[int]struct{}
}

// Parse reads "minute hour dom month dow".
func Parse(expr string) (Schedule, error) {
	parts := strings.Fields(expr)
	if len(parts) != 5 {
		return Schedule{}, fmt.Errorf("cron: want 5 fields, got %d", len(parts))
	}
	var s Schedule
	var err error
	if s.min, err = parseField(parts[0], 0, 59, false); err != nil {
		return Schedule{}, fmt.Errorf("cron: minute: %w", err)
	}
	if s.hour, err = parseField(parts[1], 0, 23, false); err != nil {
		return Schedule{}, fmt.Errorf("cron: hour: %w", err)
	}
	if s.dom, err = parseField(parts[2], 1, 31, false); err != nil {
		return Schedule{}, fmt.Errorf("cron: day of month: %w", err)
	}
	if s.mon, err = parseField(parts[3], 1, 12, false); err != nil {
		return Schedule{}, fmt.Errorf("cron: month: %w", err)
	}
	if s.dow, err = parseField(parts[4], 0, 7, true); err != nil {
		return Schedule{}, fmt.Errorf("cron: day of week: %w", err)
	}
	return s, nil
}

// Match reports whether t hits the schedule. The location of t is ignored; fields are UTC.
func (s Schedule) Match(t time.Time) bool {
	t = t.UTC()
	if !s.min.has(t.Minute()) || !s.hour.has(t.Hour()) || !s.mon.has(int(t.Month())) {
		return false
	}
	domOK := s.dom.has(t.Day())
	dowOK := s.dow.has(int(t.Weekday()))
	switch {
	case s.dom.any && s.dow.any:
		return true
	case s.dom.any:
		return dowOK
	case s.dow.any:
		return domOK
	default:
		return domOK || dowOK
	}
}

// MinGap returns the smallest gap among the next n matches.
func (s Schedule) MinGap(n int) (time.Duration, error) {
	if n < 2 {
		n = 2
	}
	t := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	limit := t.Add(366 * 24 * time.Hour)
	var prev time.Time
	var min time.Duration
	found := 0
	for ; t.Before(limit) && found < n; t = t.Add(time.Minute) {
		if !s.Match(t) {
			continue
		}
		if found > 0 {
			gap := t.Sub(prev)
			if min == 0 || gap < min {
				min = gap
			}
		}
		prev = t
		found++
	}
	if found < 2 {
		return 0, fmt.Errorf("cron: found %d matches", found)
	}
	return min, nil
}

// Allow reports whether the expression stays at or above MinInterval.
func Allow(expr string) error {
	s, err := Parse(expr)
	if err != nil {
		return err
	}
	gap, err := s.MinGap(500)
	if err != nil {
		return err
	}
	if gap < MinInterval {
		return fmt.Errorf("cron: minimum gap is %s, got %s", MinInterval, gap)
	}
	return nil
}

func (f field) has(v int) bool {
	if f.any {
		return true
	}
	_, ok := f.vals[v]
	return ok
}

func parseField(s string, lo, hi int, dow bool) (field, error) {
	if s == "*" {
		return field{any: true}, nil
	}
	vals := map[int]struct{}{}
	for _, part := range strings.Split(s, ",") {
		if part == "" {
			return field{}, fmt.Errorf("empty list item")
		}
		step := 1
		body := part
		if i := strings.IndexByte(part, '/'); i >= 0 {
			body = part[:i]
			n, err := strconv.Atoi(part[i+1:])
			if err != nil || n <= 0 {
				return field{}, fmt.Errorf("bad step %q", part)
			}
			step = n
		}
		start, end := lo, hi
		switch {
		case body == "*":
		case strings.Contains(body, "-"):
			a, b, _ := strings.Cut(body, "-")
			var err error
			start, err = strconv.Atoi(a)
			if err != nil {
				return field{}, fmt.Errorf("bad range %q", part)
			}
			end, err = strconv.Atoi(b)
			if err != nil {
				return field{}, fmt.Errorf("bad range %q", part)
			}
		default:
			n, err := strconv.Atoi(body)
			if err != nil {
				return field{}, fmt.Errorf("bad value %q", part)
			}
			start, end = n, n
		}
		if start < lo || end > hi || start > end {
			return field{}, fmt.Errorf("%d-%d outside %d-%d", start, end, lo, hi)
		}
		for v := start; v <= end; v += step {
			vals[normalize(v, dow)] = struct{}{}
		}
	}
	if len(vals) == 0 {
		return field{}, fmt.Errorf("empty field")
	}
	return field{vals: vals}, nil
}

func normalize(v int, dow bool) int {
	if dow && v == 7 {
		return 0
	}
	return v
}
