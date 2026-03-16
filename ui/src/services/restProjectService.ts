import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { apiGet, apiPost, apiPut, apiDelete } from '@/lib/api-client'

export interface ProjectPlan {
  ID: string
  Name: string
  Description: string
  WorkspaceName: string
  Status: string
  Priority: string
  CreatedAt: string
  UpdatedAt: string
}

export interface Milestone {
  ID: string
  ProjectID: string
  Name: string
  Description: string
  Status: string
  Priority: string
  DueDate: string | null
  OrderIndex: number
  AssigneePersonaID: string
}

export interface Task {
  ID: string
  MilestoneID: string
  Name: string
  Description: string
  Status: string
  Priority: string
  OrderIndex: number
  AssigneePersonaID: string
  CreatedAt: string
  UpdatedAt: string
}

export interface Comment {
  ID: string
  EntityType: string
  EntityID: string
  Content: string
  Author: string
  CreatedAt: string
}

export function useRestProjects(workspace = '') {
  const params = workspace ? `?workspace=${encodeURIComponent(workspace)}` : ''
  return useQuery({
    queryKey: ['rest-projects', workspace],
    queryFn: () => apiGet<ProjectPlan[]>(`/projects${params}`),
  })
}

export function useRestProject(id: string) {
  return useQuery({
    queryKey: ['rest-projects', id],
    queryFn: () => apiGet<ProjectPlan>(`/projects/${id}`),
    enabled: !!id,
  })
}

export function useRestCreateProject() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (args: Partial<ProjectPlan>) => apiPost<ProjectPlan>('/projects', args),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['rest-projects'] }),
  })
}

export function useRestDeleteProject() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => apiDelete(`/projects/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['rest-projects'] }),
  })
}

export function useRestUpdateProjectStatus() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, status }: { id: string; status: string }) =>
      apiPut(`/projects/${id}/status`, { status }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['rest-projects'] }),
  })
}

export function useRestMilestones(projectId: string) {
  return useQuery({
    queryKey: ['rest-milestones', projectId],
    queryFn: () => apiGet<Milestone[]>(`/projects/${projectId}/milestones`),
    enabled: !!projectId,
  })
}

export function useRestCreateMilestone() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ projectId, ...data }: Partial<Milestone> & { projectId: string }) =>
      apiPost<Milestone>(`/projects/${projectId}/milestones`, data),
    onSuccess: (_, { projectId }) => qc.invalidateQueries({ queryKey: ['rest-milestones', projectId] }),
  })
}

export function useRestTasks(projectId: string, milestoneId: string) {
  return useQuery({
    queryKey: ['rest-tasks', projectId, milestoneId],
    queryFn: () => apiGet<Task[]>(`/projects/${projectId}/milestones/${milestoneId}/tasks`),
    enabled: !!projectId && !!milestoneId,
  })
}

export function useRestCreateTask() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ projectId, milestoneId, ...data }: Partial<Task> & { projectId: string; milestoneId: string }) =>
      apiPost<Task>(`/projects/${projectId}/milestones/${milestoneId}/tasks`, data),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['rest-tasks'] }),
  })
}

export function useRestUpdateTaskStatus() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ taskId, status }: { taskId: string; status: string }) =>
      apiPut(`/projects/tasks/${taskId}/status`, { status }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['rest-tasks'] }),
  })
}

export function useRestAssignTask() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ taskId, persona_id }: { taskId: string; persona_id: string }) =>
      apiPut(`/projects/tasks/${taskId}/assign`, { persona_id }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['rest-tasks'] }),
  })
}

export function useRestUnassignTask() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (taskId: string) => apiDelete(`/projects/tasks/${taskId}/assign`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['rest-tasks'] }),
  })
}

export function useRestComments(entityType: string, entityId: string) {
  return useQuery({
    queryKey: ['rest-comments', entityType, entityId],
    queryFn: () => apiGet<Comment[]>(`/projects/comments/${entityType}/${entityId}`),
    enabled: !!entityType && !!entityId,
  })
}

export function useRestAddComment() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (args: { entity_type: string; entity_id: string; content: string; author?: string }) =>
      apiPost<Comment>('/projects/comments', args),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['rest-comments'] }),
  })
}
