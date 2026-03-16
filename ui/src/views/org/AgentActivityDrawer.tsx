import { useEffect, useRef } from 'react'
import { useQuery } from '@tanstack/react-query'
import { X, Zap, Wrench, CheckCircle2, MessageSquare, RotateCcw } from 'lucide-react'
import { mcpCall } from '@/lib/mcp-client'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'
import { getFsmState, fsmStyles } from './fsm'
import type { ActivityEntry, ActivityKind, AgentActivityState } from '@/hooks/useAgentActivity'

// ─── Agent detail fetch ───────────────────────────────────────────────────────

interface AgentRecord {
  id: string
  name: string
  status: string
  provider_id?: string
  default_model?: string
  role_template_id?: string
  clearance_level?: number
  workspace_id?: string
}

interface AgentStateRecord {
  agent_id: string
  status: string
  last_active_at?: string
  tool_calls?: number
  started_at?: string
}

function useAgentDetail(agentId: string) {
  return useQuery({
    queryKey: ['agent-detail', agentId],
    queryFn: async () => {
      const result = await mcpCall<{ agent: AgentRecord }>('get_agent', { agent_id: agentId })
      return result?.agent ?? null
    },
    enabled: !!agentId,
    staleTime: 10_000,
    retry: false,
  })
}

function useAgentState(agentId: string) {
  return useQuery({
    queryKey: ['agent-states-org', agentId],
    queryFn: async () => {
      const result = await mcpCall<AgentStateRecord>('get_agent_state', { agent_id: agentId })
      return result ?? null
    },
    enabled: !!agentId,
    staleTime: 5_000,
    retry: false,
  })
}

// ─── Activity entry display ───────────────────────────────────────────────────

const kindConfig: Record<ActivityKind, { icon: React.ReactNode; colorClass: string; label: string }> = {
  tool_dispatch: {
    icon: <Wrench className="h-3 w-3" />,
    colorClass: 'text-blue-600 dark:text-blue-400',
    label: 'Dispatch',
  },
  tool_iteration: {
    icon: <RotateCcw className="h-3 w-3" />,
    colorClass: 'text-amber-600 dark:text-amber-400',
    label: 'Iteration',
  },
  loop_complete: {
    icon: <CheckCircle2 className="h-3 w-3" />,
    colorClass: 'text-green-600 dark:text-green-400',
    label: 'Complete',
  },
  chat_started: {
    icon: <MessageSquare className="h-3 w-3" />,
    colorClass: 'text-purple-600 dark:text-purple-400',
    label: 'Started',
  },
  chat_completed: {
    icon: <CheckCircle2 className="h-3 w-3" />,
    colorClass: 'text-green-600 dark:text-green-400',
    label: 'Done',
  },
}

function formatTime(iso: string): string {
  try {
    return new Date(iso).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })
  } catch {
    return iso
  }
}

function ActivityRow({ entry }: { entry: ActivityEntry }) {
  const cfg = kindConfig[entry.kind]
  return (
    <div className="flex items-start gap-2 py-1.5 border-b border-border/40 last:border-0">
      <span className={cn('mt-0.5 shrink-0', cfg.colorClass)}>{cfg.icon}</span>
      <div className="flex-1 min-w-0">
        <p className="text-xs text-foreground truncate">{entry.label}</p>
        {entry.toolName && (
          <p className="text-[10px] font-mono text-muted-foreground truncate">{entry.toolName}</p>
        )}
        {entry.durationMs !== undefined && (
          <p className="text-[10px] text-muted-foreground">{entry.durationMs}ms</p>
        )}
      </div>
      <span className="text-[10px] text-muted-foreground tabular-nums shrink-0">{formatTime(entry.timestamp)}</span>
    </div>
  )
}

// ─── Drawer ───────────────────────────────────────────────────────────────────

interface AgentActivityDrawerProps {
  agentId: string
  open: boolean
  onClose: () => void
  /** Activity state from parent's useAgentActivity — avoids extra WebSocket connections */
  activity: AgentActivityState
}

export function AgentActivityDrawer({ agentId, open, onClose, activity }: AgentActivityDrawerProps) {
  const { data: agent, isLoading: agentLoading } = useAgentDetail(agentId)
  const { data: agentState } = useAgentState(agentId)
  const { entries, currentTool, isActive, iterationCount } = activity

  const feedRef = useRef<HTMLDivElement>(null)

  // Auto-scroll feed to bottom when new entries arrive
  useEffect(() => {
    if (feedRef.current) {
      feedRef.current.scrollTop = feedRef.current.scrollHeight
    }
  }, [entries.length])

  // Close on Escape
  useEffect(() => {
    if (!open) return
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [open, onClose])

  const status = agentState?.status ?? agent?.status ?? 'unknown'
  const state = getFsmState(status)
  const styles = fsmStyles[state]

  const displayName = agent?.name ?? agentId

  return (
    <>
      {/* Backdrop */}
      <div
        className={cn(
          'fixed inset-0 z-40 bg-black/30 transition-opacity duration-200',
          open ? 'opacity-100 pointer-events-auto' : 'opacity-0 pointer-events-none',
        )}
        onClick={onClose}
        aria-hidden="true"
      />

      {/* Slide-out panel */}
      <div
        role="dialog"
        aria-modal="true"
        aria-label={`Activity for ${displayName}`}
        className={cn(
          'fixed right-0 top-0 h-full w-80 z-50 bg-card border-l border-border shadow-xl flex flex-col',
          'transition-transform duration-200 ease-in-out',
          open ? 'translate-x-0' : 'translate-x-full',
        )}
      >
        {/* Header */}
        <div className="flex items-center justify-between px-4 py-3 border-b">
          <div className="flex-1 min-w-0">
            {agentLoading ? (
              <div className="h-4 w-32 bg-muted/60 animate-pulse rounded" />
            ) : (
              <h2 className="text-sm font-semibold truncate">{displayName}</h2>
            )}
            <code className="text-[10px] font-mono text-muted-foreground truncate block">{agentId}</code>
          </div>
          <Button size="sm" variant="ghost" className="h-7 w-7 p-0 shrink-0" onClick={onClose} aria-label="Close activity drawer">
            <X className="h-4 w-4" />
          </Button>
        </div>

        {/* Status strip */}
        <div className="px-4 py-2.5 border-b space-y-2">
          <div className="flex items-center gap-2 flex-wrap">
            <span
              className={cn(
                'inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium capitalize',
                styles.badge,
              )}
            >
              <span className={cn('h-1.5 w-1.5 rounded-full mr-1.5 shrink-0', styles.dot)} />
              {status}
            </span>

            {isActive && (
              <Badge variant="outline" className="text-[10px] border-green-500 text-green-600 dark:text-green-400 animate-pulse">
                <Zap className="h-2.5 w-2.5 mr-1" />
                Active
              </Badge>
            )}

            {currentTool && (
              <span className="text-[10px] font-mono text-muted-foreground truncate max-w-[140px]" title={currentTool}>
                {currentTool}
              </span>
            )}
          </div>

          <div className="grid grid-cols-2 gap-x-4 gap-y-1 text-xs text-muted-foreground">
            {agent?.default_model && (
              <div className="truncate" title={agent.default_model}>
                <span className="font-medium text-foreground">Model: </span>
                {agent.default_model}
              </div>
            )}
            {agentState?.tool_calls !== undefined && (
              <div>
                <span className="font-medium text-foreground">Tool calls: </span>
                {agentState.tool_calls.toLocaleString()}
              </div>
            )}
            {iterationCount > 0 && (
              <div>
                <span className="font-medium text-foreground">Iterations: </span>
                {iterationCount}
              </div>
            )}
            {agentState?.last_active_at && (
              <div className="col-span-2 truncate">
                <span className="font-medium text-foreground">Last active: </span>
                {formatTime(agentState.last_active_at)}
              </div>
            )}
          </div>
        </div>

        {/* Activity feed */}
        <div className="flex-1 flex flex-col min-h-0">
          <div className="px-4 py-2 border-b">
            <h3 className="text-xs font-medium text-muted-foreground uppercase tracking-wide">
              Live Activity
              {entries.length > 0 && (
                <span className="ml-1.5 text-muted-foreground/70">({entries.length})</span>
              )}
            </h3>
          </div>

          <div ref={feedRef} className="flex-1 overflow-y-auto px-4 py-2">
            {entries.length === 0 ? (
              <div className="flex flex-col items-center justify-center h-full text-center gap-2 text-muted-foreground py-8">
                <Zap className="h-6 w-6 opacity-20" />
                <p className="text-xs">No activity yet</p>
                <p className="text-[10px] opacity-60">Events will appear here when the agent is active.</p>
              </div>
            ) : (
              <div>
                {entries.map((entry) => (
                  <ActivityRow key={entry.id} entry={entry} />
                ))}
              </div>
            )}
          </div>
        </div>
      </div>
    </>
  )
}
