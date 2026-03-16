package workflow

import (
	"testing"
)

func TestCELEvaluator_EmptyExpression(t *testing.T) {
	eval := NewCELEvaluator()
	result, err := eval.Evaluate("", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result {
		t.Error("empty expression should return true")
	}
}

func TestCELEvaluator_TrueExpression(t *testing.T) {
	eval := NewCELEvaluator()
	result, err := eval.Evaluate("ctx.enabled == true", map[string]interface{}{
		"enabled": true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result {
		t.Error("expression should evaluate to true")
	}
}

func TestCELEvaluator_FalseExpression(t *testing.T) {
	eval := NewCELEvaluator()
	result, err := eval.Evaluate("ctx.enabled == true", map[string]interface{}{
		"enabled": false,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result {
		t.Error("expression should evaluate to false")
	}
}

func TestCELEvaluator_NumericComparison(t *testing.T) {
	eval := NewCELEvaluator()
	result, err := eval.Evaluate("ctx.count > 5", map[string]interface{}{
		"count": 10,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result {
		t.Error("10 > 5 should be true")
	}
}

func TestCELEvaluator_StringComparison(t *testing.T) {
	eval := NewCELEvaluator()
	result, err := eval.Evaluate(`ctx.env == "production"`, map[string]interface{}{
		"env": "production",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result {
		t.Error("string comparison should be true")
	}
}

func TestCELEvaluator_InvalidExpression(t *testing.T) {
	eval := NewCELEvaluator()
	_, err := eval.Evaluate("this is not valid CEL !!!", map[string]interface{}{})
	if err == nil {
		t.Error("expected error for invalid expression")
	}
}

func TestCELEvaluator_NonBoolResult(t *testing.T) {
	eval := NewCELEvaluator()
	_, err := eval.Evaluate(`"hello"`, map[string]interface{}{})
	if err == nil {
		t.Error("expected error for non-bool result")
	}
}

func TestCELEvaluator_LogicalAnd(t *testing.T) {
	eval := NewCELEvaluator()
	result, err := eval.Evaluate("ctx.a == true && ctx.b == true", map[string]interface{}{
		"a": true,
		"b": true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result {
		t.Error("both true with AND should be true")
	}
}

func TestCELEvaluator_LogicalOr(t *testing.T) {
	eval := NewCELEvaluator()
	result, err := eval.Evaluate("ctx.a == true || ctx.b == true", map[string]interface{}{
		"a": false,
		"b": true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result {
		t.Error("one true with OR should be true")
	}
}

func TestCELEvaluator_NilVariables(t *testing.T) {
	eval := NewCELEvaluator()
	// Expression that doesn't reference ctx should work with nil variables.
	result, err := eval.Evaluate("true", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result {
		t.Error("literal true should evaluate to true")
	}
}
