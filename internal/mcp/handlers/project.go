package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/hyperax/hyperax/internal/mcp"
	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/internal/storage"
	sqliteStore "github.com/hyperax/hyperax/internal/storage/sqlite"
	"github.com/hyperax/hyperax/pkg/types"
)

// actionClearanceProject maps each project action to its minimum ABAC clearance.
var actionClearanceProject = map[string]int{
	"list_projects":          0,
	"get_details":            0,
	"create":                 1,
	"archive":                1,
	"delete":                 1,
	"move_workspace":         1,
	"add_milestone":          1,
	"assign_milestone":       1,
	"unassign_milestone":     1,
	"delete_milestone":       1,
	"add_task":               1,
	"list_tasks":             0,
	"update_task_status":     1,
	"assign_task":            1,
	"unassign_task":          1,
	"delete_task":            1,
	"get_my_tasks":           0,
	"check_for_assignments":  0,
	"get_task":               0,
	"list_persona_tasks":     0,
	"add_comment":            1,
	"purge_orphans":          1,
}

// ProjectHandler implements the consolidated "project" MCP tool.
type ProjectHandler struct {
	store *storage.Store
}

// NewProjectHandler creates a ProjectHandler.
func NewProjectHandler(store *storage.Store) *ProjectHandler {
	return &ProjectHandler{store: store}
}

// RegisterTools registers the consolidated "project" MCP tool.
func (h *ProjectHandler) RegisterTools(registry *mcp.ToolRegistry) {
	registry.Register(
		"project",
		"Project management: plans, milestones, tasks, assignments, and comments",
		json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {
					"type": "string",
					"enum": [
						"list_projects", "get_details", "create", "archive", "delete",
						"move_workspace", "add_milestone", "assign_milestone", "unassign_milestone",
						"delete_milestone", "add_task", "list_tasks", "update_task_status",
						"assign_task", "unassign_task", "delete_task", "get_my_tasks",
						"check_for_assignments", "get_task", "list_persona_tasks",
						"add_comment", "purge_orphans"
					],
					"description": "The project action to perform"
				},
				"workspace_name": {"type": "string", "description": "Workspace name (for list_projects, create, list_tasks)"},
				"project_id": {"type": "string", "description": "Project plan ID"},
				"milestone_id": {"type": "string", "description": "Milestone ID"},
				"task_id": {"type": "string", "description": "Task ID"},
				"agent_id": {"type": "string", "description": "Agent ID"},
				"name": {"type": "string", "description": "Name for project/milestone/task"},
				"description": {"type": "string", "description": "Description"},
				"priority": {"type": "string", "description": "Priority: low, medium, high, critical"},
				"status": {"type": "string", "description": "Status filter or new status"},
				"due_date": {"type": "string", "description": "Due date in YYYY-MM-DD format"},
				"target_workspace_id": {"type": "string", "description": "Target workspace for move_workspace"},
				"entity_type": {"type": "string", "description": "Entity type for add_comment: project, milestone, task"},
				"entity_id": {"type": "string", "description": "Entity ID for add_comment"},
				"content": {"type": "string", "description": "Comment content for add_comment"},
				"author": {"type": "string", "description": "Author for add_comment"}
			},
			"required": ["action"]
		}`),
		h.dispatch,
	)
}

// dispatch routes the consolidated "project" tool to the appropriate handler method.
func (h *ProjectHandler) dispatch(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var envelope struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal(params, &envelope); err != nil {
		return nil, fmt.Errorf("handlers.ProjectHandler.dispatch: %w", err)
	}

	if err := checkActionClearance(ctx, envelope.Action, actionClearanceProject); err != nil {
		return types.NewErrorResult(err.Error()), nil
	}

	switch envelope.Action {
	case "list_projects":
		return h.listProjects(ctx, params)
	case "get_details":
		return h.getProjectDetails(ctx, params)
	case "create":
		return h.createProjectPlan(ctx, params)
	case "archive":
		return h.archiveProject(ctx, params)
	case "delete":
		return h.deleteProject(ctx, params)
	case "move_workspace":
		return h.moveProjectWorkspace(ctx, params)
	case "add_milestone":
		return h.addMilestone(ctx, params)
	case "assign_milestone":
		return h.assignMilestone(ctx, params)
	case "unassign_milestone":
		return h.unassignMilestone(ctx, params)
	case "delete_milestone":
		return h.deleteMilestone(ctx, params)
	case "add_task":
		return h.addTask(ctx, params)
	case "list_tasks":
		return h.listTasks(ctx, params)
	case "update_task_status":
		return h.updateTaskStatus(ctx, params)
	case "assign_task":
		return h.assignTask(ctx, params)
	case "unassign_task":
		return h.unassignTask(ctx, params)
	case "delete_task":
		return h.deleteTask(ctx, params)
	case "get_my_tasks":
		return h.getMyTasks(ctx, params)
	case "check_for_assignments":
		return h.checkForAssignments(ctx, params)
	case "get_task":
		return h.getTask(ctx, params)
	case "list_persona_tasks":
		return h.listPersonaTasks(ctx, params)
	case "add_comment":
		return h.addComment(ctx, params)
	case "purge_orphans":
		return h.purgeOrphans(ctx, params)
	default:
		return types.NewErrorResult(fmt.Sprintf("unknown project action %q", envelope.Action)), nil
	}
}

// projectRepo returns the project repository, or an error result if unavailable.
func (h *ProjectHandler) projectRepo() (repo.ProjectRepo, *types.ToolResult) {
	if h.store.Projects == nil {
		return nil, types.NewErrorResult("Project repository not available.")
	}
	return h.store.Projects, nil
}

// sqliteProjectRepo attempts to cast the project repo to the SQLite implementation
// to access the ListTasksByWorkspace helper. Returns nil if the cast fails.
func (h *ProjectHandler) sqliteProjectRepo() *sqliteStore.ProjectRepo {
	if h.store.Projects == nil {
		return nil
	}
	pr, ok := h.store.Projects.(*sqliteStore.ProjectRepo)
	if !ok {
		return nil
	}
	return pr
}

func (h *ProjectHandler) createProjectPlan(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		WorkspaceName string `json:"workspace_name"`
		Name          string `json:"name"`
		Description   string `json:"description"`
		Priority      string `json:"priority"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.ProjectHandler.createProjectPlan: %w", err)
	}

	if args.WorkspaceName == "" || args.Name == "" {
		return types.NewErrorResult("workspace_name and name are required"), nil
	}

	projects, errResult := h.projectRepo()
	if errResult != nil {
		return errResult, nil
	}

	plan := &repo.ProjectPlan{
		Name:          args.Name,
		Description:   args.Description,
		WorkspaceName: args.WorkspaceName,
		Priority:      args.Priority,
	}

	id, err := projects.CreateProjectPlan(ctx, plan)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("create project plan: %v", err)), nil
	}

	result := map[string]string{
		"id":      id,
		"name":    args.Name,
		"status":  "pending",
		"message": fmt.Sprintf("Project plan %q created.", args.Name),
	}
	return types.NewToolResult(result), nil
}

func (h *ProjectHandler) listProjects(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		WorkspaceName string `json:"workspace_name"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.ProjectHandler.listProjects: %w", err)
	}

	if args.WorkspaceName == "" {
		return types.NewErrorResult("workspace_name is required"), nil
	}

	projects, errResult := h.projectRepo()
	if errResult != nil {
		return errResult, nil
	}

	plans, err := projects.ListProjectPlans(ctx, args.WorkspaceName)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("list projects: %v", err)), nil
	}

	if len(plans) == 0 {
		return types.NewToolResult("No project plans found."), nil
	}

	type planSummary struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Status   string `json:"status"`
		Priority string `json:"priority"`
	}

	summaries := make([]planSummary, len(plans))
	for i, p := range plans {
		summaries[i] = planSummary{
			ID:       p.ID,
			Name:     p.Name,
			Status:   p.Status,
			Priority: p.Priority,
		}
	}
	return types.NewToolResult(summaries), nil
}

func (h *ProjectHandler) getProjectDetails(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		ProjectID string `json:"project_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.ProjectHandler.getProjectDetails: %w", err)
	}

	if args.ProjectID == "" {
		return types.NewErrorResult("project_id is required"), nil
	}

	projects, errResult := h.projectRepo()
	if errResult != nil {
		return errResult, nil
	}

	plan, err := projects.GetProjectPlan(ctx, args.ProjectID)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("get project: %v", err)), nil
	}

	milestones, err := projects.ListMilestones(ctx, args.ProjectID)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("list milestones: %v", err)), nil
	}

	// Build structured view with tasks nested under each milestone.
	type taskView struct {
		ID                string `json:"id"`
		Name              string `json:"name"`
		Status            string `json:"status"`
		Priority          string `json:"priority"`
		AssigneeAgentID string `json:"assignee_agent_id,omitempty"`
	}

	type milestoneView struct {
		ID          string     `json:"id"`
		Name        string     `json:"name"`
		Description string     `json:"description,omitempty"`
		Status      string     `json:"status"`
		Priority    string     `json:"priority"`
		DueDate     string     `json:"due_date,omitempty"`
		Tasks       []taskView `json:"tasks"`
	}

	type projectView struct {
		ID            string          `json:"id"`
		Name          string          `json:"name"`
		Description   string          `json:"description,omitempty"`
		WorkspaceName string          `json:"workspace_name"`
		Status        string          `json:"status"`
		Priority      string          `json:"priority"`
		Milestones    []milestoneView `json:"milestones"`
	}

	view := projectView{
		ID:            plan.ID,
		Name:          plan.Name,
		Description:   plan.Description,
		WorkspaceName: plan.WorkspaceName,
		Status:        plan.Status,
		Priority:      plan.Priority,
		Milestones:    make([]milestoneView, 0, len(milestones)),
	}

	for _, ms := range milestones {
		tasks, err := projects.ListTasks(ctx, ms.ID)
		if err != nil {
			return types.NewErrorResult(fmt.Sprintf("list tasks for milestone %s: %v", ms.ID, err)), nil
		}

		mv := milestoneView{
			ID:          ms.ID,
			Name:        ms.Name,
			Description: ms.Description,
			Status:      ms.Status,
			Priority:    ms.Priority,
			Tasks:       make([]taskView, 0, len(tasks)),
		}
		if ms.DueDate != nil {
			mv.DueDate = ms.DueDate.Format("2006-01-02")
		}

		for _, t := range tasks {
			mv.Tasks = append(mv.Tasks, taskView{
				ID:                t.ID,
				Name:              t.Name,
				Status:            t.Status,
				Priority:          t.Priority,
				AssigneeAgentID: t.AssigneeAgentID,
			})
		}

		view.Milestones = append(view.Milestones, mv)
	}

	return types.NewToolResult(view), nil
}

func (h *ProjectHandler) addMilestone(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		ProjectID   string `json:"project_id"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Priority    string `json:"priority"`
		DueDate     string `json:"due_date"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.ProjectHandler.addMilestone: %w", err)
	}

	if args.ProjectID == "" || args.Name == "" {
		return types.NewErrorResult("project_id and name are required"), nil
	}

	projects, errResult := h.projectRepo()
	if errResult != nil {
		return errResult, nil
	}

	// Verify project exists.
	_, err := projects.GetProjectPlan(ctx, args.ProjectID)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("project not found: %v", err)), nil
	}

	ms := &repo.Milestone{
		ProjectID:   args.ProjectID,
		Name:        args.Name,
		Description: args.Description,
		Priority:    args.Priority,
	}

	if args.DueDate != "" {
		// Accept YYYY-MM-DD format from the caller.
		parsed, parseErr := parseDueDate(args.DueDate)
		if parseErr != nil {
			return types.NewErrorResult(fmt.Sprintf("invalid due_date format (use YYYY-MM-DD): %v", parseErr)), nil
		}
		ms.DueDate = &parsed
	}

	// Determine order_index: place after existing milestones.
	existing, err := projects.ListMilestones(ctx, args.ProjectID)
	if err == nil {
		ms.OrderIndex = len(existing)
	}

	id, err := projects.CreateMilestone(ctx, ms)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("create milestone: %v", err)), nil
	}

	result := map[string]string{
		"id":      id,
		"name":    args.Name,
		"message": fmt.Sprintf("Milestone %q added to project.", args.Name),
	}
	return types.NewToolResult(result), nil
}

func (h *ProjectHandler) addTask(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		MilestoneID string `json:"milestone_id"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Priority    string `json:"priority"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.ProjectHandler.addTask: %w", err)
	}

	if args.MilestoneID == "" || args.Name == "" {
		return types.NewErrorResult("milestone_id and name are required"), nil
	}

	projects, errResult := h.projectRepo()
	if errResult != nil {
		return errResult, nil
	}

	// Verify milestone exists.
	_, err := projects.GetMilestone(ctx, args.MilestoneID)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("milestone not found: %v", err)), nil
	}

	task := &repo.Task{
		MilestoneID: args.MilestoneID,
		Name:        args.Name,
		Description: args.Description,
		Priority:    args.Priority,
	}

	// Determine order_index: place after existing tasks.
	existing, err := projects.ListTasks(ctx, args.MilestoneID)
	if err == nil {
		task.OrderIndex = len(existing)
	}

	id, err := projects.CreateTask(ctx, task)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("create task: %v", err)), nil
	}

	result := map[string]string{
		"id":      id,
		"name":    args.Name,
		"message": fmt.Sprintf("Task %q added to milestone.", args.Name),
	}
	return types.NewToolResult(result), nil
}

func (h *ProjectHandler) listTasks(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		WorkspaceName string `json:"workspace_name"`
		Status        string `json:"status"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.ProjectHandler.listTasks: %w", err)
	}

	if args.WorkspaceName == "" {
		return types.NewErrorResult("workspace_name is required"), nil
	}

	_, errResult := h.projectRepo()
	if errResult != nil {
		return errResult, nil
	}

	// Use the SQLite-specific helper for cross-workspace task listing.
	sqliteRepo := h.sqliteProjectRepo()
	if sqliteRepo == nil {
		return types.NewErrorResult("list_tasks requires SQLite backend"), nil
	}

	tasks, err := sqliteRepo.ListTasksByWorkspace(ctx, args.WorkspaceName, args.Status)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("list tasks: %v", err)), nil
	}

	if len(tasks) == 0 {
		msg := "No tasks found"
		if args.Status != "" {
			msg += fmt.Sprintf(" with status %q", args.Status)
		}
		msg += "."
		return types.NewToolResult(msg), nil
	}

	type taskSummary struct {
		ID                string `json:"id"`
		Name              string `json:"name"`
		Status            string `json:"status"`
		Priority          string `json:"priority"`
		MilestoneID       string `json:"milestone_id"`
		ProjectID         string `json:"project_id,omitempty"`
		AssigneeAgentID string `json:"assignee_agent_id,omitempty"`
	}

	summaries := make([]taskSummary, len(tasks))
	for i, t := range tasks {
		summaries[i] = taskSummary{
			ID:                t.ID,
			Name:              t.Name,
			Status:            t.Status,
			Priority:          t.Priority,
			MilestoneID:       t.MilestoneID,
			ProjectID:         t.ProjectID,
			AssigneeAgentID: t.AssigneeAgentID,
		}
	}
	return types.NewToolResult(summaries), nil
}

func (h *ProjectHandler) updateTaskStatus(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		TaskID string `json:"task_id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.ProjectHandler.updateTaskStatus: %w", err)
	}

	if args.TaskID == "" || args.Status == "" {
		return types.NewErrorResult("task_id and status are required"), nil
	}

	validStatuses := map[string]bool{
		"pending": true, "in-progress": true, "completed": true, "blocked": true,
	}
	if !validStatuses[args.Status] {
		return types.NewErrorResult(fmt.Sprintf("invalid status %q; valid values: pending, in-progress, completed, blocked", args.Status)), nil
	}

	projects, errResult := h.projectRepo()
	if errResult != nil {
		return errResult, nil
	}

	if err := projects.UpdateTaskStatus(ctx, args.TaskID, args.Status); err != nil {
		return types.NewErrorResult(fmt.Sprintf("update task status: %v", err)), nil
	}

	// Auto-complete parent milestone/project if all children are now completed.
	if msCompleted, pjCompleted, recErr := projects.ReconcileCompletionStatus(ctx); recErr != nil {
		slog.Warn("reconcile completion status failed", "error", recErr)
	} else if msCompleted > 0 || pjCompleted > 0 {
		slog.Info("auto-completed parents", "milestones", msCompleted, "projects", pjCompleted)
	}

	result := map[string]string{
		"task_id": args.TaskID,
		"status":  args.Status,
		"message": fmt.Sprintf("Task status updated to %q.", args.Status),
	}
	return types.NewToolResult(result), nil
}

func (h *ProjectHandler) assignTask(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		TaskID    string `json:"task_id"`
		AgentID string `json:"agent_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.ProjectHandler.assignTask: %w", err)
	}

	if args.TaskID == "" || args.AgentID == "" {
		return types.NewErrorResult("task_id and agent_id are required"), nil
	}

	projects, errResult := h.projectRepo()
	if errResult != nil {
		return errResult, nil
	}

	if err := projects.AssignTask(ctx, args.TaskID, args.AgentID); err != nil {
		return types.NewErrorResult(fmt.Sprintf("assign task: %v", err)), nil
	}

	result := map[string]string{
		"task_id":    args.TaskID,
		"agent_id": args.AgentID,
		"message":    fmt.Sprintf("Task assigned to agent %q.", args.AgentID),
	}
	return types.NewToolResult(result), nil
}

func (h *ProjectHandler) addComment(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		EntityType string `json:"entity_type"`
		EntityID   string `json:"entity_id"`
		Content    string `json:"content"`
		Author     string `json:"author"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.ProjectHandler.addComment: %w", err)
	}

	if args.EntityType == "" || args.EntityID == "" || args.Content == "" {
		return types.NewErrorResult("entity_type, entity_id, and content are required"), nil
	}

	validTypes := map[string]bool{
		"project": true, "milestone": true, "task": true,
	}
	if !validTypes[args.EntityType] {
		return types.NewErrorResult(fmt.Sprintf("invalid entity_type %q; valid values: project, milestone, task", args.EntityType)), nil
	}

	projects, errResult := h.projectRepo()
	if errResult != nil {
		return errResult, nil
	}

	comment := &repo.Comment{
		EntityType: args.EntityType,
		EntityID:   args.EntityID,
		Content:    args.Content,
		Author:     args.Author,
	}

	id, err := projects.AddComment(ctx, comment)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("add comment: %v", err)), nil
	}

	result := map[string]string{
		"id":      id,
		"message": "Comment added.",
	}
	return types.NewToolResult(result), nil
}

func (h *ProjectHandler) deleteProject(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		ProjectID string `json:"project_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.ProjectHandler.deleteProject: %w", err)
	}

	if args.ProjectID == "" {
		return types.NewErrorResult("project_id is required"), nil
	}

	projects, errResult := h.projectRepo()
	if errResult != nil {
		return errResult, nil
	}

	// Retrieve the plan name before deletion for the confirmation message.
	plan, err := projects.GetProjectPlan(ctx, args.ProjectID)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("project not found: %v", err)), nil
	}

	if err := projects.DeleteProjectPlan(ctx, args.ProjectID); err != nil {
		return types.NewErrorResult(fmt.Sprintf("delete project: %v", err)), nil
	}

	result := map[string]string{
		"project_id": args.ProjectID,
		"name":       plan.Name,
		"message":    fmt.Sprintf("Project %q and all its milestones/tasks deleted.", plan.Name),
	}
	return types.NewToolResult(result), nil
}

func (h *ProjectHandler) archiveProject(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		ProjectID string `json:"project_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.ProjectHandler.archiveProject: %w", err)
	}

	if args.ProjectID == "" {
		return types.NewErrorResult("project_id is required"), nil
	}

	projects, errResult := h.projectRepo()
	if errResult != nil {
		return errResult, nil
	}

	if err := projects.UpdateProjectStatus(ctx, args.ProjectID, "archived"); err != nil {
		return types.NewErrorResult(fmt.Sprintf("archive project: %v", err)), nil
	}

	result := map[string]string{
		"project_id": args.ProjectID,
		"status":     "archived",
		"message":    "Project archived.",
	}
	return types.NewToolResult(result), nil
}

func (h *ProjectHandler) moveProjectWorkspace(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		ProjectID         string `json:"project_id"`
		TargetWorkspaceID string `json:"target_workspace_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.ProjectHandler.moveProjectWorkspace: %w", err)
	}

	if args.ProjectID == "" || args.TargetWorkspaceID == "" {
		return types.NewErrorResult("project_id and target_workspace_id are required"), nil
	}

	projects, errResult := h.projectRepo()
	if errResult != nil {
		return errResult, nil
	}

	// Verify the project exists before moving.
	plan, err := projects.GetProjectPlan(ctx, args.ProjectID)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("project not found: %v", err)), nil
	}

	if plan.WorkspaceName == args.TargetWorkspaceID {
		return types.NewErrorResult(fmt.Sprintf("project is already in workspace %q", args.TargetWorkspaceID)), nil
	}

	if err := projects.MoveProjectWorkspace(ctx, args.ProjectID, args.TargetWorkspaceID); err != nil {
		return types.NewErrorResult(fmt.Sprintf("move project: %v", err)), nil
	}

	result := map[string]string{
		"project_id":          args.ProjectID,
		"from_workspace":      plan.WorkspaceName,
		"to_workspace":        args.TargetWorkspaceID,
		"message":             fmt.Sprintf("Project %q moved from %q to %q.", plan.Name, plan.WorkspaceName, args.TargetWorkspaceID),
	}
	return types.NewToolResult(result), nil
}

func (h *ProjectHandler) unassignTask(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.ProjectHandler.unassignTask: %w", err)
	}

	if args.TaskID == "" {
		return types.NewErrorResult("task_id is required"), nil
	}

	projects, errResult := h.projectRepo()
	if errResult != nil {
		return errResult, nil
	}

	if err := projects.UnassignTask(ctx, args.TaskID); err != nil {
		return types.NewErrorResult(fmt.Sprintf("unassign task: %v", err)), nil
	}

	result := map[string]string{
		"task_id": args.TaskID,
		"message": "Task assignment removed.",
	}
	return types.NewToolResult(result), nil
}

func (h *ProjectHandler) assignMilestone(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		MilestoneID string `json:"milestone_id"`
		AgentID   string `json:"agent_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.ProjectHandler.assignMilestone: %w", err)
	}

	if args.MilestoneID == "" || args.AgentID == "" {
		return types.NewErrorResult("milestone_id and agent_id are required"), nil
	}

	projects, errResult := h.projectRepo()
	if errResult != nil {
		return errResult, nil
	}

	if err := projects.AssignMilestone(ctx, args.MilestoneID, args.AgentID); err != nil {
		return types.NewErrorResult(fmt.Sprintf("assign milestone: %v", err)), nil
	}

	result := map[string]string{
		"milestone_id": args.MilestoneID,
		"agent_id":   args.AgentID,
		"message":      fmt.Sprintf("Milestone assigned to agent %q.", args.AgentID),
	}
	return types.NewToolResult(result), nil
}

func (h *ProjectHandler) unassignMilestone(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		MilestoneID string `json:"milestone_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.ProjectHandler.unassignMilestone: %w", err)
	}

	if args.MilestoneID == "" {
		return types.NewErrorResult("milestone_id is required"), nil
	}

	projects, errResult := h.projectRepo()
	if errResult != nil {
		return errResult, nil
	}

	if err := projects.UnassignMilestone(ctx, args.MilestoneID); err != nil {
		return types.NewErrorResult(fmt.Sprintf("unassign milestone: %v", err)), nil
	}

	result := map[string]string{
		"milestone_id": args.MilestoneID,
		"message":      "Milestone assignment removed.",
	}
	return types.NewToolResult(result), nil
}

func (h *ProjectHandler) getMyTasks(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		AgentID string `json:"agent_id"`
		Status    string `json:"status"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.ProjectHandler.getMyTasks: %w", err)
	}

	if args.AgentID == "" {
		return types.NewErrorResult("agent_id is required"), nil
	}

	projects, errResult := h.projectRepo()
	if errResult != nil {
		return errResult, nil
	}

	tasks, err := projects.ListTasksByAgent(ctx, args.AgentID, args.Status, "")
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("get my tasks: %v", err)), nil
	}

	if len(tasks) == 0 {
		msg := fmt.Sprintf("No tasks assigned to agent %q", args.AgentID)
		if args.Status != "" {
			msg += fmt.Sprintf(" with status %q", args.Status)
		}
		msg += "."
		return types.NewToolResult(msg), nil
	}

	return types.NewToolResult(formatTaskSummaries(tasks)), nil
}

func (h *ProjectHandler) checkForAssignments(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		AgentID string `json:"agent_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.ProjectHandler.checkForAssignments: %w", err)
	}

	if args.AgentID == "" {
		return types.NewErrorResult("agent_id is required"), nil
	}

	projects, errResult := h.projectRepo()
	if errResult != nil {
		return errResult, nil
	}

	// Check for pending tasks assigned to this agent.
	tasks, err := projects.ListTasksByAgent(ctx, args.AgentID, "pending", "")
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("check assignments: %v", err)), nil
	}

	type assignmentCheck struct {
		AgentID    string `json:"agent_id"`
		PendingCount int    `json:"pending_count"`
		HasPending   bool   `json:"has_pending"`
		Message      string `json:"message"`
	}

	check := assignmentCheck{
		AgentID:    args.AgentID,
		PendingCount: len(tasks),
		HasPending:   len(tasks) > 0,
	}

	if len(tasks) == 0 {
		check.Message = fmt.Sprintf("No pending assignments for agent %q.", args.AgentID)
	} else {
		check.Message = fmt.Sprintf("%d pending task(s) assigned to agent %q.", len(tasks), args.AgentID)
	}

	return types.NewToolResult(check), nil
}

func (h *ProjectHandler) getTask(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		AgentID string `json:"agent_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.ProjectHandler.getTask: %w", err)
	}

	projects, errResult := h.projectRepo()
	if errResult != nil {
		return errResult, nil
	}

	task, err := projects.GetNextTask(ctx, args.AgentID)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("get next task: %v", err)), nil
	}

	if task == nil {
		if args.AgentID != "" {
			return types.NewToolResult("No pending tasks found."), nil
		}
		return types.NewToolResult("No unassigned tasks requiring triage."), nil
	}

	type taskDetail struct {
		ID              string `json:"id"`
		Name            string `json:"name"`
		Description     string `json:"description"`
		Status          string `json:"status"`
		Priority        string `json:"priority"`
		MilestoneID     string `json:"milestone_id"`
		ProjectID       string `json:"project_id,omitempty"`
		AssigneeAgentID string `json:"assignee_agent_id,omitempty"`
		CreatedAt       string `json:"created_at"`
	}

	return types.NewToolResult(taskDetail{
		ID:              task.ID,
		Name:            task.Name,
		Description:     task.Description,
		Status:          task.Status,
		Priority:        task.Priority,
		MilestoneID:     task.MilestoneID,
		ProjectID:       task.ProjectID,
		AssigneeAgentID: task.AssigneeAgentID,
		CreatedAt:       task.CreatedAt.Format(time.RFC3339),
	}), nil
}

func (h *ProjectHandler) listPersonaTasks(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		AgentID string `json:"agent_id"`
		ProjectID string `json:"project_id"`
		Status    string `json:"status"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.ProjectHandler.listPersonaTasks: %w", err)
	}

	if args.AgentID == "" {
		return types.NewErrorResult("agent_id is required"), nil
	}

	projects, errResult := h.projectRepo()
	if errResult != nil {
		return errResult, nil
	}

	tasks, err := projects.ListTasksByAgent(ctx, args.AgentID, args.Status, args.ProjectID)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("list agent tasks: %v", err)), nil
	}

	if len(tasks) == 0 {
		msg := fmt.Sprintf("No tasks found for agent %q", args.AgentID)
		if args.Status != "" {
			msg += fmt.Sprintf(" with status %q", args.Status)
		}
		if args.ProjectID != "" {
			msg += fmt.Sprintf(" in project %q", args.ProjectID)
		}
		msg += "."
		return types.NewToolResult(msg), nil
	}

	return types.NewToolResult(formatTaskSummaries(tasks)), nil
}

// formatTaskSummaries converts a slice of tasks into a JSON-serializable summary slice.
func formatTaskSummaries(tasks []*repo.Task) []taskSummaryView {
	summaries := make([]taskSummaryView, len(tasks))
	for i, t := range tasks {
		summaries[i] = taskSummaryView{
			ID:                t.ID,
			Name:              t.Name,
			Status:            t.Status,
			Priority:          t.Priority,
			MilestoneID:       t.MilestoneID,
			AssigneeAgentID: t.AssigneeAgentID,
		}
	}
	return summaries
}

// taskSummaryView is a compact view of a task for list/search responses.
type taskSummaryView struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Status            string `json:"status"`
	Priority          string `json:"priority"`
	MilestoneID       string `json:"milestone_id"`
	AssigneeAgentID string `json:"assignee_agent_id,omitempty"`
}

// parseDueDate parses a date string in YYYY-MM-DD format. It also accepts
// YYYY-MM-DD HH:MM:SS to tolerate stored datetime strings.
func parseDueDate(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	t, err := time.Parse("2006-01-02", s)
	if err == nil {
		return t, nil
	}
	return time.Parse("2006-01-02 15:04:05", s)
}

func (h *ProjectHandler) deleteMilestone(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		MilestoneID string `json:"milestone_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.ProjectHandler.deleteMilestone: %w", err)
	}
	if args.MilestoneID == "" {
		return types.NewErrorResult("milestone_id is required"), nil
	}

	projects, errResult := h.projectRepo()
	if errResult != nil {
		return errResult, nil
	}

	// Retrieve the milestone name before deletion for the confirmation message.
	ms, err := projects.GetMilestone(ctx, args.MilestoneID)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("milestone not found: %v", err)), nil
	}

	if err := projects.DeleteMilestone(ctx, args.MilestoneID); err != nil {
		return types.NewErrorResult(fmt.Sprintf("delete milestone: %v", err)), nil
	}

	// Auto-complete parent project if remaining milestones are all completed.
	if _, pjCompleted, recErr := projects.ReconcileCompletionStatus(ctx); recErr != nil {
		slog.Warn("reconcile completion status failed", "error", recErr)
	} else if pjCompleted > 0 {
		slog.Info("auto-completed project", "projects", pjCompleted)
	}

	return types.NewToolResult(map[string]string{
		"milestone_id": args.MilestoneID,
		"name":         ms.Name,
		"message":      fmt.Sprintf("Milestone %q and all its tasks deleted.", ms.Name),
	}), nil
}

func (h *ProjectHandler) deleteTask(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.ProjectHandler.deleteTask: %w", err)
	}
	if args.TaskID == "" {
		return types.NewErrorResult("task_id is required"), nil
	}

	projects, errResult := h.projectRepo()
	if errResult != nil {
		return errResult, nil
	}

	// Retrieve the task name before deletion for the confirmation message.
	task, err := projects.GetTask(ctx, args.TaskID)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("task not found: %v", err)), nil
	}

	if err := projects.DeleteTask(ctx, args.TaskID); err != nil {
		return types.NewErrorResult(fmt.Sprintf("delete task: %v", err)), nil
	}

	// Auto-complete parent milestone/project if remaining children are all completed.
	if msCompleted, pjCompleted, recErr := projects.ReconcileCompletionStatus(ctx); recErr != nil {
		slog.Warn("reconcile completion status failed", "error", recErr)
	} else if msCompleted > 0 || pjCompleted > 0 {
		slog.Info("auto-completed parents", "milestones", msCompleted, "projects", pjCompleted)
	}

	return types.NewToolResult(map[string]string{
		"task_id": args.TaskID,
		"name":    task.Name,
		"message": fmt.Sprintf("Task %q deleted.", task.Name),
	}), nil
}

func (h *ProjectHandler) purgeOrphans(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	projects, errResult := h.projectRepo()
	if errResult != nil {
		return errResult, nil
	}

	count, err := projects.PurgeOrphans(ctx)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("purge orphans: %v", err)), nil
	}

	return types.NewToolResult(map[string]any{
		"purged_count": count,
		"message":      fmt.Sprintf("Purged %d orphaned records (milestones, tasks, and comments).", count),
	}), nil
}
