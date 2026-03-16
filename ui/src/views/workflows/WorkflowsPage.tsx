import { useState } from 'react'
import {
  GitMerge,
  Play,
  CheckCircle,
  ChevronDown,
  ChevronRight,
  Clock,
  AlertCircle,
  Loader2,
} from 'lucide-react'
import {
  useWorkflows,
  useWorkflowStatus,
  useRunWorkflow,
  useApproveWorkflowStep,
  useCancelWorkflowRun,
  type WorkflowSummary,
  type StepStatus,
} from '@/services/workflowService'
import { PageHeader } from '@/components/domain/page-header'
import { LoadingState } from '@/components/domain/loading-state'
import { ErrorState } from '@/components/domain/error-state'
import { EmptyState } from '@/components/domain/empty-state'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { toast } from '@/components/ui/use-toast'
import { cn } from '@/lib/utils'

// ─── Helpers ─────────────────────────────────────────────────────────────────

function statusVariant(
  status: string,
): 'default' | 'secondary' | 'destructive' | 'outline' {
  switch (status.toLowerCase()) {
    case 'completed':
    case 'success':
      return 'default'
    case 'running':
    case 'pending':
      return 'secondary'
    case 'failed':
    case 'error':
    case 'cancelled':
      return 'destructive'
    default:
      return 'outline'
  }
}

function formatDate(iso?: string): string {
  if (!iso) return '—'
  return new Date(iso).toLocaleString()
}

function StepIcon({ status }: { status: string }) {
  switch (status.toLowerCase()) {
    case 'completed':
    case 'success':
      return <CheckCircle className="h-3.5 w-3.5 text-green-500 shrink-0" />
    case 'running':
      return <Loader2 className="h-3.5 w-3.5 text-blue-500 animate-spin shrink-0" />
    case 'failed':
    case 'error':
      return <AlertCircle className="h-3.5 w-3.5 text-destructive shrink-0" />
    case 'pending':
    case 'waiting_approval':
      return <Clock className="h-3.5 w-3.5 text-yellow-500 shrink-0" />
    default:
      return <Clock className="h-3.5 w-3.5 text-muted-foreground shrink-0" />
  }
}

// ─── StepRow ─────────────────────────────────────────────────────────────────

interface StepRowProps {
  step: StepStatus
  runId: string
  onApprove: (runId: string, stepId: string) => void
  isApproving: boolean
}

function StepRow({ step, runId, onApprove, isApproving }: StepRowProps) {
  const needsApproval = step.status.toLowerCase() === 'waiting_approval'

  return (
    <div
      className={cn(
        'flex items-center gap-3 px-3 py-2 rounded-md text-sm',
        needsApproval ? 'bg-yellow-500/10 border border-yellow-500/30' : 'bg-muted/30',
      )}
    >
      <StepIcon status={step.status} />
      <span className="flex-1 font-medium">{step.name}</span>
      <Badge variant={statusVariant(step.status)} className="capitalize text-xs">
        {step.status.replace(/_/g, ' ')}
      </Badge>
      {step.started_at && (
        <span className="text-xs text-muted-foreground hidden sm:inline">
          {formatDate(step.started_at)}
        </span>
      )}
      {step.error && (
        <span className="text-xs text-destructive font-mono truncate max-w-[200px]" title={step.error}>
          {step.error}
        </span>
      )}
      {needsApproval && (
        <Button
          size="sm"
          variant="outline"
          className="h-6 px-2 text-xs border-yellow-500/50 text-yellow-600 hover:bg-yellow-500/10"
          disabled={isApproving}
          onClick={() => onApprove(runId, step.id)}
        >
          {isApproving ? (
            <Loader2 className="h-3 w-3 animate-spin" />
          ) : (
            'Approve'
          )}
        </Button>
      )}
    </div>
  )
}

// ─── RunStatusPanel ───────────────────────────────────────────────────────────

interface RunStatusPanelProps {
  runId: string
  onClose: () => void
}

function RunStatusPanel({ runId, onClose }: RunStatusPanelProps) {
  const { data: status, isLoading, error } = useWorkflowStatus(runId)
  const { mutate: approveStep, isPending: isApproving } = useApproveWorkflowStep()
  const { mutate: cancelRun, isPending: isCancelling } = useCancelWorkflowRun()

  function handleApprove(rid: string, stepId: string) {
    approveStep(
      { run_id: rid, step_id: stepId },
      {
        onSuccess: () => toast({ title: 'Step approved' }),
        onError: (err) =>
          toast({ title: 'Approval failed', description: (err as Error).message, variant: 'destructive' }),
      },
    )
  }

  function handleCancel() {
    cancelRun(runId, {
      onSuccess: () => {
        toast({ title: 'Run cancelled' })
        onClose()
      },
      onError: (err) =>
        toast({ title: 'Cancel failed', description: (err as Error).message, variant: 'destructive' }),
    })
  }

  return (
    <div className="border-t bg-muted/10 px-4 py-3 space-y-3">
      <div className="flex items-center justify-between gap-2">
        <div className="text-xs text-muted-foreground font-mono">
          Run: <span className="text-foreground">{runId.slice(0, 16)}...</span>
        </div>
        <div className="flex items-center gap-2">
          {status &&
            ['running', 'pending'].includes(status.status.toLowerCase()) && (
              <Button
                size="sm"
                variant="destructive"
                className="h-6 px-2 text-xs"
                disabled={isCancelling}
                onClick={handleCancel}
              >
                {isCancelling ? <Loader2 className="h-3 w-3 animate-spin" /> : 'Cancel'}
              </Button>
            )}
          <Button size="sm" variant="ghost" className="h-6 px-2 text-xs" onClick={onClose}>
            Dismiss
          </Button>
        </div>
      </div>

      {isLoading && <LoadingState message="Loading run status..." className="py-4" />}
      {error && (
        <ErrorState error={error as Error} className="py-4" />
      )}

      {status && (
        <>
          <div className="flex items-center gap-2">
            <span className="text-xs text-muted-foreground">Status:</span>
            <Badge variant={statusVariant(status.status)} className="capitalize text-xs">
              {status.status}
            </Badge>
          </div>
          {status.error && (
            <p className="text-xs text-destructive font-mono">{status.error}</p>
          )}
          {status.steps && status.steps.length > 0 && (
            <div className="space-y-1.5">
              <p className="text-xs font-medium text-muted-foreground uppercase tracking-wide">
                Steps ({status.steps.length})
              </p>
              {status.steps.map((step) => (
                <StepRow
                  key={step.id}
                  step={step}
                  runId={runId}
                  onApprove={handleApprove}
                  isApproving={isApproving}
                />
              ))}
            </div>
          )}
          {(!status.steps || status.steps.length === 0) && (
            <p className="text-xs text-muted-foreground italic">No step data available.</p>
          )}
        </>
      )}
    </div>
  )
}

// ─── WorkflowRow ─────────────────────────────────────────────────────────────

interface WorkflowRowProps {
  workflow: WorkflowSummary
}

function WorkflowRow({ workflow }: WorkflowRowProps) {
  const [expanded, setExpanded] = useState(false)
  const [activeRunId, setActiveRunId] = useState<string | null>(null)
  const { mutate: runWorkflow, isPending: isRunning } = useRunWorkflow()

  function handleRun() {
    runWorkflow(
      { workflow_id: workflow.id },
      {
        onSuccess: (result) => {
          toast({
            title: 'Workflow started',
            description: result.message,
          })
          setActiveRunId(result.run_id)
          setExpanded(true)
        },
        onError: (err) =>
          toast({
            title: 'Failed to run workflow',
            description: (err as Error).message,
            variant: 'destructive',
          }),
      },
    )
  }

  return (
    <div className="border rounded-lg overflow-hidden">
      <div className="flex items-center gap-3 px-4 py-3">
        <button
          type="button"
          className="flex items-center gap-3 flex-1 min-w-0 text-left hover:opacity-80 transition-opacity"
          onClick={() => setExpanded((p) => !p)}
        >
          {expanded ? (
            <ChevronDown className="h-4 w-4 shrink-0 text-muted-foreground" />
          ) : (
            <ChevronRight className="h-4 w-4 shrink-0 text-muted-foreground" />
          )}
          <div className="flex-1 min-w-0">
            <p className="text-sm font-medium truncate">{workflow.name}</p>
            {workflow.description && (
              <p className="text-xs text-muted-foreground truncate">{workflow.description}</p>
            )}
          </div>
        </button>
        <Badge
          variant={workflow.enabled ? 'default' : 'secondary'}
          className="text-xs shrink-0"
        >
          {workflow.enabled ? 'Enabled' : 'Disabled'}
        </Badge>
        <Button
          size="sm"
          variant="outline"
          className="h-7 px-2.5 text-xs shrink-0"
          disabled={!workflow.enabled || isRunning}
          onClick={handleRun}
        >
          {isRunning ? (
            <Loader2 className="h-3 w-3 animate-spin" />
          ) : (
            <>
              <Play className="h-3 w-3 mr-1" />
              Run
            </>
          )}
        </Button>
      </div>

      {expanded && activeRunId && (
        <RunStatusPanel
          runId={activeRunId}
          onClose={() => {
            setActiveRunId(null)
            setExpanded(false)
          }}
        />
      )}
      {expanded && !activeRunId && (
        <div className="border-t bg-muted/10 px-4 py-3">
          <p className="text-xs text-muted-foreground italic">
            Click "Run" to start this workflow and view step progress here.
          </p>
        </div>
      )}
    </div>
  )
}

// ─── WorkflowsPage ───────────────────────────────────────────────────────────

export function WorkflowsPage() {
  const { data: workflows, isLoading, error, refetch } = useWorkflows()

  if (isLoading)
    return (
      <div className="p-6 space-y-6">
        <PageHeader title="Workflows" description="Manage and run automated workflows with step tracking and approvals." />
        <LoadingState message="Loading workflows..." />
      </div>
    )

  if (error)
    return (
      <div className="p-6 space-y-6">
        <PageHeader title="Workflows" description="Manage and run automated workflows with step tracking and approvals." />
        <ErrorState error={error as Error} onRetry={() => void refetch()} />
      </div>
    )

  const items = Array.isArray(workflows) ? workflows : []

  return (
    <div className="p-6 space-y-6">
      <PageHeader
        title="Workflows"
        description="Manage and run automated workflows with step tracking and approvals."
      />

      {items.length === 0 ? (
        <EmptyState
          icon={GitMerge}
          title="No workflows defined"
          description="Workflows are configured in the backend. Once registered, they will appear here."
        />
      ) : (
        <div className="space-y-2">
          {items.map((workflow) => (
            <WorkflowRow key={workflow.id} workflow={workflow} />
          ))}
        </div>
      )}
    </div>
  )
}
