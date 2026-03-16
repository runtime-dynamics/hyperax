import { useQuery, useQueries, useMutation, useQueryClient } from '@tanstack/react-query'
import { mcpCall } from '@/lib/mcp-client'

// ─── Interfaces ───────────────────────────────────────────────────────────────

export interface MCPToken {
  id: string
  persona_id: string
  persona_name?: string
  description: string
  expires_at?: string | null
  created_at: string
  last_used_at?: string | null
  is_active: boolean
}

export interface CreateTokenArgs {
  persona_id: string
  description: string
  expires_at?: string
}

export interface CreateTokenResult {
  id: string
  token: string
  persona_id: string
  description: string
  expires_at?: string | null
  created_at: string
}

export interface RevokeTokenArgs {
  token_id: string
}

// ─── Hooks ────────────────────────────────────────────────────────────────────

export function useMCPTokens(persona_id?: string) {
  return useQuery({
    queryKey: ['mcp-tokens', persona_id ?? 'all'],
    queryFn: () =>
      mcpCall<MCPToken[]>('config', { action: 'list_mcp_tokens', ...(persona_id ? { persona_id } : {}) }),
    enabled: !!persona_id,
    retry: false,
  })
}

/**
 * Fetch tokens for multiple personas in parallel and merge the results.
 * Returns a combined loading/error state along with the flattened token list.
 */
export function useMCPTokensForPersonas(personaIds: string[]) {
  const results = useQueries({
    queries: personaIds.map((persona_id) => ({
      queryKey: ['mcp-tokens', persona_id],
      queryFn: () => mcpCall<MCPToken[]>('config', { action: 'list_mcp_tokens', persona_id }),
      retry: false,
    })),
  })

  const isLoading = results.some((r) => r.isLoading)
  const errors = results.map((r) => r.error).filter(Boolean)
  const tokens: MCPToken[] = results.flatMap((r) =>
    Array.isArray(r.data) ? r.data : [],
  )

  return { tokens, isLoading, errors }
}

export function useCreateMCPToken() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (args: CreateTokenArgs) =>
      mcpCall<CreateTokenResult>('config', { action: 'create_mcp_token', ...(args as unknown as Record<string, unknown>) }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['mcp-tokens'] }),
  })
}

export function useRevokeMCPToken() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (args: RevokeTokenArgs) =>
      mcpCall<{ status: string }>('config', { action: 'revoke_mcp_token', ...(args as unknown as Record<string, unknown>) }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['mcp-tokens'] }),
  })
}
