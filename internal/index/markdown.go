package index

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/hyperax/hyperax/internal/repo"
)

// ChunkMarkdown splits a markdown file into documentation chunks by heading.
// Each heading (lines starting with one or more '#' characters) begins a new
// chunk. Content between headings is grouped under the preceding heading.
// Text before the first heading is stored with an empty SectionHeader.
//
// Parameters:
//   - filePath:    absolute path to the markdown file
//   - workspaceID: workspace that owns this document
//   - fileHash:    SHA-256 hash of the file (for change detection)
//
// Returns a slice of DocChunk pointers with sequential ChunkIndex values,
// or an error if the file cannot be read.
func ChunkMarkdown(filePath, workspaceID, fileHash string) ([]*repo.DocChunk, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("index.ChunkMarkdown: %w", err)
	}
	defer func() { _ = f.Close() }()

	var chunks []*repo.DocChunk
	var currentHeader string
	var currentContent strings.Builder
	chunkIndex := 0

	scanner := bufio.NewScanner(f)
	// Increase buffer size for files with long lines.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()

		heading, isHeading := parseHeading(line)
		if isHeading {
			// Flush the previous chunk if it has content.
			if currentContent.Len() > 0 || chunkIndex > 0 {
				chunks = append(chunks, buildChunk(
					workspaceID, filePath, fileHash,
					currentHeader, currentContent.String(), chunkIndex,
				))
				chunkIndex++
				currentContent.Reset()
			}
			currentHeader = heading
			continue
		}

		if currentContent.Len() > 0 {
			currentContent.WriteByte('\n')
		}
		currentContent.WriteString(line)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("index.ChunkMarkdown: %w", err)
	}

	// Flush the final chunk.
	if currentContent.Len() > 0 || currentHeader != "" {
		chunks = append(chunks, buildChunk(
			workspaceID, filePath, fileHash,
			currentHeader, currentContent.String(), chunkIndex,
		))
	}

	return chunks, nil
}

// parseHeading checks whether a line is a markdown heading (starts with '#').
// Returns the heading text (without the '#' prefix) and true, or ("", false)
// if the line is not a heading.
func parseHeading(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "#") {
		return "", false
	}

	// Strip leading '#' characters and the space that follows.
	i := 0
	for i < len(trimmed) && trimmed[i] == '#' {
		i++
	}
	// A valid ATX heading requires at least one '#' followed by a space or end-of-line.
	if i == 0 {
		return "", false
	}
	heading := strings.TrimSpace(trimmed[i:])
	return heading, true
}

// buildChunk creates a DocChunk with an estimated token count.
// The token estimate uses the rough heuristic of len(content) / 4.
func buildChunk(workspaceID, filePath, fileHash, header, content string, idx int) *repo.DocChunk {
	tokenCount := len(content) / 4
	if tokenCount < 1 && len(content) > 0 {
		tokenCount = 1
	}
	return &repo.DocChunk{
		WorkspaceID:   workspaceID,
		FilePath:      filePath,
		FileHash:      fileHash,
		ChunkIndex:    idx,
		SectionHeader: header,
		Content:       content,
		TokenCount:    tokenCount,
	}
}
