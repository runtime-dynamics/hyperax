import { useQuery } from '@tanstack/react-query'
import { mcpCall } from '@/lib/mcp-client'

// ─── Types ────────────────────────────────────────────────────────────────────

export interface EngagementRuleStep {
  role: string
  action: string
  agent_name?: string
  agent_id?: string
  unassigned?: boolean
}

export interface EngagementRule {
  id: string
  trigger: string
  color: string
  chain: EngagementRuleStep[]
  source: 'template' | 'custom'
  disabled?: boolean
}

// ─── Hooks ────────────────────────────────────────────────────────────────────

export function useEffectiveEngagementRules(agentId: string | null) {
  return useQuery({
    queryKey: ['engagement-rules', agentId],
    queryFn: () =>
      mcpCall<EngagementRule[]>('agent', { action: 'get_effective_engagement_rules', agent_id: agentId! }),
    enabled: !!agentId,
    retry: false,
  })
}
