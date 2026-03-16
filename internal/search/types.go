package search

// SearchResult represents a single search hit with a fused relevance score.
// When produced by BM25-only search, Score holds the BM25 rank. When
// produced by RRF fusion, Score is the reciprocal-rank-fused value (higher
// is more relevant).
type SearchResult struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Kind        string  `json:"kind"`
	FilePath    string  `json:"file_path"`
	StartLine   int     `json:"start_line"`
	EndLine     int     `json:"end_line"`
	Signature   string  `json:"signature,omitempty"`
	WorkspaceID string  `json:"workspace_id"`
	Score       float64 `json:"score"`
}

// VectorResult represents a vector similarity search hit returned by a
// VectorSearcher implementation. Distance is the cosine (or L2) distance
// between the query embedding and the stored vector.
type VectorResult struct {
	ID          string
	Name        string
	Kind        string
	FilePath    string
	WorkspaceID string
	Distance    float64
}

// Query specifies search parameters for the HybridSearcher.
type Query struct {
	// Text is the user's search query string.
	Text string
	// Workspaces restricts the search to the given workspace IDs.
	Workspaces []string
	// Kind optionally filters results by symbol kind (e.g. "function", "struct").
	Kind string
	// Limit caps the number of results returned. Zero means default (50).
	Limit int
}

// Config holds search engine configuration. All fields have sensible
// defaults — use DefaultConfig() to obtain them.
type Config struct {
	// EnableVector enables the vector search branch and RRF fusion.
	// When false, only BM25 (FTS5/LIKE) results are returned.
	EnableVector bool `yaml:"enable_vector"`
	// EmbeddingModel is the filesystem path to the ONNX model file.
	EmbeddingModel string `yaml:"embedding_model"`
	// EmbeddingDim is the embedding vector dimension. Default: 384.
	EmbeddingDim int `yaml:"embedding_dim"`
	// FusionK is the RRF k parameter controlling rank-position impact.
	// Higher values produce more uniform fusion. Default: 60.
	FusionK int `yaml:"fusion_k"`
	// AlphaWeight controls the balance between vector and BM25 scores in
	// weighted RRF fusion. 0.0 = equal weighting (classic RRF),
	// 0.6 = 60% vector weight / 40% BM25 weight (semantic-aware).
	// Only used when EnableVector is true. Default: 0.6.
	AlphaWeight float64 `yaml:"alpha_weight"`
	// DynamicK enables dynamic k computation as min(limit/2, FusionK).
	// When false, FusionK is used directly. Default: false.
	DynamicK bool `yaml:"dynamic_k"`
	// CandidatePoolSize is the number of items fetched from each search
	// leg before fusion. Larger pools improve recall at the cost of
	// latency. Default: 100.
	CandidatePoolSize int `yaml:"candidate_pool_size"`
}

// DefaultConfig returns a Config with sensible production defaults.
// Vector search is disabled by default because it requires an embedding
// engine (ONNX Runtime) that may not be available.
func DefaultConfig() Config {
	return Config{
		EmbeddingDim:      384,
		FusionK:           60,
		AlphaWeight:       0.6,
		CandidatePoolSize: 100,
	}
}
