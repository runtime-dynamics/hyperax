package auth

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRateLimiter_AllowWithinLimit(t *testing.T) {
	rl := NewRateLimiter(5, slog.Default())

	// Should allow 5 requests immediately (initial bucket = rpm).
	for i := 0; i < 5; i++ {
		if !rl.Allow("192.168.1.1") {
			t.Fatalf("request %d should be allowed within limit", i+1)
		}
	}
}

func TestRateLimiter_DenyOverLimit(t *testing.T) {
	rl := NewRateLimiter(3, slog.Default())

	// Exhaust the bucket (initial tokens = rpm - 1 for first call, then 2 more).
	for i := 0; i < 3; i++ {
		rl.Allow("10.0.0.1")
	}

	// The 4th request should be denied.
	if rl.Allow("10.0.0.1") {
		t.Error("expected request to be denied after exceeding limit")
	}
}

func TestRateLimiter_SeparateIPs(t *testing.T) {
	rl := NewRateLimiter(2, slog.Default())

	// Exhaust IP A.
	rl.Allow("10.0.0.1")
	rl.Allow("10.0.0.1")
	if rl.Allow("10.0.0.1") {
		t.Error("IP A should be rate-limited")
	}

	// IP B should still be allowed.
	if !rl.Allow("10.0.0.2") {
		t.Error("IP B should not be affected by IP A's limit")
	}
}

func TestRateLimiter_DefaultRPM(t *testing.T) {
	rl := NewRateLimiter(0, slog.Default())
	if rl.rpm != DefaultRateLimitRPM {
		t.Errorf("expected default RPM %d, got %d", DefaultRateLimitRPM, rl.rpm)
	}
}

func TestRateLimiter_Cleanup(t *testing.T) {
	rl := NewRateLimiter(5, slog.Default())
	rl.Allow("10.0.0.1")

	// Before cleanup, bucket exists.
	rl.mu.Lock()
	if _, exists := rl.buckets["10.0.0.1"]; !exists {
		t.Error("bucket should exist before cleanup")
	}
	rl.mu.Unlock()

	// Cleanup with zero max age removes everything.
	rl.Cleanup(0)

	rl.mu.Lock()
	if _, exists := rl.buckets["10.0.0.1"]; exists {
		t.Error("bucket should be cleaned up")
	}
	rl.mu.Unlock()
}

func TestRateLimiter_Middleware_Returns429(t *testing.T) {
	rl := NewRateLimiter(1, slog.Default())

	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First request: allowed.
	req1 := httptest.NewRequest("POST", "/auth/token", nil)
	req1.RemoteAddr = "10.0.0.1:12345"
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Errorf("first request expected 200, got %d", rec1.Code)
	}

	// Second request: rate-limited.
	req2 := httptest.NewRequest("POST", "/auth/token", nil)
	req2.RemoteAddr = "10.0.0.1:12346"
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusTooManyRequests {
		t.Errorf("second request expected 429, got %d", rec2.Code)
	}
	if rec2.Header().Get("Retry-After") != "60" {
		t.Error("expected Retry-After header")
	}
}

// SEC-003: Tests for proxy header support.

func TestRateLimiter_ClientIP_NoProxy(t *testing.T) {
	rl := NewRateLimiter(10, slog.Default())

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	req.Header.Set("X-Forwarded-For", "203.0.113.50")
	req.Header.Set("X-Real-IP", "203.0.113.50")

	// Without TrustProxy, should use RemoteAddr and ignore headers.
	ip := rl.clientIP(req)
	if ip != "10.0.0.1" {
		t.Errorf("expected RemoteAddr IP '10.0.0.1', got %q", ip)
	}
}

func TestRateLimiter_ClientIP_XRealIP(t *testing.T) {
	rl := NewRateLimiterWithProxy(10, true, slog.Default())

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	req.Header.Set("X-Real-IP", "203.0.113.50")

	ip := rl.clientIP(req)
	if ip != "203.0.113.50" {
		t.Errorf("expected X-Real-IP '203.0.113.50', got %q", ip)
	}
}

func TestRateLimiter_ClientIP_XForwardedFor(t *testing.T) {
	rl := NewRateLimiterWithProxy(10, true, slog.Default())

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	req.Header.Set("X-Forwarded-For", "203.0.113.50, 70.41.3.18, 10.0.0.1")

	ip := rl.clientIP(req)
	if ip != "203.0.113.50" {
		t.Errorf("expected first X-Forwarded-For IP '203.0.113.50', got %q", ip)
	}
}

func TestRateLimiter_ClientIP_XRealIP_TakesPrecedence(t *testing.T) {
	rl := NewRateLimiterWithProxy(10, true, slog.Default())

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	req.Header.Set("X-Real-IP", "198.51.100.1")
	req.Header.Set("X-Forwarded-For", "203.0.113.50")

	// X-Real-IP should be preferred over X-Forwarded-For.
	ip := rl.clientIP(req)
	if ip != "198.51.100.1" {
		t.Errorf("expected X-Real-IP '198.51.100.1', got %q", ip)
	}
}

func TestRateLimiter_ClientIP_InvalidHeader_FallsBack(t *testing.T) {
	rl := NewRateLimiterWithProxy(10, true, slog.Default())

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	req.Header.Set("X-Real-IP", "not-an-ip")

	// Invalid IP should be rejected and fall back to RemoteAddr.
	ip := rl.clientIP(req)
	if ip != "10.0.0.1" {
		t.Errorf("expected fallback to RemoteAddr '10.0.0.1', got %q", ip)
	}
}

func TestRateLimiter_ClientIP_IPv6(t *testing.T) {
	rl := NewRateLimiterWithProxy(10, true, slog.Default())

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "[::1]:12345"
	req.Header.Set("X-Real-IP", "2001:db8::1")

	ip := rl.clientIP(req)
	if ip != "2001:db8::1" {
		t.Errorf("expected IPv6 '2001:db8::1', got %q", ip)
	}
}

func TestRateLimiter_Middleware_WithProxy(t *testing.T) {
	rl := NewRateLimiterWithProxy(1, true, slog.Default())

	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First request from proxy client: allowed.
	req1 := httptest.NewRequest("POST", "/auth/token", nil)
	req1.RemoteAddr = "10.0.0.1:12345"
	req1.Header.Set("X-Forwarded-For", "203.0.113.50")
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Errorf("first request expected 200, got %d", rec1.Code)
	}

	// Second request from same client IP: rate-limited.
	req2 := httptest.NewRequest("POST", "/auth/token", nil)
	req2.RemoteAddr = "10.0.0.1:12346"
	req2.Header.Set("X-Forwarded-For", "203.0.113.50")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusTooManyRequests {
		t.Errorf("second request expected 429, got %d", rec2.Code)
	}
}

func TestSanitizeIP(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"192.168.1.1", "192.168.1.1"},
		{"::1", "::1"},
		{"2001:db8::1", "2001:db8::1"},
		{"not-an-ip", ""},
		{"", ""},
		{"<script>alert(1)</script>", ""},
	}

	for _, tt := range tests {
		result := sanitizeIP(tt.input)
		if result != tt.expected {
			t.Errorf("sanitizeIP(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestNewRateLimiterWithProxy(t *testing.T) {
	rl := NewRateLimiterWithProxy(10, true, slog.Default())
	if !rl.trustProxy {
		t.Error("expected trustProxy to be true")
	}

	rl2 := NewRateLimiterWithProxy(10, false, slog.Default())
	if rl2.trustProxy {
		t.Error("expected trustProxy to be false")
	}
}
