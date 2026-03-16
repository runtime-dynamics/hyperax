package cron

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Schedule represents a parsed cron schedule. Either the standard 5-field
// minute/hour/day/month/weekday form, or the @every shortcut that uses a
// fixed duration.
type Schedule struct {
	Minutes  []int         // 0-59
	Hours    []int         // 0-23
	Days     []int         // 1-31
	Months   []int         // 1-12
	Weekdays []int         // 0-6 (Sunday=0)
	Every    time.Duration // non-zero for @every shortcuts
}

// Parse parses a cron expression string into a Schedule.
//
// Supported formats:
//   - Standard 5-field: "minute hour day month weekday"
//   - Shortcuts: @yearly, @annually, @monthly, @weekly, @daily, @midnight, @hourly
//   - Duration: @every <duration> (e.g., "@every 5m", "@every 1h30m")
//
// Field syntax supports: *, specific values, ranges (1-5), steps (*/15),
// and comma-separated lists (1,15,30).
func Parse(expr string) (*Schedule, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil, fmt.Errorf("cron.Parse: empty cron expression")
	}

	// Handle named shortcuts.
	switch expr {
	case "@yearly", "@annually":
		return Parse("0 0 1 1 *")
	case "@monthly":
		return Parse("0 0 1 * *")
	case "@weekly":
		return Parse("0 0 * * 0")
	case "@daily", "@midnight":
		return Parse("0 0 * * *")
	case "@hourly":
		return Parse("0 * * * *")
	}

	// Handle @every duration.
	if strings.HasPrefix(expr, "@every ") {
		durStr := strings.TrimPrefix(expr, "@every ")
		d, err := time.ParseDuration(durStr)
		if err != nil {
			return nil, fmt.Errorf("cron.Parse: invalid @every duration: %w", err)
		}
		if d <= 0 {
			return nil, fmt.Errorf("cron.Parse: @every duration must be positive")
		}
		return &Schedule{Every: d}, nil
	}

	// Parse 5-field expression: minute hour day month weekday
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return nil, fmt.Errorf("cron.Parse: expected 5 fields, got %d", len(fields))
	}

	minutes, err := parseField(fields[0], 0, 59)
	if err != nil {
		return nil, fmt.Errorf("cron.Parse: minute field: %w", err)
	}
	hours, err := parseField(fields[1], 0, 23)
	if err != nil {
		return nil, fmt.Errorf("cron.Parse: hour field: %w", err)
	}
	days, err := parseField(fields[2], 1, 31)
	if err != nil {
		return nil, fmt.Errorf("cron.Parse: day field: %w", err)
	}
	months, err := parseField(fields[3], 1, 12)
	if err != nil {
		return nil, fmt.Errorf("cron.Parse: month field: %w", err)
	}
	weekdays, err := parseField(fields[4], 0, 6)
	if err != nil {
		return nil, fmt.Errorf("cron.Parse: weekday field: %w", err)
	}

	return &Schedule{
		Minutes:  minutes,
		Hours:    hours,
		Days:     days,
		Months:   months,
		Weekdays: weekdays,
	}, nil
}

// parseField parses a single cron field into a sorted slice of integers.
// The field may contain comma-separated terms. Each term may be:
//   - "*"       : all values in [min, max]
//   - "*/step"  : every step-th value starting from min
//   - "n"       : literal value
//   - "n-m"     : range [n, m]
//   - "n-m/step": range with step
func parseField(field string, min, max int) ([]int, error) {
	// Use a set to deduplicate values.
	set := make(map[int]struct{})

	parts := strings.Split(field, ",")
	for _, part := range parts {
		vals, err := parseTerm(part, min, max)
		if err != nil {
			return nil, fmt.Errorf("cron.parseField: %w", err)
		}
		for _, v := range vals {
			set[v] = struct{}{}
		}
	}

	if len(set) == 0 {
		return nil, fmt.Errorf("cron.parseField: no values produced for field %q", field)
	}

	// Convert set to sorted slice.
	result := make([]int, 0, len(set))
	for v := range set {
		result = append(result, v)
	}
	sortInts(result)
	return result, nil
}

// parseTerm parses a single term (one element between commas).
func parseTerm(term string, min, max int) ([]int, error) {
	step := 1

	// Check for /step suffix.
	if idx := strings.Index(term, "/"); idx != -1 {
		stepStr := term[idx+1:]
		s, err := strconv.Atoi(stepStr)
		if err != nil {
			return nil, fmt.Errorf("cron.parseTerm: invalid step %q: %w", stepStr, err)
		}
		if s <= 0 {
			return nil, fmt.Errorf("cron.parseTerm: step must be positive, got %d", s)
		}
		step = s
		term = term[:idx]
	}

	var rangeMin, rangeMax int

	if term == "*" {
		rangeMin = min
		rangeMax = max
	} else if idx := strings.Index(term, "-"); idx != -1 {
		lo, err := strconv.Atoi(term[:idx])
		if err != nil {
			return nil, fmt.Errorf("cron.parseTerm: invalid range start %q: %w", term[:idx], err)
		}
		hi, err := strconv.Atoi(term[idx+1:])
		if err != nil {
			return nil, fmt.Errorf("cron.parseTerm: invalid range end %q: %w", term[idx+1:], err)
		}
		if lo < min || hi > max {
			return nil, fmt.Errorf("cron.parseTerm: range %d-%d out of bounds [%d, %d]", lo, hi, min, max)
		}
		if lo > hi {
			return nil, fmt.Errorf("cron.parseTerm: range start %d > end %d", lo, hi)
		}
		rangeMin = lo
		rangeMax = hi
	} else {
		// Single value — if we also have a step, treat it as range [val, max].
		val, err := strconv.Atoi(term)
		if err != nil {
			return nil, fmt.Errorf("cron.parseTerm: invalid value %q: %w", term, err)
		}
		if val < min || val > max {
			return nil, fmt.Errorf("cron.parseTerm: value %d out of bounds [%d, %d]", val, min, max)
		}
		if step > 1 {
			rangeMin = val
			rangeMax = max
		} else {
			return []int{val}, nil
		}
	}

	// Generate values from rangeMin to rangeMax with step.
	var vals []int
	for v := rangeMin; v <= rangeMax; v += step {
		vals = append(vals, v)
	}
	return vals, nil
}

// sortInts sorts a small slice of ints using insertion sort.
// We avoid importing sort to keep this package dependency-light.
func sortInts(a []int) {
	for i := 1; i < len(a); i++ {
		key := a[i]
		j := i - 1
		for j >= 0 && a[j] > key {
			a[j+1] = a[j]
			j--
		}
		a[j+1] = key
	}
}

// NextAfter returns the next time after "after" that matches the schedule.
// For @every schedules, it simply adds the duration.
// For 5-field schedules, it walks forward checking each candidate minute
// against the schedule constraints.
func (s *Schedule) NextAfter(after time.Time) time.Time {
	if s.Every > 0 {
		return after.Add(s.Every)
	}

	// Start from the next minute boundary after "after".
	t := after.Truncate(time.Minute).Add(time.Minute)

	// Limit search to ~4 years to prevent infinite loops on impossible schedules.
	limit := after.Add(4 * 365 * 24 * time.Hour)

	for t.Before(limit) {
		if s.matches(t) {
			return t
		}
		t = advance(t, s)
	}

	// Fallback: should not happen for valid schedules.
	return after.Add(time.Minute)
}

// matches returns true if the given time matches all schedule constraints.
func (s *Schedule) matches(t time.Time) bool {
	return contains(s.Minutes, t.Minute()) &&
		contains(s.Hours, t.Hour()) &&
		contains(s.Days, t.Day()) &&
		contains(s.Months, int(t.Month())) &&
		contains(s.Weekdays, int(t.Weekday()))
}

// advance skips forward intelligently to avoid minute-by-minute iteration.
// It checks which field does not match and jumps to the next possible value.
func advance(t time.Time, s *Schedule) time.Time {
	// If the month does not match, jump to the first day of the next matching month.
	if !contains(s.Months, int(t.Month())) {
		next := nextVal(s.Months, int(t.Month()))
		if next <= int(t.Month()) {
			// Wrap to next year.
			t = time.Date(t.Year()+1, time.Month(next), 1, 0, 0, 0, 0, t.Location())
		} else {
			t = time.Date(t.Year(), time.Month(next), 1, 0, 0, 0, 0, t.Location())
		}
		return t
	}

	// If the day does not match, jump to the next day at midnight.
	if !contains(s.Days, t.Day()) || !contains(s.Weekdays, int(t.Weekday())) {
		next := t.AddDate(0, 0, 1)
		return time.Date(next.Year(), next.Month(), next.Day(), 0, 0, 0, 0, t.Location())
	}

	// If the hour does not match, jump to the next matching hour.
	if !contains(s.Hours, t.Hour()) {
		nextH := nextVal(s.Hours, t.Hour())
		if nextH <= t.Hour() {
			// Wrap to next day at midnight (first matching hour/minute).
			nd := t.AddDate(0, 0, 1)
			return time.Date(nd.Year(), nd.Month(), nd.Day(), 0, 0, 0, 0, t.Location())
		}
		return time.Date(t.Year(), t.Month(), t.Day(), nextH, s.Minutes[0], 0, 0, t.Location())
	}

	// Minute does not match, jump to the next matching minute.
	nextM := nextVal(s.Minutes, t.Minute())
	if nextM <= t.Minute() {
		// Wrap to next hour at minute 0.
		return time.Date(t.Year(), t.Month(), t.Day(), t.Hour()+1, 0, 0, 0, t.Location())
	}
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), nextM, 0, 0, t.Location())
}

// nextVal returns the first value in sorted vals that is greater than current.
// If no such value exists, it wraps around and returns vals[0].
func nextVal(vals []int, current int) int {
	for _, v := range vals {
		if v > current {
			return v
		}
	}
	return vals[0]
}

// contains reports whether the sorted slice vals contains v.
func contains(vals []int, v int) bool {
	for _, x := range vals {
		if x == v {
			return true
		}
	}
	return false
}
