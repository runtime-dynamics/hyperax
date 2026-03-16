package observability

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// HTTPRequestDuration tracks HTTP request latency in seconds (golden signal).
	HTTPRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "HTTP request latency in seconds.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "route", "status"})

	// HTTPRequestsTotal counts total HTTP requests by method, route, and status.
	HTTPRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Total number of HTTP requests.",
	}, []string{"method", "route", "status"})

	// HTTPErrorsTotal counts HTTP 5xx responses by method and route.
	HTTPErrorsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "http_errors_total",
		Help: "Total number of HTTP 5xx responses.",
	}, []string{"method", "route"})

	// ToolInvocationDuration tracks MCP tool invocation latency in seconds.
	ToolInvocationDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "mcp_tool_invocation_duration_seconds",
		Help:    "MCP tool invocation latency in seconds.",
		Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
	}, []string{"tool"})

	// ToolInvocationsTotal counts total MCP tool invocations by tool name and status.
	ToolInvocationsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "mcp_tool_invocations_total",
		Help: "Total MCP tool invocations by tool name and status.",
	}, []string{"tool", "status"})

	// IndexFilesTotal tracks the total files indexed per workspace.
	IndexFilesTotal = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "index_files_total",
		Help: "Total files indexed per workspace.",
	}, []string{"workspace"})

	// IndexSymbolsFound tracks the total symbols found per workspace.
	IndexSymbolsFound = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "index_symbols_found",
		Help: "Total symbols found per workspace.",
	}, []string{"workspace"})

	// SearchLatency tracks search query latency by search level (like, fts5, hybrid).
	SearchLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "search_latency_seconds",
		Help:    "Search query latency in seconds.",
		Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1},
	}, []string{"level"})

	// CacheHitsTotal counts total cache hits.
	CacheHitsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "cache_hits_total",
		Help: "Total cache hits.",
	})

	// CacheMissesTotal counts total cache misses.
	CacheMissesTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "cache_misses_total",
		Help: "Total cache misses.",
	})
)
