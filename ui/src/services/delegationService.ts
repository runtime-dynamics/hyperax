import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { mcpCall } from '@/lib/mcp-client'

// ─── Interfaces ───────────────────────────────────────────────────────────────

export type DelegationType = 'clearance_elevation' | 'credential_passthrough'

export interface DelegationGrant {
  id: string
  grantor_id: string
  grantor_name?: string
  grantee_id: string
  grantee_name?: string
  delegation_type: DelegationType
  reason: string
  expires_at?: string | null
  created_at: string
  is_active: boolean
}

export interface GrantDelegationArgs {
  grantor_id: string
  grantee_id: string
  delegation_type: DelegationType
  reason: string
  credential?: string
  expires_at?: string
}

export interface RevokeDelegationArgs {
  delegation_id: string
}

// ─── Hooks ────────────────────────────────────────────────────────────────────

export function useDelegations() {
  return useQuery({
    queryKey: ['delegations'],
    queryFn: () => mcpCall<DelegationGrant[]>('agent', { action: 'list_delegations' }),
    retry: false,
  })
}

export function useGrantDelegation() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (args: GrantDelegationArgs) =>
      mcpCall<{ id: string; status: string }>('agent', { action: 'grant_delegation', ...(args as unknown as Record<string, unknown>) }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['delegations'] }),
  })
}

export function useRevokeDelegation() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (args: RevokeDelegationArgs) =>
      mcpCall<{ status: string }>('agent', { action: 'revoke_delegation', ...(args as unknown as Record<string, unknown>) }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['delegations'] }),
  })
}
