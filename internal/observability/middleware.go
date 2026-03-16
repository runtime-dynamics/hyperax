package observability

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// PrometheusMiddleware records HTTP request duration, count, and error metrics.
// It captures the three golden signals (latency, traffic, errors) for every
// HTTP request routed through the chi router.
func PrometheusMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

		next.ServeHTTP(ww, r)

		duration := time.Since(start).Seconds()
		status := strconv.Itoa(ww.Status())
		route := chi.RouteContext(r.Context()).RoutePattern()
		if route == "" {
			route = "unknown"
		}

		HTTPRequestDuration.WithLabelValues(r.Method, route, status).Observe(duration)
		HTTPRequestsTotal.WithLabelValues(r.Method, route, status).Inc()

		if ww.Status() >= 500 {
			HTTPErrorsTotal.WithLabelValues(r.Method, route).Inc()
		}
	})
}
