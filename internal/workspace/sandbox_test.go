package workspace

import (
	"testing"
)

func TestValidatePath_Valid(t *testing.T) {
	root := t.TempDir()

	got, err := ValidatePath(root, "src/main.go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == "" {
		t.Error("expected non-empty path")
	}
}

func TestValidatePath_Traversal(t *testing.T) {
	root := t.TempDir()

	_, err := ValidatePath(root, "../../etc/passwd")
	if err == nil {
		t.Error("expected error for path traversal")
	}
}

func TestValidatePath_AbsoluteRejected(t *testing.T) {
	root := t.TempDir()

	_, err := ValidatePath(root, "/etc/passwd")
	if err == nil {
		t.Error("expected error for absolute path")
	}
}

func TestValidatePath_DotPath(t *testing.T) {
	root := t.TempDir()

	got, err := ValidatePath(root, ".")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != root {
		t.Errorf("got %q, want %q", got, root)
	}
}

func TestValidatePath_CleansDots(t *testing.T) {
	root := t.TempDir()

	got, err := ValidatePath(root, "src/../src/main.go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == "" {
		t.Error("expected non-empty path")
	}
}

func TestValidatePath_DoubleSlash(t *testing.T) {
	root := t.TempDir()

	_, err := ValidatePath(root, "src//main.go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
