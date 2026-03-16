import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { apiGet, apiPost, apiPut, apiDelete } from '@/lib/api-client'

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

export interface CreateProviderArgs {
  name: string
  kind: string
  base_url?: string
  api_key?: string
  models?: string
  is_default?: boolean
  is_enabled?: boolean
}

export interface TestResult {
  success: boolean
  error?: string
  message?: string
  model_count?: number
  models?: string[]
}

export function useRestProviders() {
  return useQuery({
    queryKey: ['rest-providers'],
    queryFn: () => apiGet<Provider[]>('/providers'),
  })
}

export function useRestProvider(id: string) {
  return useQuery({
    queryKey: ['rest-providers', id],
    queryFn: () => apiGet<Provider>(`/providers/${id}`),
    enabled: !!id,
  })
}

export function useRestDefaultProvider() {
  return useQuery({
    queryKey: ['rest-providers', 'default'],
    queryFn: () => apiGet<Provider>('/providers/default'),
  })
}

function invalidateAll(qc: ReturnType<typeof useQueryClient>) {
  qc.invalidateQueries({ queryKey: ['rest-providers'] })
  qc.invalidateQueries({ queryKey: ['providers'] })
}

export function useRestCreateProvider() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (args: CreateProviderArgs) => apiPost<Provider>('/providers', args),
    onSuccess: () => invalidateAll(qc),
  })
}

export function useRestUpdateProvider() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, ...data }: Partial<Provider> & { id: string; api_key?: string }) =>
      apiPut<Provider>(`/providers/${id}`, data),
    onSuccess: () => invalidateAll(qc),
  })
}

export function useRestDeleteProvider() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => apiDelete(`/providers/${id}`),
    onSuccess: () => invalidateAll(qc),
  })
}

export function useRestSetDefaultProvider() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => apiPost(`/providers/${id}/default`),
    onSuccess: () => invalidateAll(qc),
  })
}

export function useTestProviderConnection() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => apiPost<TestResult>(`/providers/${id}/test`),
    onSuccess: () => invalidateAll(qc),
  })
}
