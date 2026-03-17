import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { mcpCall } from '@/lib/mcp-client'

// ─── Types ────────────────────────────────────────────────────────────────────

export interface Interjection {
  id: string
  scope: string
  severity: string
  source: string
  reason: string
  status: string
  resolution?: string
  created_by?: string
  source_clearance: number
  resolved_by?: string
  resolver_clearance?: number
  remediation_persona?: string
  action?: string
  trust_level?: string
  trace_id?: string
  created_at: string
  updated_at: string
}

export interface SafeModeState {
  scope: string
  active: boolean
  triggered_at: string
  interjection_id: string
}

export interface ActiveInterjectionsResult {
  interjections: Interjection[]
  count: number
}

export interface SafeModeStatusResult {
  active: boolean
  scopes: SafeModeState[]
  count: number
}

export interface InterjectionHistoryResult {
  interjections: Interjection[]
  count: number
  scope: string
}

export interface PullAndonCordArgs {
  scope: string
  severity: string
  reason: string
  source?: string
}

export interface PullAndonCordResult {
  id: string
  message: string
}

export interface ResolveInterjectionArgs {
  id: string
  resolution_action: string
  resolution: string
}

export interface ResolveInterjectionResult {
  id: string
  status: string
  action: string
  resolution: string
}

export interface RequestBypassArgs {
  interjection_id: string
  duration_seconds: number
  reason: string
}

export interface RequestBypassResult {
  id: string
  message: string
}

// ─── Query Keys ──────────────────────────────────────────────────────────────

export const interjectionKeys = {
  active: (scope?: string) => ['interjections', 'active', scope ?? 'all'] as const,
  safeMode: () => ['interjections', 'safe-mode'] as const,
  history: (scope?: string) => ['interjections', 'history', scope ?? 'all'] as const,
}

// ─── Hooks ────────────────────────────────────────────────────────────────────

export function useActiveInterjections(scope?: string) {
  return useQuery({
    queryKey: interjectionKeys.active(scope),
    queryFn: () =>
      mcpCall<ActiveInterjectionsResult>('governance', { action: 'get_active_interjections', ...(scope ? { scope } : {}) }),
    refetchInterval: 5000,
    retry: false,
  })
}

export function useSafeModeStatus() {
  return useQuery({
    queryKey: interjectionKeys.safeMode(),
    queryFn: () => mcpCall<SafeModeStatusResult>('governance', { action: 'get_safe_mode_status' }),
    refetchInterval: 5000,
    retry: false,
  })
}

export function useInterjectionHistory(scope?: string) {
  return useQuery({
    queryKey: interjectionKeys.history(scope),
    queryFn: () =>
      mcpCall<InterjectionHistoryResult>('governance', { action: 'get_interjection_history', ...(scope ? { scope } : {}) }),
    retry: false,
  })
}

export function usePullAndonCord() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (args: PullAndonCordArgs) =>
      mcpCall<PullAndonCordResult>('governance', { action: 'pull_andon_cord', ...(args as unknown as Record<string, unknown>) }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['interjections'] })
    },
  })
}

export function useResolveInterjection() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (args: ResolveInterjectionArgs) =>
      mcpCall<ResolveInterjectionResult>(
        'governance',
        { action: 'resolve_interjection', ...(args as unknown as Record<string, unknown>) },
      ),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['interjections'] })
    },
  })
}

export function useRequestTemporaryBypass() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (args: RequestBypassArgs) =>
      mcpCall<RequestBypassResult>(
        'governance',
        { action: 'request_temporary_bypass', ...(args as unknown as Record<string, unknown>) },
      ),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['interjections'] })
    },
  })
}

