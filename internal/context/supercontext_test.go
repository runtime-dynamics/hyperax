package context

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/internal/storage"
	"github.com/hyperax/hyperax/pkg/types"
)

// ---------------------------------------------------------------------------
// Mock repos
// ---------------------------------------------------------------------------

// mockWorkspaceRepo implements repo.WorkspaceRepo for testing.
type mockWorkspaceRepo struct {
	workspaces map[string]*types.WorkspaceInfo
}

func (m *mockWorkspaceRepo) WorkspaceExists(_ context.Context, name string) (bool, error) {
	_, ok := m.workspaces[name]
	return ok, nil
}

func (m *mockWorkspaceRepo) ListWorkspaces(_ context.Context) ([]*types.WorkspaceInfo, error) {
	var out []*types.WorkspaceInfo
	for _, ws := range m.workspaces {
		out = append(out, ws)
	}
	return out, nil
}

func (m *mockWorkspaceRepo) GetWorkspace(_ context.Context, name string) (*types.WorkspaceInfo, error) {
	ws, ok := m.workspaces[name]
	if !ok {
		return nil, os.ErrNotExist
	}
	return ws, nil
}

func (m *mockWorkspaceRepo) CreateWorkspace(_ context.Context, ws *types.WorkspaceInfo) error {
	m.workspaces[ws.Name] = ws
	return nil
}

func (m *mockWorkspaceRepo) DeleteWorkspace(_ context.Context, name string) error {
	if _, ok := m.workspaces[name]; !ok {
		return os.ErrNotExist
	}
	delete(m.workspaces, name)
	return nil
}

// mockProjectRepo implements repo.ProjectRepo for testing.
type mockProjectRepo struct {
	plans      []*repo.ProjectPlan
	milestones map[string][]*repo.Milestone // keyed by plan ID
	tasks      map[string][]*repo.Task      // keyed by milestone ID
}

func (m *mockProjectRepo) CreateProjectPlan(_ context.Context, _ *repo.ProjectPlan) (string, error) {
	return "", nil
}

func (m *mockProjectRepo) GetProjectPlan(_ context.Context, id string) (*repo.ProjectPlan, error) {
	for _, p := range m.plans {
		if p.ID == id {
			return p, nil
		}
	}
	return nil, os.ErrNotExist
}

func (m *mockProjectRepo) ListProjectPlans(_ context.Context, _ string) ([]*repo.ProjectPlan, error) {
	return m.plans, nil
}

func (m *mockProjectRepo) CreateMilestone(_ context.Context, _ *repo.Milestone) (string, error) {
	return "", nil
}

func (m *mockProjectRepo) GetMilestone(_ context.Context, id string) (*repo.Milestone, error) {
	for _, ms := range m.milestones {
		for _, milestone := range ms {
			if milestone.ID == id {
				return milestone, nil
			}
		}
	}
	return nil, os.ErrNotExist
}

func (m *mockProjectRepo) ListMilestones(_ context.Context, projectID string) ([]*repo.Milestone, error) {
	return m.milestones[projectID], nil
}

func (m *mockProjectRepo) CreateTask(_ context.Context, _ *repo.Task) (string, error) {
	return "", nil
}

func (m *mockProjectRepo) GetTask(_ context.Context, id string) (*repo.Task, error) {
	for _, tasks := range m.tasks {
		for _, t := range tasks {
			if t.ID == id {
				return t, nil
			}
		}
	}
	return nil, os.ErrNotExist
}

func (m *mockProjectRepo) UpdateTaskStatus(_ context.Context, _ string, _ string) error {
	return nil
}

func (m *mockProjectRepo) ListTasks(_ context.Context, milestoneID string) ([]*repo.Task, error) {
	return m.tasks[milestoneID], nil
}

func (m *mockProjectRepo) AddComment(_ context.Context, _ *repo.Comment) (string, error) {
	return "", nil
}

func (m *mockProjectRepo) ListComments(_ context.Context, _, _ string) ([]*repo.Comment, error) {
	return nil, nil
}

func (m *mockProjectRepo) DeleteProjectPlan(_ context.Context, _ string) error {
	return nil
}

func (m *mockProjectRepo) UpdateProjectStatus(_ context.Context, _ string, _ string) error {
	return nil
}

func (m *mockProjectRepo) MoveProjectWorkspace(_ context.Context, _ string, _ string) error {
	return nil
}

func (m *mockProjectRepo) AssignMilestone(_ context.Context, _ string, _ string) error {
	return nil
}

func (m *mockProjectRepo) UnassignMilestone(_ context.Context, _ string) error {
	return nil
}

func (m *mockProjectRepo) AssignTask(_ context.Context, _ string, _ string) error {
	return nil
}

func (m *mockProjectRepo) UnassignTask(_ context.Context, _ string) error {
	return nil
}

func (m *mockProjectRepo) DeleteMilestone(_ context.Context, _ string) error { return nil }
func (m *mockProjectRepo) DeleteTask(_ context.Context, _ string) error      { return nil }
func (m *mockProjectRepo) PurgeOrphans(_ context.Context) (int64, error)     { return 0, nil }
func (m *mockProjectRepo) ReconcileCompletionStatus(_ context.Context) (int, int, error) {
	return 0, 0, nil
}

func (m *mockProjectRepo) ListTasksByAgent(_ context.Context, _ string, _ string, _ string) ([]*repo.Task, error) {
	return nil, nil
}
func (m *mockProjectRepo) GetNextTask(_ context.Context, _ string) (*repo.Task, error) {
	return nil, nil
}

// mockPipelineRepo implements repo.PipelineRepo for testing.
type mockPipelineRepo struct {
	pipelines []*repo.Pipeline
}

func (m *mockPipelineRepo) CreatePipeline(_ context.Context, _ *repo.Pipeline) (string, error) {
	return "", nil
}

func (m *mockPipelineRepo) GetPipeline(_ context.Context, id string) (*repo.Pipeline, error) {
	for _, p := range m.pipelines {
		if p.ID == id {
			return p, nil
		}
	}
	return nil, os.ErrNotExist
}

func (m *mockPipelineRepo) ListPipelines(_ context.Context, _ string) ([]*repo.Pipeline, error) {
	return m.pipelines, nil
}

func (m *mockPipelineRepo) CreateJob(_ context.Context, _, _ string) (string, error) {
	return "", nil
}

func (m *mockPipelineRepo) GetJob(_ context.Context, _ string) (*repo.PipelineJob, error) {
	return nil, os.ErrNotExist
}

func (m *mockPipelineRepo) UpdateJobStatus(_ context.Context, _, _, _ string) error {
	return nil
}

func (m *mockPipelineRepo) ListJobs(_ context.Context, _ string) ([]*repo.PipelineJob, error) {
	return nil, nil
}

func (m *mockPipelineRepo) CreateStepResult(_ context.Context, _ *repo.StepResult) (string, error) {
	return "", nil
}

func (m *mockPipelineRepo) ListStepResults(_ context.Context, _ string) ([]*repo.StepResult, error) {
	return nil, nil
}

func (m *mockPipelineRepo) SearchPipelines(_ context.Context, _ string, _ string) ([]*repo.Pipeline, error) {
	return m.pipelines, nil
}

func (m *mockPipelineRepo) ListJobsFiltered(_ context.Context, _ string, _ repo.JobFilter) ([]*repo.PipelineJob, error) {
	return nil, nil
}

func (m *mockPipelineRepo) CreateAssignment(_ context.Context, _ *repo.PipelineAssignment) (string, error) {
	return "", nil
}

func (m *mockPipelineRepo) ListAssignments(_ context.Context, _ string, _ string) ([]*repo.PipelineAssignment, error) {
	return nil, nil
}

func (m *mockPipelineRepo) DeleteAssignment(_ context.Context, _ string) error {
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newTestStore creates a Store with all the mock repos wired up.
func newTestStore(root string, projects *mockProjectRepo, pipelines *mockPipelineRepo) *storage.Store {
	store := &storage.Store{
		Workspaces: &mockWorkspaceRepo{
			workspaces: map[string]*types.WorkspaceInfo{
				"test-ws": {ID: "ws-001", Name: "test-ws", RootPath: root},
			},
		},
	}
	if projects != nil {
		store.Projects = projects
	}
	if pipelines != nil {
		store.Pipelines = pipelines
	}
	return store
}

// writeTestFile creates a file relative to root.
func writeTestFile(t *testing.T, root, relPath, content string) {
	t.Helper()
	full := filepath.Join(root, relPath)
	dir := filepath.Dir(full)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", full, err)
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestGenerate_MinimalWorkspace(t *testing.T) {
	root := t.TempDir()
	store := newTestStore(root, nil, nil)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	gen := NewSuperContextGenerator(store, nil, logger)

	ctx := context.Background()
	content, err := gen.Generate(ctx, "test-ws", root)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	// Must contain the managed header.
	if !strings.HasPrefix(content, managedHeader) {
		t.Error("generated content should start with CASAT_MANAGED header")
	}

	// Must contain workspace name.
	if !strings.Contains(content, "test-ws") {
		t.Error("generated content should contain workspace name")
	}

	// Must contain root path.
	if !strings.Contains(content, root) {
		t.Error("generated content should contain root path")
	}

	// Must contain tool workflow section.
	if !strings.Contains(content, "Tool-First Workflow") {
		t.Error("generated content should contain Tool-First Workflow section")
	}

	// Must contain operational mandates.
	if !strings.Contains(content, "CASAT Operational Mandates") {
		t.Error("generated content should contain operational mandates")
	}
}

func TestGenerate_WithProjectPlans(t *testing.T) {
	root := t.TempDir()
	projects := &mockProjectRepo{
		plans: []*repo.ProjectPlan{
			{
				ID:       "plan-001",
				Name:     "Phase 1 Implementation",
				Status:   "in-progress",
				Priority: "high",
			},
			{
				ID:       "plan-002",
				Name:     "Documentation Uplift",
				Status:   "pending",
				Priority: "medium",
			},
		},
		milestones: map[string][]*repo.Milestone{},
		tasks:      map[string][]*repo.Task{},
	}
	store := newTestStore(root, projects, nil)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	gen := NewSuperContextGenerator(store, nil, logger)

	ctx := context.Background()
	content, err := gen.Generate(ctx, "test-ws", root)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if !strings.Contains(content, "Phase 1 Implementation") {
		t.Error("generated content should list plan name")
	}
	if !strings.Contains(content, "plan-001") {
		t.Error("generated content should list plan ID")
	}
	if !strings.Contains(content, "Documentation Uplift") {
		t.Error("generated content should list second plan")
	}
}

func TestGenerate_WithActiveTasks(t *testing.T) {
	root := t.TempDir()
	projects := &mockProjectRepo{
		plans: []*repo.ProjectPlan{
			{ID: "plan-001", Name: "Main Plan", Status: "in-progress", Priority: "high"},
		},
		milestones: map[string][]*repo.Milestone{
			"plan-001": {
				{ID: "ms-001", ProjectID: "plan-001", Name: "Core Features"},
			},
		},
		tasks: map[string][]*repo.Task{
			"ms-001": {
				{ID: "task-001", MilestoneID: "ms-001", Name: "Build search", Status: "in-progress", Priority: "high"},
				{ID: "task-002", MilestoneID: "ms-001", Name: "Build API", Status: "completed", Priority: "medium"},
				{ID: "task-003", MilestoneID: "ms-001", Name: "Build CLI", Status: "in-progress", Priority: "low"},
			},
		},
	}
	store := newTestStore(root, projects, nil)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	gen := NewSuperContextGenerator(store, nil, logger)

	ctx := context.Background()
	content, err := gen.Generate(ctx, "test-ws", root)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	// Active tasks section should appear.
	if !strings.Contains(content, "Active Tasks") {
		t.Error("generated content should contain Active Tasks section")
	}

	// In-progress tasks should be listed.
	if !strings.Contains(content, "Build search") {
		t.Error("generated content should list in-progress task 'Build search'")
	}
	if !strings.Contains(content, "Build CLI") {
		t.Error("generated content should list in-progress task 'Build CLI'")
	}

	// Completed tasks should NOT be listed in active.
	if strings.Contains(content, "Build API") {
		t.Error("completed task 'Build API' should not appear in Active Tasks")
	}
}

func TestGenerate_WithPipelines(t *testing.T) {
	root := t.TempDir()
	pipelines := &mockPipelineRepo{
		pipelines: []*repo.Pipeline{
			{ID: "pipe-001", Name: "build", Description: "Go build pipeline", WorkspaceName: "test-ws"},
			{ID: "pipe-002", Name: "test", Description: "Go test pipeline", WorkspaceName: "test-ws"},
		},
	}
	store := newTestStore(root, nil, pipelines)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	gen := NewSuperContextGenerator(store, nil, logger)

	ctx := context.Background()
	content, err := gen.Generate(ctx, "test-ws", root)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if !strings.Contains(content, "Pipelines") {
		t.Error("generated content should contain Pipelines section")
	}
	if !strings.Contains(content, "Go build pipeline") {
		t.Error("generated content should list build pipeline")
	}
	if !strings.Contains(content, "Go test pipeline") {
		t.Error("generated content should list test pipeline")
	}
}

func TestGenerate_WithDocs(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "docs/Architecture.md", "# Architecture\n")
	writeTestFile(t, root, "docs/Vision.md", "# Vision\n")
	writeTestFile(t, root, "docs/notes.txt", "not markdown")

	store := newTestStore(root, nil, nil)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	gen := NewSuperContextGenerator(store, nil, logger)

	ctx := context.Background()
	content, err := gen.Generate(ctx, "test-ws", root)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if !strings.Contains(content, "Documentation") {
		t.Error("generated content should contain Documentation section")
	}
	if !strings.Contains(content, "Architecture.md") {
		t.Error("generated content should list Architecture.md")
	}
	if !strings.Contains(content, "Vision.md") {
		t.Error("generated content should list Vision.md")
	}
	// Non-markdown files should not appear.
	if strings.Contains(content, "notes.txt") {
		t.Error("notes.txt should not appear in docs listing")
	}
}

func TestGenerate_WithCodingStandards(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "docs/CodingGuidelines.md", "# Standards\n\nUse gofmt.\n")

	store := newTestStore(root, nil, nil)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	gen := NewSuperContextGenerator(store, nil, logger)

	ctx := context.Background()
	content, err := gen.Generate(ctx, "test-ws", root)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if !strings.Contains(content, "Coding Standards") {
		t.Error("generated content should contain Coding Standards section")
	}
	if !strings.Contains(content, "get_standards") {
		t.Error("generated content should reference get_standards tool")
	}
}

func TestGenerate_NoCodingStandards(t *testing.T) {
	root := t.TempDir()
	// No docs directory at all.

	store := newTestStore(root, nil, nil)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	gen := NewSuperContextGenerator(store, nil, logger)

	ctx := context.Background()
	content, err := gen.Generate(ctx, "test-ws", root)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if !strings.Contains(content, "No coding standards file found") {
		t.Error("generated content should note missing coding standards")
	}
}

func TestWriteContextFile_NewFile(t *testing.T) {
	root := t.TempDir()
	store := newTestStore(root, nil, nil)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	gen := NewSuperContextGenerator(store, nil, logger)

	ctx := context.Background()
	err := gen.WriteContextFile(ctx, "test-ws", root)
	if err != nil {
		t.Fatalf("WriteContextFile: %v", err)
	}

	// Verify file exists and has the managed header.
	data, err := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read CLAUDE.md: %v", err)
	}

	content := string(data)
	if !strings.HasPrefix(content, managedHeader) {
		t.Error("written file should start with CASAT_MANAGED header")
	}
	if !strings.Contains(content, "test-ws") {
		t.Error("written file should contain workspace name")
	}
}

func TestWriteContextFile_OverwritesManaged(t *testing.T) {
	root := t.TempDir()
	claudePath := filepath.Join(root, "CLAUDE.md")

	// Write an existing managed file.
	oldContent := managedHeader + "\n\n# Old content\n"
	if err := os.WriteFile(claudePath, []byte(oldContent), 0o644); err != nil {
		t.Fatalf("write old: %v", err)
	}

	store := newTestStore(root, nil, nil)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	gen := NewSuperContextGenerator(store, nil, logger)

	ctx := context.Background()
	err := gen.WriteContextFile(ctx, "test-ws", root)
	if err != nil {
		t.Fatalf("WriteContextFile: %v", err)
	}

	// File should be updated.
	data, err := os.ReadFile(claudePath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(string(data), "Old content") {
		t.Error("old content should have been replaced")
	}
}

func TestWriteContextFile_RefusesOverwriteUnmanaged(t *testing.T) {
	root := t.TempDir()
	claudePath := filepath.Join(root, "CLAUDE.md")

	// Write an existing user-authored file (no CASAT_MANAGED header).
	userContent := "# My Custom Rules\n\nDo not overwrite me.\n"
	if err := os.WriteFile(claudePath, []byte(userContent), 0o644); err != nil {
		t.Fatalf("write user file: %v", err)
	}

	store := newTestStore(root, nil, nil)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	gen := NewSuperContextGenerator(store, nil, logger)

	ctx := context.Background()
	err := gen.WriteContextFile(ctx, "test-ws", root)
	if err == nil {
		t.Fatal("expected error when overwriting unmanaged file, got nil")
	}
	if !strings.Contains(err.Error(), "not CASAT-managed") {
		t.Errorf("expected 'not CASAT-managed' error, got: %v", err)
	}

	// Verify the original content is preserved.
	data, err := os.ReadFile(claudePath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(data), "Do not overwrite me") {
		t.Error("user-authored content should be preserved")
	}
}

func TestGenerate_NilLogger(t *testing.T) {
	root := t.TempDir()
	store := newTestStore(root, nil, nil)

	// Pass nil logger -- should use slog.Default() and not panic.
	gen := NewSuperContextGenerator(store, nil, nil)

	ctx := context.Background()
	content, err := gen.Generate(ctx, "test-ws", root)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.Contains(content, "test-ws") {
		t.Error("generated content should contain workspace name")
	}
}

func TestGenerate_FullIntegration(t *testing.T) {
	root := t.TempDir()

	// Set up docs.
	writeTestFile(t, root, "docs/Architecture.md", "# Architecture\n")
	writeTestFile(t, root, "docs/CodingGuidelines.md", "# Standards\n")

	// Set up project plans with active tasks.
	projects := &mockProjectRepo{
		plans: []*repo.ProjectPlan{
			{ID: "p1", Name: "Big Plan", Status: "in-progress", Priority: "high"},
		},
		milestones: map[string][]*repo.Milestone{
			"p1": {{ID: "m1", ProjectID: "p1", Name: "Foundation"}},
		},
		tasks: map[string][]*repo.Task{
			"m1": {
				{ID: "t1", MilestoneID: "m1", Name: "Active Task", Status: "in-progress", Priority: "high",
					CreatedAt: time.Now(), UpdatedAt: time.Now()},
			},
		},
	}

	pipelines := &mockPipelineRepo{
		pipelines: []*repo.Pipeline{
			{ID: "pip1", Name: "validate", Description: "Run linters and tests"},
		},
	}

	store := newTestStore(root, projects, pipelines)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	gen := NewSuperContextGenerator(store, nil, logger)

	ctx := context.Background()
	content, err := gen.Generate(ctx, "test-ws", root)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	// Verify all major sections are present.
	requiredSections := []string{
		"CASAT_MANAGED",
		"Project Overview",
		"Active Project Plans",
		"Active Tasks",
		"CASAT Operational Mandates",
		"Tool-First Workflow",
		"Coding Standards",
		"Documentation",
		"Pipelines",
	}

	for _, section := range requiredSections {
		if !strings.Contains(content, section) {
			t.Errorf("generated content missing required section: %s", section)
		}
	}

	// Verify specific content.
	if !strings.Contains(content, "Big Plan") {
		t.Error("should contain plan name")
	}
	if !strings.Contains(content, "Active Task") {
		t.Error("should contain active task name")
	}
	if !strings.Contains(content, "validate") {
		t.Error("should contain pipeline name")
	}
}

func TestGeneratedAt(t *testing.T) {
	ts := GeneratedAt()
	if ts == "" {
		t.Error("GeneratedAt should return non-empty string")
	}
	// Should be parseable as RFC3339.
	if _, err := time.Parse(time.RFC3339, ts); err != nil {
		t.Errorf("GeneratedAt returned non-RFC3339 timestamp: %s", ts)
	}
}
