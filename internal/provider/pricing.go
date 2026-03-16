package provider

import "strings"

// tokenRate holds the per-million-token pricing for a model in USD.
type tokenRate struct {
	InputPerMillion  float64
	OutputPerMillion float64
}

// providerPricing maps provider kind -> model prefix -> token rates.
// Rates are sourced from public pricing pages (as of 2025-05).
// Models are matched by prefix to handle versioned suffixes (e.g.,
// "claude-sonnet-4-20250514" matches the "claude-sonnet-4" entry).
var providerPricing = map[string][]struct {
	Prefix string
	Rate   tokenRate
}{
	"anthropic": {
		{Prefix: "claude-opus-4", Rate: tokenRate{InputPerMillion: 15.0, OutputPerMillion: 75.0}},
		{Prefix: "claude-sonnet-4", Rate: tokenRate{InputPerMillion: 3.0, OutputPerMillion: 15.0}},
		{Prefix: "claude-haiku-4", Rate: tokenRate{InputPerMillion: 0.80, OutputPerMillion: 4.0}},
		{Prefix: "claude-3-5-sonnet", Rate: tokenRate{InputPerMillion: 3.0, OutputPerMillion: 15.0}},
		{Prefix: "claude-3-5-haiku", Rate: tokenRate{InputPerMillion: 0.80, OutputPerMillion: 4.0}},
		{Prefix: "claude-3-opus", Rate: tokenRate{InputPerMillion: 15.0, OutputPerMillion: 75.0}},
	},
	"openai": {
		{Prefix: "o3", Rate: tokenRate{InputPerMillion: 10.0, OutputPerMillion: 40.0}},
		{Prefix: "o4-mini", Rate: tokenRate{InputPerMillion: 1.10, OutputPerMillion: 4.40}},
		{Prefix: "gpt-4o-mini", Rate: tokenRate{InputPerMillion: 0.15, OutputPerMillion: 0.60}},
		{Prefix: "gpt-4o", Rate: tokenRate{InputPerMillion: 2.50, OutputPerMillion: 10.0}},
		{Prefix: "gpt-4-turbo", Rate: tokenRate{InputPerMillion: 10.0, OutputPerMillion: 30.0}},
		{Prefix: "gpt-4", Rate: tokenRate{InputPerMillion: 30.0, OutputPerMillion: 60.0}},
		{Prefix: "gpt-3.5-turbo", Rate: tokenRate{InputPerMillion: 0.50, OutputPerMillion: 1.50}},
	},
	"google": {
		{Prefix: "gemini-2.5-pro", Rate: tokenRate{InputPerMillion: 1.25, OutputPerMillion: 10.0}},
		{Prefix: "gemini-2.5-flash", Rate: tokenRate{InputPerMillion: 0.15, OutputPerMillion: 0.60}},
		{Prefix: "gemini-2.0-flash-lite", Rate: tokenRate{InputPerMillion: 0.075, OutputPerMillion: 0.30}},
		{Prefix: "gemini-2.0-flash", Rate: tokenRate{InputPerMillion: 0.10, OutputPerMillion: 0.40}},
		{Prefix: "gemini-1.5-pro", Rate: tokenRate{InputPerMillion: 1.25, OutputPerMillion: 5.0}},
		{Prefix: "gemini-1.5-flash", Rate: tokenRate{InputPerMillion: 0.075, OutputPerMillion: 0.30}},
	},
	"azure": {
		// Azure uses the same OpenAI models at parity pricing.
		{Prefix: "gpt-4o-mini", Rate: tokenRate{InputPerMillion: 0.15, OutputPerMillion: 0.60}},
		{Prefix: "gpt-4o", Rate: tokenRate{InputPerMillion: 2.50, OutputPerMillion: 10.0}},
		{Prefix: "gpt-4", Rate: tokenRate{InputPerMillion: 30.0, OutputPerMillion: 60.0}},
	},
	"bedrock": {
		// Bedrock Anthropic models — pricing at Bedrock rates.
		{Prefix: "anthropic.claude-sonnet-4", Rate: tokenRate{InputPerMillion: 3.0, OutputPerMillion: 15.0}},
		{Prefix: "anthropic.claude-haiku-4", Rate: tokenRate{InputPerMillion: 0.80, OutputPerMillion: 4.0}},
		{Prefix: "anthropic.claude-3", Rate: tokenRate{InputPerMillion: 3.0, OutputPerMillion: 15.0}},
		{Prefix: "amazon.nova-pro", Rate: tokenRate{InputPerMillion: 0.80, OutputPerMillion: 3.20}},
		{Prefix: "amazon.nova-lite", Rate: tokenRate{InputPerMillion: 0.06, OutputPerMillion: 0.24}},
		{Prefix: "meta.llama3", Rate: tokenRate{InputPerMillion: 0.72, OutputPerMillion: 0.72}},
	},
}

// defaultACURate is the fallback cost-per-token when the provider/model is
// unknown. Expressed as a reasonable mid-range estimate in USD per token.
// This corresponds roughly to $2/1M input + $8/1M output averaged.
const defaultACURate = 0.000005

// EstimateProviderCost calculates the USD cost for a completion based on the
// provider kind, model name, and token counts. It uses public pricing tables
// for known providers and falls back to a conservative ACU-based estimate for
// unknown or custom providers.
//
// Returns 0.0 if both token counts are zero.
func EstimateProviderCost(kind string, model string, tokensIn int64, tokensOut int64) float64 {
	if tokensIn == 0 && tokensOut == 0 {
		return 0.0
	}

	kindLower := strings.ToLower(kind)
	modelLower := strings.ToLower(model)

	// Look up pricing entries for this provider kind.
	entries, ok := providerPricing[kindLower]
	if ok {
		for _, entry := range entries {
			if strings.HasPrefix(modelLower, strings.ToLower(entry.Prefix)) {
				inCost := float64(tokensIn) * entry.Rate.InputPerMillion / 1_000_000.0
				outCost := float64(tokensOut) * entry.Rate.OutputPerMillion / 1_000_000.0
				return inCost + outCost
			}
		}
	}

	// Fallback: ACU-based estimation for custom/unknown providers.
	return float64(tokensIn+tokensOut) * defaultACURate
}

// LookupTokenRate returns the per-million-token rates for a known provider/model
// combination, or nil if no pricing data is available.
func LookupTokenRate(kind string, model string) *tokenRate {
	kindLower := strings.ToLower(kind)
	modelLower := strings.ToLower(model)

	entries, ok := providerPricing[kindLower]
	if !ok {
		return nil
	}
	for _, entry := range entries {
		if strings.HasPrefix(modelLower, strings.ToLower(entry.Prefix)) {
			rate := entry.Rate
			return &rate
		}
	}
	return nil
}
