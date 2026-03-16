import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { apiGet, apiPut } from '@/lib/api-client'

export interface ConfigKeyMeta {
  key: string
  scope_type: string
  value_type: string
  default_val: string
  critical: boolean
  description: string
}

export interface ConfigValue {
  key: string
  value: string
  scope_type: string
  scope_id: string
}

export interface ConfigChange {
  key: string
  old_value: string
  new_value: string
  scope_type: string
  scope_id: string
  actor: string
  changed_at: string
}

export function useRestConfigKeys() {
  return useQuery({
    queryKey: ['rest-config-keys'],
    queryFn: () => apiGet<ConfigKeyMeta[]>('/config/keys'),
  })
}

export function useRestConfigValues(scope = 'global', scopeId = '') {
  const params = new URLSearchParams({ scope })
  if (scopeId) params.set('scope_id', scopeId)
  return useQuery({
    queryKey: ['rest-config-values', scope, scopeId],
    queryFn: () => apiGet<ConfigValue[]>(`/config/values?${params}`),
  })
}

export function useRestConfigKey(key: string) {
  return useQuery({
    queryKey: ['rest-config-keys', key],
    queryFn: () => apiGet<{ meta: ConfigKeyMeta; value: string; scope: { type: string; id: string } }>(`/config/keys/${encodeURIComponent(key)}`),
    enabled: !!key,
  })
}

export function useRestSetConfig() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (args: { key: string; value: string; scope?: string; scope_id?: string; actor?: string }) =>
      apiPut(`/config/keys/${encodeURIComponent(args.key)}`, args),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['rest-config-keys'] })
      qc.invalidateQueries({ queryKey: ['rest-config-values'] })
    },
  })
}

export function useRestConfigHistory(key: string, limit = 20) {
  return useQuery({
    queryKey: ['rest-config-history', key],
    queryFn: () => apiGet<ConfigChange[]>(`/config/keys/${encodeURIComponent(key)}/history?limit=${limit}`),
    enabled: !!key,
  })
}
