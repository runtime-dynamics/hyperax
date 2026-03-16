import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { mcpCall } from '@/lib/mcp-client'

// ── Interfaces ──────────────────────────────────────────────────────────────

export interface DocFile {
  name: string
  path: string
  size: number
  source?: string
  readonly?: boolean
}

export interface ExternalDocSource {
  id: string
  name: string
  path: string
  created_at: string
}

export interface DocTag {
  tag: string
  file_path: string
  source_type: string
  created_at: string
}

export interface WorkspaceDocStatus {
  has_architecture: boolean
  has_standards: boolean
  architecture_doc: string | null
  standards_doc: string | null
}

export interface DocSearchResult {
  file_path: string
  section_header: string
  snippet: string
  workspace?: string
}

// ── Hooks ───────────────────────────────────────────────────────────────────

export function useDocs(workspaceName: string) {
  return useQuery({
    queryKey: ['docs', workspaceName],
    queryFn: async () => {
      const result = await mcpCall<DocFile[] | string>('doc', {
        action: 'list',
        workspace_name: workspaceName,
      })
      // Backend returns a string message when no docs found
      if (typeof result === 'string') return [] as DocFile[]
      return result
    },
    enabled: !!workspaceName,
  })
}

export function useDocContent(workspaceName: string, path: string) {
  return useQuery({
    queryKey: ['doc-content', workspaceName, path],
    queryFn: () =>
      mcpCall<string>('doc', {
        action: 'get_content',
        workspace_name: workspaceName,
        path,
        limit: -1, // full content
      }),
    enabled: !!workspaceName && !!path,
  })
}

export function useSearchDocs(workspaceName: string, query: string, allWorkspaceNames?: string[]) {
  return useQuery({
    queryKey: ['search-docs', workspaceName || '__all__', query],
    queryFn: async () => {
      // If a specific workspace is selected, search just that one
      if (workspaceName) {
        const result = await mcpCall<DocSearchResult[] | string>('doc', {
          action: 'search',
          workspace_name: workspaceName,
          query,
          limit: 30,
        })
        if (typeof result === 'string') return [] as DocSearchResult[]
        return result.map((r) => ({ ...r, workspace: workspaceName }))
      }

      // "All Projects" — search across all workspaces and merge
      if (!allWorkspaceNames || allWorkspaceNames.length === 0) return [] as DocSearchResult[]

      const results = await Promise.allSettled(
        allWorkspaceNames.map(async (ws) => {
          const result = await mcpCall<DocSearchResult[] | string>('doc', {
            action: 'search',
            workspace_name: ws,
            query,
            limit: 10,
          })
          if (typeof result === 'string') return [] as DocSearchResult[]
          return result.map((r) => ({ ...r, workspace: ws }))
        }),
      )

      return results.flatMap((r) => (r.status === 'fulfilled' ? r.value : []))
    },
    enabled: (!!workspaceName || (allWorkspaceNames ?? []).length > 0) && !!query,
  })
}

// ── External doc source hooks ────────────────────────────────────────────────

export function useExternalDocSources(workspaceName: string) {
  return useQuery({
    queryKey: ['external-doc-sources', workspaceName],
    queryFn: async () => {
      const result = await mcpCall<ExternalDocSource[] | string>('doc', {
        action: 'list_sources',
        workspace_name: workspaceName,
      })
      if (typeof result === 'string') return [] as ExternalDocSource[]
      return result
    },
    enabled: !!workspaceName,
  })
}

export function useAddExternalDocSource() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (args: { workspace_name: string; name: string; path: string }) =>
      mcpCall<{ id: string; message: string; files_indexed: number }>('doc', { action: 'add_source', ...args }),
    onSuccess: (_data, vars) => {
      void qc.invalidateQueries({ queryKey: ['external-doc-sources', vars.workspace_name] })
      void qc.invalidateQueries({ queryKey: ['docs', vars.workspace_name] })
    },
  })
}

export function useRemoveExternalDocSource() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (args: { workspace_name: string; source_id: string }) =>
      mcpCall<{ message: string }>('doc', { action: 'remove_source', ...args }),
    onSuccess: (_data, vars) => {
      void qc.invalidateQueries({ queryKey: ['external-doc-sources', vars.workspace_name] })
      void qc.invalidateQueries({ queryKey: ['docs', vars.workspace_name] })
    },
  })
}

// ── Document tagging hooks ──────────────────────────────────────────────────

export function useDocTags(workspaceName: string) {
  return useQuery({
    queryKey: ['doc-tags', workspaceName],
    queryFn: async () => {
      const result = await mcpCall<DocTag[] | string>('doc', {
        action: 'list_tags',
        workspace_name: workspaceName,
      })
      if (typeof result === 'string') return [] as DocTag[]
      return result
    },
    enabled: !!workspaceName,
  })
}

export function useTagDocument() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (args: { workspace_name: string; file_path: string; tag: 'architecture' | 'standards' }) =>
      mcpCall<string>('doc', { action: 'tag', ...args }),
    onSuccess: (_data, vars) => {
      void qc.invalidateQueries({ queryKey: ['doc-tags', vars.workspace_name] })
      void qc.invalidateQueries({ queryKey: ['workspace-doc-status', vars.workspace_name] })
    },
  })
}

export function useUntagDocument() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (args: { workspace_name: string; tag: 'architecture' | 'standards' }) =>
      mcpCall<string>('doc', { action: 'untag', ...args }),
    onSuccess: (_data, vars) => {
      void qc.invalidateQueries({ queryKey: ['doc-tags', vars.workspace_name] })
      void qc.invalidateQueries({ queryKey: ['workspace-doc-status', vars.workspace_name] })
    },
  })
}

export function useWorkspaceDocStatus(workspaceName: string) {
  return useQuery({
    queryKey: ['workspace-doc-status', workspaceName],
    queryFn: () =>
      mcpCall<WorkspaceDocStatus>('doc', {
        action: 'workspace_status',
        workspace_name: workspaceName,
      }),
    enabled: !!workspaceName,
  })
}

// ── Code search ─────────────────────────────────────────────────────────────

export function useSearchCode(workspaceName: string, query: string, allWorkspaceNames?: string[]) {
  return useQuery({
    queryKey: ['search-code', workspaceName || '__all__', query],
    queryFn: async () => {
      // If a specific workspace is selected, search just that one
      if (workspaceName) {
        return mcpCall<string>('code', {
          action: 'search',
          workspace_name: workspaceName,
          query,
          limit: 30,
        })
      }

      // "All Projects" — search across all workspaces and merge
      if (!allWorkspaceNames || allWorkspaceNames.length === 0) return ''

      const results = await Promise.allSettled(
        allWorkspaceNames.map(async (ws) => {
          const result = await mcpCall<string>('code', {
            action: 'search',
            workspace_name: ws,
            query,
            limit: 10,
          })
          if (!result || typeof result !== 'string') return ''
          return `── ${ws} ──\n${result}`
        }),
      )

      return results
        .filter((r): r is PromiseFulfilledResult<string> => r.status === 'fulfilled' && !!r.value)
        .map((r) => r.value)
        .join('\n\n')
    },
    enabled: (!!workspaceName || (allWorkspaceNames ?? []).length > 0) && !!query,
  })
}
