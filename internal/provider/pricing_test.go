package provider

import (
	"math"
	"testing"
)

func TestEstimateProviderCost_Anthropic(t *testing.T) {
	// claude-opus-4: $15/$75 per 1M tokens
	cost := EstimateProviderCost("anthropic", "claude-opus-4-20250514", 1000, 500)
	// Expected: (1000 * 15 / 1M) + (500 * 75 / 1M) = 0.015 + 0.0375 = 0.0525
	expected := 0.0525
	if math.Abs(cost-expected) > 0.0001 {
		t.Errorf("opus cost = %f, want %f", cost, expected)
	}

	// claude-sonnet-4: $3/$15 per 1M tokens
	cost = EstimateProviderCost("anthropic", "claude-sonnet-4-20250514", 10000, 2000)
	expected = (10000.0*3.0 + 2000.0*15.0) / 1_000_000.0
	if math.Abs(cost-expected) > 0.0001 {
		t.Errorf("sonnet cost = %f, want %f", cost, expected)
	}

	// claude-haiku-4: $0.80/$4 per 1M tokens
	cost = EstimateProviderCost("anthropic", "claude-haiku-4-5-20251001", 50000, 10000)
	expected = (50000.0*0.80 + 10000.0*4.0) / 1_000_000.0
	if math.Abs(cost-expected) > 0.0001 {
		t.Errorf("haiku cost = %f, want %f", cost, expected)
	}
}

func TestEstimateProviderCost_OpenAI(t *testing.T) {
	// gpt-4o: $2.50/$10 per 1M tokens
	cost := EstimateProviderCost("openai", "gpt-4o", 5000, 1000)
	expected := (5000.0*2.50 + 1000.0*10.0) / 1_000_000.0
	if math.Abs(cost-expected) > 0.0001 {
		t.Errorf("gpt-4o cost = %f, want %f", cost, expected)
	}

	// gpt-4o-mini: $0.15/$0.60
	cost = EstimateProviderCost("openai", "gpt-4o-mini", 100000, 50000)
	expected = (100000.0*0.15 + 50000.0*0.60) / 1_000_000.0
	if math.Abs(cost-expected) > 0.0001 {
		t.Errorf("gpt-4o-mini cost = %f, want %f", cost, expected)
	}

	// o3: $10/$40
	cost = EstimateProviderCost("openai", "o3", 2000, 1000)
	expected = (2000.0*10.0 + 1000.0*40.0) / 1_000_000.0
	if math.Abs(cost-expected) > 0.0001 {
		t.Errorf("o3 cost = %f, want %f", cost, expected)
	}
}

func TestEstimateProviderCost_Google(t *testing.T) {
	// gemini-2.5-pro: $1.25/$10
	cost := EstimateProviderCost("google", "gemini-2.5-pro-preview-05-06", 10000, 5000)
	expected := (10000.0*1.25 + 5000.0*10.0) / 1_000_000.0
	if math.Abs(cost-expected) > 0.0001 {
		t.Errorf("gemini-2.5-pro cost = %f, want %f", cost, expected)
	}

	// gemini-2.5-flash: $0.15/$0.60
	cost = EstimateProviderCost("google", "gemini-2.5-flash-preview-05-20", 100000, 30000)
	expected = (100000.0*0.15 + 30000.0*0.60) / 1_000_000.0
	if math.Abs(cost-expected) > 0.0001 {
		t.Errorf("gemini-2.5-flash cost = %f, want %f", cost, expected)
	}
}

func TestEstimateProviderCost_ZeroTokens(t *testing.T) {
	cost := EstimateProviderCost("anthropic", "claude-opus-4-20250514", 0, 0)
	if cost != 0.0 {
		t.Errorf("expected 0.0 for zero tokens, got %f", cost)
	}
}

func TestEstimateProviderCost_UnknownProvider(t *testing.T) {
	cost := EstimateProviderCost("custom", "my-model", 10000, 5000)
	// Fallback: (10000+5000) * 0.000005 = 0.075
	expected := 15000.0 * defaultACURate
	if math.Abs(cost-expected) > 0.0001 {
		t.Errorf("custom fallback cost = %f, want %f", cost, expected)
	}
}

func TestEstimateProviderCost_UnknownModel(t *testing.T) {
	// Known provider but unknown model — should fall back to ACU.
	cost := EstimateProviderCost("anthropic", "claude-9000-turbo", 10000, 5000)
	expected := 15000.0 * defaultACURate
	if math.Abs(cost-expected) > 0.0001 {
		t.Errorf("unknown model fallback = %f, want %f", cost, expected)
	}
}

func TestEstimateProviderCost_CaseInsensitive(t *testing.T) {
	cost1 := EstimateProviderCost("Anthropic", "Claude-Opus-4-20250514", 1000, 500)
	cost2 := EstimateProviderCost("anthropic", "claude-opus-4-20250514", 1000, 500)
	if cost1 != cost2 {
		t.Errorf("case sensitivity mismatch: %f vs %f", cost1, cost2)
	}
}

func TestEstimateProviderCost_Bedrock(t *testing.T) {
	cost := EstimateProviderCost("bedrock", "anthropic.claude-sonnet-4-20250514-v1:0", 10000, 2000)
	expected := (10000.0*3.0 + 2000.0*15.0) / 1_000_000.0
	if math.Abs(cost-expected) > 0.0001 {
		t.Errorf("bedrock claude-sonnet cost = %f, want %f", cost, expected)
	}
}

func TestLookupTokenRate_Known(t *testing.T) {
	rate := LookupTokenRate("anthropic", "claude-opus-4-20250514")
	if rate == nil {
		t.Fatal("expected non-nil rate for claude-opus-4")
	}
	if rate.InputPerMillion != 15.0 {
		t.Errorf("input rate = %f, want 15.0", rate.InputPerMillion)
	}
	if rate.OutputPerMillion != 75.0 {
		t.Errorf("output rate = %f, want 75.0", rate.OutputPerMillion)
	}
}

func TestLookupTokenRate_Unknown(t *testing.T) {
	rate := LookupTokenRate("custom", "whatever")
	if rate != nil {
		t.Error("expected nil rate for unknown provider/model")
	}
}
