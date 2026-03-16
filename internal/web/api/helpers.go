package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/hyperax/hyperax/internal/web/render"
)

// respondJSON writes a JSON response with the given status code.
// Delegates to the render package for consistent content-type negotiation
// and response formatting.
func respondJSON(w http.ResponseWriter, r *http.Request, status int, data any) {
	render.JSON(w, r, data, status)
}

// respondError writes a JSON error response.
// Delegates to the render package for consistent error formatting.
func respondError(w http.ResponseWriter, r *http.Request, status int, message string) {
	render.Error(w, r, message, status)
}

// decodeBody decodes the request body into the target struct.
func decodeBody(r *http.Request, target any) error {
	defer func() { _ = r.Body.Close() }()
	return json.NewDecoder(r.Body).Decode(target)
}

// urlParam extracts a URL parameter from the chi context.
func urlParam(r *http.Request, key string) string {
	return chi.URLParam(r, key)
}

// queryInt extracts an integer query parameter with a default value.
func queryInt(r *http.Request, key string, defaultVal int) int {
	s := r.URL.Query().Get(key)
	if s == "" {
		return defaultVal
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return defaultVal
	}
	return v
}

// queryStr extracts a string query parameter with a default value.
func queryStr(r *http.Request, key string, defaultVal string) string {
	s := r.URL.Query().Get(key)
	if s == "" {
		return defaultVal
	}
	return s
}
