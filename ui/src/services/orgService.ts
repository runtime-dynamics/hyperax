import { useQuery } from '@tanstack/react-query'
import { mcpCall } from '@/lib/mcp-client'

// ─── Types ────────────────────────────────────────────────────────────────────

export interface HierarchyNode {
  agent_id: string
  parent_id?: string
  children: HierarchyNode[]
}

export interface RuntimeStateSummary {
  agent_id: string
  name?: string
  status: string
  status_reason?: string
  started_at?: string
  last_active_at?: string
  workspace?: string
  tool_calls?: number
  error?: string
  role_template_id?: string
  default_model?: string
  clearance_level?: number
  provider_id?: string
}

export interface RuntimeStateDetail extends RuntimeStateSummary {
  config?: Record<string, unknown>
  metrics?: Record<string, unknown>
  metadata?: Record<string, unknown>
}

// ─── Hooks ────────────────────────────────────────────────────────────────────

export function useAgentHierarchy() {
  return useQuery({
    queryKey: ['agent-hierarchy'],
    queryFn: async () => {
      const result = await mcpCall<HierarchyNode[]>('comm', { action: 'get_hierarchy' })
      return Array.isArray(result) ? result : []
    },
    retry: false,
  })
}

export function useAgentStates() {
  return useQuery({
    queryKey: ['agent-states-org'],
    queryFn: async () => {
      const result = await mcpCall<RuntimeStateSummary[]>('observability', { action: 'list_runtime_states' })
      return Array.isArray(result) ? result : []
    },
    refetchInterval: 10_000,
    retry: false,
  })
}

export interface AgentInbox {
  agent_id: string
  size: number
}

export function useAgentInboxes() {
  return useQuery({
    queryKey: ['agent-inboxes'],
    queryFn: async () => {
      const result = await mcpCall<AgentInbox[]>('comm', { action: 'list_inboxes' })
      return Array.isArray(result) ? result : []
    },
    refetchInterval: 10_000,
    retry: false,
  })
}

// ─── Comm Log ────────────────────────────────────────────────────────────────

export interface CommLogEntry {
  id: string
  from_agent: string
  to_agent: string
  content: string
  content_type?: string
  direction?: string
  session_id?: string
  created_at: string
}

export function useAgentCommLog(agentId: string | null, limit = 15) {
  return useQuery({
    queryKey: ['agent-comm-log', agentId, limit],
    queryFn: async () => {
      const result = await mcpCall<CommLogEntry[]>('comm', {
        action: 'get_log',
        agent_id: agentId!,
        limit,
      })
      return Array.isArray(result) ? result : []
    },
    enabled: !!agentId,
    staleTime: 5_000,
    refetchInterval: 10_000,
    retry: false,
  })
}

// ─── Agent Detail ────────────────────────────────────────────────────────────

export function useAgentDetail(agentId: string | null) {
  return useQuery({
    queryKey: ['agent-detail', agentId],
    queryFn: async () => {
      // Try runtime state first, fall back to agent record
      try {
        return await mcpCall<RuntimeStateDetail>('observability', { action: 'get_runtime_state', agent_id: agentId! })
      } catch {
        // Fall back to get_agent which returns the agent record directly
        const a = await mcpCall<Record<string, unknown>>('agent', { action: 'get_agent', agent_id: agentId! })
        return {
          agent_id: a.id as string,
          name: a.name as string,
          status: (a.status as string) || 'idle',
          status_reason: a.status_reason as string | undefined,
          error: a.status_reason as string | undefined,
          workspace: a.workspace_id as string | undefined,
          config: {
            personality: a.personality,
            clearance_level: a.clearance_level,
            provider_id: a.provider_id,
            default_model: a.default_model,
            chat_model: a.chat_model,
            role_template_id: a.role_template_id,
            system_prompt: a.system_prompt,
            is_internal: a.is_internal,
            guard_bypass: a.guard_bypass,
          },
        } as RuntimeStateDetail
      }
    },
    enabled: !!agentId,
    retry: false,
  })
}
