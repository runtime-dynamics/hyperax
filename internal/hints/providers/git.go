package providers

import (
	"context"
	"fmt"

	"github.com/hyperax/hyperax/internal/hints"
	"github.com/hyperax/hyperax/internal/repo"
)

// GitProvider returns hints based on the workspace's current git state
// (branch, dirty status, recent commit info).
type GitProvider struct {
	git repo.GitRepo
}

// NewGitProvider creates a GitProvider. A nil git repo is tolerated.
func NewGitProvider(git repo.GitRepo) *GitProvider {
	return &GitProvider{git: git}
}

// Name returns the provider identifier.
func (p *GitProvider) Name() string { return "git" }

// GetHints retrieves the workspace's git info and surfaces it as contextual
// hints (branch name, dirty status, latest commit hash).
func (p *GitProvider) GetHints(ctx context.Context, req *hints.HintRequest) ([]hints.Hint, error) {
	if p.git == nil {
		return nil, nil
	}
	if req.WorkspaceID == "" {
		return nil, nil
	}

	info, err := p.git.GetInfo(ctx, req.WorkspaceID)
	if err != nil {
		return nil, nil // graceful degradation
	}

	var results []hints.Hint

	// Branch hint.
	if info.Branch != "" {
		results = append(results, hints.Hint{
			Provider:  "git",
			Category:  "git",
			Content:   fmt.Sprintf("Current branch: %s", info.Branch),
			Relevance: 0.4,
			Source:    req.WorkspaceID,
		})
	}

	// Dirty state hint — more relevant because the user may need reminding.
	if info.IsDirty {
		results = append(results, hints.Hint{
			Provider:  "git",
			Category:  "git",
			Content:   "Working tree has uncommitted changes.",
			Relevance: 0.6,
			Source:    req.WorkspaceID,
		})
	}

	// Latest commit hash for context.
	if info.CommitHash != "" {
		short := info.CommitHash
		if len(short) > 8 {
			short = short[:8]
		}
		results = append(results, hints.Hint{
			Provider:  "git",
			Category:  "git",
			Content:   fmt.Sprintf("HEAD commit: %s", short),
			Relevance: 0.3,
			Source:    req.WorkspaceID,
		})
	}

	// Remote URL hint for orientation.
	if info.RemoteURL != "" {
		results = append(results, hints.Hint{
			Provider:  "git",
			Category:  "git",
			Content:   fmt.Sprintf("Remote: %s", info.RemoteURL),
			Relevance: 0.2,
			Source:    req.WorkspaceID,
		})
	}

	// Submodules — surface count as a contextual hint.
	subs, err := p.git.ListSubmodules(ctx, req.WorkspaceID)
	if err == nil && len(subs) > 0 {
		results = append(results, hints.Hint{
			Provider:  "git",
			Category:  "git",
			Content:   fmt.Sprintf("Workspace contains %d submodule(s).", len(subs)),
			Relevance: 0.3,
			Source:    req.WorkspaceID,
		})
	}

	return results, nil
}
