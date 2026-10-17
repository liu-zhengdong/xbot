// Package cron matches a 5-field cron expression. Both day-of-month and
// day-of-week restricted means either one may match, which is the usual cron rule.
package cron

import (
	"fmt"
	"sort"
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
func (s Schedule) Match(t time.Time) bool { return s.MatchIn(t, time.UTC) }

// MatchIn reports whether t hits the schedule when its fields are read in loc.
func (s Schedule) MatchIn(t time.Time, loc *time.Location) bool {
	if loc == nil {
		loc = time.UTC
	}
	t = t.In(loc)
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

// Next returns the first whole minute strictly after after that matches in loc,
// searching at most 366 days.
func (s Schedule) Next(after time.Time, loc *time.Location) (time.Time, bool) {
	if loc == nil {
		loc = time.UTC
	}
	t := after.Truncate(time.Minute).Add(time.Minute)
	limit := after.Add(366 * 24 * time.Hour)
	for t.Before(limit) {
		if s.MatchIn(t, loc) {
			return t, true
		}
		t = t.Add(time.Minute)
	}
	return time.Time{}, false
}

// Convert returns an expression that, evaluated in to, matches the same instants
// as expr evaluated in from. ok is false when no exact conversion exists.
func Convert(expr string, from, to *time.Location, start time.Time) (string, bool, error) {
	if from == nil {
		from = time.UTC
	}
	if to == nil {
		to = time.UTC
	}
	s, err := Parse(expr)
	if err != nil {
		return "", false, err
	}
	parts := strings.Fields(expr)
	if from == to || (from == time.UTC && to == time.UTC) {
		return expr, true, nil
	}
	_, offFrom := start.In(from).Zone()
	_, offTo := start.In(to).Zone()
	d := (offTo - offFrom) / 60
	type pair struct{ min, hour int }
	pairs := map[pair]struct{}{}
	mins := map[int]struct{}{}
	hours := map[int]struct{}{}
	carry := 0
	first := true
	for m := 0; m < 60; m++ {
		if !s.min.has(m) {
			continue
		}
		for h := 0; h < 24; h++ {
			if !s.hour.has(h) {
				continue
			}
			c, nm, nh := carryOf(h*60+m, d)
			pairs[pair{nm, nh}] = struct{}{}
			mins[nm] = struct{}{}
			hours[nh] = struct{}{}
			if first {
				carry, first = c, false
			} else if c != carry {
				carry = 2 // mixed; never valid to shift a weekday
			}
		}
	}
	if len(pairs) != len(mins)*len(hours) {
		return "", false, nil
	}
	minText, ok := renderField(s.min, mins, 0, 59, parts[0])
	if !ok {
		return "", false, nil
	}
	hourText, ok := renderField(s.hour, hours, 0, 23, parts[1])
	if !ok {
		return "", false, nil
	}
	dowText := parts[4]
	if carry != 0 {
		if !(s.dom.any && s.mon.any && s.dow.any) {
			if !s.dom.any || !s.mon.any || carry == 2 {
				return "", false, nil
			}
			shifted := map[int]struct{}{}
			for v := 0; v < 7; v++ {
				if s.dow.has(v) {
					shifted[(v+carry+7)%7] = struct{}{}
				}
			}
			dowText, ok = renderField(s.dow, shifted, 0, 6, parts[4])
			if !ok {
				return "", false, nil
			}
		}
	}
	out := strings.Join([]string{minText, hourText, parts[2], parts[3], dowText}, " ")
	ns, err := Parse(out)
	if err != nil {
		return "", false, nil
	}
	limit := start.Truncate(time.Minute).Add(366 * 24 * time.Hour)
	for t := start.Truncate(time.Minute); t.Before(limit); t = t.Add(time.Minute) {
		if s.MatchIn(t, from) != ns.MatchIn(t, to) {
			return "", false, nil
		}
	}
	return out, true, nil
}

func carryOf(total, d int) (int, int, int) {
	n := total + d
	m := ((n % 1440) + 1440) % 1440
	return (n - m) / 1440, m % 60, m / 60
}

func renderField(f field, vals map[int]struct{}, lo, hi int, text string) (string, bool) {
	if len(vals) == 0 {
		return "", false
	}
	same := true
	for v := lo; v <= hi; v++ {
		if f.has(v) {
			if _, ok := vals[v]; !ok {
				same = false
			}
		} else if _, ok := vals[v]; ok {
			same = false
		}
	}
	if same {
		return text, true
	}
	if len(vals) == hi-lo+1 {
		return "*", true
	}
	out := make([]int, 0, len(vals))
	for v := range vals {
		out = append(out, v)
	}
	sort.Ints(out)
	ss := make([]string, len(out))
	for i, v := range out {
		ss[i] = strconv.Itoa(v)
	}
	return strings.Join(ss, ","), true
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
