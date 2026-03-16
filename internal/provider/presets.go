package provider

// Preset represents a known LLM provider configuration template.
type Preset struct {
	Kind     string   `json:"kind"`
	Name     string   `json:"name"`
	BaseURL  string   `json:"base_url"`
	Models   []string `json:"models"`
	NeedsKey bool     `json:"needs_key"`
}

// Presets returns all known provider presets.
func Presets() []Preset {
	return []Preset{
		{
			Kind:     "anthropic",
			Name:     "Anthropic",
			BaseURL:  "https://api.anthropic.com",
			Models:   []string{"claude-sonnet-4-20250514", "claude-opus-4-20250514", "claude-haiku-4-5-20251001"},
			NeedsKey: true,
		},
		{
			Kind:     "openai",
			Name:     "OpenAI",
			BaseURL:  "https://api.openai.com/v1",
			Models:   []string{"gpt-4o", "gpt-4o-mini", "o3", "o4-mini"},
			NeedsKey: true,
		},
		{
			Kind:     "ollama",
			Name:     "Ollama (Local)",
			BaseURL:  "http://localhost:11434",
			Models:   []string{"llama3.2", "mistral", "codellama", "phi3"},
			NeedsKey: false,
		},
		{
			Kind:     "azure",
			Name:     "Azure OpenAI",
			BaseURL:  "https://{deployment}.openai.azure.com",
			Models:   []string{"gpt-4o", "gpt-4o-mini"},
			NeedsKey: true,
		},
		{
			Kind:     "google",
			Name:     "Google Gemini",
			BaseURL:  "https://generativelanguage.googleapis.com",
			Models:   []string{"gemini-2.5-flash-preview-05-20", "gemini-2.5-pro-preview-05-06", "gemini-2.0-flash", "gemini-2.0-flash-lite", "gemini-1.5-pro", "gemini-1.5-flash"},
			NeedsKey: true,
		},
		{
			Kind:     "bedrock",
			Name:     "AWS Bedrock",
			BaseURL:  "https://bedrock-runtime.us-east-1.amazonaws.com",
			Models:   []string{"anthropic.claude-sonnet-4-20250514-v1:0", "anthropic.claude-haiku-4-5-20251001-v1:0", "amazon.nova-pro-v1:0", "amazon.nova-lite-v1:0", "meta.llama3-1-70b-instruct-v1:0"},
			NeedsKey: true,
		},
		{
			Kind:     "custom",
			Name:     "OpenAI Compatible API",
			BaseURL:  "",
			Models:   []string{},
			NeedsKey: false,
		},
	}
}
