package index

import (
	"os"
	"path/filepath"
	"testing"
)

// writeGoFile creates a temporary Go source file with the given content and
// returns its absolute path. The file is cleaned up when the test finishes.
func writeGoFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp go file: %v", err)
	}
	return path
}

func TestExtractGoSymbols_Functions(t *testing.T) {
	dir := t.TempDir()
	src := `package example

func Hello() string {
	return "hello"
}

func Add(a, b int) int {
	return a + b
}
`
	path := writeGoFile(t, dir, "funcs.go", src)

	symbols, err := ExtractGoSymbols(path)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	if len(symbols) != 2 {
		t.Fatalf("expected 2 symbols, got %d", len(symbols))
	}

	// Verify first function.
	if symbols[0].Name != "Hello" {
		t.Errorf("symbol 0 name = %q, want Hello", symbols[0].Name)
	}
	if symbols[0].Kind != "function" {
		t.Errorf("symbol 0 kind = %q, want function", symbols[0].Kind)
	}
	if symbols[0].Signature != "func Hello() string" {
		t.Errorf("symbol 0 signature = %q, want %q", symbols[0].Signature, "func Hello() string")
	}

	// Verify second function.
	if symbols[1].Name != "Add" {
		t.Errorf("symbol 1 name = %q, want Add", symbols[1].Name)
	}
	if symbols[1].Signature != "func Add(a int, b int) int" {
		t.Errorf("symbol 1 signature = %q, want %q", symbols[1].Signature, "func Add(a int, b int) int")
	}
}

func TestExtractGoSymbols_StructsAndInterfaces(t *testing.T) {
	dir := t.TempDir()
	src := `package example

type Server struct {
	Addr string
	Port int
}

type Handler interface {
	ServeHTTP(w Writer, r *Reader)
}
`
	path := writeGoFile(t, dir, "types.go", src)

	symbols, err := ExtractGoSymbols(path)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	if len(symbols) != 2 {
		t.Fatalf("expected 2 symbols, got %d", len(symbols))
	}

	// Struct.
	if symbols[0].Name != "Server" {
		t.Errorf("symbol 0 name = %q, want Server", symbols[0].Name)
	}
	if symbols[0].Kind != "struct" {
		t.Errorf("symbol 0 kind = %q, want struct", symbols[0].Kind)
	}
	if symbols[0].Signature != "type Server struct" {
		t.Errorf("symbol 0 signature = %q, want %q", symbols[0].Signature, "type Server struct")
	}

	// Interface.
	if symbols[1].Name != "Handler" {
		t.Errorf("symbol 1 name = %q, want Handler", symbols[1].Name)
	}
	if symbols[1].Kind != "interface" {
		t.Errorf("symbol 1 kind = %q, want interface", symbols[1].Kind)
	}
	if symbols[1].Signature != "type Handler interface" {
		t.Errorf("symbol 1 signature = %q, want %q", symbols[1].Signature, "type Handler interface")
	}
}

func TestExtractGoSymbols_MethodsWithReceivers(t *testing.T) {
	dir := t.TempDir()
	src := `package example

type Service struct {
	Name string
}

func (s *Service) Start() error {
	return nil
}

func (s Service) String() string {
	return s.Name
}
`
	path := writeGoFile(t, dir, "methods.go", src)

	symbols, err := ExtractGoSymbols(path)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	// Expect: Service (struct), Start (method), String (method)
	if len(symbols) != 3 {
		t.Fatalf("expected 3 symbols, got %d", len(symbols))
	}

	// The struct comes first in source order.
	if symbols[0].Name != "Service" {
		t.Errorf("symbol 0 name = %q, want Service", symbols[0].Name)
	}

	// First method with pointer receiver.
	if symbols[1].Name != "Start" {
		t.Errorf("symbol 1 name = %q, want Start", symbols[1].Name)
	}
	if symbols[1].Kind != "method" {
		t.Errorf("symbol 1 kind = %q, want method", symbols[1].Kind)
	}
	if symbols[1].Signature != "func (s *Service) Start() error" {
		t.Errorf("symbol 1 signature = %q, want %q", symbols[1].Signature, "func (s *Service) Start() error")
	}

	// Second method with value receiver.
	if symbols[2].Name != "String" {
		t.Errorf("symbol 2 name = %q, want String", symbols[2].Name)
	}
	if symbols[2].Kind != "method" {
		t.Errorf("symbol 2 kind = %q, want method", symbols[2].Kind)
	}
	if symbols[2].Signature != "func (s Service) String() string" {
		t.Errorf("symbol 2 signature = %q, want %q", symbols[2].Signature, "func (s Service) String() string")
	}
}

func TestExtractGoSymbols_ConstantsAndVariables(t *testing.T) {
	dir := t.TempDir()
	src := `package example

const MaxRetries = 5

const (
	ModeRead  = "read"
	ModeWrite = "write"
)

var DefaultTimeout int

var (
	ErrNotFound = "not found"
	ErrTimeout  = "timeout"
)
`
	path := writeGoFile(t, dir, "values.go", src)

	symbols, err := ExtractGoSymbols(path)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	// Expect: MaxRetries, ModeRead, ModeWrite (constants), DefaultTimeout, ErrNotFound, ErrTimeout (variables)
	if len(symbols) != 6 {
		t.Fatalf("expected 6 symbols, got %d", len(symbols))
	}

	// Verify constants.
	constantCount := 0
	variableCount := 0
	for _, sym := range symbols {
		switch sym.Kind {
		case "constant":
			constantCount++
		case "variable":
			variableCount++
		}
	}

	if constantCount != 3 {
		t.Errorf("constant count = %d, want 3", constantCount)
	}
	if variableCount != 3 {
		t.Errorf("variable count = %d, want 3", variableCount)
	}

	// Verify a specific constant signature.
	if symbols[0].Name != "MaxRetries" {
		t.Errorf("symbol 0 name = %q, want MaxRetries", symbols[0].Name)
	}
	if symbols[0].Signature != "const MaxRetries" {
		t.Errorf("symbol 0 signature = %q, want %q", symbols[0].Signature, "const MaxRetries")
	}
}

func TestExtractGoSymbols_LineNumbers(t *testing.T) {
	dir := t.TempDir()
	src := `package example

func First() {
}

func Second() {
}
`
	path := writeGoFile(t, dir, "lines.go", src)

	symbols, err := ExtractGoSymbols(path)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	if len(symbols) != 2 {
		t.Fatalf("expected 2 symbols, got %d", len(symbols))
	}

	if symbols[0].StartLine != 3 {
		t.Errorf("First start line = %d, want 3", symbols[0].StartLine)
	}
	if symbols[0].EndLine != 4 {
		t.Errorf("First end line = %d, want 4", symbols[0].EndLine)
	}

	if symbols[1].StartLine != 6 {
		t.Errorf("Second start line = %d, want 6", symbols[1].StartLine)
	}
	if symbols[1].EndLine != 7 {
		t.Errorf("Second end line = %d, want 7", symbols[1].EndLine)
	}
}

func TestExtractGoSymbols_InvalidFile(t *testing.T) {
	dir := t.TempDir()
	path := writeGoFile(t, dir, "bad.go", "this is not valid go")

	_, err := ExtractGoSymbols(path)
	if err == nil {
		t.Fatal("expected error for invalid Go file")
	}
}

func TestExtractGoSymbols_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := writeGoFile(t, dir, "empty.go", "package empty\n")

	symbols, err := ExtractGoSymbols(path)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	if len(symbols) != 0 {
		t.Errorf("expected 0 symbols, got %d", len(symbols))
	}
}

func TestExtractGoSymbols_MultipleReturnValues(t *testing.T) {
	dir := t.TempDir()
	src := `package example

func Divide(a, b float64) (float64, error) {
	if b == 0 {
		return 0, nil
	}
	return a / b, nil
}
`
	path := writeGoFile(t, dir, "multi.go", src)

	symbols, err := ExtractGoSymbols(path)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	if len(symbols) != 1 {
		t.Fatalf("expected 1 symbol, got %d", len(symbols))
	}

	expected := "func Divide(a float64, b float64) (float64, error)"
	if symbols[0].Signature != expected {
		t.Errorf("signature = %q, want %q", symbols[0].Signature, expected)
	}
}
