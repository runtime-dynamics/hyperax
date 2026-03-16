import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { mcpCall } from '@/lib/mcp-client'

export interface Workspace {
  id: string
  name: string
  root_path: string
  created_at: string
  metadata?: string
}

export function useWorkspaces() {
  return useQuery({
    queryKey: ['workspaces'],
    queryFn: () => mcpCall<Workspace[]>('workspace', { action: 'list' }),
  })
}

export function useRegisterWorkspace() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (args: { name: string; root_path: string }) =>
      mcpCall('workspace', { action: 'register', ...(args as Record<string, unknown>) }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['workspaces'] }),
  })
}

export function useDeleteWorkspace() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (name: string) => mcpCall('workspace', { action: 'delete', workspace_name: name }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['workspaces'] }),
  })
}
