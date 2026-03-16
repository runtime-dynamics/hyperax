package refactor

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ExtractCodeBlock reads lines startLine..endLine (1-based, inclusive) from
// filePath and returns them as a single string with newlines preserved.
// The file itself is not modified.
func ExtractCodeBlock(filePath string, startLine, endLine int) (string, error) {
	if startLine < 1 {
		return "", fmt.Errorf("refactor.ExtractCodeBlock: start_line must be >= 1, got %d", startLine)
	}
	if endLine < startLine {
		return "", fmt.Errorf("refactor.ExtractCodeBlock: end_line %d < start_line %d", endLine, startLine)
	}

	lines, err := readLines(filePath)
	if err != nil {
		return "", fmt.Errorf("refactor.ExtractCodeBlock: %w", err)
	}

	if startLine > len(lines) {
		return "", fmt.Errorf("refactor.ExtractCodeBlock: start_line %d exceeds file length %d", startLine, len(lines))
	}
	if endLine > len(lines) {
		return "", fmt.Errorf("refactor.ExtractCodeBlock: end_line %d exceeds file length %d", endLine, len(lines))
	}

	extracted := lines[startLine-1 : endLine]
	return strings.Join(extracted, "\n"), nil
}

// InsertCodeBlock inserts content after afterLine (1-based) in filePath. If
// afterLine is 0 the content is prepended to the file. The write is performed
// atomically via a temporary file and rename.
func InsertCodeBlock(filePath string, afterLine int, content string) error {
	if afterLine < 0 {
		return fmt.Errorf("refactor.InsertCodeBlock: after_line must be >= 0, got %d", afterLine)
	}

	lines, err := readLines(filePath)
	if err != nil {
		return fmt.Errorf("refactor.InsertCodeBlock: %w", err)
	}

	if afterLine > len(lines) {
		return fmt.Errorf("refactor.InsertCodeBlock: after_line %d exceeds file length %d", afterLine, len(lines))
	}

	insertLines := strings.Split(content, "\n")

	// Build the new file: lines before + inserted content + lines after.
	newLines := make([]string, 0, len(lines)+len(insertLines))
	newLines = append(newLines, lines[:afterLine]...)
	newLines = append(newLines, insertLines...)
	newLines = append(newLines, lines[afterLine:]...)

	if err := writeLines(filePath, newLines); err != nil {
		return fmt.Errorf("refactor.InsertCodeBlock: %w", err)
	}
	return nil
}

// DeleteCodeBlock removes lines startLine..endLine (1-based, inclusive) from
// filePath. The write is performed atomically.
func DeleteCodeBlock(filePath string, startLine, endLine int) error {
	if startLine < 1 {
		return fmt.Errorf("refactor.DeleteCodeBlock: start_line must be >= 1, got %d", startLine)
	}
	if endLine < startLine {
		return fmt.Errorf("refactor.DeleteCodeBlock: end_line %d < start_line %d", endLine, startLine)
	}

	lines, err := readLines(filePath)
	if err != nil {
		return fmt.Errorf("refactor.DeleteCodeBlock: %w", err)
	}

	if startLine > len(lines) {
		return fmt.Errorf("refactor.DeleteCodeBlock: start_line %d exceeds file length %d", startLine, len(lines))
	}
	if endLine > len(lines) {
		return fmt.Errorf("refactor.DeleteCodeBlock: end_line %d exceeds file length %d", endLine, len(lines))
	}

	newLines := make([]string, 0, len(lines)-(endLine-startLine+1))
	newLines = append(newLines, lines[:startLine-1]...)
	newLines = append(newLines, lines[endLine:]...)

	if err := writeLines(filePath, newLines); err != nil {
		return fmt.Errorf("refactor.DeleteCodeBlock: %w", err)
	}
	return nil
}

// MoveSymbol extracts lines startLine..endLine from srcFile, deletes them from
// srcFile, and inserts them into destFile after afterLine. Both files are
// written atomically. If either file operation fails the function returns an
// error (callers should use transactions for rollback safety).
func MoveSymbol(srcFile string, startLine, endLine int, destFile string, afterLine int) error {
	extracted, err := ExtractCodeBlock(srcFile, startLine, endLine)
	if err != nil {
		return fmt.Errorf("refactor.MoveSymbol: %w", err)
	}

	if err := DeleteCodeBlock(srcFile, startLine, endLine); err != nil {
		return fmt.Errorf("refactor.MoveSymbol: %w", err)
	}

	if err := InsertCodeBlock(destFile, afterLine, extracted); err != nil {
		return fmt.Errorf("refactor.MoveSymbol: %w", err)
	}

	return nil
}

// readLines reads a file into a slice of lines (without trailing newlines).
func readLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return lines, nil
}

// writeLines writes lines to a file atomically via a temp file and rename.
// Each line is terminated with a newline character.
func writeLines(path string, lines []string) error {
	// Ensure parent directory exists.
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}

	// Preserve original permissions if the file already exists.
	perm := os.FileMode(0644)
	if info, err := os.Stat(path); err == nil {
		perm = info.Mode().Perm()
	}

	tmp, err := os.CreateTemp(dir, ".hyperax-write-*")
	if err != nil {
		return fmt.Errorf("create temp in %s: %w", dir, err)
	}
	tmpPath := tmp.Name()

	w := bufio.NewWriter(tmp)
	for _, line := range lines {
		if _, err := w.WriteString(line + "\n"); err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmpPath)
			return fmt.Errorf("write: %w", err)
		}
	}
	if err := w.Flush(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("flush: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close temp: %w", err)
	}

	if err := os.Chmod(tmpPath, perm); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("chmod: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename to %s: %w", path, err)
	}
	return nil
}
