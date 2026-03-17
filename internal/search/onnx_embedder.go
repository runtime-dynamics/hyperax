package search

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"os"
	"sync"
)

// ONNXEmbedder implements the Embedder interface using ONNX Runtime for
// inference with the all-MiniLM-L6-v2 model (384-dimensional embeddings).
//
// The embedder uses lazy initialisation: the ONNX model session is created
// on the first call to Embed(), not at construction time. This allows the
// application to start quickly and degrade gracefully if the model file is
// missing or ONNX Runtime is not available.
//
// When the model cannot be loaded, the embedder records the failure and all
// subsequent Embed() calls return an error, triggering the HybridSearcher's
// graceful degradation to BM25-only search.
type ONNXEmbedder struct {
	modelPath string
	dim       int
	logger    *slog.Logger

	once       sync.Once
	session    *onnxSession // nil until first Embed()
	initErr    error        // non-nil if session creation failed
	normalise  bool         // L2-normalise output vectors
}

// onnxSession wraps the ONNX inference session state.
// This is a placeholder struct — the actual ONNX Runtime binding will be
// plugged in via the onnx_runtime.go file behind a build tag. For now,
// the embedder operates in "stub" mode and returns an error indicating
// that ONNX Runtime is not compiled in.
type onnxSession struct {
	// Placeholder: real session would hold ort.Session, input/output names, etc.
}

// ONNXEmbedderConfig holds configuration for the ONNX embedder.
type ONNXEmbedderConfig struct {
	// ModelPath is the filesystem path to the .onnx model file.
	ModelPath string
	// Dim is the expected embedding dimension. Default: 384.
	Dim int
	// Normalise enables L2 normalisation of output vectors. Default: true.
	Normalise bool
	// Logger is used for debug/warn messages during initialisation.
	Logger *slog.Logger
}

// NewONNXEmbedder creates a lazy-loading ONNX embedder. The model is not
// loaded until the first call to Embed(). If modelPath does not exist or
// is empty, the embedder degrades to returning errors (triggering BM25
// fallback in the HybridSearcher).
//
// Returns nil if modelPath is empty, signalling that no ONNX embedder
// should be configured.
func NewONNXEmbedder(cfg ONNXEmbedderConfig) *ONNXEmbedder {
	if cfg.ModelPath == "" {
		return nil
	}
	if cfg.Dim <= 0 {
		cfg.Dim = 384
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &ONNXEmbedder{
		modelPath: cfg.ModelPath,
		dim:       cfg.Dim,
		logger:    cfg.Logger,
		normalise: cfg.Normalise,
	}
}

// Embed converts the input text into a dense float32 embedding vector.
// On the first call, the ONNX model is loaded lazily. If loading fails,
// all subsequent calls return an error.
func (e *ONNXEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	e.once.Do(e.initSession)

	if e.initErr != nil {
		return nil, e.initErr
	}

	if text == "" {
		return nil, fmt.Errorf("search.ONNXEmbedder.Embed: empty input text")
	}

	// Run inference.
	embedding, err := e.runInference(ctx, text) //nolint:staticcheck // SA4023: stub always errors; real impl behind build tag
	if err != nil {                              //nolint:staticcheck // SA4023: see above
		return nil, fmt.Errorf("search.ONNXEmbedder.Embed: %w", err)
	}

	// Validate dimension.
	if len(embedding) != e.dim {
		return nil, fmt.Errorf("search.ONNXEmbedder.Embed: dimension mismatch: got %d, expected %d", len(embedding), e.dim)
	}

	// L2 normalise if requested.
	if e.normalise {
		embedding = l2Normalise(embedding)
	}

	return embedding, nil
}

// Close releases the ONNX Runtime session and any associated resources.
func (e *ONNXEmbedder) Close() error {
	if e.session != nil {
		e.logger.Debug("onnx session closed")
		e.session = nil
	}
	return nil
}

// initSession performs lazy one-time initialisation of the ONNX Runtime session.
// Called via sync.Once on the first Embed() call.
func (e *ONNXEmbedder) initSession() {
	// Check if model file exists.
	if _, err := os.Stat(e.modelPath); err != nil {
		e.initErr = fmt.Errorf("search.ONNXEmbedder.initSession: model not found at %q: %w", e.modelPath, err)
		e.logger.Warn("ONNX embedder disabled: model file not found",
			"path", e.modelPath,
			"error", err,
		)
		return
	}

	// Attempt to create the ONNX Runtime session.
	session, err := createONNXSession(e.modelPath, e.dim)
	if err != nil {
		e.initErr = fmt.Errorf("search.ONNXEmbedder.initSession: %w", err)
		e.logger.Warn("ONNX embedder disabled: session creation failed",
			"path", e.modelPath,
			"error", err,
		)
		return
	}

	e.session = session
	e.logger.Info("ONNX embedder initialised",
		"model", e.modelPath,
		"dim", e.dim,
		"normalise", e.normalise,
	)
}

// runInference executes the model on the input text and returns the embedding.
// This is the hot path — called on every Embed() after initialisation.
func (e *ONNXEmbedder) runInference(_ context.Context, _ string) ([]float32, error) { //nolint:staticcheck // SA4023: stub; real impl behind build tag
	// Stub implementation: ONNX Runtime native bindings are not compiled in.
	// When the yalue/onnxruntime_go package is available (behind build tag),
	// this function will be replaced with actual inference logic that:
	//   1. Tokenizes the input text (WordPiece tokenizer)
	//   2. Creates input tensors (input_ids, attention_mask, token_type_ids)
	//   3. Runs the ONNX session
	//   4. Mean-pools the token embeddings from the last hidden state
	//
	// For now, return an error to trigger graceful degradation.
	return nil, fmt.Errorf("onnx runtime not available: compile with -tags onnx to enable native inference")
}

// createONNXSession creates an ONNX Runtime inference session.
// Stub: returns a placeholder session that will error on inference.
func createONNXSession(modelPath string, dim int) (*onnxSession, error) {
	// Stub: validate model file is readable.
	f, err := os.Open(modelPath)
	if err != nil {
		return nil, fmt.Errorf("search.createONNXSession: %w", err)
	}
	_ = f.Close()

	return &onnxSession{}, nil
}

// l2Normalise normalises a vector to unit length (L2 norm = 1.0).
// This is important for cosine similarity: when vectors are L2-normalised,
// cosine similarity equals the dot product, making distance computation faster.
func l2Normalise(v []float32) []float32 {
	var sumSq float64
	for _, x := range v {
		sumSq += float64(x) * float64(x)
	}
	norm := math.Sqrt(sumSq)
	if norm == 0 {
		return v
	}

	result := make([]float32, len(v))
	for i, x := range v {
		result[i] = float32(float64(x) / norm)
	}
	return result
}

// CosineSimilarity computes the cosine similarity between two vectors.
// Both vectors must be the same length. Returns a value in [-1, 1].
// For L2-normalised vectors, this is simply the dot product.
func CosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}
	var dot float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
	}
	return dot
}

// CosineDistance returns 1 - CosineSimilarity, useful for ranking where
// lower distance means higher similarity.
func CosineDistance(a, b []float32) float64 {
	return 1.0 - CosineSimilarity(a, b)
}
