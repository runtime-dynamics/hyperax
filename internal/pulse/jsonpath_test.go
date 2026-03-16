package pulse

import (
	"testing"

	"github.com/hyperax/hyperax/pkg/types"
)

func TestJSONPathExtract_TopLevel(t *testing.T) {
	eval := NewJSONPathEvaluator()
	data := `{"status": "healthy", "count": 42, "active": true}`

	tests := []struct {
		path string
		want any
	}{
		{"$.status", "healthy"},
		{"$.count", float64(42)},
		{"$.active", true},
		{"$.missing", nil},
	}

	for _, tt := range tests {
		got := eval.Extract(data, tt.path)
		if got != tt.want {
			t.Errorf("Extract(%q) = %v (%T), want %v (%T)", tt.path, got, got, tt.want, tt.want)
		}
	}
}

func TestJSONPathExtract_Nested(t *testing.T) {
	eval := NewJSONPathEvaluator()
	data := `{"data": {"items": [{"name": "alpha"}, {"name": "beta"}], "total": 2}}`

	tests := []struct {
		path string
		want any
	}{
		{"$.data.total", float64(2)},
		{"$.data.items[0].name", "alpha"},
		{"$.data.items[1].name", "beta"},
	}

	for _, tt := range tests {
		got := eval.Extract(data, tt.path)
		if got != tt.want {
			t.Errorf("Extract(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestJSONPathExtract_ArrayOutOfBounds(t *testing.T) {
	eval := NewJSONPathEvaluator()
	data := `{"items": [1, 2, 3]}`

	got := eval.Extract(data, "$.items[5]")
	if got != nil {
		t.Errorf("expected nil for out-of-bounds index, got %v", got)
	}
}

func TestJSONPathExtract_EmptyInput(t *testing.T) {
	eval := NewJSONPathEvaluator()

	if got := eval.Extract("", "$.foo"); got != nil {
		t.Errorf("expected nil for empty JSON, got %v", got)
	}
	if got := eval.Extract(`{"foo": 1}`, ""); got != nil {
		t.Errorf("expected nil for empty path, got %v", got)
	}
}

func TestJSONPathExtract_InvalidJSON(t *testing.T) {
	eval := NewJSONPathEvaluator()
	if got := eval.Extract("not json", "$.foo"); got != nil {
		t.Errorf("expected nil for invalid JSON, got %v", got)
	}
}

func TestCompare_Eq(t *testing.T) {
	tests := []struct {
		extracted any
		expected  string
		want      bool
	}{
		{"healthy", "healthy", true},
		{"healthy", "unhealthy", false},
		{float64(42), "42", true},
		{true, "true", true},
	}

	for _, tt := range tests {
		if got := compare(tt.extracted, "eq", tt.expected); got != tt.want {
			t.Errorf("compare(%v, eq, %q) = %v, want %v", tt.extracted, tt.expected, got, tt.want)
		}
	}
}

func TestCompare_Ne(t *testing.T) {
	if !compare("a", "ne", "b") {
		t.Error("expected 'a' ne 'b' to be true")
	}
	if compare("a", "ne", "a") {
		t.Error("expected 'a' ne 'a' to be false")
	}
}

func TestCompare_Numeric(t *testing.T) {
	tests := []struct {
		extracted any
		op        string
		expected  string
		want      bool
	}{
		{float64(10), "gt", "5", true},
		{float64(5), "gt", "10", false},
		{float64(5), "lt", "10", true},
		{float64(10), "gte", "10", true},
		{float64(10), "lte", "10", true},
		{float64(9), "gte", "10", false},
		{float64(11), "lte", "10", false},
	}

	for _, tt := range tests {
		if got := compare(tt.extracted, tt.op, tt.expected); got != tt.want {
			t.Errorf("compare(%v, %s, %q) = %v, want %v", tt.extracted, tt.op, tt.expected, got, tt.want)
		}
	}
}

func TestCompare_Contains(t *testing.T) {
	if !compare("hello world", "contains", "world") {
		t.Error("expected 'hello world' contains 'world'")
	}
	if compare("hello", "contains", "world") {
		t.Error("expected 'hello' NOT contains 'world'")
	}
}

func TestCompare_Matches(t *testing.T) {
	if !compare("error-500", "matches", `error-\d+`) {
		t.Error("expected 'error-500' matches 'error-\\d+'")
	}
	if compare("success", "matches", `error-\d+`) {
		t.Error("expected 'success' NOT matches 'error-\\d+'")
	}
}

func TestCompare_InvalidRegex(t *testing.T) {
	if compare("test", "matches", "[invalid") {
		t.Error("expected false for invalid regex")
	}
}

func TestCompare_UnknownOperator(t *testing.T) {
	if compare("a", "xor", "b") {
		t.Error("expected false for unknown operator")
	}
}

func TestEvaluateAll_ANDLogic(t *testing.T) {
	eval := NewJSONPathEvaluator()
	data := `{"status": "healthy", "count": 5}`

	// Both criteria match.
	criteria := []types.MatchCriteria{
		{JSONPath: "$.status", Operator: "eq", Value: "healthy"},
		{JSONPath: "$.count", Operator: "gt", Value: "3"},
	}
	if !eval.EvaluateAll(data, criteria) {
		t.Error("expected all criteria to match")
	}

	// Second criterion fails.
	criteria[1].Value = "10"
	if eval.EvaluateAll(data, criteria) {
		t.Error("expected criteria to NOT all match when count < 10")
	}
}

func TestEvaluateAll_EmptyCriteria(t *testing.T) {
	eval := NewJSONPathEvaluator()
	if !eval.EvaluateAll(`{"foo": 1}`, nil) {
		t.Error("expected true for empty criteria (vacuous truth)")
	}
}

func TestEvaluate_MissingPath(t *testing.T) {
	eval := NewJSONPathEvaluator()
	criterion := types.MatchCriteria{JSONPath: "$.nonexistent", Operator: "eq", Value: "x"}
	if eval.Evaluate(`{"foo": 1}`, criterion) {
		t.Error("expected false when JSONPath resolves to nil")
	}
}

func TestToString(t *testing.T) {
	tests := []struct {
		input any
		want  string
	}{
		{"hello", "hello"},
		{float64(42), "42"},
		{float64(3.14), "3.14"},
		{true, "true"},
		{false, "false"},
		{nil, ""},
	}
	for _, tt := range tests {
		got := toString(tt.input)
		if got != tt.want {
			t.Errorf("toString(%v) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
