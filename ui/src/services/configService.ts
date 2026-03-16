import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { mcpCall } from '@/lib/mcp-client'

export interface ConfigKey {
  key: string
  scope_type: string
  value_type: string
  default_val: string
  critical: boolean
  description: string
  current_value?: string
}

export interface SetConfigArgs {
  key: string
  value: string
  scope_type?: string
  workspace_name?: string
}

export function useConfigKeys() {
  return useQuery({
    queryKey: ['config-keys'],
    queryFn: () => mcpCall<ConfigKey[]>('config', { action: 'list_config_keys' }),
  })
}

export function useSetConfig() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (args: SetConfigArgs) => mcpCall('config', { action: 'set_config', ...(args as unknown as Record<string, unknown>) }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['config-keys'] }),
  })
}
