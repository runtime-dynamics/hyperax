import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { mcpCall } from '@/lib/mcp-client'

// ─── Interfaces ───────────────────────────────────────────────────────────────

export interface Comment {
  id: string
  task_id: string
  content: string
  author?: string
  created_at: string
}

export interface Task {
  id: string
  milestone_id?: string
  project_id?: string
  // Backend returns `name`; we normalize to `title` in the query functions below
  title: string
  description?: string
  status: string // pending, in_progress, completed, blocked
  priority: string
  assignee?: string
  assignee_persona_id?: string
  /** Resolved agent name for display (set by TasksPage after agent list loads). */
  displayAssignee?: string
  created_at: string
  updated_at?: string
  comments?: Comment[]
}

/** Normalize a raw API task that may have `name` instead of `title`.
 *  Also normalizes status: backend uses "in-progress" (hyphen) but the
 *  frontend Kanban columns expect "in_progress" (underscore).
 *  Maps `assignee_agent_id` → `assignee` for UI display/matching. */
function normalizeTask(raw: Task & { name?: string; assignee_agent_id?: string }): Task {
  return {
    ...raw,
    title: raw.title || raw.name || '',
    assignee: raw.assignee || raw.assignee_agent_id || '',
    status: raw.status?.replace(/-/g, '_') ?? 'pending',
  }
}

export interface Milestone {
  id: string
  project_id: string
  name: string
  description?: string
  priority: string
  due_date?: string
  assignee?: string
  tasks?: Task[]
}

export interface Project {
  id: string
  name: string
  description?: string
  workspace: string
  priority: string
  status: string
  created_at: string
  archived?: boolean
  milestones?: Milestone[]
  tasks?: Task[]
}

// ─── Query Hooks ──────────────────────────────────────────────────────────────

export function useProjects(workspace = '_org') {
  return useQuery({
    queryKey: ['mcp-projects', workspace],
    queryFn: () => mcpCall<Project[]>('project', { action: 'list_projects', workspace_name: workspace }),
    retry: false,
  })
}

export function useProjectDetails(projectId: string | null) {
  return useQuery({
    queryKey: ['mcp-project-details', projectId],
    queryFn: async () => {
      const result = await mcpCall<Project>('project', { action: 'get_details', project_id: projectId! })
      if (!result) return result
      // Normalize task names in milestones
      if (Array.isArray(result.milestones)) {
        result.milestones = result.milestones.map((m) => ({
          ...m,
          tasks: Array.isArray(m.tasks) ? m.tasks.map(normalizeTask) : [],
        }))
      }
      if (Array.isArray(result.tasks)) {
        result.tasks = result.tasks.map(normalizeTask)
      }
      return result
    },
    enabled: !!projectId,
    retry: false,
  })
}

export function useTasks(workspace = '_org') {
  return useQuery({
    queryKey: ['mcp-tasks', workspace],
    queryFn: async () => {
      const result = await mcpCall<(Task & { name?: string })[]>('project', { action: 'list_tasks', workspace_name: workspace })
      if (!Array.isArray(result)) return []
      return result.map(normalizeTask)
    },
    retry: false,
  })
}

// ─── Mutation Hooks ───────────────────────────────────────────────────────────

export interface CreateProjectArgs {
  workspace_name: string
  name: string
  description?: string
  priority?: string
}

export function useCreateProject() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (args: CreateProjectArgs) =>
      mcpCall<{ id: string; message: string }>('project', { action: 'create', ...(args as unknown as Record<string, unknown>) }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['mcp-projects'] }),
  })
}

export function useDeleteProject() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (projectId: string) =>
      mcpCall<{ message: string }>('project', { action: 'delete', project_id: projectId }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['mcp-projects'] })
      void qc.invalidateQueries({ queryKey: ['mcp-project-details'] })
    },
  })
}

export function useArchiveProject() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (projectId: string) =>
      mcpCall<{ message: string }>('project', { action: 'archive', project_id: projectId }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['mcp-projects'] }),
  })
}

export interface AddMilestoneArgs {
  project_id: string
  name: string
  description?: string
  priority?: string
  due_date?: string
}

export function useAddMilestone() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (args: AddMilestoneArgs) =>
      mcpCall<{ id: string; message: string }>('project', { action: 'add_milestone', ...(args as unknown as Record<string, unknown>) }),
    onSuccess: (_data, args) => {
      void qc.invalidateQueries({ queryKey: ['mcp-project-details', args.project_id] })
    },
  })
}

export interface AddTaskArgs {
  milestone_id: string
  title: string
  description?: string
  priority?: string
  assignee?: string
}

export function useAddTask() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (args: AddTaskArgs) =>
      mcpCall<{ id: string; message: string }>('project', {
        action: 'add_task',
        milestone_id: args.milestone_id,
        name: args.title,
        description: args.description,
        priority: args.priority,
      } as unknown as Record<string, unknown>),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['mcp-project-details'] })
      void qc.invalidateQueries({ queryKey: ['mcp-tasks'] })
    },
  })
}

export interface UpdateTaskStatusArgs {
  task_id: string
  status: string
}

export function useUpdateTaskStatus() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (args: UpdateTaskStatusArgs) =>
      mcpCall<{ message: string }>('project', {
        action: 'update_task_status',
        ...args,
        // Backend expects "in-progress" (hyphen); frontend columns use "in_progress" (underscore).
        status: args.status.replace(/_/g, '-'),
      } as unknown as Record<string, unknown>),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['mcp-project-details'] })
      void qc.invalidateQueries({ queryKey: ['mcp-tasks'] })
    },
  })
}

export interface AssignTaskArgs {
  task_id: string
  assignee: string
}

export function useAssignTask() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (args: AssignTaskArgs) =>
      mcpCall<{ message: string }>('project', {
        action: 'assign_task',
        task_id: args.task_id,
        agent_id: args.assignee,
      } as unknown as Record<string, unknown>),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['mcp-project-details'] })
      void qc.invalidateQueries({ queryKey: ['mcp-tasks'] })
    },
  })
}

export function useUnassignTask() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (taskId: string) =>
      mcpCall<{ message: string }>('project', { action: 'unassign_task', task_id: taskId }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['mcp-project-details'] })
      void qc.invalidateQueries({ queryKey: ['mcp-tasks'] })
    },
  })
}

export interface AddCommentArgs {
  task_id: string
  content: string
  author?: string
}

export function useAddComment() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (args: AddCommentArgs) =>
      mcpCall<{ message: string }>('project', { action: 'add_comment', ...(args as unknown as Record<string, unknown>) }),
    onSuccess: (_data, args) => {
      void qc.invalidateQueries({ queryKey: ['mcp-project-details'] })
      void qc.invalidateQueries({ queryKey: ['mcp-task-comments', args.task_id] })
    },
  })
}

export function useDeleteTask() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (taskId: string) =>
      mcpCall<{ message: string }>('project', { action: 'delete_task', task_id: taskId }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['mcp-project-details'] })
      void qc.invalidateQueries({ queryKey: ['mcp-tasks'] })
    },
  })
}

export function useDeleteMilestone() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (milestoneId: string) =>
      mcpCall<{ message: string }>('project', { action: 'delete_milestone', milestone_id: milestoneId }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['mcp-project-details'] })
      void qc.invalidateQueries({ queryKey: ['mcp-tasks'] })
    },
  })
}

export function usePurgeOrphans() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (_: void) => mcpCall<{ message: string }>('project', { action: 'purge_orphans' }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['mcp-projects'] })
      void qc.invalidateQueries({ queryKey: ['mcp-project-details'] })
      void qc.invalidateQueries({ queryKey: ['mcp-tasks'] })
    },
  })
}

export function useBulkUpdateTaskStatus() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async ({ taskIds, status }: { taskIds: string[]; status: string }) => {
      const normalized = status.replace(/_/g, '-')
      await Promise.all(
        taskIds.map((task_id) =>
          mcpCall<{ message: string }>('project', {
            action: 'update_task_status',
            task_id,
            status: normalized,
          } as unknown as Record<string, unknown>),
        ),
      )
      return { count: taskIds.length }
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['mcp-project-details'] })
      void qc.invalidateQueries({ queryKey: ['mcp-tasks'] })
    },
  })
}
