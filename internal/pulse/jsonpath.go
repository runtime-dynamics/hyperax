package pulse

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/hyperax/hyperax/pkg/types"
)

// JSONPathEvaluator evaluates JSONPath expressions against JSON data
// and compares extracted values using comparison operators.
//
// Supported operators: eq, ne, gt, lt, gte, lte, contains, matches (regex).
//
// JSONPath support is a simplified subset covering common patterns:
//   - $.field — top-level field access
//   - $.field.nested — nested field access
//   - $.array[0] — array index access
//   - $.array[*].field — array wildcard (returns first match)
type JSONPathEvaluator struct{}

// NewJSONPathEvaluator creates a new evaluator instance.
func NewJSONPathEvaluator() *JSONPathEvaluator {
	return &JSONPathEvaluator{}
}

// EvaluateAll checks all criteria against the response body.
// Returns true if ALL criteria match (AND logic).
func (e *JSONPathEvaluator) EvaluateAll(responseBody string, criteria []types.MatchCriteria) bool {
	if len(criteria) == 0 {
		return true // No criteria means always match.
	}

	for _, c := range criteria {
		if !e.Evaluate(responseBody, c) {
			return false
		}
	}
	return true
}

// Evaluate checks a single match criterion against the response body.
func (e *JSONPathEvaluator) Evaluate(responseBody string, criterion types.MatchCriteria) bool {
	extracted := e.Extract(responseBody, criterion.JSONPath)
	if extracted == nil {
		return false
	}

	return compare(extracted, criterion.Operator, criterion.Value)
}

// Extract retrieves a value from a JSON string using a JSONPath expression.
// Returns nil if the path cannot be resolved.
func (e *JSONPathEvaluator) Extract(jsonData, path string) any {
	if jsonData == "" || path == "" {
		return nil
	}

	var root any
	if err := json.Unmarshal([]byte(jsonData), &root); err != nil {
		return nil
	}

	return resolvePath(root, path)
}

// resolvePath navigates a parsed JSON structure using a simplified JSONPath.
func resolvePath(root any, path string) any {
	// Strip leading "$." prefix.
	if strings.HasPrefix(path, "$.") {
		path = path[2:]
	} else if path == "$" {
		return root
	}

	current := root
	segments := splitPath(path)

	for _, seg := range segments {
		if current == nil {
			return nil
		}

		// Check for array index: "field[0]" or "[0]".
		if idx, field, ok := parseArrayAccess(seg); ok {
			if field != "" {
				current = accessField(current, field)
				if current == nil {
					return nil
				}
			}
			arr, ok := current.([]any)
			if !ok {
				return nil
			}
			if idx == -1 {
				// Wildcard [*] — return the array for further navigation.
				current = arr
			} else if idx >= 0 && idx < len(arr) {
				current = arr[idx]
			} else {
				return nil
			}
			continue
		}

		current = accessField(current, seg)
	}

	return current
}

// splitPath splits a dotted JSONPath into segments, preserving bracketed indices.
func splitPath(path string) []string {
	var segments []string
	var buf strings.Builder
	bracketDepth := 0

	for _, ch := range path {
		switch ch {
		case '[':
			bracketDepth++
			buf.WriteRune(ch)
		case ']':
			bracketDepth--
			buf.WriteRune(ch)
		case '.':
			if bracketDepth == 0 {
				if buf.Len() > 0 {
					segments = append(segments, buf.String())
					buf.Reset()
				}
			} else {
				buf.WriteRune(ch)
			}
		default:
			buf.WriteRune(ch)
		}
	}
	if buf.Len() > 0 {
		segments = append(segments, buf.String())
	}
	return segments
}

// parseArrayAccess checks if a segment contains an array access pattern.
// Returns (index, fieldName, true) for patterns like "field[0]" or "[0]".
// Index -1 indicates wildcard [*].
func parseArrayAccess(seg string) (int, string, bool) {
	bracketIdx := strings.Index(seg, "[")
	if bracketIdx == -1 {
		return 0, "", false
	}

	field := seg[:bracketIdx]
	indexStr := strings.TrimSuffix(seg[bracketIdx+1:], "]")

	if indexStr == "*" {
		return -1, field, true
	}

	idx, err := strconv.Atoi(indexStr)
	if err != nil {
		return 0, "", false
	}
	return idx, field, true
}

// accessField retrieves a field from a map.
func accessField(data any, field string) any {
	switch v := data.(type) {
	case map[string]any:
		return v[field]
	default:
		return nil
	}
}

// compare applies the operator to the extracted value and the expected string value.
// Type coercion is applied: if the extracted value is numeric, the expected value
// is parsed as a number; if boolean, parsed as bool; otherwise string comparison.
func compare(extracted any, operator, expected string) bool {
	switch operator {
	case "eq":
		return toString(extracted) == expected
	case "ne":
		return toString(extracted) != expected
	case "gt":
		return numCompare(extracted, expected) > 0
	case "lt":
		return numCompare(extracted, expected) < 0
	case "gte":
		return numCompare(extracted, expected) >= 0
	case "lte":
		return numCompare(extracted, expected) <= 0
	case "contains":
		return strings.Contains(toString(extracted), expected)
	case "matches":
		re, err := regexp.Compile(expected)
		if err != nil {
			return false
		}
		return re.MatchString(toString(extracted))
	default:
		return false
	}
}

// toString converts any value to its string representation.
func toString(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case float64:
		if val == math.Trunc(val) {
			return strconv.FormatInt(int64(val), 10)
		}
		return strconv.FormatFloat(val, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(val)
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", val)
	}
}

// numCompare compares the extracted value as a number against the expected string.
// Returns -1, 0, or 1 (like strings.Compare but numeric).
// If either value is not numeric, falls back to string comparison.
func numCompare(extracted any, expected string) int {
	var extractedNum float64
	switch v := extracted.(type) {
	case float64:
		extractedNum = v
	case string:
		var err error
		extractedNum, err = strconv.ParseFloat(v, 64)
		if err != nil {
			return strings.Compare(toString(extracted), expected)
		}
	default:
		return strings.Compare(toString(extracted), expected)
	}

	expectedNum, err := strconv.ParseFloat(expected, 64)
	if err != nil {
		return strings.Compare(toString(extracted), expected)
	}

	diff := extractedNum - expectedNum
	if diff < 0 {
		return -1
	}
	if diff > 0 {
		return 1
	}
	return 0
}

// MatchCriteria is a convenience alias used internally by SensorManager.
// In API-facing code, use types.MatchCriteria directly.
type MatchCriteria = types.MatchCriteria
