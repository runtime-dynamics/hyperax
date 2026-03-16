import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { mcpCall } from '@/lib/mcp-client'

export interface Agent {
  id: string
  name: string
  persona_id: string
  parent_agent_id?: string
  workspace_id?: string
  status: string
  status_reason?: string
  created_at: string
  updated_at: string
  // Full agent fields
  personality?: string
  role_template_id?: string
  clearance_level?: number
  provider_id?: string
  default_model?: string
  chat_model?: string
  is_internal?: boolean
  is_favorite?: boolean
  system_prompt?: string
  guard_bypass?: boolean
  // Enriched fields from backend
  persona_name?: string
  persona_role?: string
}

export interface CreateAgentArgs {
  name: string
  persona_id: string
  parent_agent_id?: string
  workspace_id?: string
  status?: string
  personality?: string
  role_template_id?: string
  clearance_level?: number
  provider_id?: string
  default_model?: string
  chat_model?: string
  is_internal?: boolean
  system_prompt?: string
  guard_bypass?: boolean
}
export interface UpdateAgentArgs {
  agent_id: string
  name?: string
  persona_id?: string
  parent_agent_id?: string | null
  workspace_id?: string
  status?: string
  personality?: string
  role_template_id?: string
  clearance_level?: number
  provider_id?: string
  default_model?: string
  chat_model?: string
  is_internal?: boolean
  is_favorite?: boolean
  system_prompt?: string
  guard_bypass?: boolean
}

export function useAgents(workspaceId?: string) {
  return useQuery({
    queryKey: ['agents', workspaceId],
    queryFn: () => {
      const params: Record<string, unknown> = {}
      if (workspaceId) params.workspace_id = workspaceId
      return mcpCall<Agent[]>('agent', { action: 'list_agents', ...params })
    },
  })
}

export function useAgent(agentId: string | null) {
  return useQuery({
    queryKey: ['agent', agentId],
    queryFn: () => mcpCall<{ agent: Agent; persona?: Record<string, unknown> }>('agent', { action: 'get_agent', agent_id: agentId! }),
    enabled: !!agentId,
  })
}

export function useCreateAgent() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (args: CreateAgentArgs) =>
      mcpCall('agent', { action: 'create_agent', ...(args as unknown as Record<string, unknown>) }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['agents'] })
      qc.invalidateQueries({ queryKey: ['agent-hierarchy'] })
    },
  })
}

export function useUpdateAgent() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (args: UpdateAgentArgs) =>
      mcpCall('agent', { action: 'update_agent', ...(args as unknown as Record<string, unknown>) }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['agents'] })
      qc.invalidateQueries({ queryKey: ['agent'] })
      qc.invalidateQueries({ queryKey: ['agent-detail'] })
      qc.invalidateQueries({ queryKey: ['agent-hierarchy'] })
    },
  })
}

export function useDeleteAgent() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (agentId: string) => mcpCall('agent', { action: 'delete_agent', agent_id: agentId }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['agents'] })
      qc.invalidateQueries({ queryKey: ['agent-hierarchy'] })
    },
  })
}
