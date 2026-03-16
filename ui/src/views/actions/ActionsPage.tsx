import { useState, useEffect, useCallback } from 'react'
import { ShieldCheck, Clock, CheckCircle2, XCircle, ChevronDown, ChevronUp } from 'lucide-react'
import {
  usePendingActions,
  useActionHistory,
  useApproveAction,
  useRejectAction,
  type GuardAction,
} from '@/services/actionService'
import { InterjectionsPage } from '@/views/interjections/InterjectionsPage'
import { useActiveInterjections } from '@/services/interjectionService'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import { PageHeader } from '@/components/domain/page-header'
import { LoadingState } from '@/components/domain/loading-state'
import { EmptyState } from '@/components/domain/empty-state'
import { ErrorState } from '@/components/domain/error-state'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { Card, CardContent, CardHeader } from '@/components/ui/card'
import { toast } from '@/components/ui/use-toast'
import { cn } from '@/lib/utils'
import { useQueryClient } from '@tanstack/react-query'
import { useEventStream } from '@/hooks/useEventStream'

// ─── Helpers ──────────────────────────────────────────────────────────────────

function formatTimestamp(iso: string): string {
  try {
    return new Date(iso).toLocaleString(undefined, {
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
    })
  } catch {
    return iso
  }
}

function parseParamsPreview(raw: string): string {
  try {
    const parsed = JSON.parse(raw)
    const str = JSON.stringify(parsed, null, 0)
    return str.length > 120 ? str.slice(0, 117) + '...' : str
  } catch {
    return raw.length > 120 ? raw.slice(0, 117) + '...' : raw
  }
}

// ─── Countdown Timer ──────────────────────────────────────────────────────────

function useCountdown(expiresAt: string): { remaining: string; urgent: boolean } {
  const [now, setNow] = useState(() => Date.now())

  useEffect(() => {
    const interval = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(interval)
  }, [])

  const expiryMs = new Date(expiresAt).getTime()
  const diffMs = expiryMs - now

  if (diffMs <= 0) {
    return { remaining: 'Expired', urgent: true }
  }

  const totalSeconds = Math.floor(diffMs / 1000)
  const minutes = Math.floor(totalSeconds / 60)
  const seconds = totalSeconds % 60
  const remaining =
    minutes > 0
      ? `${minutes}m ${seconds.toString().padStart(2, '0')}s`
      : `${seconds}s`

  return { remaining, urgent: totalSeconds < 30 }
}

// ─── Status Badge ─────────────────────────────────────────────────────────────

function StatusBadge({ status }: { status: string }) {
  const config: Record<string, { label: string; className: string }> = {
    pending: {
      label: 'Pending',
      className: 'border-transparent bg-yellow-500/15 text-yellow-600 dark:text-yellow-400',
    },
    approved: {
      label: 'Approved',
      className: 'border-transparent bg-green-500/15 text-green-600 dark:text-green-400',
    },
    rejected: {
      label: 'Rejected',
      className: 'border-transparent bg-destructive/15 text-destructive',
    },
    timeout: {
      label: 'Timeout',
      className: 'border-transparent bg-muted text-muted-foreground',
    },
  }

  const { label, className } = config[status] ?? {
    label: status,
    className: 'border-transparent bg-muted text-muted-foreground',
  }

  return <Badge className={className}>{label}</Badge>
}

// ─── Countdown Display ────────────────────────────────────────────────────────

function CountdownBadge({ expiresAt }: { expiresAt: string }) {
  const { remaining, urgent } = useCountdown(expiresAt)
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1 text-xs font-medium',
        urgent ? 'text-destructive' : 'text-muted-foreground',
      )}
    >
      <Clock className="h-3 w-3 shrink-0" />
      {remaining}
    </span>
  )
}

// ─── Pending Action Card ──────────────────────────────────────────────────────

interface PendingCardProps {
  action: GuardAction
}

function PendingActionCard({ action }: PendingCardProps) {
  const [expanded, setExpanded] = useState(false)
  const [notes, setNotes] = useState('')
  const approve = useApproveAction()
  const reject = useRejectAction()

  const handleApprove = useCallback(() => {
    approve.mutate(
      { id: action.id, notes: notes.trim() || undefined },
      {
        onSuccess: () => {
          toast({ title: 'Action approved', description: `${action.tool_name} approved.` })
          setNotes('')
        },
        onError: (err) => {
          toast({
            title: 'Approval failed',
            description: err instanceof Error ? err.message : 'Unknown error',
            variant: 'destructive',
          })
        },
      },
    )
  }, [approve, action.id, action.tool_name, notes])

  const handleReject = useCallback(() => {
    reject.mutate(
      { id: action.id, notes: notes.trim() || undefined },
      {
        onSuccess: () => {
          toast({ title: 'Action rejected', description: `${action.tool_name} rejected.` })
          setNotes('')
        },
        onError: (err) => {
          toast({
            title: 'Rejection failed',
            description: err instanceof Error ? err.message : 'Unknown error',
            variant: 'destructive',
          })
        },
      },
    )
  }, [reject, action.id, action.tool_name, notes])

  const isPending = approve.isPending || reject.isPending
  const paramsPreview = parseParamsPreview(action.tool_params)

  return (
    <Card className="border-yellow-500/30">
      <CardHeader className="p-4 pb-0">
        <div className="flex items-start justify-between gap-3">
          <div className="min-w-0 flex-1 space-y-1">
            <div className="flex items-center gap-2 flex-wrap">
              <span className="font-semibold text-sm font-mono">{action.tool_name}</span>
              {action.tool_action && action.tool_action !== action.tool_name && (
                <span className="text-xs text-muted-foreground font-mono">/ {action.tool_action}</span>
              )}
              <StatusBadge status={action.status} />
            </div>
            <div className="flex items-center gap-3 text-xs text-muted-foreground flex-wrap">
              <span>
                Guard: <span className="text-foreground font-medium">{action.guard_name}</span>
              </span>
              <span>
                Caller: <span className="text-foreground font-medium">{action.caller_persona}</span>
              </span>
              {action.trace_id && (
                <span className="font-mono opacity-60">#{action.trace_id.slice(0, 8)}</span>
              )}
            </div>
          </div>
          <CountdownBadge expiresAt={action.expires_at} />
        </div>
      </CardHeader>

      <CardContent className="p-4 space-y-3">
        {/* Params preview */}
        <div className="rounded-md bg-muted/50 px-3 py-2">
          <p className="text-xs text-muted-foreground mb-1 font-medium">Parameters</p>
          <pre className="text-xs font-mono whitespace-pre-wrap break-all text-foreground leading-relaxed">
            {paramsPreview}
          </pre>
        </div>

        {/* Collapsible notes */}
        <button
          type="button"
          onClick={() => setExpanded((v) => !v)}
          className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground transition-colors"
        >
          {expanded ? (
            <ChevronUp className="h-3.5 w-3.5" />
          ) : (
            <ChevronDown className="h-3.5 w-3.5" />
          )}
          {expanded ? 'Hide notes' : 'Add notes (optional)'}
        </button>

        {expanded && (
          <Textarea
            placeholder="Notes for the audit log..."
            value={notes}
            onChange={(e) => setNotes(e.target.value)}
            className="text-sm resize-none h-20"
            disabled={isPending}
          />
        )}

        {/* Action buttons */}
        <div className="flex items-center gap-2 pt-1">
          <Button
            size="sm"
            onClick={handleApprove}
            disabled={isPending}
            className="bg-green-600 hover:bg-green-700 text-white"
          >
            <CheckCircle2 className="h-3.5 w-3.5 mr-1.5" />
            Approve
          </Button>
          <Button
            size="sm"
            variant="destructive"
            onClick={handleReject}
            disabled={isPending}
          >
            <XCircle className="h-3.5 w-3.5 mr-1.5" />
            Reject
          </Button>
        </div>
      </CardContent>
    </Card>
  )
}

// ─── History Table ────────────────────────────────────────────────────────────

function HistoryTable({ actions }: { actions: GuardAction[] }) {
  if (actions.length === 0) {
    return (
      <p className="text-sm text-muted-foreground py-4 text-center">No history yet.</p>
    )
  }

  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b text-xs text-muted-foreground">
            <th className="text-left pb-2 pr-4 font-medium">Status</th>
            <th className="text-left pb-2 pr-4 font-medium">Tool</th>
            <th className="text-left pb-2 pr-4 font-medium">Guard</th>
            <th className="text-left pb-2 pr-4 font-medium">Caller</th>
            <th className="text-left pb-2 pr-4 font-medium">Decided By</th>
            <th className="text-left pb-2 pr-4 font-medium">Time</th>
            <th className="text-left pb-2 font-medium">Notes</th>
          </tr>
        </thead>
        <tbody>
          {actions.map((action) => (
            <tr key={action.id} className="border-b last:border-0 hover:bg-muted/30 transition-colors">
              <td className="py-2 pr-4">
                <StatusBadge status={action.status} />
              </td>
              <td className="py-2 pr-4 font-mono text-xs">
                <div>{action.tool_name}</div>
                {action.tool_action && action.tool_action !== action.tool_name && (
                  <div className="text-muted-foreground">/ {action.tool_action}</div>
                )}
              </td>
              <td className="py-2 pr-4 text-xs">{action.guard_name}</td>
              <td className="py-2 pr-4 text-xs">{action.caller_persona}</td>
              <td className="py-2 pr-4 text-xs text-muted-foreground">
                {action.decided_by ?? '—'}
              </td>
              <td className="py-2 pr-4 text-xs text-muted-foreground whitespace-nowrap">
                {action.decided_at
                  ? formatTimestamp(action.decided_at)
                  : formatTimestamp(action.created_at)}
              </td>
              <td className="py-2 text-xs text-muted-foreground max-w-xs truncate">
                {action.notes ?? '—'}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

// ─── Guard Actions Content ────────────────────────────────────────────────────

function GuardActionsContent() {
  const qc = useQueryClient()
  const { events } = useEventStream()
  const pendingQuery = usePendingActions()
  const historyQuery = useActionHistory(50)

  // Invalidate guard queries on guard.* WebSocket events
  useEffect(() => {
    if (events.length === 0) return
    const latest = events[events.length - 1]
    if (latest.type.startsWith('guard.')) {
      void qc.invalidateQueries({ queryKey: ['guard'] })
    }
  }, [events, qc])

  const pendingActions = pendingQuery.data?.actions ?? []
  const historyActions = (historyQuery.data?.actions ?? []).slice().sort(
    (a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime(),
  )

  return (
    <div className="p-6 space-y-8 max-w-5xl mx-auto">
      {/* ── Pending Actions ─────────────────────────────────────── */}
      <section className="space-y-4">
        <div className="flex items-center gap-2">
          <h3 className="text-lg font-semibold">Pending Actions</h3>
          {pendingActions.length > 0 && (
            <span className="inline-flex items-center justify-center h-5 min-w-5 px-1.5 rounded-full bg-yellow-500 text-white text-xs font-bold">
              {pendingActions.length}
            </span>
          )}
        </div>

        {pendingQuery.isLoading && <LoadingState message="Loading pending actions..." />}

        {pendingQuery.isError && (
          <ErrorState
            error={
              pendingQuery.error instanceof Error
                ? pendingQuery.error
                : new Error('Failed to load pending actions.')
            }
            onRetry={() => void pendingQuery.refetch()}
          />
        )}

        {!pendingQuery.isLoading && !pendingQuery.isError && pendingActions.length === 0 && (
          <EmptyState
            icon={ShieldCheck}
            title="No pending actions"
            description="Guard policies intercept agent tool calls that require human approval. Pending requests will appear here in real time."
          />
        )}

        {pendingActions.length > 0 && (
          <div className="grid gap-4 sm:grid-cols-1 lg:grid-cols-2">
            {pendingActions.map((action) => (
              <PendingActionCard key={action.id} action={action} />
            ))}
          </div>
        )}
      </section>

      {/* ── Recent History ───────────────────────────────────────── */}
      <section className="space-y-4">
        <div className="flex items-center gap-2">
          <h3 className="text-lg font-semibold">Recent History</h3>
          {historyActions.length > 0 && (
            <span className="text-xs text-muted-foreground">({historyActions.length})</span>
          )}
        </div>

        {historyQuery.isLoading && <LoadingState message="Loading history..." />}

        {historyQuery.isError && (
          <ErrorState
            error={
              historyQuery.error instanceof Error
                ? historyQuery.error
                : new Error('Failed to load action history.')
            }
            onRetry={() => void historyQuery.refetch()}
          />
        )}

        {!historyQuery.isLoading && !historyQuery.isError && (
          <Card>
            <CardContent className="p-4">
              <HistoryTable actions={historyActions} />
            </CardContent>
          </Card>
        )}
      </section>
    </div>
  )
}

// ─── Page ─────────────────────────────────────────────────────────────────────

export function ActionsPage() {
  const pendingQuery = usePendingActions()
  const { data: activeData } = useActiveInterjections()

  const pendingCount = pendingQuery.data?.count ?? 0
  const interjectionCount = activeData?.count ?? 0

  return (
    <div className="max-w-5xl mx-auto">
      <div className="p-6 pb-0">
        <PageHeader
          title="Actions"
          description="Items requiring your attention — guard approvals, safety interjections, and system alerts."
        />
      </div>

      <Tabs defaultValue="approvals" className="flex flex-col">
        <div className="px-6 pt-2 border-b">
          <TabsList>
            <TabsTrigger value="approvals" className="gap-1.5">
              Approvals
              {pendingCount > 0 && (
                <span className="inline-flex items-center justify-center h-4 min-w-4 px-1 rounded-full bg-yellow-500 text-white text-[10px] font-bold leading-none">
                  {pendingCount > 99 ? '99+' : pendingCount}
                </span>
              )}
            </TabsTrigger>
            <TabsTrigger value="safety" className="gap-1.5">
              Safety
              {interjectionCount > 0 && (
                <span className="inline-flex items-center justify-center h-4 min-w-4 px-1 rounded-full bg-red-500 text-white text-[10px] font-bold leading-none">
                  {interjectionCount > 99 ? '99+' : interjectionCount}
                </span>
              )}
            </TabsTrigger>
          </TabsList>
        </div>
        <TabsContent value="approvals" className="mt-0">
          <GuardActionsContent />
        </TabsContent>
        <TabsContent value="safety" className="mt-0">
          <InterjectionsPage />
        </TabsContent>
      </Tabs>
    </div>
  )
}
