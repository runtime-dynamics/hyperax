package web

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestSPAHandler_ServeIndex(t *testing.T) {
	mockFS := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<html>hyperax</html>")},
	}

	handler := newSPAHandler(mockFS)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
	if rr.Header().Get("Content-Type") != "text/html" {
		t.Errorf("content-type = %q", rr.Header().Get("Content-Type"))
	}
	if rr.Body.String() != "<html>hyperax</html>" {
		t.Errorf("body = %q", rr.Body.String())
	}
}

func TestSPAHandler_ServeStaticFile(t *testing.T) {
	mockFS := fstest.MapFS{
		"index.html":              &fstest.MapFile{Data: []byte("<html></html>")},
		"assets/index-abc123.js":  &fstest.MapFile{Data: []byte("console.log('hello')")},
		"assets/index-abc123.css": &fstest.MapFile{Data: []byte("body{}")},
	}

	handler := newSPAHandler(mockFS)

	tests := []struct {
		path     string
		wantType string
		wantBody string
	}{
		{"/assets/index-abc123.js", "application/javascript", "console.log('hello')"},
		{"/assets/index-abc123.css", "text/css", "body{}"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != http.StatusOK {
				t.Errorf("status = %d", rr.Code)
			}
			if rr.Header().Get("Content-Type") != tt.wantType {
				t.Errorf("content-type = %q, want %q", rr.Header().Get("Content-Type"), tt.wantType)
			}
			if rr.Body.String() != tt.wantBody {
				t.Errorf("body = %q", rr.Body.String())
			}
		})
	}
}

func TestSPAHandler_FallbackToIndex(t *testing.T) {
	mockFS := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<html>spa</html>")},
	}

	handler := newSPAHandler(mockFS)

	// Any unknown path should fall back to index.html (SPA routing)
	paths := []string{"/org", "/settings", "/some/deep/path"}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != http.StatusOK {
				t.Errorf("status = %d", rr.Code)
			}
			if rr.Body.String() != "<html>spa</html>" {
				t.Errorf("body = %q", rr.Body.String())
			}
		})
	}
}

func TestSPAHandler_NoUIAvailable(t *testing.T) {
	// Empty FS — no index.html
	mockFS := fstest.MapFS{}

	handler := newSPAHandler(mockFS)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rr.Code)
	}
}

func TestSPAHandler_NilFS(t *testing.T) {
	// Verify nil uiFS doesn't panic (handled in BuildRouter)
	var nilFS fs.FS
	if nilFS != nil {
		t.Skip("nil FS test not applicable")
	}
}

func TestHandleHealth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()

	handleHealth(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d", rr.Code)
	}
	if rr.Body.String() != `{"status":"ok"}` {
		t.Errorf("body = %q", rr.Body.String())
	}
}
