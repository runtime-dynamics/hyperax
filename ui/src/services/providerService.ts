import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { mcpCall } from '@/lib/mcp-client'

export interface Provider {
  id: string
  name: string
  kind: string
  base_url: string
  secret_key_ref: string
  is_default: boolean
  is_enabled: boolean
  models: string
  metadata: string
  created_at: string
  updated_at: string
}

export interface ProviderPreset {
  kind: string
  name: string
  base_url: string
  models: string[]
  needs_key: boolean
}

export interface CreateProviderArgs {
  name: string
  kind: string
  base_url: string
  secret_key_ref?: string
  models?: string
  metadata?: string
  is_default?: boolean
}

export interface UpdateProviderArgs {
  provider_id: string
  name?: string
  kind?: string
  base_url?: string
  secret_key_ref?: string
  models?: string
  metadata?: string
  is_enabled?: boolean
}

export function useProviders() {
  return useQuery({
    queryKey: ['providers'],
    queryFn: () => mcpCall<Provider[]>('config', { action: 'list_providers' }),
  })
}

export function useProvider(id: string) {
  return useQuery({
    queryKey: ['providers', id],
    queryFn: () => mcpCall<Provider>('config', { action: 'get_provider', provider_id: id }),
    enabled: !!id,
  })
}

export function useProviderPresets() {
  return useQuery({
    queryKey: ['provider-presets'],
    queryFn: () => mcpCall<ProviderPreset[]>('config', { action: 'list_provider_presets' }),
  })
}

export function useDefaultProvider() {
  return useQuery({
    queryKey: ['providers', 'default'],
    queryFn: () => mcpCall<Provider>('config', { action: 'get_default_provider' }),
  })
}

export function useCreateProvider() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (args: CreateProviderArgs) =>
      mcpCall('config', { action: 'create_provider', ...(args as unknown as Record<string, unknown>) }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['providers'] }),
  })
}

export function useUpdateProvider() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (args: UpdateProviderArgs) =>
      mcpCall('config', { action: 'update_provider', ...(args as unknown as Record<string, unknown>) }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['providers'] }),
  })
}

export function useDeleteProvider() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (provider_id: string) => mcpCall('config', { action: 'delete_provider', provider_id }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['providers'] }),
  })
}

export function useSetDefaultProvider() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (provider_id: string) => mcpCall('config', { action: 'set_default_provider', provider_id }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['providers'] }),
  })
}


export interface RefreshProviderModelsResult {
  models: string[]
  provider_id: string
  count: number
}

export function parseModels(modelsJson: string): string[] {
  try {
    const parsed = JSON.parse(modelsJson || '[]')
    return Array.isArray(parsed) ? parsed : []
  } catch {
    return []
  }
}

export function useRefreshProviderModels() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (provider_id: string) =>
      mcpCall<RefreshProviderModelsResult>('config', { action: 'refresh_provider_models', provider_id }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['providers'] })
      void qc.invalidateQueries({ queryKey: ['rest-providers'] })
    },
  })
}
