package sqlite

import (
	"testing"
	"time"

	"github.com/hyperax/hyperax/internal/repo"
)

// --- ProjectPlan Tests ---

func TestProjectRepo_CreateAndGetPlan(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &ProjectRepo{db: db.db}

	plan := &repo.ProjectPlan{
		Name:          "Test Plan",
		Description:   "A test project plan",
		WorkspaceName: "ws-test",
		Priority:      "high",
	}

	id, err := r.CreateProjectPlan(ctx, plan)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty ID")
	}

	got, err := r.GetProjectPlan(ctx, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if got.Name != "Test Plan" {
		t.Errorf("name = %q, want %q", got.Name, "Test Plan")
	}
	if got.Description != "A test project plan" {
		t.Errorf("description = %q, want %q", got.Description, "A test project plan")
	}
	if got.WorkspaceName != "ws-test" {
		t.Errorf("workspace_name = %q, want %q", got.WorkspaceName, "ws-test")
	}
	if got.Status != "pending" {
		t.Errorf("status = %q, want %q", got.Status, "pending")
	}
	if got.Priority != "high" {
		t.Errorf("priority = %q, want %q", got.Priority, "high")
	}
	if got.CreatedAt.IsZero() {
		t.Error("created_at should not be zero")
	}
}

func TestProjectRepo_GetPlanNotFound(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &ProjectRepo{db: db.db}

	_, err := r.GetProjectPlan(ctx, "nonexistent-id")
	if err == nil {
		t.Error("expected error for nonexistent plan")
	}
}

func TestProjectRepo_ListPlans(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &ProjectRepo{db: db.db}

	// Empty initially.
	plans, err := r.ListProjectPlans(ctx, "ws-test")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(plans) != 0 {
		t.Errorf("expected 0, got %d", len(plans))
	}

	// Add plans to two different workspaces.
	_, err = r.CreateProjectPlan(ctx, &repo.ProjectPlan{Name: "Plan A", WorkspaceName: "ws-test"})
	if err != nil {
		t.Fatalf("create a: %v", err)
	}
	_, err = r.CreateProjectPlan(ctx, &repo.ProjectPlan{Name: "Plan B", WorkspaceName: "ws-test"})
	if err != nil {
		t.Fatalf("create b: %v", err)
	}
	_, err = r.CreateProjectPlan(ctx, &repo.ProjectPlan{Name: "Plan C", WorkspaceName: "ws-other"})
	if err != nil {
		t.Fatalf("create c: %v", err)
	}

	plans, err = r.ListProjectPlans(ctx, "ws-test")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(plans) != 2 {
		t.Errorf("expected 2 plans for ws-test, got %d", len(plans))
	}
}

// --- Milestone Tests ---

func TestProjectRepo_CreateAndGetMilestone(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &ProjectRepo{db: db.db}

	planID, err := r.CreateProjectPlan(ctx, &repo.ProjectPlan{Name: "Plan", WorkspaceName: "ws"})
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}

	ms := &repo.Milestone{
		ProjectID:   planID,
		Name:        "Milestone 1",
		Description: "First milestone",
		Priority:    "high",
		OrderIndex:  0,
	}
	msID, err := r.CreateMilestone(ctx, ms)
	if err != nil {
		t.Fatalf("create milestone: %v", err)
	}
	if msID == "" {
		t.Fatal("expected non-empty milestone ID")
	}

	got, err := r.GetMilestone(ctx, msID)
	if err != nil {
		t.Fatalf("get milestone: %v", err)
	}

	if got.Name != "Milestone 1" {
		t.Errorf("name = %q, want %q", got.Name, "Milestone 1")
	}
	if got.ProjectID != planID {
		t.Errorf("project_id = %q, want %q", got.ProjectID, planID)
	}
	if got.Status != "pending" {
		t.Errorf("status = %q, want %q", got.Status, "pending")
	}
	if got.Priority != "high" {
		t.Errorf("priority = %q, want %q", got.Priority, "high")
	}
}

func TestProjectRepo_ListMilestones(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &ProjectRepo{db: db.db}

	planID, err := r.CreateProjectPlan(ctx, &repo.ProjectPlan{Name: "Plan", WorkspaceName: "ws"})
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}

	_, err = r.CreateMilestone(ctx, &repo.Milestone{ProjectID: planID, Name: "MS 1", OrderIndex: 1})
	if err != nil {
		t.Fatalf("create ms 1: %v", err)
	}
	_, err = r.CreateMilestone(ctx, &repo.Milestone{ProjectID: planID, Name: "MS 0", OrderIndex: 0})
	if err != nil {
		t.Fatalf("create ms 0: %v", err)
	}

	milestones, err := r.ListMilestones(ctx, planID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(milestones) != 2 {
		t.Fatalf("expected 2, got %d", len(milestones))
	}

	// Should be ordered by order_index: MS 0 first, then MS 1.
	if milestones[0].Name != "MS 0" {
		t.Errorf("first milestone = %q, want %q", milestones[0].Name, "MS 0")
	}
	if milestones[1].Name != "MS 1" {
		t.Errorf("second milestone = %q, want %q", milestones[1].Name, "MS 1")
	}
}

// --- Task Tests ---

func TestProjectRepo_CreateAndGetTask(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &ProjectRepo{db: db.db}

	planID, _ := r.CreateProjectPlan(ctx, &repo.ProjectPlan{Name: "Plan", WorkspaceName: "ws"})
	msID, _ := r.CreateMilestone(ctx, &repo.Milestone{ProjectID: planID, Name: "MS"})

	task := &repo.Task{
		MilestoneID: msID,
		Name:        "Task 1",
		Description: "Do the thing",
		Priority:    "critical",
	}
	taskID, err := r.CreateTask(ctx, task)
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	got, err := r.GetTask(ctx, taskID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}

	if got.Name != "Task 1" {
		t.Errorf("name = %q, want %q", got.Name, "Task 1")
	}
	if got.Description != "Do the thing" {
		t.Errorf("description = %q, want %q", got.Description, "Do the thing")
	}
	if got.Status != "pending" {
		t.Errorf("status = %q, want %q", got.Status, "pending")
	}
	if got.Priority != "critical" {
		t.Errorf("priority = %q, want %q", got.Priority, "critical")
	}
	if got.CreatedAt.IsZero() {
		t.Error("created_at should not be zero")
	}
}

func TestProjectRepo_UpdateTaskStatus(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &ProjectRepo{db: db.db}

	planID, _ := r.CreateProjectPlan(ctx, &repo.ProjectPlan{Name: "Plan", WorkspaceName: "ws"})
	msID, _ := r.CreateMilestone(ctx, &repo.Milestone{ProjectID: planID, Name: "MS"})
	taskID, _ := r.CreateTask(ctx, &repo.Task{MilestoneID: msID, Name: "Task"})

	if err := r.UpdateTaskStatus(ctx, taskID, "in-progress"); err != nil {
		t.Fatalf("update: %v", err)
	}

	got, err := r.GetTask(ctx, taskID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != "in-progress" {
		t.Errorf("status = %q, want %q", got.Status, "in-progress")
	}
}

func TestProjectRepo_UpdateTaskStatusNotFound(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &ProjectRepo{db: db.db}

	err := r.UpdateTaskStatus(ctx, "nonexistent", "completed")
	if err == nil {
		t.Error("expected error for nonexistent task")
	}
}

func TestProjectRepo_ListTasks(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &ProjectRepo{db: db.db}

	planID, _ := r.CreateProjectPlan(ctx, &repo.ProjectPlan{Name: "Plan", WorkspaceName: "ws"})
	msID, _ := r.CreateMilestone(ctx, &repo.Milestone{ProjectID: planID, Name: "MS"})

	_, _ = r.CreateTask(ctx, &repo.Task{MilestoneID: msID, Name: "Task B", OrderIndex: 1})
	_, _ = r.CreateTask(ctx, &repo.Task{MilestoneID: msID, Name: "Task A", OrderIndex: 0})

	tasks, err := r.ListTasks(ctx, msID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2, got %d", len(tasks))
	}

	// Should be ordered by order_index.
	if tasks[0].Name != "Task A" {
		t.Errorf("first task = %q, want %q", tasks[0].Name, "Task A")
	}
	if tasks[1].Name != "Task B" {
		t.Errorf("second task = %q, want %q", tasks[1].Name, "Task B")
	}
}

func TestProjectRepo_ListTasksByWorkspace(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &ProjectRepo{db: db.db}

	// Create two projects in different workspaces.
	planA, _ := r.CreateProjectPlan(ctx, &repo.ProjectPlan{Name: "Plan A", WorkspaceName: "ws-1"})
	planB, _ := r.CreateProjectPlan(ctx, &repo.ProjectPlan{Name: "Plan B", WorkspaceName: "ws-2"})
	msA, _ := r.CreateMilestone(ctx, &repo.Milestone{ProjectID: planA, Name: "MS A"})
	msB, _ := r.CreateMilestone(ctx, &repo.Milestone{ProjectID: planB, Name: "MS B"})
	_, _ = r.CreateTask(ctx, &repo.Task{MilestoneID: msA, Name: "Task in WS1"})
	_, _ = r.CreateTask(ctx, &repo.Task{MilestoneID: msB, Name: "Task in WS2"})

	tasks, err := r.ListTasksByWorkspace(ctx, "ws-1", "")
	if err != nil {
		t.Fatalf("list by workspace: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task for ws-1, got %d", len(tasks))
	}
	if tasks[0].Name != "Task in WS1" {
		t.Errorf("name = %q, want %q", tasks[0].Name, "Task in WS1")
	}
}

func TestProjectRepo_ListTasksByWorkspaceWithStatusFilter(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &ProjectRepo{db: db.db}

	planID, _ := r.CreateProjectPlan(ctx, &repo.ProjectPlan{Name: "Plan", WorkspaceName: "ws"})
	msID, _ := r.CreateMilestone(ctx, &repo.Milestone{ProjectID: planID, Name: "MS"})
	taskID, _ := r.CreateTask(ctx, &repo.Task{MilestoneID: msID, Name: "Done Task"})
	_, _ = r.CreateTask(ctx, &repo.Task{MilestoneID: msID, Name: "Pending Task"})

	// Mark one task completed.
	_ = r.UpdateTaskStatus(ctx, taskID, "completed")

	tasks, err := r.ListTasksByWorkspace(ctx, "ws", "completed")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 completed task, got %d", len(tasks))
	}
	if tasks[0].Name != "Done Task" {
		t.Errorf("name = %q, want %q", tasks[0].Name, "Done Task")
	}
}

func TestProjectRepo_AssignTask(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &ProjectRepo{db: db.db}

	planID, _ := r.CreateProjectPlan(ctx, &repo.ProjectPlan{Name: "Plan", WorkspaceName: "ws"})
	msID, _ := r.CreateMilestone(ctx, &repo.Milestone{ProjectID: planID, Name: "MS"})
	taskID, _ := r.CreateTask(ctx, &repo.Task{MilestoneID: msID, Name: "Task"})

	if err := r.AssignTask(ctx, taskID, "persona-42"); err != nil {
		t.Fatalf("assign: %v", err)
	}

	got, err := r.GetTask(ctx, taskID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.AssigneeAgentID != "persona-42" {
		t.Errorf("assignee = %q, want %q", got.AssigneeAgentID, "persona-42")
	}
}

// --- Comment Tests ---

func TestProjectRepo_AddAndListComments(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &ProjectRepo{db: db.db}

	planID, _ := r.CreateProjectPlan(ctx, &repo.ProjectPlan{Name: "Plan", WorkspaceName: "ws"})

	c1 := &repo.Comment{
		EntityType: "project",
		EntityID:   planID,
		Content:    "First comment",
		Author:     "alice",
	}
	id1, err := r.AddComment(ctx, c1)
	if err != nil {
		t.Fatalf("add comment 1: %v", err)
	}
	if id1 == "" {
		t.Fatal("expected non-empty comment ID")
	}

	c2 := &repo.Comment{
		EntityType: "project",
		EntityID:   planID,
		Content:    "Second comment",
		Author:     "bob",
	}
	_, err = r.AddComment(ctx, c2)
	if err != nil {
		t.Fatalf("add comment 2: %v", err)
	}

	comments, err := r.ListComments(ctx, "project", planID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(comments) != 2 {
		t.Fatalf("expected 2, got %d", len(comments))
	}

	if comments[0].Content != "First comment" {
		t.Errorf("first comment = %q, want %q", comments[0].Content, "First comment")
	}
	if comments[0].Author != "alice" {
		t.Errorf("first author = %q, want %q", comments[0].Author, "alice")
	}
	if comments[1].Content != "Second comment" {
		t.Errorf("second comment = %q, want %q", comments[1].Content, "Second comment")
	}
}

func TestProjectRepo_ListCommentsEmpty(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &ProjectRepo{db: db.db}

	comments, err := r.ListComments(ctx, "task", "nonexistent-id")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(comments) != 0 {
		t.Errorf("expected 0, got %d", len(comments))
	}
}

func TestProjectRepo_MilestoneDueDate(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &ProjectRepo{db: db.db}

	planID, _ := r.CreateProjectPlan(ctx, &repo.ProjectPlan{Name: "Plan", WorkspaceName: "ws"})

	// Milestone with no due date.
	msID1, _ := r.CreateMilestone(ctx, &repo.Milestone{ProjectID: planID, Name: "No Due"})
	got1, err := r.GetMilestone(ctx, msID1)
	if err != nil {
		t.Fatalf("get ms1: %v", err)
	}
	if got1.DueDate != nil {
		t.Errorf("expected nil due_date, got %v", got1.DueDate)
	}

	// Milestone with a due date.
	dueDate := parseTestDate(t, "2026-06-15 00:00:00")
	msID2, _ := r.CreateMilestone(ctx, &repo.Milestone{ProjectID: planID, Name: "Has Due", DueDate: &dueDate})
	got2, err := r.GetMilestone(ctx, msID2)
	if err != nil {
		t.Fatalf("get ms2: %v", err)
	}
	if got2.DueDate == nil {
		t.Fatal("expected non-nil due_date")
	}
	if got2.DueDate.Format("2006-01-02") != "2026-06-15" {
		t.Errorf("due_date = %q, want %q", got2.DueDate.Format("2006-01-02"), "2026-06-15")
	}
}

func TestProjectRepo_DefaultStatusAndPriority(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &ProjectRepo{db: db.db}

	// Plan with empty status/priority should get defaults.
	id, err := r.CreateProjectPlan(ctx, &repo.ProjectPlan{Name: "Defaults", WorkspaceName: "ws"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := r.GetProjectPlan(ctx, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != "pending" {
		t.Errorf("status = %q, want %q", got.Status, "pending")
	}
	if got.Priority != "medium" {
		t.Errorf("priority = %q, want %q", got.Priority, "medium")
	}
}

// parseTestDate is a test helper for creating time.Time values.
func parseTestDate(t *testing.T, s string) (result time.Time) {
	t.Helper()
	parsed, err := time.Parse("2006-01-02 15:04:05", s)
	if err != nil {
		t.Fatalf("parse date %q: %v", s, err)
	}
	return parsed
}
