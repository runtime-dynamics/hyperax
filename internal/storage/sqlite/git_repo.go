package sqlite

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/hyperax/hyperax/internal/repo"
)

// GitRepo implements repo.GitRepo by executing git commands against workspace
// root directories. It resolves workspace names to filesystem paths via the
// WorkspaceRepo.
type GitRepo struct {
	workspaces repo.WorkspaceRepo
}

// NewGitRepo creates a GitRepo that resolves workspace paths through the
// provided WorkspaceRepo.
func NewGitRepo(workspaces repo.WorkspaceRepo) *GitRepo {
	return &GitRepo{workspaces: workspaces}
}

// GetInfo returns branch, commit hash, remote URL, and dirty status for the
// given workspace's git repository.
func (r *GitRepo) GetInfo(ctx context.Context, workspaceName string) (*repo.GitInfo, error) {
	rootPath, err := r.resolveWorkspacePath(ctx, workspaceName)
	if err != nil {
		return nil, fmt.Errorf("sqlite.GitRepo.GetInfo: %w", err)
	}

	info := &repo.GitInfo{WorkspaceName: workspaceName}

	// Current branch
	branch, err := r.runGit(ctx, rootPath, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("sqlite.GitRepo.GetInfo: %w", err)
	}
	info.Branch = strings.TrimSpace(branch)

	// Current commit hash
	hash, err := r.runGit(ctx, rootPath, "rev-parse", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("sqlite.GitRepo.GetInfo: %w", err)
	}
	info.CommitHash = strings.TrimSpace(hash)

	// Remote URL (best-effort; repos without remotes are valid)
	remote, err := r.runGit(ctx, rootPath, "remote", "get-url", "origin")
	if err == nil {
		info.RemoteURL = strings.TrimSpace(remote)
	}

	// Dirty check via porcelain status
	status, err := r.runGit(ctx, rootPath, "status", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("sqlite.GitRepo.GetInfo: %w", err)
	}
	info.IsDirty = strings.TrimSpace(status) != ""

	return info, nil
}

// ListSubmodules parses the .gitmodules file in the workspace root and returns
// all declared submodules.
func (r *GitRepo) ListSubmodules(ctx context.Context, workspaceName string) ([]*repo.Submodule, error) {
	rootPath, err := r.resolveWorkspacePath(ctx, workspaceName)
	if err != nil {
		return nil, fmt.Errorf("sqlite.GitRepo.ListSubmodules: %w", err)
	}

	gitmodulesPath := filepath.Join(rootPath, ".gitmodules")
	f, err := os.Open(gitmodulesPath)
	if os.IsNotExist(err) {
		return nil, nil // no submodules
	}
	if err != nil {
		return nil, fmt.Errorf("sqlite.GitRepo.ListSubmodules: %w", err)
	}
	defer func() { _ = f.Close() }()

	return parseGitmodules(f)
}

// GetRecentChanges returns recent commits and their changed files for the given
// workspace. The limit parameter controls how many commits to return (clamped to
// 1-100). An optional pathFilter restricts results to commits touching that path.
func (r *GitRepo) GetRecentChanges(ctx context.Context, workspaceName string, limit int, pathFilter string) ([]*repo.RecentChange, error) {
	rootPath, err := r.resolveWorkspacePath(ctx, workspaceName)
	if err != nil {
		return nil, fmt.Errorf("sqlite.GitRepo.GetRecentChanges: %w", err)
	}

	// Clamp limit to a sane range.
	if limit < 1 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}

	// Build git log command: --format produces "HASH SUBJECT" per commit,
	// --name-only appends changed file paths, separated by blank lines.
	args := []string{
		"log",
		fmt.Sprintf("--max-count=%d", limit),
		"--format=%h %s",
		"--name-only",
	}
	if pathFilter != "" {
		args = append(args, "--", pathFilter)
	}

	out, err := r.runGit(ctx, rootPath, args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite.GitRepo.GetRecentChanges: %w", err)
	}

	return parseRecentChanges(out), nil
}

// parseRecentChanges splits the git log --name-only output into structured
// RecentChange entries. Each commit block is separated by a blank line: the
// first line contains "hash subject", subsequent non-empty lines are file paths.
func parseRecentChanges(output string) []*repo.RecentChange {
	var changes []*repo.RecentChange
	var current *repo.RecentChange

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)

		if line == "" {
			// Blank line separates commit blocks; start a new block on the next
			// non-empty line.
			continue
		}

		// Heuristic: a line that starts with a short hex hash followed by a space
		// is a commit header. Git short hashes are 7-12 hex characters.
		if isCommitHeader(line) {
			// Save previous commit if present.
			if current != nil {
				changes = append(changes, current)
			}
			hash, subject := splitCommitHeader(line)
			current = &repo.RecentChange{
				Hash:    hash,
				Subject: subject,
			}
		} else if current != nil {
			// Non-header, non-empty line is a file path.
			current.Files = append(current.Files, line)
		}
	}

	// Flush last commit.
	if current != nil {
		changes = append(changes, current)
	}

	return changes
}

// isCommitHeader returns true if the line looks like a "hash subject" commit
// header from git log --format="%h %s".
func isCommitHeader(line string) bool {
	spaceIdx := strings.IndexByte(line, ' ')
	if spaceIdx < 1 {
		// Could be a hash-only line (no subject) -- still valid.
		return isHexString(line) && len(line) >= 7
	}
	hash := line[:spaceIdx]
	return len(hash) >= 7 && len(hash) <= 12 && isHexString(hash)
}

// splitCommitHeader splits "hash subject" into its two parts.
func splitCommitHeader(line string) (hash, subject string) {
	spaceIdx := strings.IndexByte(line, ' ')
	if spaceIdx < 0 {
		return line, ""
	}
	return line[:spaceIdx], line[spaceIdx+1:]
}

// isHexString returns true if every byte in s is a hexadecimal digit.
func isHexString(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return len(s) > 0
}

// resolveWorkspacePath looks up the root filesystem path for a workspace name.
func (r *GitRepo) resolveWorkspacePath(ctx context.Context, workspaceName string) (string, error) {
	ws, err := r.workspaces.GetWorkspace(ctx, workspaceName)
	if err != nil {
		return "", fmt.Errorf("sqlite.GitRepo.resolveWorkspacePath: resolve workspace %q: %w", workspaceName, err)
	}
	return ws.RootPath, nil
}

// runGit executes a git command in the specified directory and returns its
// combined stdout output. stderr is discarded.
func (r *GitRepo) runGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("sqlite.GitRepo.runGit: git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

// parseGitmodules reads a .gitmodules INI-style file and extracts submodule
// entries. Each [submodule "name"] section is expected to contain path, url,
// and optionally branch keys.
func parseGitmodules(f *os.File) ([]*repo.Submodule, error) {
	var submodules []*repo.Submodule
	var current *repo.Submodule

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Section header: [submodule "name"]
		if strings.HasPrefix(line, "[submodule") {
			if current != nil {
				submodules = append(submodules, current)
			}
			name := extractSubmoduleName(line)
			current = &repo.Submodule{Name: name}
			continue
		}

		if current == nil {
			continue
		}

		// Key-value pairs within a section
		key, value, ok := parseINILine(line)
		if !ok {
			continue
		}

		switch key {
		case "path":
			current.Path = value
		case "url":
			current.URL = value
		case "branch":
			current.Branch = value
		}
	}

	// Flush the last section
	if current != nil {
		submodules = append(submodules, current)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("sqlite.parseGitmodules: %w", err)
	}

	return submodules, nil
}

// extractSubmoduleName pulls the quoted name out of a [submodule "name"] header.
func extractSubmoduleName(line string) string {
	start := strings.Index(line, "\"")
	end := strings.LastIndex(line, "\"")
	if start >= 0 && end > start {
		return line[start+1 : end]
	}
	return ""
}

// parseINILine splits a "key = value" line. Returns false if the line is not
// a valid key-value pair.
func parseINILine(line string) (key, value string, ok bool) {
	parts := strings.SplitN(line, "=", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), true
}
