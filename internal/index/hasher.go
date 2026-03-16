package index

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
)

// HashFile computes the SHA-256 hash of the file at path and returns the
// lowercase hex-encoded digest. Returns an error if the file cannot be read.
func HashFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("index.HashFile: %w", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
