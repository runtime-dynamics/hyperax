import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { mcpCall } from '@/lib/mcp-client'

// ─── Types ────────────────────────────────────────────────────────────────────

export interface RoleTemplate {
  id: string
  name: string
  description: string
  system_prompt?: string
  suggested_model: string
  clearance_level: number
  built_in: boolean
  has_override?: boolean
}

// ─── Hooks ────────────────────────────────────────────────────────────────────

export function useRoleTemplates() {
  return useQuery({
    queryKey: ['role-templates'],
    queryFn: async () => {
      const result = await mcpCall<{ count: number; templates: RoleTemplate[] }>(
        'agent',
        { action: 'list_role_templates' },
      )
      return result.templates ?? []
    },
    retry: false,
  })
}

export function useOverrideRoleTemplate() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (params: { template_id: string; system_prompt: string }) => {
      return mcpCall<string>('agent', { action: 'override_role_template', ...params })
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['role-templates'] })
    },
  })
}

export function useRemoveRoleTemplateOverride() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (params: { template_id: string }) => {
      return mcpCall<string>('agent', { action: 'remove_role_template_override', ...params })
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['role-templates'] })
    },
  })
}
