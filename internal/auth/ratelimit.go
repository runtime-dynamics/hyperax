// Package auth rate limiting for the token endpoint.
//
// RateLimiter implements a per-IP token bucket algorithm that limits the rate
// of POST /auth/token requests. When a client exceeds the configured requests
// per minute, the middleware responds with HTTP 429 Too Many Requests.
//
// The rate limit is configurable via the "auth.rate_limit_rpm" config key
// (default: 30 requests per minute).
//
// When deployed behind a reverse proxy, enable TrustProxy to extract the real
// client IP from X-Forwarded-For or X-Real-IP headers (SEC-003).
package auth

import (
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/hyperax/hyperax/internal/web/render"
)

// DefaultRateLimitRPM is the default maximum requests per minute per IP.
const DefaultRateLimitRPM = 30

// tokenBucket tracks request tokens for a single IP address.
type tokenBucket struct {
	tokens    float64
	lastCheck time.Time
}

// RateLimiter enforces per-IP request rate limits using a token bucket algorithm.
// Each IP gets a bucket that refills at a constant rate up to the configured maximum.
// Stale buckets are cleaned up periodically to prevent memory leaks.
//
// When TrustProxy is true, the rate limiter extracts client IPs from
// X-Forwarded-For and X-Real-IP headers. Only enable this when deployed
// behind a trusted reverse proxy; otherwise clients can trivially bypass
// rate limiting by forging these headers.
type RateLimiter struct {
	mu         sync.Mutex
	buckets    map[string]*tokenBucket
	rpm        int
	trustProxy bool
	logger     *slog.Logger
}

// NewRateLimiter creates a RateLimiter with the given requests-per-minute limit.
// If rpm is <= 0, DefaultRateLimitRPM is used.
func NewRateLimiter(rpm int, logger *slog.Logger) *RateLimiter {
	if rpm <= 0 {
		rpm = DefaultRateLimitRPM
	}
	rl := &RateLimiter{
		buckets: make(map[string]*tokenBucket),
		rpm:     rpm,
		logger:  logger,
	}
	return rl
}

// NewRateLimiterWithProxy creates a RateLimiter that optionally trusts proxy
// headers (X-Forwarded-For, X-Real-IP) for client IP extraction. Only set
// trustProxy to true when the server is behind a trusted reverse proxy (SEC-003).
func NewRateLimiterWithProxy(rpm int, trustProxy bool, logger *slog.Logger) *RateLimiter {
	rl := NewRateLimiter(rpm, logger)
	rl.trustProxy = trustProxy
	return rl
}

// Allow checks whether a request from the given IP is permitted.
// Returns true if the request is allowed, false if rate-limited.
func (rl *RateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	bucket, exists := rl.buckets[ip]
	if !exists {
		bucket = &tokenBucket{
			tokens:    float64(rl.rpm) - 1,
			lastCheck: now,
		}
		rl.buckets[ip] = bucket
		return true
	}

	// Refill tokens based on elapsed time.
	elapsed := now.Sub(bucket.lastCheck).Seconds()
	refillRate := float64(rl.rpm) / 60.0 // tokens per second
	bucket.tokens += elapsed * refillRate
	if bucket.tokens > float64(rl.rpm) {
		bucket.tokens = float64(rl.rpm)
	}
	bucket.lastCheck = now

	if bucket.tokens < 1 {
		return false
	}

	bucket.tokens--
	return true
}

// Cleanup removes stale buckets that haven't been accessed in the given duration.
// Call this periodically to prevent unbounded memory growth.
func (rl *RateLimiter) Cleanup(maxAge time.Duration) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	for ip, bucket := range rl.buckets {
		if bucket.lastCheck.Before(cutoff) {
			delete(rl.buckets, ip)
		}
	}
}

// Middleware returns an http.Handler that rate-limits requests by client IP.
// Requests that exceed the limit receive HTTP 429 with a JSON error body.
// The client IP is extracted from proxy headers (when trusted) or RemoteAddr.
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := rl.clientIP(r)
		if !rl.Allow(ip) {
			rl.logger.Warn("rate limit exceeded",
				"ip", ip,
				"endpoint", r.URL.Path,
				"rpm_limit", rl.rpm,
			)
			w.Header().Set("Retry-After", "60")
			render.Error(w, r, "rate limit exceeded, try again later", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// clientIP extracts the client IP from the request. When TrustProxy is enabled,
// the X-Real-IP header is checked first, then the first entry in X-Forwarded-For.
// Falls back to RemoteAddr, stripping the port if present (SEC-003).
func (rl *RateLimiter) clientIP(r *http.Request) string {
	if rl.trustProxy {
		// X-Real-IP is typically set by nginx/envoy to the original client IP.
		if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); realIP != "" {
			if ip := sanitizeIP(realIP); ip != "" {
				return ip
			}
		}

		// X-Forwarded-For contains a comma-separated list; the leftmost entry
		// is the original client IP (when the proxy chain is trusted).
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.SplitN(xff, ",", 2)
			if ip := sanitizeIP(strings.TrimSpace(parts[0])); ip != "" {
				return ip
			}
		}
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// sanitizeIP validates that the given string looks like an IP address and
// returns it, or returns empty string if invalid. This prevents header
// injection attacks where a forged header contains non-IP content.
func sanitizeIP(s string) string {
	ip := net.ParseIP(s)
	if ip == nil {
		return ""
	}
	return ip.String()
}
