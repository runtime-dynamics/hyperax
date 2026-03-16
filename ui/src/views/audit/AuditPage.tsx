import { useState } from 'react'
import {
  ClipboardList,
  ChevronDown,
  ChevronRight,
  CheckCircle2,
  Loader2,
  RefreshCw,
  PlusCircle,
} from 'lucide-react'
import {
  useAudits,
  useAuditProgress,
  useAuditItems,
  useCompleteAuditItem,
  useUpdateAuditItem,
  type AuditRun,
  type AuditItem,
} from '@/services/auditService'
import { PageHeader } from '@/components/domain/page-header'
import { LoadingState } from '@/components/domain/loading-state'
import { ErrorState } from '@/components/domain/error-state'
import { EmptyState } from '@/components/domain/empty-state'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { toast } from '@/components/ui/use-toast'
import { cn } from '@/lib/utils'
import { CreateAuditDialog } from './CreateAuditDialog'

// ─── Helpers ─────────────────────────────────────────────────────────────────

function statusVariant(
  status: string,
): 'default' | 'secondary' | 'destructive' | 'outline' {
  switch (status.toLowerCase()) {
    case 'completed':
    case 'passed':
      return 'default'
    case 'in_progress':
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

// ─── ProgressBar ─────────────────────────────────────────────────────────────

function ProgressBar({ percent }: { percent: number }) {
  const clamped = Math.min(100, Math.max(0, percent))
  return (
    <div className="w-full bg-muted rounded-full h-2 overflow-hidden">
      <div
        className={cn(
          'h-2 rounded-full transition-all',
          clamped >= 100 ? 'bg-green-500' : 'bg-primary',
        )}
        style={{ width: `${clamped}%` }}
        role="progressbar"
        aria-valuenow={clamped}
        aria-valuemin={0}
        aria-valuemax={100}
      />
    </div>
  )
}

// ─── AuditItemRow ─────────────────────────────────────────────────────────────

interface AuditItemRowProps {
  item: AuditItem
  onComplete: (id: string) => void
  isCompletingId: string | null
}

function AuditItemRow({ item, onComplete, isCompletingId }: AuditItemRowProps) {
  const isCompleting = isCompletingId === item.id
  const isDone = item.status.toLowerCase() === 'completed' || item.status.toLowerCase() === 'passed'

  return (
    <div
      className={cn(
        'flex items-start gap-3 px-3 py-2.5 border rounded-md text-sm',
        isDone ? 'bg-muted/20 opacity-60' : '',
      )}
    >
      <div className="flex-1 min-w-0">
        <p className={cn('font-medium truncate', isDone && 'line-through text-muted-foreground')}>
          {item.title}
        </p>
        {item.description && (
          <p className="text-xs text-muted-foreground mt-0.5 line-clamp-2">{item.description}</p>
        )}
        {item.notes && (
          <p className="text-xs text-muted-foreground italic mt-0.5">Note: {item.notes}</p>
        )}
      </div>
      <Badge variant={statusVariant(item.status)} className="text-xs capitalize shrink-0">
        {item.status}
      </Badge>
      {!isDone && (
        <Button
          size="sm"
          variant="outline"
          className="h-7 px-2 text-xs shrink-0"
          disabled={isCompleting}
          onClick={() => onComplete(item.id)}
          title="Mark complete"
        >
          {isCompleting ? (
            <Loader2 className="h-3 w-3 animate-spin" />
          ) : (
            <>
              <CheckCircle2 className="h-3 w-3 mr-1" />
              Done
            </>
          )}
        </Button>
      )}
    </div>
  )
}

// ─── AuditItemsPanel ─────────────────────────────────────────────────────────

function AuditItemsPanel({ auditId }: { auditId: string }) {
  const [completingId, setCompletingId] = useState<string | null>(null)

  const { data: items, isLoading, error, refetch } = useAuditItems(auditId)
  const { data: progress } = useAuditProgress(auditId)
  const { mutate: completeItem } = useCompleteAuditItem()
  const { mutate: updateItem } = useUpdateAuditItem()

  function handleComplete(itemId: string) {
    setCompletingId(itemId)
    completeItem(
      { item_id: itemId },
      {
        onSuccess: () => toast({ title: 'Item completed' }),
        onError: (err) => {
          // Fall back to update_audit_item if complete_audit_item fails
          updateItem(
            { item_id: itemId, status: 'completed' },
            {
              onSuccess: () => toast({ title: 'Item marked complete' }),
              onError: (e2) =>
                toast({ title: 'Failed to complete', description: (e2 as Error).message, variant: 'destructive' }),
              onSettled: () => setCompletingId(null),
            },
          )
          void err
          return
        },
        onSettled: () => setCompletingId(null),
      },
    )
  }

  if (isLoading) return <LoadingState message="Loading items..." className="py-4" />
  if (error) return <ErrorState error={error as Error} onRetry={() => void refetch()} className="py-4" />

  const entries = Array.isArray(items) ? items : []

  return (
    <div className="space-y-3">
      {progress && (
        <div className="space-y-1">
          <div className="flex items-center justify-between text-xs text-muted-foreground">
            <span>
              {progress.completed} / {progress.total} complete
            </span>
            <span>{Math.round(progress.percent)}%</span>
          </div>
          <ProgressBar percent={progress.percent} />
        </div>
      )}
      {entries.length === 0 ? (
        <p className="text-xs text-muted-foreground italic">No items in this audit.</p>
      ) : (
        <div className="space-y-1.5">
          {entries.map((item) => (
            <AuditItemRow
              key={item.id}
              item={item}
              onComplete={handleComplete}
              isCompletingId={completingId}
            />
          ))}
        </div>
      )}
    </div>
  )
}

// ─── AuditRunRow ─────────────────────────────────────────────────────────────

function AuditRunRow({ run }: { run: AuditRun }) {
  const [expanded, setExpanded] = useState(false)

  const percentDone =
    run.total_items > 0 ? Math.round((run.completed_items / run.total_items) * 100) : 0

  return (
    <div className="border rounded-lg overflow-hidden">
      <div className="flex items-center gap-2 px-4 py-3 flex-wrap sm:flex-nowrap">
        <button
          type="button"
          className="flex items-center gap-2 text-left hover:opacity-80 transition-opacity shrink-0"
          onClick={() => setExpanded((p) => !p)}
          aria-expanded={expanded}
          aria-label={`${expanded ? 'Collapse' : 'Expand'} ${run.name}`}
        >
          {expanded ? (
            <ChevronDown className="h-4 w-4 text-muted-foreground" />
          ) : (
            <ChevronRight className="h-4 w-4 text-muted-foreground" />
          )}
        </button>

        <div className="flex-1 min-w-0">
          <p className="text-sm font-medium truncate">{run.name}</p>
          <div className="flex items-center gap-2 mt-0.5 text-xs text-muted-foreground">
            <span>Created {formatDate(run.created_at)}</span>
            {run.workspace && <Badge variant="outline" className="text-xs">{run.workspace}</Badge>}
          </div>
        </div>

        <div className="flex items-center gap-3 shrink-0">
          <div className="text-xs text-muted-foreground text-right hidden sm:block">
            <span className="tabular-nums">
              {run.completed_items}/{run.total_items}
            </span>
            <span className="ml-1 text-muted-foreground/70">items</span>
          </div>
          <div className="w-20 hidden md:block">
            <ProgressBar percent={percentDone} />
          </div>
          <Badge variant={statusVariant(run.status)} className="text-xs capitalize">
            {run.status}
          </Badge>
        </div>
      </div>

      {expanded && (
        <div className="border-t bg-muted/10 px-4 py-3">
          <AuditItemsPanel auditId={run.id} />
        </div>
      )}
    </div>
  )
}

// ─── AuditPage ────────────────────────────────────────────────────────────────

export function AuditPage() {
  const [showCreate, setShowCreate] = useState(false)
  const { data: audits, isLoading, error, refetch } = useAudits()

  if (isLoading)
    return (
      <div className="p-6 space-y-6">
        <PageHeader title="Audit Dashboard" description="Review and complete structured audit runs." />
        <LoadingState message="Loading audits..." />
      </div>
    )

  if (error)
    return (
      <div className="p-6 space-y-6">
        <PageHeader title="Audit Dashboard" description="Review and complete structured audit runs." />
        <ErrorState error={error as Error} onRetry={() => void refetch()} />
      </div>
    )

  const items = Array.isArray(audits) ? audits : []

  return (
    <div className="p-6 space-y-6">
      <PageHeader
        title="Audit Dashboard"
        description="Review and complete structured audit runs with item-level progress tracking."
      >
        <Button size="sm" onClick={() => setShowCreate(true)}>
          <PlusCircle className="h-4 w-4 mr-2" />
          Create Audit
        </Button>
        <Button size="sm" variant="outline" onClick={() => void refetch()}>
          <RefreshCw className="h-4 w-4 mr-2" />
          Refresh
        </Button>
      </PageHeader>

      <CreateAuditDialog open={showCreate} onOpenChange={setShowCreate} />

      {items.length === 0 ? (
        <EmptyState
          icon={ClipboardList}
          title="No audit runs found"
          description="Audit runs will appear here once they have been created by the system."
        />
      ) : (
        <div className="space-y-2">
          {items.map((run) => (
            <AuditRunRow key={run.id} run={run} />
          ))}
        </div>
      )}
    </div>
  )
}
