//go:build !cgo || !treesitter

package index

import (
	"path/filepath"
	"strings"

	"github.com/hyperax/hyperax/internal/repo"
)

// UniversalExtractor extracts symbols from source files. Without the
// treesitter build tag, only Go files are supported (via the stdlib AST
// parser). Non-Go files return nil, nil.
//
// Build with -tags treesitter to enable Python, TypeScript, Rust, and
// JavaScript support via Tree-sitter CGO bindings.
func UniversalExtractor(filePath string) ([]*repo.Symbol, error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	if ext == ".go" {
		return ExtractGoSymbols(filePath)
	}
	return nil, nil
}

// TreeSitterAvailable returns false when the binary was compiled without
// Tree-sitter support.
func TreeSitterAvailable() bool {
	return false
}

// SupportedExtensions returns only .go when Tree-sitter is not available.
func SupportedExtensions() map[string]string {
	return map[string]string{".go": "go"}
}
