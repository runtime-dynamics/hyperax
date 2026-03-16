package search

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestNewONNXEmbedder_EmptyPath(t *testing.T) {
	e := NewONNXEmbedder(ONNXEmbedderConfig{ModelPath: ""})
	if e != nil {
		t.Error("expected nil for empty model path")
	}
}

func TestNewONNXEmbedder_DefaultDim(t *testing.T) {
	e := NewONNXEmbedder(ONNXEmbedderConfig{ModelPath: "/nonexistent"})
	if e.dim != 384 {
		t.Errorf("expected default dim 384, got %d", e.dim)
	}
}

func TestONNXEmbedder_MissingModel(t *testing.T) {
	e := NewONNXEmbedder(ONNXEmbedderConfig{ModelPath: "/nonexistent/model.onnx"})
	ctx := context.Background()

	_, err := e.Embed(ctx, "hello")
	if err == nil {
		t.Error("expected error for missing model file")
	}
}

func TestONNXEmbedder_StubReturnsError(t *testing.T) {
	// Create a dummy file so initSession doesn't fail on stat.
	dir := t.TempDir()
	modelPath := filepath.Join(dir, "model.onnx")
	_ = os.WriteFile(modelPath, []byte("dummy"), 0644)

	e := NewONNXEmbedder(ONNXEmbedderConfig{ModelPath: modelPath, Dim: 4})
	ctx := context.Background()

	// The stub runInference returns an error about onnx runtime not available.
	_, err := e.Embed(ctx, "hello")
	if err == nil {
		t.Error("expected error from stub inference")
	}
}

func TestONNXEmbedder_EmptyText(t *testing.T) {
	dir := t.TempDir()
	modelPath := filepath.Join(dir, "model.onnx")
	_ = os.WriteFile(modelPath, []byte("dummy"), 0644)

	e := NewONNXEmbedder(ONNXEmbedderConfig{ModelPath: modelPath})
	ctx := context.Background()

	// Empty text should error before inference.
	// Note: initSession will succeed (stub creates placeholder session),
	// but the stub inference returns error anyway. The empty text check
	// happens after initSession.
	_, err := e.Embed(ctx, "")
	if err == nil {
		t.Error("expected error for empty text")
	}
}

func TestONNXEmbedder_Close(t *testing.T) {
	e := NewONNXEmbedder(ONNXEmbedderConfig{ModelPath: "/nonexistent"})
	err := e.Close()
	if err != nil {
		t.Errorf("close: %v", err)
	}
}

// ---------- L2 normalisation ----------

func TestL2Normalise(t *testing.T) {
	v := []float32{3, 4}
	normed := l2Normalise(v)

	// Expected: [0.6, 0.8] since norm = 5.
	if math.Abs(float64(normed[0])-0.6) > 1e-5 {
		t.Errorf("expected normed[0]=0.6, got %f", normed[0])
	}
	if math.Abs(float64(normed[1])-0.8) > 1e-5 {
		t.Errorf("expected normed[1]=0.8, got %f", normed[1])
	}

	// Verify L2 norm is 1.0.
	var sumSq float64
	for _, x := range normed {
		sumSq += float64(x) * float64(x)
	}
	if math.Abs(sumSq-1.0) > 1e-5 {
		t.Errorf("expected unit norm, got %f", math.Sqrt(sumSq))
	}
}

func TestL2Normalise_ZeroVector(t *testing.T) {
	v := []float32{0, 0, 0}
	normed := l2Normalise(v)
	// Zero vector should be returned unchanged.
	for i, x := range normed {
		if x != 0 {
			t.Errorf("expected 0 at index %d, got %f", i, x)
		}
	}
}
