import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { mcpCall } from '@/lib/mcp-client'

// ─── Types ────────────────────────────────────────────────────────────────────

export interface Pipeline {
  id: string
  name: string
  description: string
  workspace_name: string
  project_name: string
  swimlanes: string
  setup_commands: string
  environment: string
  created_at: string
  updated_at: string
}

export interface PipelineJob {
  id: string
  pipeline_id: string
  status: string
  workspace_name: string
  started_at: string | null
  completed_at: string | null
  error: string
  result: string
}

export interface StepResult {
  id: string
  job_id: string
  swimlane_id: string
  step_id: string
  step_name: string
  status: string
  exit_code: number | null
  started_at: string | null
  completed_at: string | null
  duration_ms: number | null
  output_log: string
  error: string
}

// ─── Hooks ────────────────────────────────────────────────────────────────────

export function usePipelines(workspace = 'hyperax') {
  const ws = workspace || 'hyperax'
  return useQuery({
    queryKey: ['pipelines', ws],
    queryFn: async () => {
      const result = await mcpCall<Pipeline[]>('pipeline', { action: 'list_pipelines', workspace_name: ws })
      return Array.isArray(result) ? result : []
    },
    retry: false,
  })
}

export function usePipeline(id: string) {
  return useQuery({
    queryKey: ['pipelines', id],
    queryFn: () => mcpCall<Pipeline>('pipeline', { action: 'get_pipeline', pipeline_id: id }),
    enabled: !!id,
    retry: false,
  })
}

export function usePipelineJobs(pipelineId: string, status = '', limit = 10) {
  return useQuery({
    queryKey: ['pipeline-jobs', pipelineId, status, limit],
    queryFn: async () => {
      const args: Record<string, unknown> = { pipeline_id: pipelineId }
      if (status) args.status = status
      if (limit) args.limit = limit
      const result = await mcpCall<PipelineJob[]>('pipeline', { action: 'list_pipeline_jobs', ...args })
      return Array.isArray(result) ? result : []
    },
    enabled: !!pipelineId,
    retry: false,
  })
}

export function usePipelineJobStatus(pipelineId: string, jobId: string) {
  return useQuery({
    queryKey: ['pipeline-job-status', pipelineId, jobId],
    queryFn: () =>
      mcpCall<PipelineJob>('pipeline', { action: 'pipeline_job_status', pipeline_id: pipelineId, job_id: jobId }),
    enabled: !!pipelineId && !!jobId,
    retry: false,
  })
}

export function useStepResults(pipelineId: string, jobId: string) {
  return useQuery({
    queryKey: ['step-results', pipelineId, jobId],
    queryFn: async () => {
      const result = await mcpCall<StepResult[]>('pipeline', {
        action: 'pipeline_job_log',
        pipeline_id: pipelineId,
        job_id: jobId,
      })
      return Array.isArray(result) ? result : []
    },
    enabled: !!pipelineId && !!jobId,
    retry: false,
  })
}

export function useRunPipeline() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (pipelineId: string) =>
      mcpCall<{ job_id: string; message: string }>('pipeline', { action: 'run_pipeline', pipeline_id: pipelineId }),
    onSuccess: (_data, pipelineId) => {
      void qc.invalidateQueries({ queryKey: ['pipeline-jobs', pipelineId] })
    },
  })
}

export function useCancelPipelineJob() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ pipelineId, jobId }: { pipelineId: string; jobId: string }) =>
      mcpCall<{ message: string }>('pipeline', {
        action: 'cancel_pipeline_job',
        pipeline_id: pipelineId,
        job_id: jobId,
      }),
    onSuccess: (_data, { pipelineId, jobId }) => {
      void qc.invalidateQueries({ queryKey: ['pipeline-jobs', pipelineId] })
      void qc.invalidateQueries({ queryKey: ['pipeline-job-status', pipelineId, jobId] })
    },
  })
}
