package refactor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTestFile creates a file in a temp directory with the given lines and
// returns its absolute path. The temp directory is cleaned up automatically.
func writeTestFile(t *testing.T, lines []string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.go")
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}
	return path
}

// readTestFile reads the file and returns its lines (without trailing newlines).
func readTestFile(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read test file: %v", err)
	}
	// Trim trailing newline before splitting to avoid an empty last element.
	text := strings.TrimSuffix(string(data), "\n")
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

func TestExtractCodeBlockReadsCorrectLines(t *testing.T) {
	path := writeTestFile(t, []string{
		"package main",
		"",
		"func main() {",
		"\tfmt.Println(\"hello\")",
		"}",
	})

	got, err := ExtractCodeBlock(path, 3, 5)
	if err != nil {
		t.Fatalf("ExtractCodeBlock() error: %v", err)
	}

	want := "func main() {\n\tfmt.Println(\"hello\")\n}"
	if got != want {
		t.Errorf("ExtractCodeBlock() =\n%q\nwant:\n%q", got, want)
	}
}

func TestExtractCodeBlockSingleLine(t *testing.T) {
	path := writeTestFile(t, []string{"alpha", "beta", "gamma"})

	got, err := ExtractCodeBlock(path, 2, 2)
	if err != nil {
		t.Fatalf("ExtractCodeBlock() error: %v", err)
	}
	if got != "beta" {
		t.Errorf("ExtractCodeBlock() = %q, want %q", got, "beta")
	}
}

func TestInsertCodeBlockInsertsAtCorrectPosition(t *testing.T) {
	path := writeTestFile(t, []string{"line1", "line2", "line3"})

	if err := InsertCodeBlock(path, 2, "inserted_a\ninserted_b"); err != nil {
		t.Fatalf("InsertCodeBlock() error: %v", err)
	}

	got := readTestFile(t, path)
	want := []string{"line1", "line2", "inserted_a", "inserted_b", "line3"}
	if len(got) != len(want) {
		t.Fatalf("line count = %d, want %d\ngot: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i+1, got[i], want[i])
		}
	}
}

func TestInsertCodeBlockPrepend(t *testing.T) {
	path := writeTestFile(t, []string{"existing"})

	if err := InsertCodeBlock(path, 0, "prepended"); err != nil {
		t.Fatalf("InsertCodeBlock(afterLine=0) error: %v", err)
	}

	got := readTestFile(t, path)
	want := []string{"prepended", "existing"}
	if len(got) != len(want) {
		t.Fatalf("line count = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i+1, got[i], want[i])
		}
	}
}

func TestDeleteCodeBlockRemovesCorrectLines(t *testing.T) {
	path := writeTestFile(t, []string{"line1", "line2", "line3", "line4", "line5"})

	if err := DeleteCodeBlock(path, 2, 4); err != nil {
		t.Fatalf("DeleteCodeBlock() error: %v", err)
	}

	got := readTestFile(t, path)
	want := []string{"line1", "line5"}
	if len(got) != len(want) {
		t.Fatalf("line count = %d, want %d\ngot: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i+1, got[i], want[i])
		}
	}
}

func TestMoveSymbolTransfersCodeBetweenFiles(t *testing.T) {
	srcPath := writeTestFile(t, []string{
		"package src",
		"",
		"func Foo() {}",
		"",
		"func Bar() {}",
	})

	destDir := t.TempDir()
	destPath := filepath.Join(destDir, "dest.go")
	if err := os.WriteFile(destPath, []byte("package dest\n\n// existing code\n"), 0644); err != nil {
		t.Fatalf("write dest file: %v", err)
	}

	// Move func Foo() {} (line 3) from src to dest after line 2.
	if err := MoveSymbol(srcPath, 3, 3, destPath, 2); err != nil {
		t.Fatalf("MoveSymbol() error: %v", err)
	}

	// Verify source no longer has the moved line.
	srcLines := readTestFile(t, srcPath)
	for _, line := range srcLines {
		if strings.Contains(line, "func Foo") {
			t.Error("source file still contains moved symbol 'func Foo'")
		}
	}

	// Verify destination has the moved line.
	destLines := readTestFile(t, destPath)
	found := false
	for _, line := range destLines {
		if strings.Contains(line, "func Foo") {
			found = true
			break
		}
	}
	if !found {
		t.Error("destination file does not contain moved symbol 'func Foo'")
	}
}

func TestOutOfRangeLineNumbersReturnErrors(t *testing.T) {
	path := writeTestFile(t, []string{"one", "two", "three"})

	// start_line > file length
	if _, err := ExtractCodeBlock(path, 10, 12); err == nil {
		t.Error("ExtractCodeBlock(start=10) on 3-line file should return error")
	}

	// end_line > file length
	if _, err := ExtractCodeBlock(path, 1, 10); err == nil {
		t.Error("ExtractCodeBlock(end=10) on 3-line file should return error")
	}

	// start_line < 1
	if _, err := ExtractCodeBlock(path, 0, 2); err == nil {
		t.Error("ExtractCodeBlock(start=0) should return error")
	}

	// end_line < start_line
	if _, err := ExtractCodeBlock(path, 3, 1); err == nil {
		t.Error("ExtractCodeBlock(end < start) should return error")
	}

	// InsertCodeBlock after_line > file length
	if err := InsertCodeBlock(path, 10, "new"); err == nil {
		t.Error("InsertCodeBlock(afterLine=10) on 3-line file should return error")
	}

	// DeleteCodeBlock start > file length
	if err := DeleteCodeBlock(path, 10, 12); err == nil {
		t.Error("DeleteCodeBlock(start=10) on 3-line file should return error")
	}

	// DeleteCodeBlock end > file length
	if err := DeleteCodeBlock(path, 1, 10); err == nil {
		t.Error("DeleteCodeBlock(end=10) on 3-line file should return error")
	}
}

func TestOperationsOnNonexistentFileReturnErrors(t *testing.T) {
	badPath := filepath.Join(t.TempDir(), "does-not-exist.go")

	if _, err := ExtractCodeBlock(badPath, 1, 1); err == nil {
		t.Error("ExtractCodeBlock on nonexistent file should return error")
	}

	if err := InsertCodeBlock(badPath, 0, "content"); err == nil {
		t.Error("InsertCodeBlock on nonexistent file should return error")
	}

	if err := DeleteCodeBlock(badPath, 1, 1); err == nil {
		t.Error("DeleteCodeBlock on nonexistent file should return error")
	}
}

func TestDeleteCodeBlockInvalidRange(t *testing.T) {
	path := writeTestFile(t, []string{"one", "two", "three"})

	// start_line < 1
	if err := DeleteCodeBlock(path, 0, 2); err == nil {
		t.Error("DeleteCodeBlock(start=0) should return error")
	}

	// end_line < start_line
	if err := DeleteCodeBlock(path, 3, 1); err == nil {
		t.Error("DeleteCodeBlock(end < start) should return error")
	}
}

func TestInsertCodeBlockNegativeAfterLine(t *testing.T) {
	path := writeTestFile(t, []string{"one"})

	if err := InsertCodeBlock(path, -1, "bad"); err == nil {
		t.Error("InsertCodeBlock(afterLine=-1) should return error")
	}
}
