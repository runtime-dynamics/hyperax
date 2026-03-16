import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { mcpCall } from '@/lib/mcp-client'

export interface WorkflowSummary {
  id: string
  name: string
  description: string
  enabled: boolean
}

export interface StepStatus {
  id: string
  name: string
  status: string
  started_at?: string
  completed_at?: string
  error?: string
}

export interface WorkflowStatus {
  run_id: string
  workflow_id: string
  status: string
  error?: string
  steps: StepStatus[]
}

export interface RunWorkflowArgs {
  workflow_id: string
  input?: Record<string, unknown>
}

export interface RunWorkflowResult {
  run_id: string
  message: string
}

export interface ApproveStepArgs {
  run_id: string
  step_id: string
}

export function useWorkflows() {
  return useQuery({
    queryKey: ['workflows'],
    queryFn: () => mcpCall<WorkflowSummary[]>('pipeline', { action: 'list_workflows' }),
    retry: false,
  })
}

export function useWorkflowStatus(runId: string | null) {
  return useQuery({
    queryKey: ['workflow-status', runId],
    queryFn: () => mcpCall<WorkflowStatus>('pipeline', { action: 'get_workflow_status', run_id: runId! }),
    enabled: !!runId,
    refetchInterval: 3000,
    retry: false,
  })
}

export function useRunWorkflow() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (args: RunWorkflowArgs) =>
      mcpCall<RunWorkflowResult>('pipeline', { action: 'run_workflow', ...(args as unknown as Record<string, unknown>) }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['workflows'] }),
  })
}

export function useApproveWorkflowStep() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (args: ApproveStepArgs) =>
      mcpCall<{ message: string }>('pipeline', { action: 'approve_workflow_step', ...(args as unknown as Record<string, unknown>) }),
    onSuccess: (_data, args) =>
      void qc.invalidateQueries({ queryKey: ['workflow-status', args.run_id] }),
  })
}

export function useCancelWorkflowRun() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (runId: string) =>
      mcpCall<{ message: string }>('pipeline', { action: 'cancel_workflow', run_id: runId }),
    onSuccess: (_data, runId) =>
      void qc.invalidateQueries({ queryKey: ['workflow-status', runId] }),
  })
}
