import { memo, useState, useCallback, useEffect, useRef } from 'react'
import { createPortal } from 'react-dom'
import { Handle, Position, type NodeProps } from '@xyflow/react'
import { Plus, Star, Lock, MessageSquare, UserPlus, RotateCcw, Activity, Trash2 } from 'lucide-react'
import { cn } from '@/lib/utils'
import type { RuntimeStateSummary } from '@/services/orgService'
import type { Task } from '@/services/taskService'
import { getFsmState, fsmStyles } from './fsm'
import { providerBrandColors } from '@/lib/provider-colors'

// ─── Node data type ──────────────────────────────────────────────────────────

export interface AgentNodeData {
  agent: RuntimeStateSummary
  roleName?: string
  tasks: Task[]
  inboxSize: number
  selected: boolean
  onSelect: (agentId: string) => void
  onAddChild: (parentId: string) => void
  onToggleFavorite?: (agentId: string, isFavorite: boolean) => void
  isFavorite?: boolean
  onStartChat?: (agentId: string) => void
  onResetState?: (agentId: string) => void
  onDeleteAgent?: (agentId: string) => void
  onOpenActivity?: (agentId: string) => void
  /** Current tool name when agent is actively processing (from useAgentActivity) */
  currentTool?: string | null
  /** Whether the agent is currently in an active tool-use loop */
  isThinking?: boolean
  isProviderDisabled?: boolean
  providerKind?: string
}

// ─── Context menu ───────────────────────────────────────────────────────────

interface ContextMenuPos {
  x: number
  y: number
}

interface ContextMenuItem {
  label: string
  icon: React.ReactNode
  onClick: () => void
  className?: string
}

function NodeContextMenu({
  pos,
  items,
  onClose,
}: {
  pos: ContextMenuPos
  items: ContextMenuItem[]
  onClose: () => void
}) {
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    function handleClickOutside(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) {
        onClose()
      }
    }
    function handleEscape(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose()
    }
    // Use capture phase so we close before ReactFlow processes the click.
    document.addEventListener('mousedown', handleClickOutside, true)
    document.addEventListener('keydown', handleEscape)
    return () => {
      document.removeEventListener('mousedown', handleClickOutside, true)
      document.removeEventListener('keydown', handleEscape)
    }
  }, [onClose])

  // Clamp to viewport so the menu doesn't overflow off-screen.
  const style: React.CSSProperties = {
    position: 'fixed',
    top: Math.min(pos.y, window.innerHeight - 180),
    left: Math.min(pos.x, window.innerWidth - 200),
    zIndex: 9999,
  }

  return createPortal(
    <div
      ref={ref}
      style={style}
      className="min-w-[180px] rounded-md border bg-popover text-popover-foreground shadow-md p-1 animate-in fade-in-0 zoom-in-95"
    >
      {items.map((item, i) => (
        <button
          key={i}
          type="button"
          className={cn(
            'w-full flex items-center gap-2 rounded-sm px-2 py-1.5 text-sm outline-none transition-colors hover:bg-accent hover:text-accent-foreground cursor-default',
            item.className,
          )}
          onClick={() => {
            item.onClick()
            onClose()
          }}
        >
          {item.icon}
          {item.label}
        </button>
      ))}
    </div>,
    document.body,
  )
}

// ─── Tooltip ─────────────────────────────────────────────────────────────────

function formatDate(iso?: string | null): string {
  if (!iso) return '—'
  try {
    return new Date(iso).toLocaleString()
  } catch {
    return iso
  }
}

function NodeTooltip({ agent, tasks, inboxSize, isProviderDisabled }: { agent: RuntimeStateSummary; tasks: Task[]; inboxSize: number; isProviderDisabled?: boolean }) {
  const state = getFsmState(agent.status)
  const styles = fsmStyles[state]
  const pendingTasks = tasks.filter((t) => t.status === 'pending')
  const activeTasks = tasks.filter((t) => t.status === 'in_progress')
  const blockedTasks = tasks.filter((t) => t.status === 'blocked')

  return (
    <div className="absolute bottom-full left-1/2 -translate-x-1/2 mb-2 z-50 w-64 rounded-md border bg-popover text-popover-foreground shadow-md p-3 text-xs space-y-1.5 pointer-events-none">
      <div className="flex items-center gap-1.5">
        <span className={cn('inline-block h-2 w-2 rounded-full shrink-0', styles.dot)} />
        <span className="font-semibold capitalize">{agent.status}</span>
      </div>
      {agent.workspace && (
        <div className="text-muted-foreground">
          Workspace: <span className="text-foreground">{agent.workspace}</span>
        </div>
      )}
      {agent.tool_calls !== undefined && (
        <div className="text-muted-foreground">
          Tool calls: <span className="text-foreground">{agent.tool_calls.toLocaleString()}</span>
        </div>
      )}
      {agent.last_active_at && (
        <div className="text-muted-foreground">
          Last active: <span className="text-foreground">{formatDate(agent.last_active_at)}</span>
        </div>
      )}
      {isProviderDisabled && (
        <div className="text-red-500 dark:text-red-400 font-medium">Provider is disabled — agent cannot operate</div>
      )}
      {(agent.status_reason || agent.error) && (
        <div className="text-red-600 dark:text-red-400 font-medium break-words">
          {agent.status_reason || agent.error}
        </div>
      )}
      {inboxSize > 0 && (
        <div className="pt-1 border-t">
          <span className="font-medium text-foreground">{inboxSize}</span> message{inboxSize !== 1 ? 's' : ''} queued
        </div>
      )}
      {tasks.length > 0 && (
        <div className={cn(inboxSize === 0 && 'pt-1 border-t')}>
          <div className="text-muted-foreground font-medium mb-1">Tasks ({tasks.length})</div>
          {activeTasks.length > 0 && (
            <div className="flex items-center gap-1 text-blue-600 dark:text-blue-400">
              <span className="inline-block h-1.5 w-1.5 rounded-full bg-blue-500" />
              {activeTasks.length} in progress
            </div>
          )}
          {pendingTasks.length > 0 && (
            <div className="flex items-center gap-1 text-muted-foreground">
              <span className="inline-block h-1.5 w-1.5 rounded-full bg-gray-400" />
              {pendingTasks.length} pending
            </div>
          )}
          {blockedTasks.length > 0 && (
            <div className="flex items-center gap-1 text-red-600 dark:text-red-400">
              <span className="inline-block h-1.5 w-1.5 rounded-full bg-red-500" />
              {blockedTasks.length} blocked
            </div>
          )}
        </div>
      )}
      {tasks.length === 0 && inboxSize === 0 && (
        <div className="pt-1 border-t text-muted-foreground/70">No queued work</div>
      )}
    </div>
  )
}

function AgentNodeComponent({ data }: NodeProps) {
  const { agent, roleName, tasks, inboxSize, selected, onSelect, onAddChild, onToggleFavorite, isFavorite, onStartChat, onResetState, onDeleteAgent, onOpenActivity, currentTool, isThinking, isProviderDisabled: isProviderDisabledProp, providerKind } = data as unknown as AgentNodeData
  const isProviderDisabled = isProviderDisabledProp ?? false
  const providerBg = providerKind ? providerBrandColors[providerKind]?.bg : undefined
  const [hovered, setHovered] = useState(false)
  const [ctxMenu, setCtxMenu] = useState<ContextMenuPos | null>(null)

  const state = getFsmState(agent.status)
  const styles = fsmStyles[state]
  const displayName = agent.name ?? agent.agent_id
  const isProcessing = state === 'onboarding'
  const isActive = state === 'active'
  const missingModel = !agent.default_model
  const isChiefOfStaff = agent.clearance_level === 3

  const handleClick = useCallback(() => {
    onSelect(agent.agent_id)
  }, [onSelect, agent.agent_id])

  const handleAddChild = useCallback(
    (e: React.MouseEvent) => {
      e.stopPropagation()
      onAddChild(agent.agent_id)
    },
    [onAddChild, agent.agent_id],
  )

  const handleToggleFavorite = useCallback(
    (e: React.MouseEvent) => {
      e.stopPropagation()
      if (onToggleFavorite) {
        onToggleFavorite(agent.agent_id, !isFavorite)
      }
    },
    [onToggleFavorite, agent.agent_id, isFavorite],
  )

  const handleStartChat = useCallback(
    (e: React.MouseEvent) => {
      e.stopPropagation()
      if (onStartChat) {
        onStartChat(agent.agent_id)
      }
    },
    [onStartChat, agent.agent_id],
  )

  const handleOpenActivity = useCallback(
    (e: React.MouseEvent) => {
      e.stopPropagation()
      if (onOpenActivity) {
        onOpenActivity(agent.agent_id)
      }
    },
    [onOpenActivity, agent.agent_id],
  )

  const handleContextMenu = useCallback(
    (e: React.MouseEvent) => {
      e.preventDefault()
      e.stopPropagation()
      setCtxMenu({ x: e.clientX, y: e.clientY })
    },
    [],
  )

  const closeContextMenu = useCallback(() => setCtxMenu(null), [])

  // Build context menu items
  const contextMenuItems: ContextMenuItem[] = []
  if (onStartChat) {
    contextMenuItems.push({
      label: `Chat with ${displayName}`,
      icon: <MessageSquare className="h-4 w-4 text-blue-500" />,
      onClick: () => onStartChat(agent.agent_id),
      className: 'font-medium',
    })
  }
  contextMenuItems.push({
    label: 'Add child agent',
    icon: <UserPlus className="h-4 w-4 text-muted-foreground" />,
    onClick: () => onAddChild(agent.agent_id),
  })
  if (onToggleFavorite) {
    contextMenuItems.push({
      label: isFavorite ? 'Remove from favorites' : 'Add to favorites',
      icon: <Star className={cn('h-4 w-4', isFavorite ? 'text-yellow-500 fill-yellow-500' : 'text-muted-foreground')} />,
      onClick: () => onToggleFavorite(agent.agent_id, !isFavorite),
    })
  }
  if (onResetState && (agent.status === 'error' || agent.status === 'halted' || agent.status === 'suspended')) {
    contextMenuItems.push({
      label: 'Reset state',
      icon: <RotateCcw className="h-4 w-4 text-orange-500" />,
      onClick: () => onResetState(agent.agent_id),
      className: 'font-medium text-orange-600 dark:text-orange-400',
    })
  }
  if (onDeleteAgent) {
    contextMenuItems.push({
      label: 'Delete agent',
      icon: <Trash2 className="h-4 w-4 text-destructive" />,
      onClick: () => {
        if (confirm(`Delete agent "${agent.name ?? agent.agent_id}"? This cannot be undone.`)) {
          onDeleteAgent(agent.agent_id)
        }
      },
      className: 'font-medium text-destructive',
    })
  }

  return (
    <div
      className="relative"
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
      onContextMenu={handleContextMenu}
    >
      {/* Target handle at top */}
      <Handle
        type="target"
        position={Position.Top}
        className="!w-2 !h-2 !bg-border !border-border"
      />

      {hovered && !ctxMenu && <NodeTooltip agent={agent} tasks={tasks} inboxSize={inboxSize} isProviderDisabled={isProviderDisabled} />}

      {ctxMenu && (
        <NodeContextMenu pos={ctxMenu} items={contextMenuItems} onClose={closeContextMenu} />
      )}

      <div
        className={cn(
          'w-[180px] rounded-md border-l-4 border border-border text-left transition-all duration-300 shadow-sm cursor-pointer',
          styles.border,
          providerBg ?? styles.bg,
          missingModel && 'border-red-500 border-2',
          selected && 'ring-2 ring-primary ring-offset-1',
          isProcessing && 'animate-pulse',
          isProviderDisabled && !missingModel && 'opacity-50',
        )}
        onClick={handleClick}
      >
        {isProcessing && (
          <span
            className={cn(
              'absolute -inset-1 rounded-md border-2 opacity-50 animate-ping pointer-events-none',
              styles.ring,
            )}
          />
        )}
        {isActive && (
          <span className="absolute -inset-px rounded-md border-2 border-green-400/70 animate-pulse pointer-events-none" />
        )}

        <div className="px-3 py-2.5 space-y-1">
          <p className="text-xs font-semibold truncate leading-tight" title={displayName}>
            {displayName}
          </p>
          {roleName && (
            <p className="text-[10px] text-muted-foreground truncate leading-tight" title={roleName}>
              {roleName}
            </p>
          )}
          <div className="flex items-center gap-1.5">
            <span
              className={cn(
                'inline-flex items-center rounded-full px-1.5 py-0.5 text-[10px] font-medium capitalize',
                styles.badge,
              )}
            >
              {agent.status}
            </span>
            {inboxSize > 0 && (
              <span className="inline-flex items-center rounded-full px-1.5 py-0.5 text-[10px] font-medium bg-blue-100 text-blue-700 dark:bg-blue-900/50 dark:text-blue-300">
                {inboxSize} msg
              </span>
            )}
          </div>
          {isProviderDisabled && (
            <p className="text-[10px] text-red-500 dark:text-red-400 truncate font-medium">
              Provider disabled
            </p>
          )}
          {isThinking && (
            <div className="flex items-center gap-1 text-[10px] text-blue-600 dark:text-blue-400">
              <span className="inline-flex gap-0.5">
                <span className="h-1 w-1 rounded-full bg-blue-500 animate-bounce [animation-delay:0ms]" />
                <span className="h-1 w-1 rounded-full bg-blue-500 animate-bounce [animation-delay:150ms]" />
                <span className="h-1 w-1 rounded-full bg-blue-500 animate-bounce [animation-delay:300ms]" />
              </span>
              {currentTool && (
                <span className="truncate font-mono" title={currentTool}>{currentTool}</span>
              )}
            </div>
          )}
          {tasks.length > 0 && (
            <p className="text-[10px] text-muted-foreground truncate">
              {tasks.filter((t) => t.status === 'in_progress').length} active / {tasks.length} tasks
            </p>
          )}
        </div>

        {/* Favorite and Start Chat buttons */}
        {hovered && (
          <div className="absolute -top-10 left-0 right-0 flex items-center justify-center gap-1 z-10">
            {/* Favorite button */}
            <button
              type="button"
              className={cn(
                'h-6 w-6 rounded flex items-center justify-center shadow hover:scale-110 transition-transform',
                isFavorite
                  ? 'bg-yellow-400 text-yellow-900 dark:bg-yellow-500 dark:text-yellow-950'
                  : 'bg-gray-300 text-gray-700 dark:bg-gray-600 dark:text-gray-300 hover:bg-yellow-300',
              )}
              onClick={handleToggleFavorite}
              title={isFavorite ? 'Remove from favorites' : 'Add to favorites'}
              disabled={isChiefOfStaff && isFavorite}
              aria-label={`${isFavorite ? 'Remove from' : 'Add to'} favorites`}
            >
              {isChiefOfStaff && isFavorite ? (
                <Lock className="h-3.5 w-3.5" />
              ) : (
                <Star className={cn('h-3.5 w-3.5', isFavorite && 'fill-current')} />
              )}
            </button>

            {/* Start Chat button */}
            <button
              type="button"
              className="h-6 w-6 rounded flex items-center justify-center shadow bg-blue-400 text-blue-900 dark:bg-blue-500 dark:text-blue-950 hover:scale-110 transition-transform"
              onClick={handleStartChat}
              title="Start chat with this agent"
              aria-label={`Start chat with ${displayName}`}
            >
              <MessageSquare className="h-3.5 w-3.5" />
            </button>

            {/* Activity drawer button */}
            {onOpenActivity && (
              <button
                type="button"
                className="h-6 w-6 rounded flex items-center justify-center shadow bg-gray-300 text-gray-700 dark:bg-gray-600 dark:text-gray-300 hover:bg-purple-300 hover:text-purple-900 dark:hover:bg-purple-600 dark:hover:text-purple-100 hover:scale-110 transition-all"
                onClick={handleOpenActivity}
                title="View agent activity"
                aria-label={`View activity for ${displayName}`}
              >
                <Activity className="h-3.5 w-3.5" />
              </button>
            )}
          </div>
        )}


        {/* Add-child button */}
        {hovered && (
          <button
            type="button"
            className="absolute -bottom-3 left-1/2 -translate-x-1/2 z-10 h-5 w-5 rounded-full bg-primary text-primary-foreground flex items-center justify-center shadow hover:scale-110 transition-transform"
            onClick={handleAddChild}
            title="Add child agent"
            aria-label={`Add child agent under ${displayName}`}
          >
            <Plus className="h-3 w-3" />
          </button>
        )}
      </div>

      {/* Source handle at bottom */}
      <Handle
        type="source"
        position={Position.Bottom}
        className="!w-2 !h-2 !bg-border !border-border"
      />
    </div>
  )
}

export const AgentNode = memo(AgentNodeComponent)

