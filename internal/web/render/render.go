// Package render provides standardised HTTP response helpers for the Hyperax
// web layer. All HTTP handlers should use these functions instead of writing
// responses directly. This centralises content-type negotiation, logging, and
// header management so issues can be fixed in one place.
package render

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
)

// logger is the package-level logger. Set via SetLogger at startup.
var logger *slog.Logger

// SetLogger configures the package-level logger for render diagnostics.
func SetLogger(l *slog.Logger) {
	logger = l
}

// Content writes a response body with the given status code and content type.
// If contentType is empty, it defaults to "application/json". When the client
// Accept header is set, the function respects it:
//   - If Accept includes "text/plain" but not "application/json", and content
//     is a non-string type, the JSON representation is returned as text/plain.
//   - Otherwise JSON is the default.
//
// For non-struct/slice content (string, []byte), the raw value is written
// directly with the appropriate content type.
func Content(w http.ResponseWriter, r *http.Request, content any, status int, contentType string) {
	if contentType == "" {
		contentType = negotiateContentType(r)
	}

	var body []byte
	var err error

	switch v := content.(type) {
	case []byte:
		body = v
	case string:
		body = []byte(v)
	case json.RawMessage:
		body = v
	default:
		if strings.HasPrefix(contentType, "text/plain") {
			// Marshal to JSON then serve as text/plain for text clients.
			body, err = json.MarshalIndent(content, "", "  ")
		} else {
			body, err = json.Marshal(content)
			if contentType == "" {
				contentType = "application/json"
			}
		}
		if err != nil {
			logError("marshal failed", err)
			http.Error(w, `{"error":"internal marshal error"}`, http.StatusInternalServerError)
			return
		}
	}

	writeResponse(w, status, contentType, body)
}

// JSON is a convenience wrapper for Content with application/json.
func JSON(w http.ResponseWriter, r *http.Request, content any, status int) {
	Content(w, r, content, status, "application/json")
}

// Error writes a structured error response. The response format adapts to the
// client's Accept header (JSON by default, plain text if requested).
func Error(w http.ResponseWriter, r *http.Request, msg string, status int) {
	ct := negotiateContentType(r)
	var body []byte

	if strings.HasPrefix(ct, "text/plain") {
		body = []byte(msg)
	} else {
		ct = "application/json"
		var marshalErr error
		body, marshalErr = json.Marshal(map[string]string{"error": msg})
		if marshalErr != nil {
			if logger != nil {
				logger.Error("render: failed to marshal error response", "original_message", msg, "error", marshalErr)
			} else {
				slog.Error("render: failed to marshal error response", "original_message", msg, "error", marshalErr)
			}
			body = []byte(`{"error":"internal server error"}`)
		}
	}

	logError(msg, nil, "status", status, "path", requestPath(r))
	writeResponse(w, status, ct, body)
}

// Success writes a 200 OK response with a standard success payload.
func Success(w http.ResponseWriter, r *http.Request) {
	ct := negotiateContentType(r)
	var body []byte

	if strings.HasPrefix(ct, "text/plain") {
		body = []byte("ok")
	} else {
		ct = "application/json"
		body = []byte(`{"status":"ok"}`)
	}

	writeResponse(w, http.StatusOK, ct, body)
}

// Redirect sends an HTTP redirect response (301 or 302).
func Redirect(w http.ResponseWriter, r *http.Request, url string, code int) {
	if code != http.StatusMovedPermanently && code != http.StatusFound {
		code = http.StatusFound // default to 302
	}
	http.Redirect(w, r, url, code)
}

// Flush writes a JSON response and flushes the underlying connection. This is
// used for MCP JSON-RPC responses where immediate delivery is required.
func Flush(w http.ResponseWriter, status int, data []byte) {
	writeResponse(w, status, "application/json", data)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

// --- internal helpers --------------------------------------------------------

// negotiateContentType inspects the Accept header and returns the best
// content type. Defaults to "application/json" when no preference is stated.
func negotiateContentType(r *http.Request) string {
	if r == nil {
		return "application/json"
	}
	accept := r.Header.Get("Accept")
	if accept == "" || accept == "*/*" {
		return "application/json"
	}

	// Simple negotiation: check if text/plain is preferred over JSON.
	// A full quality-factor parser is overkill for our use case.
	acceptLower := strings.ToLower(accept)
	hasJSON := strings.Contains(acceptLower, "application/json")
	hasText := strings.Contains(acceptLower, "text/plain")

	if hasText && !hasJSON {
		return "text/plain; charset=utf-8"
	}
	return "application/json"
}

// writeResponse is the single exit point for all HTTP response writes.
func writeResponse(w http.ResponseWriter, status int, contentType string, body []byte) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
	w.WriteHeader(status)
	if _, err := w.Write(body); err != nil {
		if logger != nil {
			logger.Error("render: failed to write HTTP response body", "status", status, "error", err)
		} else {
			slog.Error("render: failed to write HTTP response body", "status", status, "error", err)
		}
	}
}

// requestPath safely extracts the request path for logging.
func requestPath(r *http.Request) string {
	if r == nil {
		return ""
	}
	return r.URL.Path
}

// logError logs a render error if a logger is configured.
func logError(msg string, err error, extra ...any) {
	if logger == nil {
		return
	}
	args := []any{}
	if err != nil {
		args = append(args, "error", err)
	}
	args = append(args, extra...)
	logger.Warn("render: "+msg, args...)
}
