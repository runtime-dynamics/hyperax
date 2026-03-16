import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { mcpCall } from '@/lib/mcp-client'

// ─── Interfaces ───────────────────────────────────────────────────────────────

export interface GuardAction {
  id: string
  tool_name: string
  tool_action: string
  tool_params: string
  guard_name: string
  caller_persona: string
  status: string // pending, approved, rejected, timeout
  decided_by?: string
  notes?: string
  created_at: string
  decided_at?: string
  expires_at: string
  trace_id?: string
}

export interface PendingActionsResponse {
  actions: GuardAction[]
  count: number
}

export interface ActionHistoryResponse {
  actions: GuardAction[]
  count: number
}

// ─── Query Hooks ──────────────────────────────────────────────────────────────

export function usePendingActions() {
  return useQuery({
    queryKey: ['guard', 'pending'],
    queryFn: () => mcpCall<PendingActionsResponse>('governance', { action: 'get_pending_actions' }),
    refetchInterval: 3000,
    retry: false,
  })
}

export function useActionHistory(limit = 50) {
  return useQuery({
    queryKey: ['guard', 'history', limit],
    queryFn: () =>
      mcpCall<ActionHistoryResponse>('governance', { action: 'get_action_history', limit }),
    retry: false,
  })
}

export function useActionDetail(id: string | null) {
  return useQuery({
    queryKey: ['guard', 'detail', id],
    queryFn: () => mcpCall<GuardAction>('governance', { action: 'get_action_detail', id: id! }),
    enabled: !!id,
    retry: false,
  })
}

// ─── Mutation Hooks ───────────────────────────────────────────────────────────

export interface ApproveActionArgs {
  id: string
  notes?: string
}

export function useApproveAction() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (params: ApproveActionArgs) =>
      mcpCall<{ message: string }>('governance', { action: 'approve_action', ...(params as unknown as Record<string, unknown>) }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['guard'] })
    },
  })
}

export interface RejectActionArgs {
  id: string
  notes?: string
}

export function useRejectAction() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (params: RejectActionArgs) =>
      mcpCall<{ message: string }>('governance', { action: 'reject_action', ...(params as unknown as Record<string, unknown>) }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['guard'] })
    },
  })
}
