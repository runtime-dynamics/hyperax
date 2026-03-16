// Package index implements file scanning, symbol extraction, and document
// chunking for Hyperax workspaces. It walks directory trees, computes file
// hashes for incremental indexing, extracts Go symbols via the stdlib AST,
// and splits markdown documents into searchable chunks.
package index

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/pkg/types"
)

// EventIndexWorkspaceIndexed is the event type published when a full workspace
// index operation completes. The payload contains an IndexResult.
const EventIndexWorkspaceIndexed types.EventType = "index.workspace_indexed"

// maxFileSize is the upper bound for files the indexer will process (1 MB).
const maxFileSize = 1 << 20

// ignoredDirs contains directory names that the indexer always skips.
var ignoredDirs = map[string]struct{}{
	".git":        {},
	"node_modules": {},
	"vendor":      {},
	".build":      {},
	"__pycache__": {},
}

// IndexResult summarises the outcome of an indexing operation.
type IndexResult struct {
	FilesScanned int           `json:"files_scanned"`
	FilesSkipped int           `json:"files_skipped"`
	SymbolsFound int           `json:"symbols_found"`
	DocsChunked  int           `json:"docs_chunked"`
	Duration     time.Duration `json:"duration"`
}

// Indexer scans workspace files, extracts symbols from Go sources, chunks
// markdown documents, and persists everything via the SymbolRepo and SearchRepo
// interfaces. It publishes a nervous-system event when a workspace scan
// completes.
type Indexer struct {
	symbols repo.SymbolRepo
	search  repo.SearchRepo
	bus     *nervous.EventBus
	logger  *slog.Logger
}

// NewIndexer creates an Indexer with the provided repositories, event bus,
// and structured logger.
//
// Parameters:
//   - symbols: repository for file hashes and code symbols
//   - search:  repository for documentation chunks
//   - bus:     event bus for publishing indexing events (may be nil for testing)
//   - logger:  structured logger (may be nil; a no-op logger is substituted)
func NewIndexer(symbols repo.SymbolRepo, search repo.SearchRepo, bus *nervous.EventBus, logger *slog.Logger) *Indexer {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Indexer{
		symbols: symbols,
		search:  search,
		bus:     bus,
		logger:  logger,
	}
}

// IndexWorkspace performs a full scan of the workspace rooted at rootPath.
// It walks the directory tree respecting ignore rules, indexes Go source files
// for symbols and markdown files for documentation chunks. Files whose hash
// has not changed since the last index are skipped (incremental).
//
// On completion an "index.workspace_indexed" event is published to the EventBus.
//
// Parameters:
//   - ctx:         context for cancellation
//   - workspaceID: identifier of the workspace being indexed
//   - rootPath:    absolute filesystem path to the workspace root
//
// Returns an IndexResult summarising the operation, or an error if the walk
// itself fails.
func (idx *Indexer) IndexWorkspace(ctx context.Context, workspaceID, rootPath string) (*IndexResult, error) {
	start := time.Now()
	result := &IndexResult{}

	idx.logger.Info("starting workspace index",
		slog.String("workspace_id", workspaceID),
		slog.String("root", rootPath),
	)

	err := filepath.WalkDir(rootPath, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			idx.logger.Warn("walk error", slog.String("path", path), slog.Any("error", walkErr))
			return nil // continue walking
		}

		// Check cancellation periodically.
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Skip ignored directories.
		if d.IsDir() {
			if shouldIgnore(path) {
				return fs.SkipDir
			}
			return nil
		}

		// Compute the relative path for storage.
		relPath, err := filepath.Rel(rootPath, path)
		if err != nil {
			idx.logger.Warn("rel path error", slog.String("path", path), slog.Any("error", err))
			return nil
		}

		// Skip files that exceed size limit or are binary.
		if skip, reason := shouldSkipFile(path, d); skip {
			idx.logger.Debug("skipping file", slog.String("path", relPath), slog.String("reason", reason))
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		switch ext {
		case ".go":
			scanned, skipped, symbolCount, err := idx.indexGoFile(ctx, workspaceID, rootPath, relPath, path)
			if err != nil {
				idx.logger.Warn("index go file failed",
					slog.String("path", relPath), slog.Any("error", err))
				return nil
			}
			result.FilesScanned += scanned
			result.FilesSkipped += skipped
			result.SymbolsFound += symbolCount

		case ".md":
			scanned, skipped, chunks, err := idx.indexMarkdownFile(ctx, workspaceID, relPath, path)
			if err != nil {
				idx.logger.Warn("index markdown file failed",
					slog.String("path", relPath), slog.Any("error", err))
				return nil
			}
			result.FilesScanned += scanned
			result.FilesSkipped += skipped
			result.DocsChunked += chunks

		default:
			// Try Tree-sitter universal extractor for other supported languages.
			if _, supported := SupportedExtensions()[ext]; supported {
				scanned, skipped, symbolCount, err := idx.indexSourceFile(ctx, workspaceID, rootPath, relPath, path)
				if err != nil {
					idx.logger.Warn("index source file failed",
						slog.String("path", relPath), slog.Any("error", err))
					return nil
				}
				result.FilesScanned += scanned
				result.FilesSkipped += skipped
				result.SymbolsFound += symbolCount
			}
		}

		return nil
	})

	result.Duration = time.Since(start)

	if err != nil {
		return result, fmt.Errorf("walk workspace %s: %w", rootPath, err)
	}

	idx.logger.Info("workspace index complete",
		slog.String("workspace_id", workspaceID),
		slog.Int("files_scanned", result.FilesScanned),
		slog.Int("files_skipped", result.FilesSkipped),
		slog.Int("symbols_found", result.SymbolsFound),
		slog.Int("docs_chunked", result.DocsChunked),
		slog.Duration("duration", result.Duration),
	)

	if idx.bus != nil {
		idx.bus.Publish(nervous.NewEvent(
			EventIndexWorkspaceIndexed,
			"indexer",
			workspaceID,
			result,
		))
	}

	return result, nil
}

// IndexFile indexes a single file within a workspace. The file is identified
// by its path relative to rootPath. Go, markdown, and Tree-sitter supported
// files (.py, .ts, .tsx, .js, .jsx, .rs) are processed; other extensions
// are silently ignored.
//
// Parameters:
//   - ctx:         context for cancellation
//   - workspaceID: identifier of the workspace
//   - rootPath:    absolute filesystem path to the workspace root
//   - relPath:     path to the file relative to rootPath
func (idx *Indexer) IndexFile(ctx context.Context, workspaceID, rootPath, relPath string) error {
	absPath := filepath.Join(rootPath, relPath)

	info, err := os.Stat(absPath)
	if err != nil {
		return fmt.Errorf("stat file %s: %w", relPath, err)
	}
	if info.IsDir() {
		return fmt.Errorf("path is a directory: %s", relPath)
	}

	if info.Size() > maxFileSize {
		return fmt.Errorf("file exceeds size limit (%d bytes): %s", info.Size(), relPath)
	}

	if isBinaryFile(absPath) {
		return fmt.Errorf("binary file skipped: %s", relPath)
	}

	ext := strings.ToLower(filepath.Ext(absPath))
	switch ext {
	case ".go":
		_, _, _, err = idx.indexGoFile(ctx, workspaceID, rootPath, relPath, absPath)
		if err != nil {
			return fmt.Errorf("index go file %s: %w", relPath, err)
		}
	case ".md":
		_, _, _, err = idx.indexMarkdownFile(ctx, workspaceID, relPath, absPath)
		if err != nil {
			return fmt.Errorf("index markdown file %s: %w", relPath, err)
		}
	default:
		if _, supported := SupportedExtensions()[ext]; supported {
			_, _, _, err = idx.indexSourceFile(ctx, workspaceID, rootPath, relPath, absPath)
			if err != nil {
				return fmt.Errorf("index source file %s: %w", relPath, err)
			}
		}
	}

	return nil
}

// IndexFileAs indexes a file at absPath but stores chunks/hashes under
// storagePath instead of the default relPath derived from rootPath. This is
// used for external docs where the on-disk location differs from the logical
// @ext/{name}/ path used for lookups and display.
func (idx *Indexer) IndexFileAs(ctx context.Context, workspaceID, absPath, storagePath string) error {
	info, err := os.Stat(absPath)
	if err != nil {
		return fmt.Errorf("stat file %s: %w", storagePath, err)
	}
	if info.IsDir() {
		return fmt.Errorf("path is a directory: %s", storagePath)
	}
	if info.Size() > maxFileSize {
		return fmt.Errorf("file exceeds size limit (%d bytes): %s", info.Size(), storagePath)
	}
	if isBinaryFile(absPath) {
		return fmt.Errorf("binary file skipped: %s", storagePath)
	}

	ext := strings.ToLower(filepath.Ext(absPath))
	if ext == ".md" {
		_, _, _, err = idx.indexMarkdownFile(ctx, workspaceID, storagePath, absPath)
		if err != nil {
			return fmt.Errorf("index markdown file %s: %w", storagePath, err)
		}
	}
	return nil
}

// codeChunkSize is the number of lines per code content chunk. Chunks of this
// size provide enough context for search results while keeping token counts
// manageable.
const codeChunkSize = 50

// indexGoFile indexes a single Go source file: computes hash, checks for
// changes, extracts symbols, chunks the file body for content search, and
// stores both. Returns counts for aggregation.
func (idx *Indexer) indexGoFile(ctx context.Context, workspaceID, rootPath, relPath, absPath string) (scanned, skipped, symbolCount int, err error) {
	hash, err := HashFile(absPath)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("hash file: %w", err)
	}

	// Check if the file has changed since the last index.
	storedHash, hashErr := idx.symbols.GetFileHash(ctx, workspaceID, relPath)
	if hashErr == nil && storedHash == hash {
		return 0, 1, 0, nil // unchanged
	}
	// If hashErr is non-nil and not "no rows" we treat the file as new.
	if hashErr != nil && !errors.Is(hashErr, sql.ErrNoRows) {
		// The sqlite implementation wraps the error, so a string check is needed
		// as a fallback. This is defensive — the primary check is errors.Is above.
		if !strings.Contains(hashErr.Error(), "no rows") {
			idx.logger.Warn("get file hash error (treating as new)",
				slog.String("path", relPath), slog.Any("error", hashErr))
		}
	}

	// Upsert the file hash and get the file ID for symbol association.
	fileID, err := idx.symbols.UpsertFileHash(ctx, workspaceID, relPath, hash)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("upsert file hash: %w", err)
	}

	// Delete stale symbols before re-extracting.
	if err := idx.symbols.DeleteByFile(ctx, fileID); err != nil {
		return 0, 0, 0, fmt.Errorf("delete old symbols: %w", err)
	}

	// Delete stale code content chunks before re-chunking.
	if err := idx.search.DeleteDocChunksByPath(ctx, workspaceID, relPath); err != nil {
		idx.logger.Warn("delete old code chunks failed",
			slog.String("path", relPath), slog.Any("error", err))
	}

	// Extract and store new symbols.
	symbols, err := ExtractGoSymbols(absPath)
	if err != nil {
		return 1, 0, 0, fmt.Errorf("extract symbols: %w", err)
	}

	for _, sym := range symbols {
		sym.FileID = fileID
		sym.WorkspaceID = workspaceID
		if err := idx.symbols.Upsert(ctx, sym); err != nil {
			return 1, 0, 0, fmt.Errorf("upsert symbol %s: %w", sym.Name, err)
		}
	}

	// Chunk the file body for code content search.
	if _, chunkErr := idx.chunkSourceFile(ctx, workspaceID, relPath, absPath, hash, symbols); chunkErr != nil {
		idx.logger.Warn("code content chunking failed",
			slog.String("path", relPath), slog.Any("error", chunkErr))
	}

	return 1, 0, len(symbols), nil
}

// indexMarkdownFile indexes a markdown file: computes hash, checks for changes,
// chunks the document, and stores chunks. Returns counts for aggregation.
func (idx *Indexer) indexMarkdownFile(ctx context.Context, workspaceID, relPath, absPath string) (scanned, skipped, chunks int, err error) {
	hash, err := HashFile(absPath)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("hash file: %w", err)
	}

	// Check if the file has changed since the last index.
	storedHash, hashErr := idx.symbols.GetFileHash(ctx, workspaceID, relPath)
	if hashErr == nil && storedHash == hash {
		return 0, 1, 0, nil // unchanged
	}

	// Upsert the file hash (markdown files also get tracked for change detection).
	if _, err := idx.symbols.UpsertFileHash(ctx, workspaceID, relPath, hash); err != nil {
		return 0, 0, 0, fmt.Errorf("upsert file hash: %w", err)
	}

	docChunks, err := ChunkMarkdown(absPath, workspaceID, hash)
	if err != nil {
		return 1, 0, 0, fmt.Errorf("chunk markdown: %w", err)
	}

	// Use the relative path for storage so chunks are workspace-portable.
	for _, chunk := range docChunks {
		chunk.FilePath = relPath
		if err := idx.search.UpsertDocChunk(ctx, chunk); err != nil {
			return 1, 0, 0, fmt.Errorf("upsert doc chunk: %w", err)
		}
	}

	return 1, 0, len(docChunks), nil
}

// indexSourceFile indexes a non-Go source file using the UniversalExtractor
// (Tree-sitter when available, no-op otherwise). Also chunks the file body
// for code content search. Follows the same hash-based incremental pattern
// as indexGoFile.
func (idx *Indexer) indexSourceFile(ctx context.Context, workspaceID, rootPath, relPath, absPath string) (scanned, skipped, symbolCount int, err error) {
	hash, err := HashFile(absPath)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("hash file: %w", err)
	}

	storedHash, hashErr := idx.symbols.GetFileHash(ctx, workspaceID, relPath)
	if hashErr == nil && storedHash == hash {
		return 0, 1, 0, nil // unchanged
	}

	fileID, err := idx.symbols.UpsertFileHash(ctx, workspaceID, relPath, hash)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("upsert file hash: %w", err)
	}

	if err := idx.symbols.DeleteByFile(ctx, fileID); err != nil {
		return 0, 0, 0, fmt.Errorf("delete old symbols: %w", err)
	}

	// Delete stale code content chunks before re-chunking.
	if err := idx.search.DeleteDocChunksByPath(ctx, workspaceID, relPath); err != nil {
		idx.logger.Warn("delete old code chunks failed",
			slog.String("path", relPath), slog.Any("error", err))
	}

	symbols, err := UniversalExtractor(absPath)
	if err != nil {
		return 1, 0, 0, fmt.Errorf("extract symbols: %w", err)
	}
	if symbols == nil {
		// Unsupported extension at runtime — still chunk the file body.
		if _, chunkErr := idx.chunkSourceFile(ctx, workspaceID, relPath, absPath, hash, nil); chunkErr != nil {
			idx.logger.Warn("code content chunking failed",
				slog.String("path", relPath), slog.Any("error", chunkErr))
		}
		return 1, 0, 0, nil
	}

	for _, sym := range symbols {
		sym.FileID = fileID
		sym.WorkspaceID = workspaceID
		if err := idx.symbols.Upsert(ctx, sym); err != nil {
			return 1, 0, 0, fmt.Errorf("upsert symbol %s: %w", sym.Name, err)
		}
	}

	// Chunk the file body for code content search.
	if _, chunkErr := idx.chunkSourceFile(ctx, workspaceID, relPath, absPath, hash, symbols); chunkErr != nil {
		idx.logger.Warn("code content chunking failed",
			slog.String("path", relPath), slog.Any("error", chunkErr))
	}

	return 1, 0, len(symbols), nil
}

// chunkSourceFile reads a source file and creates searchable content chunks
// stored as doc_chunks with content_type="code". Extracted symbols are used as
// section headers to provide context about the code region each chunk covers.
//
// Parameters:
//   - ctx:         context for cancellation
//   - workspaceID: identifier of the workspace
//   - relPath:     path to the file relative to the workspace root
//   - absPath:     absolute filesystem path to the file
//   - fileHash:    SHA-256 hash of the file content for change tracking
//   - symbols:     extracted symbols from the file (may be nil)
//
// Returns the number of chunks created, or an error if the file cannot be read
// or a chunk cannot be stored.
func (idx *Indexer) chunkSourceFile(ctx context.Context, workspaceID, relPath, absPath, fileHash string, symbols []*repo.Symbol) (int, error) {
	content, err := os.ReadFile(absPath)
	if err != nil {
		return 0, fmt.Errorf("read file: %w", err)
	}

	lines := strings.Split(string(content), "\n")
	chunkIdx := 0

	for start := 0; start < len(lines); start += codeChunkSize {
		end := start + codeChunkSize
		if end > len(lines) {
			end = len(lines)
		}

		chunkContent := strings.Join(lines[start:end], "\n")
		if strings.TrimSpace(chunkContent) == "" {
			continue
		}

		// Find the nearest symbol that starts within or before this chunk
		// to use as a descriptive section header.
		sectionHeader := fmt.Sprintf("%s (lines %d-%d)", relPath, start+1, end)
		for _, sym := range symbols {
			if sym.StartLine >= start+1 && sym.StartLine <= end {
				sectionHeader = fmt.Sprintf("%s %s (line %d)", sym.Kind, sym.Name, sym.StartLine)
				break
			}
		}

		chunk := &repo.DocChunk{
			WorkspaceID:   workspaceID,
			FilePath:      relPath,
			FileHash:      fileHash,
			ChunkIndex:    chunkIdx,
			SectionHeader: sectionHeader,
			Content:       chunkContent,
			TokenCount:    len(strings.Fields(chunkContent)),
			ContentType:   "code",
		}

		if err := idx.search.UpsertDocChunk(ctx, chunk); err != nil {
			return chunkIdx, fmt.Errorf("upsert code chunk: %w", err)
		}
		chunkIdx++
	}

	return chunkIdx, nil
}

// shouldIgnore returns true if the path is a directory that the indexer should
// skip. Ignored directories: .git, node_modules, vendor, .build, __pycache__,
// and any directory whose name starts with a dot (hidden directories).
func shouldIgnore(path string) bool {
	base := filepath.Base(path)

	if _, ok := ignoredDirs[base]; ok {
		return true
	}

	// Skip hidden directories (names starting with '.'), except the root itself.
	if strings.HasPrefix(base, ".") && base != "." {
		return true
	}

	return false
}

// shouldSkipFile returns true and a reason if the file should not be indexed.
// Files are skipped if they exceed maxFileSize or appear to be binary.
func shouldSkipFile(path string, d fs.DirEntry) (bool, string) {
	info, err := d.Info()
	if err != nil {
		return true, "stat error"
	}

	if info.Size() > maxFileSize {
		return true, "exceeds 1MB limit"
	}

	if isBinaryFile(path) {
		return true, "binary file"
	}

	return false, ""
}

// RemoveFile removes all indexed data (symbols, file hash, doc chunks) for a
// single file within a workspace. Used when a file is deleted from disk.
//
// Parameters:
//   - ctx:         context for cancellation
//   - workspaceID: identifier of the workspace
//   - relPath:     path to the file relative to the workspace root
//
// Returns an error if any repository deletion fails.
func (idx *Indexer) RemoveFile(ctx context.Context, workspaceID, relPath string) error {
	if err := idx.symbols.DeleteByWorkspacePath(ctx, workspaceID, relPath); err != nil {
		return fmt.Errorf("remove symbols for %s: %w", relPath, err)
	}

	// Delete doc_chunks for this file. This covers both markdown doc chunks
	// (content_type='doc') and source code content chunks (content_type='code').
	if err := idx.search.DeleteDocChunksByPath(ctx, workspaceID, relPath); err != nil {
		return fmt.Errorf("remove doc chunks for %s: %w", relPath, err)
	}

	idx.logger.Info("removed indexed file",
		slog.String("workspace_id", workspaceID),
		slog.String("path", relPath),
	)
	return nil
}

// isBinaryFile checks whether a file appears to be binary by reading the first
// 512 bytes and looking for null bytes.
func isBinaryFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()

	buf := make([]byte, 512)
	n, err := f.Read(buf)
	if err != nil {
		return false
	}

	for i := 0; i < n; i++ {
		if buf[i] == 0 {
			return true
		}
	}
	return false
}
