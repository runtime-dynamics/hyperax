import { useQuery } from '@tanstack/react-query'
import { mcpCall } from '@/lib/mcp-client'

// ─── Interfaces ───────────────────────────────────────────────────────────────

// Mirrors Go CheckABACPermissions response shape.
// Backend is the enforcer — this hook is for UI convenience only (hiding elements).
export interface ABACSession {
  persona_id: string
  persona_name: string
  clearance_level: number
  // Derived permission booleans returned by the backend
  can_read: boolean
  can_write: boolean
  can_delete: boolean
  can_admin: boolean
}

export interface Permissions {
  canRead: boolean
  canWrite: boolean
  canDelete: boolean
  canAdmin: boolean
  clearanceLevel: number
  isLoaded: boolean
}

// Fallback used when the session is not available (backend not yet implemented).
// Defaults to permissive so the UI does not lock itself out before auth lands.
const PERMISSIVE_FALLBACK: Permissions = {
  canRead: true,
  canWrite: true,
  canDelete: true,
  canAdmin: true,
  clearanceLevel: 2,
  isLoaded: false,
}

// ─── Hook ─────────────────────────────────────────────────────────────────────

/**
 * Returns ABAC-derived permission flags for the current session persona.
 *
 * The backend is the authoritative enforcer. This hook purely reflects what
 * the backend says so the UI can hide elements that the user cannot use.
 * Never rely on these flags as a security boundary.
 */
export function usePermissions(): Permissions {
  const { data, isSuccess } = useQuery({
    queryKey: ['abac-session'],
    queryFn: () => mcpCall<ABACSession>('get_abac_session', {}),
    // Don't retry aggressively — the endpoint may not exist yet during Wave III rollout.
    retry: 1,
    staleTime: 30_000,
  })

  if (!isSuccess || !data) {
    return PERMISSIVE_FALLBACK
  }

  return {
    canRead: data.can_read,
    canWrite: data.can_write,
    canDelete: data.can_delete,
    canAdmin: data.can_admin,
    clearanceLevel: data.clearance_level,
    isLoaded: true,
  }
}
