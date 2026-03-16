import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { mcpCall } from '@/lib/mcp-client'

// ─── Interfaces ───────────────────────────────────────────────────────────────

// Matches memorySummary returned by recall_memory handler
export interface MemoryEntry {
  id: string
  scope: string   // "global", "project", "persona"
  type: string    // "episodic", "semantic", "procedural"
  content: string
  score?: number
  rank?: number
  workspace_id?: string
  persona_id?: string
}

// store_memory args — backend requires: content (required), scope, type, workspace_id, persona_id, tags, source, anchored
export interface MemoryStoreArgs {
  content: string
  scope?: string   // "global" | "project" | "persona" — defaults to "project"
  type?: string    // "episodic" | "semantic" | "procedural" — defaults to "episodic"
  workspace_id?: string
  persona_id?: string
  tags?: string[]
  source?: string
  anchored?: boolean
}

// recall_memory args — backend requires: query, workspace_id, persona_id, max_results
export interface MemoryRecallArgs {
  query: string
  workspace_id?: string
  persona_id?: string
  max_results?: number
}

// forget_memory args — backend requires: id (not key+scope)
export interface MemoryForgetArgs {
  id: string
}

// ─── Hooks ────────────────────────────────────────────────────────────────────

export function useMemoryRecall(args: MemoryRecallArgs, enabled: boolean) {
  return useQuery({
    queryKey: ['memory-recall', args],
    queryFn: () =>
      mcpCall<MemoryEntry[]>('memory', { action: 'recall', ...(args as unknown as Record<string, unknown>) }),
    enabled,
    retry: false,
  })
}

export function useMemoryStore() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (args: MemoryStoreArgs) =>
      mcpCall<{ id: string; message: string }>('memory', { action: 'store', ...(args as unknown as Record<string, unknown>) }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['memory-recall'] }),
  })
}

export function useMemoryForget() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (args: MemoryForgetArgs) =>
      mcpCall<{ id: string; status: string }>('memory', { action: 'forget', ...(args as unknown as Record<string, unknown>) }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['memory-recall'] }),
  })
}
