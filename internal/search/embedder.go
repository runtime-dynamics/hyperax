package search

import (
	"context"
	"fmt"
)

// Embedder generates vector embeddings from text. Implementations wrap a
// model inference engine (e.g. ONNX Runtime). The interface is intentionally
// narrow so that callers can swap implementations without changing search
// logic.
type Embedder interface {
	// Embed converts the input text into a dense float32 vector.
	// The returned slice length must equal Config.EmbeddingDim.
	Embed(ctx context.Context, text string) ([]float32, error)

	// Close releases any resources held by the embedder (model sessions,
	// GPU memory, etc.).
	Close() error
}

// NoOpEmbedder always returns an error, triggering graceful degradation to
// BM25-only search. It is the default embedder when no ONNX model is
// configured.
type NoOpEmbedder struct{}

// Embed returns an error indicating that vector search is not available.
func (NoOpEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	return nil, fmt.Errorf("vector search not available: no embedding engine configured")
}

// Close is a no-op for the NoOpEmbedder.
func (NoOpEmbedder) Close() error { return nil }
