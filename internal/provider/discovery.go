package provider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	v4signer "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/credentials"
)

// discoveryTimeout is the HTTP client timeout for model discovery requests.
const discoveryTimeout = 10 * time.Second

// DiscoverModels fetches the list of available models from a provider's API.
// Supported kinds: ollama, openai, anthropic, azure, custom.
// Custom (OpenAI Compatible API) providers attempt OpenAI-style discovery
// and fall back to an empty slice with a logged warning on failure.
func DiscoverModels(ctx context.Context, kind, baseURL, apiKey string) ([]string, error) {
	switch strings.ToLower(kind) {
	case "ollama":
		return discoverOllama(ctx, baseURL)
	case "openai":
		return discoverOpenAI(ctx, baseURL, apiKey)
	case "anthropic":
		return discoverAnthropic(ctx, baseURL, apiKey)
	case "azure":
		return discoverAzure(ctx, baseURL, apiKey)
	case "google":
		return discoverGoogle(ctx, baseURL, apiKey)
	case "bedrock":
		return discoverBedrock(ctx, baseURL, apiKey)
	case "custom":
		// Attempt OpenAI-compatible discovery; fall back to empty on failure.
		models, err := discoverOpenAI(ctx, baseURL, apiKey)
		if err != nil {
			slog.Warn("auto-discovery failed for OpenAI-compatible provider; add models manually",
				"base_url", baseURL,
				"error", err,
			)
			return []string{}, nil
		}
		return models, nil
	default:
		return nil, fmt.Errorf("provider.DiscoverModels: unsupported provider kind %q", kind)
	}
}

// ValidateModel checks whether a target model name exists in the given model
// list. The comparison is case-sensitive.
func ValidateModel(models []string, target string) bool {
	for _, m := range models {
		if m == target {
			return true
		}
	}
	return false
}

// -- Provider-specific discovery implementations ----------------------------

// ollamaTagsResponse represents the Ollama /api/tags JSON response.
type ollamaTagsResponse struct {
	Models []struct {
		Name string `json:"name"`
	} `json:"models"`
}

// discoverOllama fetches models from the Ollama local server.
// Endpoint: GET {baseURL}/api/tags
func discoverOllama(ctx context.Context, baseURL string) ([]string, error) {
	url := strings.TrimRight(baseURL, "/") + "/api/tags"

	body, err := doGet(ctx, url, nil)
	if err != nil {
		return nil, fmt.Errorf("provider.discoverOllama: %w", err)
	}

	var resp ollamaTagsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("provider.discoverOllama: parse response: %w", err)
	}

	models := make([]string, 0, len(resp.Models))
	for _, m := range resp.Models {
		if m.Name != "" {
			models = append(models, m.Name)
		}
	}
	sort.Strings(models)
	return models, nil
}

// dataModelsResponse represents the common {"data": [{"id": "..."}]} response
// shape used by OpenAI, Anthropic, and Azure model listing endpoints.
type dataModelsResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

// parseDataModels extracts sorted model IDs from the common data-array
// response format. It is shared by OpenAI, Anthropic, and Azure parsers.
func parseDataModels(body []byte, providerLabel string) ([]string, error) {
	var resp dataModelsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("provider.parseDataModels(%s): parse response: %w", providerLabel, err)
	}

	models := make([]string, 0, len(resp.Data))
	for _, m := range resp.Data {
		if m.ID != "" {
			models = append(models, m.ID)
		}
	}
	sort.Strings(models)
	return models, nil
}

// discoverOpenAI fetches models from OpenAI-compatible APIs.
// Endpoint: GET {baseURL}/models with Bearer token auth.
func discoverOpenAI(ctx context.Context, baseURL, apiKey string) ([]string, error) {
	url := strings.TrimRight(baseURL, "/") + "/models"

	headers := map[string]string{
		"Authorization": "Bearer " + apiKey,
	}

	body, err := doGet(ctx, url, headers)
	if err != nil {
		return nil, fmt.Errorf("provider.discoverOpenAI: %w", err)
	}

	return parseDataModels(body, "openai")
}

// discoverAnthropic fetches models from the Anthropic API.
// Endpoint: GET {baseURL}/v1/models with x-api-key and anthropic-version headers.
func discoverAnthropic(ctx context.Context, baseURL, apiKey string) ([]string, error) {
	url := strings.TrimRight(baseURL, "/") + "/v1/models"

	headers := map[string]string{
		"x-api-key":         apiKey,
		"anthropic-version": "2023-06-01",
	}

	body, err := doGet(ctx, url, headers)
	if err != nil {
		return nil, fmt.Errorf("provider.discoverAnthropic: %w", err)
	}

	return parseDataModels(body, "anthropic")
}

// discoverAzure fetches models from the Azure OpenAI API.
// Endpoint: GET {baseURL}/openai/models?api-version=2024-02-01 with api-key header.
func discoverAzure(ctx context.Context, baseURL, apiKey string) ([]string, error) {
	url := strings.TrimRight(baseURL, "/") + "/openai/models?api-version=2024-02-01"

	headers := map[string]string{
		"api-key": apiKey,
	}

	body, err := doGet(ctx, url, headers)
	if err != nil {
		return nil, fmt.Errorf("provider.discoverAzure: %w", err)
	}

	return parseDataModels(body, "azure")
}

// googleModelsResponse represents the Google Gemini /v1beta/models JSON response.
type googleModelsResponse struct {
	Models []struct {
		Name string `json:"name"` // e.g. "models/gemini-2.0-flash"
	} `json:"models"`
}

// discoverGoogle fetches models from the Google Gemini API.
// Endpoint: GET {baseURL}/v1beta/models with x-goog-api-key header.
// Model names are returned without the "models/" prefix for consistency.
func discoverGoogle(ctx context.Context, baseURL, apiKey string) ([]string, error) {
	url := strings.TrimRight(baseURL, "/") + "/v1beta/models"

	headers := map[string]string{
		"x-goog-api-key": apiKey,
	}

	body, err := doGet(ctx, url, headers)
	if err != nil {
		return nil, fmt.Errorf("provider.discoverGoogle: %w", err)
	}

	var resp googleModelsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("provider.discoverGoogle: parse response: %w", err)
	}

	models := make([]string, 0, len(resp.Models))
	for _, m := range resp.Models {
		name := m.Name
		// Strip the "models/" prefix so IDs are clean (e.g. "gemini-2.0-flash").
		name = strings.TrimPrefix(name, "models/")
		if name != "" {
			models = append(models, name)
		}
	}
	sort.Strings(models)
	return models, nil
}

// discoverBedrock fetches available foundation models from AWS Bedrock.
// The base URL points to bedrock-runtime; discovery uses the bedrock control plane.
// Endpoint: GET https://bedrock.{region}.amazonaws.com/foundation-models
// Auth is handled via aws-sdk-go-v2 Signature V4 signer.
func discoverBedrock(ctx context.Context, baseURL, apiKey string) ([]string, error) {
	accessKey, secretKey, err := parseBedrockCredentials(apiKey)
	if err != nil {
		return nil, fmt.Errorf("provider.discoverBedrock: %w", err)
	}

	region := extractBedrockRegion(baseURL)
	// Discovery uses the bedrock control plane, not bedrock-runtime.
	discoverURL := fmt.Sprintf("https://bedrock.%s.amazonaws.com/foundation-models", region)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, discoverURL, nil)
	if err != nil {
		return nil, fmt.Errorf("provider.discoverBedrock: build request: %w", err)
	}

	// Sign the request using the AWS SDK v4 signer.
	credsProvider := credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")
	creds, err := credsProvider.Retrieve(ctx)
	if err != nil {
		return nil, fmt.Errorf("provider.discoverBedrock: retrieve credentials: %w", err)
	}
	payloadHash := sha256Hex(nil)
	signer := v4signer.NewSigner()
	if err := signer.SignHTTP(ctx, creds, httpReq, payloadHash, "bedrock", region, time.Now()); err != nil {
		return nil, fmt.Errorf("provider.discoverBedrock: sign request: %w", err)
	}

	client := &http.Client{Timeout: discoveryTimeout}
	httpResp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("provider.discoverBedrock: connect: %w", err)
	}
	defer func() {
		if cerr := httpResp.Body.Close(); cerr != nil {
			slog.Debug("discoverBedrock: failed to close response body", "error", cerr)
		}
	}()

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("provider.discoverBedrock: read response: %w", err)
	}

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		snippet := string(body)
		if len(snippet) > 512 {
			snippet = snippet[:512] + "..."
		}
		return nil, fmt.Errorf("provider.discoverBedrock: HTTP %d: %s", httpResp.StatusCode, snippet)
	}

	var resp struct {
		ModelSummaries []struct {
			ModelID string `json:"modelId"`
		} `json:"modelSummaries"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("provider.discoverBedrock: parse models: %w", err)
	}

	models := make([]string, 0, len(resp.ModelSummaries))
	for _, m := range resp.ModelSummaries {
		if m.ModelID != "" {
			models = append(models, m.ModelID)
		}
	}
	sort.Strings(models)
	return models, nil
}

// sha256Hex computes the SHA-256 hash of data and returns it as a hex string.
// Used for the AWS Signature V4 payload hash.
func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// -- HTTP helper ------------------------------------------------------------

// doGet performs an HTTP GET request with the given headers and returns the
// response body. It enforces the discovery timeout and returns descriptive
// errors for connection failures, non-2xx responses, and read errors.
func doGet(ctx context.Context, url string, headers map[string]string) ([]byte, error) {
	client := &http.Client{Timeout: discoveryTimeout}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("provider.doGet: build request for %s: %w", url, err)
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("provider.doGet: connect to %s: %w", url, err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			slog.Debug("doGet: failed to close response body", "error", cerr)
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("provider.doGet: read response from %s: %w", url, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Include up to 512 bytes of the response body for diagnostic context.
		snippet := string(body)
		if len(snippet) > 512 {
			snippet = snippet[:512] + "..."
		}
		return nil, fmt.Errorf("provider.doGet: HTTP %d from %s: %s", resp.StatusCode, url, snippet)
	}

	return body, nil
}
