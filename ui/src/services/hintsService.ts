import { useQuery } from '@tanstack/react-query'
import { mcpCall } from '@/lib/mcp-client'

// ─── Interfaces ───────────────────────────────────────────────────────────────

export interface Hint {
  id?: string
  provider: string
  content: string
  priority?: number
  tags?: string[]
  file_path?: string
  scope?: string
}

export interface HintProvider {
  id: string
  name: string
  description?: string
  enabled: boolean
}

export interface GetHintsArgs {
  file_path?: string
  scope?: string
}

// ─── Hooks ────────────────────────────────────────────────────────────────────

export function useHints(args: GetHintsArgs, enabled: boolean) {
  return useQuery({
    queryKey: ['hints', args],
    queryFn: () =>
      mcpCall<Hint[]>('agent', { action: 'get_hints', ...(args as unknown as Record<string, unknown>) }),
    enabled,
    retry: false,
  })
}

export function useHintProviders() {
  return useQuery({
    queryKey: ['hint-providers'],
    queryFn: () => mcpCall<HintProvider[]>('agent', { action: 'list_hint_providers' }),
    retry: false,
  })
}
