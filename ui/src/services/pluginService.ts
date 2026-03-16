import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { mcpCall } from '@/lib/mcp-client'

// ─── Interfaces ───────────────────────────────────────────────────────────────

export interface PluginSummary {
  id: string
  name: string
  version?: string
  description?: string
  enabled: boolean
  status?: string
  error?: string
  manifest_url?: string
}

export interface PluginEnvVar {
  name: string
  required: boolean
  default?: string
  description?: string
}

export type PluginVariableType = 'string' | 'int' | 'float' | 'bool' | 'array_string' | 'array_int' | 'array_float'
export type PluginIntegration = 'channel' | 'tooling' | 'secret_provider' | 'sensor' | 'guard' | 'audit'

export interface PluginVariable {
  name: string
  type: PluginVariableType
  required: boolean
  default?: unknown
  description?: string
  secret: boolean
  dynamic: boolean
  env_name?: string
}

export interface PluginResource {
  type: string
  name: string
  config?: Record<string, unknown>
}

export interface PluginInfo extends PluginSummary {
  author?: string
  tools?: string[]
  permissions?: string[]
  source_repo?: string
  source_hash?: string
  env?: PluginEnvVar[]
  integration?: PluginIntegration
  variables?: PluginVariable[]
  config?: Record<string, string>
  approval_required?: boolean
  approved?: boolean
  resources?: PluginResource[]
  metadata?: Record<string, unknown>
}

// ─── Hooks ────────────────────────────────────────────────────────────────────

export function usePlugins() {
  return useQuery({
    queryKey: ['plugins'],
    queryFn: () => mcpCall<PluginSummary[]>('plugin', { action: 'list' }),
    refetchInterval: 10000,
    retry: false,
  })
}

export function usePluginInfo(pluginId: string | null) {
  return useQuery({
    queryKey: ['plugin-info', pluginId],
    queryFn: () => mcpCall<PluginInfo>('plugin', { action: 'get_info', plugin_id: pluginId! }),
    enabled: !!pluginId,
    retry: false,
  })
}

export function useEnablePlugin() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (pluginName: string) =>
      mcpCall<{ name: string; status: string }>('plugin', { action: 'enable', name: pluginName }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['plugins'] })
      void qc.invalidateQueries({ queryKey: ['plugin-info'] })
      void qc.invalidateQueries({ queryKey: ['catalog'] })
    },
  })
}

export function useDisablePlugin() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (pluginName: string) =>
      mcpCall<{ name: string; status: string }>('plugin', { action: 'disable', name: pluginName }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['plugins'] })
      void qc.invalidateQueries({ queryKey: ['plugin-info'] })
      void qc.invalidateQueries({ queryKey: ['catalog'] })
    },
  })
}

export interface InstallPluginParams {
  mode: 'local' | 'remote' | 'github'
  value: string
}

export function useInstallPlugin() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (params: InstallPluginParams) => {
      let mcpParams: Record<string, string>
      switch (params.mode) {
        case 'local':
          mcpParams = { path: params.value }
          break
        case 'remote':
          mcpParams = { manifest_url: params.value }
          break
        case 'github':
          mcpParams = { source: params.value }
          break
      }
      return mcpCall<{ name: string; version: string; tools: number; source: string; integration: string }>(
        'plugin',
        { action: 'install', ...mcpParams },
      )
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['plugins'] })
      void qc.invalidateQueries({ queryKey: ['catalog'] })
    },
  })
}

export function useUninstallPlugin() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (pluginName: string) =>
      mcpCall<{ name: string; status: string }>('plugin', { action: 'uninstall', name: pluginName }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['plugins'] })
      void qc.invalidateQueries({ queryKey: ['plugin-info'] })
      void qc.invalidateQueries({ queryKey: ['catalog'] })
    },
  })
}

export interface UpgradePluginParams {
  name: string
  mode: 'local' | 'remote' | 'github'
  value: string
}

export interface UpgradePluginResult {
  name: string
  old_version: string
  new_version: string
  status: string
  enabled?: boolean
  enable_error?: string
  message: string
}

export function useUpgradePlugin() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (params: UpgradePluginParams) => {
      let mcpParams: Record<string, string> = { name: params.name }
      switch (params.mode) {
        case 'local':
          mcpParams.path = params.value
          break
        case 'remote':
          mcpParams.manifest_url = params.value
          break
        case 'github':
          mcpParams.source = params.value
          break
      }
      return mcpCall<UpgradePluginResult>('plugin', { action: 'upgrade', ...mcpParams })
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['plugins'] })
      void qc.invalidateQueries({ queryKey: ['plugin-info'] })
      void qc.invalidateQueries({ queryKey: ['catalog'] })
    },
  })
}

export interface ConfigurePluginParams {
  name: string
  variable: string
  value: string
}

export function useConfigurePlugin() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (params: ConfigurePluginParams) =>
      mcpCall<{ plugin: string; variable: string; value: string }>('plugin', { action: 'configure', ...params }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['plugin-info'] }),
  })
}

export interface LinkPluginSecretParams {
  plugin_name: string
  variable: string
  secret_key: string
  secret_scope?: string
}

export function useLinkPluginSecret() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (params: LinkPluginSecretParams) =>
      mcpCall<{ plugin: string; variable: string; secret_ref: string }>('plugin', { action: 'link_secret', ...params }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['plugin-info'] }),
  })
}

export interface RequestApprovalResult {
  plugin: string
  status: 'challenge_sent' | 'challenge_generated' | 'already_approved'
  channel_id?: string
  code?: string
  message?: string
  warning?: string
}

export function useRequestPluginApproval() {
  return useMutation({
    mutationFn: (params: { name: string; channel_id: string }) =>
      mcpCall<RequestApprovalResult>('plugin', { action: 'request_approval', ...params }),
  })
}

export interface ApprovePluginParams {
  name: string
  code?: string
}

export function useApprovePlugin() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (params: ApprovePluginParams) =>
      mcpCall<{ plugin: string; approved: boolean }>('plugin', { action: 'approve', ...params }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['plugins'] })
      void qc.invalidateQueries({ queryKey: ['plugin-info'] })
    },
  })
}
