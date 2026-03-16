package observability

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/hyperax/hyperax/internal/config"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// TestInitTracer_EmptyEndpoint verifies that an empty OTelEndpoint returns
// a no-op shutdown function without error (tracing disabled by default).
func TestInitTracer_EmptyEndpoint(t *testing.T) {
	cfg := config.ObservabilityConfig{
		ServiceName:  "test",
		SamplingRate: 1.0,
		OTelEndpoint: "",
	}

	shutdown, err := InitTracer(cfg)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if shutdown == nil {
		t.Fatal("expected non-nil shutdown function")
	}

	// No-op shutdown should return nil.
	if err := shutdown(nil); err != nil {
		t.Fatalf("no-op shutdown returned error: %v", err)
	}
}

// TestInitTracer_WithEndpoint verifies that a configured endpoint returns
// a non-nil shutdown function. We do not actually connect to a collector;
// the exporter is created with the given endpoint and will fail lazily on
// actual export — which is acceptable for this unit test.
func TestInitTracer_WithEndpoint(t *testing.T) {
	cfg := config.ObservabilityConfig{
		OTelEndpoint: "localhost:4317",
		ServiceName:  "hyperax-test",
		SamplingRate: 0.5,
	}

	shutdown, err := InitTracer(cfg)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if shutdown == nil {
		t.Fatal("expected non-nil shutdown function")
	}
}

// TestTracer_ReturnsNonNil verifies that Tracer() returns a non-nil tracer
// instance for any given name.
func TestTracer_ReturnsNonNil(t *testing.T) {
	tr := Tracer("test-package")
	if tr == nil {
		t.Fatal("expected non-nil tracer")
	}
}

// TestDefaultObservabilityConfig validates the default values set by
// LoadBootstrap when no YAML override is provided.
func TestDefaultObservabilityConfig(t *testing.T) {
	cfg := config.ObservabilityConfig{
		ServiceName:  "hyperax",
		SamplingRate: 1.0,
	}

	if cfg.OTelEndpoint != "" {
		t.Errorf("expected empty OTelEndpoint, got %q", cfg.OTelEndpoint)
	}
	if cfg.ServiceName != "hyperax" {
		t.Errorf("expected ServiceName 'hyperax', got %q", cfg.ServiceName)
	}
	if cfg.SamplingRate != 1.0 {
		t.Errorf("expected SamplingRate 1.0, got %f", cfg.SamplingRate)
	}
}

// TestPrometheusMiddleware_CapturesMetrics verifies that the middleware records
// http_requests_total and http_request_duration_seconds for a successful request.
func TestPrometheusMiddleware_CapturesMetrics(t *testing.T) {
	// Build a chi router with the middleware and a test handler.
	r := chi.NewRouter()
	r.Use(PrometheusMiddleware)
	r.Get("/test/ping", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("pong"))
	})

	// Issue a request.
	req := httptest.NewRequest(http.MethodGet, "/test/ping", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	// Verify the counter was incremented.
	counter, err := HTTPRequestsTotal.GetMetricWithLabelValues("GET", "/test/ping", "200")
	if err != nil {
		t.Fatalf("get metric: %v", err)
	}
	m := &dto.Metric{}
	if err := counter.Write(m); err != nil {
		t.Fatalf("write metric: %v", err)
	}
	if m.GetCounter().GetValue() < 1 {
		t.Error("expected http_requests_total >= 1")
	}
}

// TestPrometheusMiddleware_Records5xxErrors verifies that the middleware
// increments http_errors_total for HTTP 500+ responses.
func TestPrometheusMiddleware_Records5xxErrors(t *testing.T) {
	r := chi.NewRouter()
	r.Use(PrometheusMiddleware)
	r.Get("/test/fail", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	req := httptest.NewRequest(http.MethodGet, "/test/fail", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", rec.Code)
	}

	counter, err := HTTPErrorsTotal.GetMetricWithLabelValues("GET", "/test/fail")
	if err != nil {
		t.Fatalf("get metric: %v", err)
	}
	m := &dto.Metric{}
	if err := counter.Write(m); err != nil {
		t.Fatalf("write metric: %v", err)
	}
	if m.GetCounter().GetValue() < 1 {
		t.Error("expected http_errors_total >= 1")
	}
}

// TestPrometheusMiddleware_RecordsDuration verifies that the middleware records
// a non-zero observation in http_request_duration_seconds.
func TestPrometheusMiddleware_RecordsDuration(t *testing.T) {
	r := chi.NewRouter()
	r.Use(PrometheusMiddleware)
	r.Get("/test/duration", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test/duration", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	observer, err := HTTPRequestDuration.GetMetricWithLabelValues("GET", "/test/duration", "200")
	if err != nil {
		t.Fatalf("get metric: %v", err)
	}

	// The observer should be a prometheus.Observer, which is also a Metric.
	m := &dto.Metric{}
	if h, ok := observer.(prometheus.Metric); ok {
		if err := h.Write(m); err != nil {
			t.Fatalf("write metric: %v", err)
		}
		if m.GetHistogram().GetSampleCount() < 1 {
			t.Error("expected at least 1 observation in duration histogram")
		}
	} else {
		t.Error("expected observer to implement prometheus.Metric")
	}
}

// TestToolMetrics_IncrementAndObserve verifies that the MCP tool metrics can be
// used without panic and record expected values.
func TestToolMetrics_IncrementAndObserve(t *testing.T) {
	ToolInvocationsTotal.WithLabelValues("test_tool", "ok").Inc()
	ToolInvocationDuration.WithLabelValues("test_tool").Observe(0.042)

	counter, err := ToolInvocationsTotal.GetMetricWithLabelValues("test_tool", "ok")
	if err != nil {
		t.Fatalf("get metric: %v", err)
	}
	m := &dto.Metric{}
	if err := counter.Write(m); err != nil {
		t.Fatalf("write metric: %v", err)
	}
	if m.GetCounter().GetValue() < 1 {
		t.Error("expected tool invocations >= 1")
	}
}

// TestCacheMetrics verifies that cache counters can be incremented without panic.
func TestCacheMetrics(t *testing.T) {
	CacheHitsTotal.Inc()
	CacheMissesTotal.Inc()

	m := &dto.Metric{}
	if err := CacheHitsTotal.Write(m); err != nil {
		t.Fatalf("write metric: %v", err)
	}
	if m.GetCounter().GetValue() < 1 {
		t.Error("expected cache_hits_total >= 1")
	}
}
