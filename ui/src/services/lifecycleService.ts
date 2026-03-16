import { useQuery } from '@tanstack/react-query'
import { mcpCall } from '@/lib/mcp-client'

// ─── Interfaces ───────────────────────────────────────────────────────────────

export interface RuntimeStateSummary {
  agent_id: string
  name?: string
  status: string
  started_at?: string
  last_active_at?: string
  workspace?: string
  tool_calls?: number
  error?: string
}

export interface RuntimeStateDetail extends RuntimeStateSummary {
  config?: Record<string, unknown>
  metrics?: Record<string, unknown>
  metadata?: Record<string, unknown>
}

// ─── Hooks ────────────────────────────────────────────────────────────────────

export function useRuntimeStates() {
  return useQuery({
    queryKey: ['runtime-states'],
    queryFn: () => mcpCall<RuntimeStateSummary[]>('observability', { action: 'list_runtime_states' }),
    refetchInterval: 10_000,
    retry: false,
  })
}

export function useRuntimeState(agentId: string | null) {
  return useQuery({
    queryKey: ['runtime-state', agentId],
    queryFn: () => mcpCall<RuntimeStateDetail>('observability', { action: 'get_runtime_state', agent_id: agentId! }),
    enabled: !!agentId,
    refetchInterval: 10_000,
    retry: false,
  })
}
