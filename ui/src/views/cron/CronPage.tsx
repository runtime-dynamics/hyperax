import { useState } from 'react'
import {
  Clock,
  PlusCircle,
  Play,
  Trash2,
  ChevronDown,
  ChevronRight,
  Loader2,
  ToggleLeft,
  ToggleRight,
} from 'lucide-react'
import {
  useCronJobs,
  useCronHistory,
  useCreateCronJob,
  useUpdateCronJob,
  useDeleteCronJob,
  useTriggerCronJob,
  type CronJobSummary,
  type CronExecution,
  type CreateCronJobArgs,
} from '@/services/cronService'
import { PageHeader } from '@/components/domain/page-header'
import { LoadingState } from '@/components/domain/loading-state'
import { ErrorState } from '@/components/domain/error-state'
import { EmptyState } from '@/components/domain/empty-state'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '@/components/ui/dialog'
import { toast } from '@/components/ui/use-toast'

// ─── Helpers ─────────────────────────────────────────────────────────────────

function statusVariant(
  status: string,
): 'default' | 'secondary' | 'destructive' | 'outline' {
  switch (status.toLowerCase()) {
    case 'success':
    case 'completed':
      return 'default'
    case 'running':
      return 'secondary'
    case 'failed':
    case 'error':
      return 'destructive'
    default:
      return 'outline'
  }
}

function formatDate(iso?: string | null): string {
  if (!iso) return '—'
  try {
    return new Date(iso).toLocaleString()
  } catch {
    return iso
  }
}

// ─── ExecutionRow ────────────────────────────────────────────────────────────

function ExecutionRow({ exec }: { exec: CronExecution }) {
  return (
    <div className="flex items-center gap-3 px-3 py-2 text-xs border rounded-md bg-muted/20">
      <Badge variant={statusVariant(exec.status)} className="text-xs capitalize shrink-0">
        {exec.status}
      </Badge>
      <span className="text-muted-foreground shrink-0">{formatDate(exec.started_at)}</span>
      {exec.completed_at && (
        <span className="text-muted-foreground shrink-0">→ {formatDate(exec.completed_at)}</span>
      )}
      {exec.error && (
        <span className="text-destructive font-mono truncate" title={exec.error}>
          {exec.error}
        </span>
      )}
    </div>
  )
}

// ─── HistoryPanel ─────────────────────────────────────────────────────────────

function HistoryPanel({ jobId }: { jobId: string }) {
  const { data: history, isLoading, error, refetch } = useCronHistory(jobId, 20)

  if (isLoading) return <LoadingState message="Loading history..." className="py-4" />
  if (error) return <ErrorState error={error as Error} onRetry={() => void refetch()} className="py-4" />

  const entries = Array.isArray(history) ? history : []

  if (entries.length === 0) {
    return <p className="text-xs text-muted-foreground italic">No executions recorded yet.</p>
  }

  return (
    <div className="space-y-1.5">
      <p className="text-xs font-medium text-muted-foreground uppercase tracking-wide">
        Recent Executions ({entries.length})
      </p>
      {entries.map((exec) => (
        <ExecutionRow key={exec.id} exec={exec} />
      ))}
    </div>
  )
}

// ─── CronJobRow ──────────────────────────────────────────────────────────────

interface CronJobRowProps {
  job: CronJobSummary
  onDelete: (id: string) => void
  onToggleEnabled: (job: CronJobSummary) => void
  onTrigger: (id: string) => void
  isTriggeringId: string | null
  isDeletingId: string | null
  isTogglingId: string | null
}

function CronJobRow({
  job,
  onDelete,
  onToggleEnabled,
  onTrigger,
  isTriggeringId,
  isDeletingId,
  isTogglingId,
}: CronJobRowProps) {
  const [expanded, setExpanded] = useState(false)

  const isTriggering = isTriggeringId === job.id
  const isDeleting = isDeletingId === job.id
  const isToggling = isTogglingId === job.id

  return (
    <div className="border rounded-lg overflow-hidden">
      <div className="flex items-center gap-2 px-4 py-3 flex-wrap sm:flex-nowrap">
        <button
          type="button"
          className="flex items-center gap-2 text-left hover:opacity-80 transition-opacity shrink-0"
          onClick={() => setExpanded((p) => !p)}
          aria-expanded={expanded}
          aria-label={`${expanded ? 'Collapse' : 'Expand'} ${job.name}`}
        >
          {expanded ? (
            <ChevronDown className="h-4 w-4 text-muted-foreground" />
          ) : (
            <ChevronRight className="h-4 w-4 text-muted-foreground" />
          )}
        </button>

        <div className="flex-1 min-w-0">
          <p className="text-sm font-medium truncate">{job.name}</p>
          <div className="flex items-center gap-2 flex-wrap mt-0.5">
            <code className="text-xs font-mono text-muted-foreground bg-muted/50 px-1.5 py-0.5 rounded">
              {job.schedule}
            </code>
            <Badge variant="outline" className="text-xs">{job.job_type}</Badge>
          </div>
        </div>

        <div className="flex items-center gap-2 text-xs text-muted-foreground shrink-0 flex-wrap">
          {job.last_status && (
            <Badge variant={statusVariant(job.last_status)} className="text-xs capitalize">
              {job.last_status}
            </Badge>
          )}
          <span className="hidden md:inline">Next: {formatDate(job.next_run_at)}</span>
        </div>

        <div className="flex items-center gap-1 shrink-0">
          <button
            type="button"
            title={job.enabled ? 'Disable job' : 'Enable job'}
            disabled={isToggling}
            className="text-muted-foreground hover:text-foreground transition-colors disabled:opacity-50"
            onClick={() => onToggleEnabled(job)}
          >
            {isToggling ? (
              <Loader2 className="h-4 w-4 animate-spin" />
            ) : job.enabled ? (
              <ToggleRight className="h-5 w-5 text-green-500" />
            ) : (
              <ToggleLeft className="h-5 w-5" />
            )}
          </button>
          <Button
            size="sm"
            variant="outline"
            className="h-7 px-2 text-xs"
            disabled={isTriggering}
            onClick={() => onTrigger(job.id)}
            title="Trigger now"
          >
            {isTriggering ? (
              <Loader2 className="h-3 w-3 animate-spin" />
            ) : (
              <>
                <Play className="h-3 w-3 mr-1" />
                Run
              </>
            )}
          </Button>
          <Button
            size="sm"
            variant="ghost"
            className="h-7 w-7 p-0 text-muted-foreground hover:text-destructive"
            disabled={isDeleting}
            onClick={() => onDelete(job.id)}
            title="Delete job"
          >
            {isDeleting ? (
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
            ) : (
              <Trash2 className="h-3.5 w-3.5" />
            )}
          </Button>
        </div>
      </div>

      {expanded && (
        <div className="border-t bg-muted/10 px-4 py-3">
          <HistoryPanel jobId={job.id} />
        </div>
      )}
    </div>
  )
}

// ─── CreateCronDialog ─────────────────────────────────────────────────────────

interface CreateCronDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onCreate: (
    args: CreateCronJobArgs,
    cb: { onSuccess: () => void; onError: (e: Error) => void },
  ) => void
  isPending: boolean
}

function CreateCronDialog({ open, onOpenChange, onCreate, isPending }: CreateCronDialogProps) {
  const [name, setName] = useState('')
  const [schedule, setSchedule] = useState('')
  const [jobType, setJobType] = useState('')
  const [target, setTarget] = useState('')
  const [nameError, setNameError] = useState('')
  const [scheduleError, setScheduleError] = useState('')
  const [jobTypeError, setJobTypeError] = useState('')
  const [targetError, setTargetError] = useState('')

  function resetForm() {
    setName('')
    setSchedule('')
    setJobType('')
    setTarget('')
    setNameError('')
    setScheduleError('')
    setJobTypeError('')
    setTargetError('')
  }

  function handleOpenChange(next: boolean) {
    if (!next) resetForm()
    onOpenChange(next)
  }

  function validate(): boolean {
    let valid = true
    if (!name.trim()) { setNameError('Name is required'); valid = false } else setNameError('')
    if (!schedule.trim()) { setScheduleError('Schedule is required'); valid = false } else setScheduleError('')
    if (!jobType.trim()) { setJobTypeError('Job type is required'); valid = false } else setJobTypeError('')
    if (!target.trim()) { setTargetError('Target is required'); valid = false } else setTargetError('')
    return valid
  }

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (!validate()) return
    onCreate(
      { name, schedule, job_type: jobType, target, enabled: true },
      {
        onSuccess: () => {
          toast({ title: 'Cron job created', description: `"${name}" has been scheduled.` })
          handleOpenChange(false)
        },
        onError: (err) =>
          toast({ title: 'Create failed', description: err.message, variant: 'destructive' }),
      },
    )
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>Create Cron Job</DialogTitle>
          <DialogDescription>
            Schedule a recurring job using a cron expression or shortcut (e.g., @hourly).
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="space-y-1.5">
            <Label htmlFor="cron-name">Name *</Label>
            <Input
              id="cron-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="Daily cleanup"
              autoFocus
            />
            {nameError && <p className="text-xs text-destructive">{nameError}</p>}
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="cron-schedule">Schedule *</Label>
            <Input
              id="cron-schedule"
              value={schedule}
              onChange={(e) => setSchedule(e.target.value)}
              placeholder="0 2 * * * or @daily"
              className="font-mono"
            />
            {scheduleError && <p className="text-xs text-destructive">{scheduleError}</p>}
            <p className="text-xs text-muted-foreground">
              5-field cron expression or shortcut: @hourly, @daily, @weekly
            </p>
          </div>

          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1.5">
              <Label htmlFor="cron-type">Job Type *</Label>
              <Input
                id="cron-type"
                value={jobType}
                onChange={(e) => setJobType(e.target.value)}
                placeholder="pipeline"
              />
              {jobTypeError && <p className="text-xs text-destructive">{jobTypeError}</p>}
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="cron-target">Target *</Label>
              <Input
                id="cron-target"
                value={target}
                onChange={(e) => setTarget(e.target.value)}
                placeholder="pipeline-id or tool-name"
              />
              {targetError && <p className="text-xs text-destructive">{targetError}</p>}
            </div>
          </div>

          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => handleOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={isPending}>
              {isPending ? (
                <>
                  <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                  Creating...
                </>
              ) : (
                'Create Job'
              )}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

// ─── CronPage ────────────────────────────────────────────────────────────────

export function CronPage() {
  const [dialogOpen, setDialogOpen] = useState(false)
  const [triggeringId, setTriggeringId] = useState<string | null>(null)
  const [deletingId, setDeletingId] = useState<string | null>(null)
  const [togglingId, setTogglingId] = useState<string | null>(null)

  const { data: jobs, isLoading, error, refetch } = useCronJobs()
  const { mutate: createJob, isPending: isCreating } = useCreateCronJob()
  const { mutate: updateJob } = useUpdateCronJob()
  const { mutate: deleteJob } = useDeleteCronJob()
  const { mutate: triggerJob } = useTriggerCronJob()

  function handleCreate(
    args: CreateCronJobArgs,
    cb: { onSuccess: () => void; onError: (e: Error) => void },
  ) {
    createJob(args, cb)
  }

  function handleTrigger(id: string) {
    setTriggeringId(id)
    triggerJob(id, {
      onSuccess: (result) =>
        toast({ title: 'Job triggered', description: result.message }),
      onError: (err) =>
        toast({ title: 'Trigger failed', description: (err as Error).message, variant: 'destructive' }),
      onSettled: () => setTriggeringId(null),
    })
  }

  function handleDelete(id: string) {
    const job = jobs?.find((j) => j.id === id)
    if (!job) return
    if (!confirm(`Delete cron job "${job.name}"? This cannot be undone.`)) return
    setDeletingId(id)
    deleteJob(id, {
      onSuccess: () => toast({ title: 'Job deleted', description: `"${job.name}" has been removed.` }),
      onError: (err) =>
        toast({ title: 'Delete failed', description: (err as Error).message, variant: 'destructive' }),
      onSettled: () => setDeletingId(null),
    })
  }

  function handleToggleEnabled(job: CronJobSummary) {
    setTogglingId(job.id)
    updateJob(
      { id: job.id, enabled: !job.enabled },
      {
        onSuccess: () =>
          toast({
            title: job.enabled ? 'Job disabled' : 'Job enabled',
            description: `"${job.name}" is now ${job.enabled ? 'disabled' : 'enabled'}.`,
          }),
        onError: (err) =>
          toast({ title: 'Update failed', description: (err as Error).message, variant: 'destructive' }),
        onSettled: () => setTogglingId(null),
      },
    )
  }

  if (isLoading)
    return (
      <div className="p-6 space-y-6">
        <PageHeader title="Cron Jobs" description="Schedule and manage recurring tasks." />
        <LoadingState message="Loading cron jobs..." />
      </div>
    )

  if (error)
    return (
      <div className="p-6 space-y-6">
        <PageHeader title="Cron Jobs" description="Schedule and manage recurring tasks." />
        <ErrorState error={error as Error} onRetry={() => void refetch()} />
      </div>
    )

  const items = Array.isArray(jobs) ? jobs : []

  return (
    <div className="p-6 space-y-6">
      <PageHeader
        title="Cron Jobs"
        description="Schedule and manage recurring tasks with execution history."
      >
        <Button size="sm" onClick={() => setDialogOpen(true)}>
          <PlusCircle className="h-4 w-4 mr-2" />
          Create Job
        </Button>
      </PageHeader>

      {items.length === 0 ? (
        <EmptyState
          icon={Clock}
          title="No cron jobs scheduled"
          description="Create a cron job to run recurring tasks on a schedule."
          action={
            <Button size="sm" onClick={() => setDialogOpen(true)}>
              Create your first job
            </Button>
          }
        />
      ) : (
        <div className="space-y-2">
          {items.map((job) => (
            <CronJobRow
              key={job.id}
              job={job}
              onDelete={handleDelete}
              onToggleEnabled={handleToggleEnabled}
              onTrigger={handleTrigger}
              isTriggeringId={triggeringId}
              isDeletingId={deletingId}
              isTogglingId={togglingId}
            />
          ))}
        </div>
      )}

      <CreateCronDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        onCreate={handleCreate}
        isPending={isCreating}
      />
    </div>
  )
}
