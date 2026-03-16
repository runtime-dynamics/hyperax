import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { mcpCall } from '@/lib/mcp-client'

// ─── Interfaces ───────────────────────────────────────────────────────────────

export interface CatalogEntry {
  name: string
  display_name: string
  description: string
  category: 'channel' | 'tooling' | 'secret_provider' | 'sensor'
  author: string
  license?: string
  source: string
  homepage?: string
  min_hyperax_version?: string
  latest_version: string
  verified: boolean
  tags?: string[]
  icon?: string
}

export interface CatalogEntryWithStatus extends CatalogEntry {
  installed: boolean
  installed_version?: string
  enabled: boolean
}

export interface RefreshCatalogResult {
  added: number
  updated: number
  message: string
}

export interface PluginVersionsResult {
  name: string
  versions: string[]
  count: number
}


// ─── Hooks ────────────────────────────────────────────────────────────────────

export function useCatalog(category?: string, verifiedOnly?: boolean) {
  return useQuery({
    queryKey: ['catalog', category, verifiedOnly],
    queryFn: () =>
      mcpCall<CatalogEntryWithStatus[]>('plugin', {
        action: 'list_catalog',
        ...(category && { category }),
        ...(verifiedOnly && { verified_only: verifiedOnly }),
      }),
    retry: false,
  })
}

export function useSearchCatalog() {
  return useMutation({
    mutationFn: (params: { query: string; category?: string }) =>
      mcpCall<CatalogEntryWithStatus[]>('plugin', { action: 'search_catalog', ...params }),
  })
}

export function useRefreshCatalog() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () => mcpCall<RefreshCatalogResult>('plugin', { action: 'refresh_catalog' }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['catalog'] }),
  })
}

export function usePluginVersions(name: string | null) {
  return useQuery({
    queryKey: ['plugin-versions', name],
    queryFn: () => mcpCall<PluginVersionsResult>('plugin', { action: 'list_versions', name: name! }),
    enabled: !!name,
    staleTime: 5 * 60 * 1000, // cache for 5 minutes — versions don't change often
    retry: false,
  })
}

