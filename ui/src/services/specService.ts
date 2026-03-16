import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { mcpCall } from '@/lib/mcp-client'

// ── Interfaces ──────────────────────────────────────────────────────────────

export interface SpecSummary {
  id: string
  spec_number: number
  label: string
  title: string
  status: string
  project_id?: string
  created_by?: string
}

export interface SpecTask {
  id: string
  title: string
  requirement: string
  acceptance_criteria: string
  task_id?: string
}

export interface SpecMilestone {
  id: string
  title: string
  description?: string
  milestone_id?: string
  tasks: SpecTask[]
}

export interface SpecAmendment {
  id: string
  title: string
  description: string
  author?: string
  created_at: string
}

export interface SpecDetail {
  id: string
  spec_number: number
  label: string
  title: string
  description: string
  status: string
  project_id?: string
  workspace_name: string
  created_by?: string
  created_at: string
  updated_at: string
  milestones: SpecMilestone[]
  amendments?: SpecAmendment[]
}

// ── Hooks ───────────────────────────────────────────────────────────────────

export function useSpecs(workspaceName: string) {
  return useQuery({
    queryKey: ['specs', workspaceName],
    queryFn: async () => {
      const result = await mcpCall<SpecSummary[] | string>('doc', {
        action: 'list_specs',
        workspace_name: workspaceName,
      })
      if (typeof result === 'string') return [] as SpecSummary[]
      return result
    },
    enabled: !!workspaceName,
  })
}

export function useSpecDetail(specId: string) {
  return useQuery({
    queryKey: ['spec-detail', specId],
    queryFn: () => mcpCall<SpecDetail>('doc', { action: 'get_spec', spec_id: specId }),
    enabled: !!specId,
  })
}

export function useAmendSpec() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (args: { spec_id: string; title: string; description: string; author?: string }) =>
      mcpCall<{ id: string; message: string }>('doc', { action: 'amend_spec', ...args }),
    onSuccess: (_data, vars) => {
      void qc.invalidateQueries({ queryKey: ['spec-detail', vars.spec_id] })
      void qc.invalidateQueries({ queryKey: ['specs'] })
    },
  })
}

export function useUpdateSpecStatus() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (args: { spec_id: string; status: string }) =>
      mcpCall<{ message: string }>('doc', { action: 'update_spec_status', ...args }),
    onSuccess: (_data, vars) => {
      void qc.invalidateQueries({ queryKey: ['spec-detail', vars.spec_id] })
      void qc.invalidateQueries({ queryKey: ['specs'] })
    },
  })
}
