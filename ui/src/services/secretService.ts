import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { mcpCall } from '@/lib/mcp-client'

// ─── Interfaces ───────────────────────────────────────────────────────────────

export interface SecretEntry {
  key: string
  scope?: string
  access_scope?: string
  created_at?: string
  updated_at?: string
}

export interface SetSecretArgs {
  key: string
  value: string
  scope?: string
}

export interface DeleteSecretArgs {
  key: string
  scope?: string
}

// ─── Hooks ────────────────────────────────────────────────────────────────────

export function useSecrets() {
  return useQuery({
    queryKey: ['secrets'],
    queryFn: () => mcpCall<SecretEntry[]>('secret', { action: 'list' }),
    retry: false,
  })
}

export function useSetSecret() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (args: SetSecretArgs) =>
      mcpCall<{ key: string; status: string }>('secret', { action: 'set', ...(args as unknown as Record<string, unknown>) }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['secrets'] }),
  })
}

export function useDeleteSecret() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (args: DeleteSecretArgs) =>
      mcpCall<{ key: string; status: string }>('secret', { action: 'delete', ...(args as unknown as Record<string, unknown>) }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['secrets'] }),
  })
}

/** Fetches structured secret entries (key, scope, access_scope) via list_secret_entries. */
export function useSecretEntries() {
  return useQuery({
    queryKey: ['secret-entries'],
    queryFn: async () => {
      const result = await mcpCall<{ entries: SecretEntry[] }>('secret', { action: 'list_entries' })
      // Handle both wrapped { entries: [...] } and direct array responses.
      if (Array.isArray(result)) return result as unknown as SecretEntry[]
      return result?.entries ?? []
    },
    retry: false,
  })
}

export function useUpdateSecretAccessScope() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (args: { key: string; scope?: string; access_scope: string }) =>
      mcpCall<{ key: string; status: string }>('secret', {
        action: 'update_scope',
        key: args.key,
        scope: args.scope ?? 'global',
        access_scope: args.access_scope,
      }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['secrets'] }),
  })
}
