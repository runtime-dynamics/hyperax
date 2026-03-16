import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { apiGet, apiPost, apiDelete } from '@/lib/api-client'

export interface Workspace {
  id: string
  name: string
  root_path: string
  created_at: string
  metadata?: string
}

export function useRestWorkspaces() {
  return useQuery({
    queryKey: ['rest-workspaces'],
    queryFn: () => apiGet<Workspace[]>('/workspaces'),
  })
}

export function useRestWorkspace(name: string) {
  return useQuery({
    queryKey: ['rest-workspaces', name],
    queryFn: () => apiGet<Workspace>(`/workspaces/${encodeURIComponent(name)}`),
    enabled: !!name,
  })
}

export function useRestCreateWorkspace() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (args: { name: string; root_path: string; metadata?: string }) =>
      apiPost<Workspace>('/workspaces', args),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['rest-workspaces'] }),
  })
}

export function useRestDeleteWorkspace() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (name: string) => apiDelete(`/workspaces/${encodeURIComponent(name)}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['rest-workspaces'] }),
  })
}
