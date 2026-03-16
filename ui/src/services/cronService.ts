import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { mcpCall } from '@/lib/mcp-client'

export interface CronJobSummary {
  id: string
  name: string
  schedule: string
  job_type: string
  enabled: boolean
  next_run_at: string
  last_run_at: string
  last_status: string
}

export interface CronExecution {
  id: string
  job_id: string
  started_at: string
  completed_at?: string
  status: string
  error?: string
}

export interface CreateCronJobArgs {
  name: string
  schedule: string
  job_type: string
  target: string
  args?: Record<string, unknown>
  enabled?: boolean
}

export interface UpdateCronJobArgs {
  id: string
  name?: string
  schedule?: string
  job_type?: string
  target?: string
  args?: Record<string, unknown>
  enabled?: boolean
}

export function useCronJobs() {
  return useQuery({
    queryKey: ['cron-jobs'],
    queryFn: () => mcpCall<CronJobSummary[]>('pipeline', { action: 'list_cron_jobs' }),
    retry: false,
  })
}

export function useCronHistory(jobId: string | null, limit = 20) {
  return useQuery({
    queryKey: ['cron-history', jobId, limit],
    queryFn: () => mcpCall<CronExecution[]>('pipeline', { action: 'get_cron_history', id: jobId!, limit }),
    enabled: !!jobId,
    retry: false,
  })
}

export function useCreateCronJob() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (args: CreateCronJobArgs) =>
      mcpCall<{ id: string; message: string }>('pipeline', { action: 'create_cron_job', ...(args as unknown as Record<string, unknown>) }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['cron-jobs'] }),
  })
}

export function useUpdateCronJob() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (args: UpdateCronJobArgs) =>
      mcpCall<{ id: string; name: string; status: string }>('pipeline', { action: 'update_cron_job', ...(args as unknown as Record<string, unknown>) }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['cron-jobs'] }),
  })
}

export function useDeleteCronJob() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) =>
      mcpCall<{ id: string; status: string }>('pipeline', { action: 'delete_cron_job', id }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['cron-jobs'] }),
  })
}

export function useTriggerCronJob() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) =>
      mcpCall<{ id: string; message: string }>('pipeline', { action: 'trigger_cron_job', id }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['cron-jobs'] })
      void qc.invalidateQueries({ queryKey: ['cron-history'] })
    },
  })
}
