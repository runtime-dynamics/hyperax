import { useState } from 'react'
import { Activity, ChevronDown, ChevronRight, RefreshCw } from 'lucide-react'
import {
  useRuntimeStates,
  useRuntimeState,
  type RuntimeStateSummary,
} from '@/services/lifecycleService'
import { PageHeader } from '@/components/domain/page-header'
import { LoadingState } from '@/components/domain/loading-state'
import { ErrorState } from '@/components/domain/error-state'
import { EmptyState } from '@/components/domain/empty-state'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'

// ─── Helpers ─────────────────────────────────────────────────────────────────

function statusVariant(
  status: string,
): 'default' | 'secondary' | 'destructive' | 'outline' {
  switch (status.toLowerCase()) {
    case 'running':
    case 'active':
      return 'default'
    case 'idle':
    case 'waiting':
      return 'secondary'
    case 'error':
    case 'failed':
    case 'crashed':
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

// ─── AgentDetailPanel ────────────────────────────────────────────────────────

function AgentDetailPanel({ agentId }: { agentId: string }) {
  const { data: detail, isLoading, error, refetch } = useRuntimeState(agentId)

  if (isLoading) return <LoadingState message="Loading agent state..." className="py-4" />
  if (error) return <ErrorState error={error as Error} onRetry={() => void refetch()} className="py-4" />
  if (!detail) return <p className="text-xs text-muted-foreground italic">No detail available.</p>

  const sections: { label: string; data: Record<string, unknown> | undefined }[] = [
    { label: 'Config', data: detail.config },
    { label: 'Metrics', data: detail.metrics },
    { label: 'Metadata', data: detail.metadata },
  ]

  return (
    <div className="space-y-3 text-xs">
      <div className="grid grid-cols-2 sm:grid-cols-3 gap-x-6 gap-y-1 text-muted-foreground">
        <div>
          <span className="font-medium text-foreground">Workspace: </span>
          {detail.workspace ?? '—'}
        </div>
        <div>
          <span className="font-medium text-foreground">Tool Calls: </span>
          {detail.tool_calls?.toLocaleString() ?? '—'}
        </div>
        <div>
          <span className="font-medium text-foreground">Started: </span>
          {formatDate(detail.started_at)}
        </div>
        <div>
          <span className="font-medium text-foreground">Last Active: </span>
          {formatDate(detail.last_active_at)}
        </div>
        {detail.error && (
          <div className="col-span-full text-destructive">
            <span className="font-medium">Error: </span>
            {detail.error}
          </div>
        )}
      </div>

      {sections.map(
        ({ label, data }) =>
          data &&
          Object.keys(data).length > 0 && (
            <div key={label}>
              <p className="text-xs font-medium text-muted-foreground uppercase tracking-wide mb-1">
                {label}
              </p>
              <pre className="text-xs font-mono bg-muted/30 rounded p-2 overflow-auto whitespace-pre-wrap break-all">
                {JSON.stringify(data, null, 2)}
              </pre>
            </div>
          ),
      )}
    </div>
  )
}

// ─── AgentStateRow ────────────────────────────────────────────────────────────

function AgentStateRow({ agent }: { agent: RuntimeStateSummary }) {
  const [expanded, setExpanded] = useState(false)

  return (
    <div className="border rounded-lg overflow-hidden">
      <div className="flex items-center gap-2 px-4 py-3 flex-wrap sm:flex-nowrap">
        <button
          type="button"
          className="flex items-center gap-2 text-left hover:opacity-80 transition-opacity shrink-0"
          onClick={() => setExpanded((p) => !p)}
          aria-expanded={expanded}
          aria-label={`${expanded ? 'Collapse' : 'Expand'} ${agent.name ?? agent.agent_id}`}
        >
          {expanded ? (
            <ChevronDown className="h-4 w-4 text-muted-foreground" />
          ) : (
            <ChevronRight className="h-4 w-4 text-muted-foreground" />
          )}
        </button>

        <div className="flex-1 min-w-0">
          <p className="text-sm font-medium truncate">{agent.name ?? agent.agent_id}</p>
          <div className="flex items-center gap-2 mt-0.5 text-xs text-muted-foreground flex-wrap">
            <code className="font-mono bg-muted/50 px-1 py-0.5 rounded">{agent.agent_id}</code>
            {agent.workspace && (
              <Badge variant="outline" className="text-xs">{agent.workspace}</Badge>
            )}
          </div>
        </div>

        <div className="flex items-center gap-3 shrink-0 flex-wrap">
          {agent.tool_calls !== undefined && (
            <span className="text-xs text-muted-foreground hidden md:inline">
              {agent.tool_calls.toLocaleString()} calls
            </span>
          )}
          <span className="text-xs text-muted-foreground hidden sm:inline">
            {formatDate(agent.last_active_at)}
          </span>
          <Badge variant={statusVariant(agent.status)} className="text-xs capitalize">
            {agent.status}
          </Badge>
        </div>
      </div>

      {expanded && (
        <div className={cn('border-t bg-muted/10 px-4 py-3')}>
          <AgentDetailPanel agentId={agent.agent_id} />
        </div>
      )}
    </div>
  )
}

// ─── LifecyclePage ────────────────────────────────────────────────────────────

export function LifecyclePage() {
  const { data: agents, isLoading, error, refetch } = useRuntimeStates()

  if (isLoading)
    return (
      <div className="p-6 space-y-6">
        <PageHeader title="Agent Lifecycle" description="Monitor agent runtime states and execution contexts." />
        <LoadingState message="Loading runtime states..." />
      </div>
    )

  if (error)
    return (
      <div className="p-6 space-y-6">
        <PageHeader title="Agent Lifecycle" description="Monitor agent runtime states and execution contexts." />
        <ErrorState error={error as Error} onRetry={() => void refetch()} />
      </div>
    )

  const items = Array.isArray(agents) ? agents : []

  const statusCounts = items.reduce<Record<string, number>>((acc, a) => {
    const s = a.status.toLowerCase()
    acc[s] = (acc[s] ?? 0) + 1
    return acc
  }, {})

  return (
    <div className="p-6 space-y-6">
      <PageHeader
        title="Agent Lifecycle"
        description="Monitor real-time runtime states for all registered agents."
      >
        <Button size="sm" variant="outline" onClick={() => void refetch()}>
          <RefreshCw className="h-4 w-4 mr-2" />
          Refresh
        </Button>
      </PageHeader>

      {items.length > 0 && (
        <div className="flex items-center gap-2 flex-wrap">
          {Object.entries(statusCounts).map(([status, count]) => (
            <Badge key={status} variant={statusVariant(status)} className="text-xs capitalize">
              {count} {status}
            </Badge>
          ))}
        </div>
      )}

      {items.length === 0 ? (
        <EmptyState
          icon={Activity}
          title="No active agents"
          description="Runtime states will appear here as agents register and begin operating."
        />
      ) : (
        <div className="space-y-2">
          {items.map((agent) => (
            <AgentStateRow key={agent.agent_id} agent={agent} />
          ))}
        </div>
      )}
    </div>
  )
}
