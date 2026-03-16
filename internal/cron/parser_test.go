package cron

import (
	"testing"
	"time"
)

func TestParse_Shortcuts(t *testing.T) {
	tests := []struct {
		expr    string
		minutes []int
		hours   []int
		days    []int
		months  []int
	}{
		{"@yearly", []int{0}, []int{0}, []int{1}, []int{1}},
		{"@annually", []int{0}, []int{0}, []int{1}, []int{1}},
		{"@monthly", []int{0}, []int{0}, []int{1}, nil},
		{"@weekly", []int{0}, []int{0}, nil, nil},
		{"@daily", []int{0}, []int{0}, nil, nil},
		{"@midnight", []int{0}, []int{0}, nil, nil},
		{"@hourly", []int{0}, nil, nil, nil},
	}

	for _, tt := range tests {
		s, err := Parse(tt.expr)
		if err != nil {
			t.Errorf("%s: unexpected error: %v", tt.expr, err)
			continue
		}
		if s.Every != 0 {
			t.Errorf("%s: expected standard schedule, got @every", tt.expr)
			continue
		}
		if !intsEqual(s.Minutes, tt.minutes) && tt.minutes != nil {
			t.Errorf("%s: minutes = %v, want %v", tt.expr, s.Minutes, tt.minutes)
		}
		if !intsEqual(s.Hours, tt.hours) && tt.hours != nil {
			t.Errorf("%s: hours = %v, want %v", tt.expr, s.Hours, tt.hours)
		}
		if tt.days != nil && !intsEqual(s.Days, tt.days) {
			t.Errorf("%s: days = %v, want %v", tt.expr, s.Days, tt.days)
		}
		if tt.months != nil && !intsEqual(s.Months, tt.months) {
			t.Errorf("%s: months = %v, want %v", tt.expr, s.Months, tt.months)
		}
	}
}

func TestParse_Every(t *testing.T) {
	s, err := Parse("@every 5m")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Every != 5*time.Minute {
		t.Errorf("every = %v, want 5m", s.Every)
	}

	s, err = Parse("@every 1h30m")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Every != 90*time.Minute {
		t.Errorf("every = %v, want 1h30m", s.Every)
	}
}

func TestParse_EveryInvalid(t *testing.T) {
	_, err := Parse("@every invalid")
	if err == nil {
		t.Error("expected error for invalid duration")
	}

	_, err = Parse("@every -5m")
	if err == nil {
		t.Error("expected error for negative duration")
	}
}

func TestParse_Standard(t *testing.T) {
	// Every 15 minutes
	s, err := Parse("*/15 * * * *")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := []int{0, 15, 30, 45}
	if !intsEqual(s.Minutes, expected) {
		t.Errorf("minutes = %v, want %v", s.Minutes, expected)
	}

	// At 9:30 on weekdays
	s, err = Parse("30 9 * * 1-5")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !intsEqual(s.Minutes, []int{30}) {
		t.Errorf("minutes = %v, want [30]", s.Minutes)
	}
	if !intsEqual(s.Hours, []int{9}) {
		t.Errorf("hours = %v, want [9]", s.Hours)
	}
	if !intsEqual(s.Weekdays, []int{1, 2, 3, 4, 5}) {
		t.Errorf("weekdays = %v, want [1,2,3,4,5]", s.Weekdays)
	}
}

func TestParse_CommaSeparated(t *testing.T) {
	s, err := Parse("0,15,30,45 * * * *")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !intsEqual(s.Minutes, []int{0, 15, 30, 45}) {
		t.Errorf("minutes = %v, want [0,15,30,45]", s.Minutes)
	}
}

func TestParse_RangeWithStep(t *testing.T) {
	s, err := Parse("1-30/10 * * * *")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !intsEqual(s.Minutes, []int{1, 11, 21}) {
		t.Errorf("minutes = %v, want [1,11,21]", s.Minutes)
	}
}

func TestParse_Wildcard(t *testing.T) {
	s, err := Parse("* * * * *")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(s.Minutes) != 60 {
		t.Errorf("minutes count = %d, want 60", len(s.Minutes))
	}
	if len(s.Hours) != 24 {
		t.Errorf("hours count = %d, want 24", len(s.Hours))
	}
}

func TestParse_InvalidFieldCount(t *testing.T) {
	_, err := Parse("* * *")
	if err == nil {
		t.Error("expected error for 3 fields")
	}
}

func TestParse_InvalidRange(t *testing.T) {
	_, err := Parse("60 * * * *")
	if err == nil {
		t.Error("expected error for minute=60")
	}

	_, err = Parse("* 25 * * *")
	if err == nil {
		t.Error("expected error for hour=25")
	}
}

func TestParse_Empty(t *testing.T) {
	_, err := Parse("")
	if err == nil {
		t.Error("expected error for empty expression")
	}
}

func TestNextAfter_Every(t *testing.T) {
	s := &Schedule{Every: 10 * time.Minute}

	base := time.Date(2026, 3, 8, 10, 0, 0, 0, time.UTC)
	next := s.NextAfter(base)

	expected := base.Add(10 * time.Minute)
	if !next.Equal(expected) {
		t.Errorf("next = %v, want %v", next, expected)
	}
}

func TestNextAfter_Standard(t *testing.T) {
	// Every hour at minute 0.
	s, err := Parse("0 * * * *")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	base := time.Date(2026, 3, 8, 10, 30, 0, 0, time.UTC)
	next := s.NextAfter(base)

	expected := time.Date(2026, 3, 8, 11, 0, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Errorf("next = %v, want %v", next, expected)
	}
}

func TestNextAfter_Daily(t *testing.T) {
	s, err := Parse("0 0 * * *")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	base := time.Date(2026, 3, 8, 23, 59, 0, 0, time.UTC)
	next := s.NextAfter(base)

	expected := time.Date(2026, 3, 9, 0, 0, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Errorf("next = %v, want %v", next, expected)
	}
}

func TestNextAfter_SpecificDayAndTime(t *testing.T) {
	// 9:30 on the 15th of every month.
	s, err := Parse("30 9 15 * *")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	base := time.Date(2026, 3, 15, 9, 30, 0, 0, time.UTC)
	next := s.NextAfter(base)

	// Should be next month's 15th.
	expected := time.Date(2026, 4, 15, 9, 30, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Errorf("next = %v, want %v", next, expected)
	}
}

func TestNextAfter_EveryFifteenMinutes(t *testing.T) {
	s, err := Parse("*/15 * * * *")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	base := time.Date(2026, 3, 8, 10, 2, 0, 0, time.UTC)
	next := s.NextAfter(base)

	expected := time.Date(2026, 3, 8, 10, 15, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Errorf("next = %v, want %v", next, expected)
	}
}

// intsEqual returns true if two int slices are equal.
func intsEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
