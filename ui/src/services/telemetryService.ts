import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { mcpCall } from '@/lib/mcp-client'

// ─── Interfaces ───────────────────────────────────────────────────────────────

// Matches types.Session in pkg/types/telemetry.go
export interface Session {
  id: string
  agent_id: string
  provider_id?: string   // provider used for this session
  model?: string         // model used for this session
  started_at: string
  ended_at?: string | null
  duration?: number
  tool_calls: number     // running count of tool invocations
  total_cost?: number    // accumulated estimated cost (note: NOT total_tokens)
  status: string         // "active", "completed", "abandoned"
  metadata?: string      // free-form JSON
  created_at?: string
}

export interface SessionTelemetry {
  session: Session
  metrics: MetricsSummary
  summary: Record<string, unknown>
}

export interface MetricsSummary {
  total_calls: number
  avg_duration_ms: number
  p50_ms: number
  p95_ms: number
  p99_ms: number
  tools_by_count?: Record<string, number>
}

// Matches types.CostEstimate in pkg/types/telemetry.go
// Note: backend get_cost_report returns CostEstimate (aggregated by tool), not per-call entries
export interface CostEntry {
  tool_name: string
  provider_id?: string
  call_count: number
  total_cost: number
  avg_cost: number
  avg_duration?: number
  // Legacy fallback fields (kept for backward compat if backend shape changes)
  id?: string
  session_id?: string
  agent_id?: string
  scope?: string
  tokens_in?: number
  tokens_out?: number
  cost_usd?: number      // alias for total_cost from older implementations
  created_at?: string
}

// Matches types.Alert in pkg/types/telemetry.go
export interface Alert {
  id: string
  name: string
  metric: string
  operator: string  // "gt", "lt", "gte", "lte", "eq"
  threshold: number
  window: string    // "1h", "24h", "7d"
  severity: string  // "info", "warning", "critical"
  enabled: boolean
  last_fired_at?: string | null
  created_at: string
  updated_at?: string
}

// Backend create_alert requires: name, metric, operator (gt/lt/gte/lte/eq), threshold
export interface CreateAlertArgs {
  name: string
  metric: string
  operator: string  // must be one of: gt, lt, gte, lte, eq
  threshold: number
  window?: string
  severity?: string
}

// ─── Hooks ────────────────────────────────────────────────────────────────────

export function useSessions(limit?: number) {
  return useQuery({
    queryKey: ['sessions', limit],
    queryFn: () =>
      mcpCall<Session[]>('observability', { action: 'list_sessions', ...(limit !== undefined ? { limit } : {}) }),
    retry: false,
  })
}

export function useSessionTelemetry(sessionId: string) {
  return useQuery({
    queryKey: ['session-telemetry', sessionId],
    queryFn: () =>
      mcpCall<SessionTelemetry>('observability', { action: 'get_session_telemetry', session_id: sessionId }),
    enabled: !!sessionId,
    retry: false,
  })
}

export function useMetricsSummary(since?: string) {
  return useQuery({
    queryKey: ['metrics-summary', since],
    queryFn: () => {
      const args: Record<string, unknown> = {}
      if (since) args.since = since
      return mcpCall<MetricsSummary>('observability', { action: 'get_metrics_summary', ...args })
    },
    refetchInterval: 10_000,
    retry: false,
  })
}

export function useCostReport(since?: string) {
  return useQuery({
    queryKey: ['cost-report', since],
    queryFn: () => {
      const args: Record<string, unknown> = {}
      if (since) args.since = since
      return mcpCall<CostEntry[]>('observability', { action: 'get_cost_report', ...args })
    },
    retry: false,
  })
}

export function useAlerts() {
  return useQuery({
    queryKey: ['alerts'],
    queryFn: () => mcpCall<Alert[]>('observability', { action: 'list_alerts' }),
    retry: false,
  })
}

export function useCreateAlert() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (args: CreateAlertArgs) =>
      mcpCall<{ id: string; message: string }>('observability', { action: 'create_alert', ...(args as unknown as Record<string, unknown>) }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['alerts'] }),
  })
}

export function useDeleteAlert() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (alertId: string) =>
      mcpCall<{ id: string; status: string }>('observability', { action: 'delete_alert', alert_id: alertId }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['alerts'] }),
  })
}
