import { useState, useEffect, useRef } from 'react'
import {
  Terminal,
  Circle,
  AlertTriangle,
  CheckCircle2,
  Send,
  LogOut,
  ClipboardList,
  Clock,
} from 'lucide-react'
import { toast } from '@/components/ui/use-toast'
import { cn } from '@/lib/utils'
import {
  useChannelSessions,
  useChannelHistory,
  useChannelPermissions,
  useSendChannelMessage,
  useResolvePermission,
  useDisconnectSession,
  useDispatchTask,
  type ChannelSession,
  type ChannelMessage,
  type PermissionRequest,
} from '@/services/channelService'
import { useTasks, type Task } from '@/services/taskService'
import { EmptyState } from '@/components/domain/empty-state'
import { LoadingState } from '@/components/domain/loading-state'
import { ErrorState } from '@/components/domain/error-state'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'

// ─── Helpers ──────────────────────────────────────────────────────────────────

function formatRelativeTime(iso: string): string {
  try {
    const ms = Date.now() - new Date(iso).getTime()
    const s = Math.floor(ms / 1000)
    if (s < 60) return `${s}s ago`
    const m = Math.floor(s / 60)
    if (m < 60) return `${m}m ago`
    const h = Math.floor(m / 60)
    return `${h}h ago`
  } catch {
    return ''
  }
}

function formatTime(iso: string): string {
  try {
    return new Date(iso).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  } catch {
    return ''
  }
}

function statusColor(status: ChannelSession['status']): string {
  switch (status) {
    case 'connected': return 'bg-green-500'
    case 'idle': return 'bg-amber-500'
    default: return 'bg-muted-foreground/40'
  }
}

function statusLabel(status: ChannelSession['status']): string {
  switch (status) {
    case 'connected': return 'Connected'
    case 'idle': return 'Idle'
    default: return 'Disconnected'
  }
}

// ─── Session Card ─────────────────────────────────────────────────────────────

interface SessionCardProps {
  session: ChannelSession
  isSelected: boolean
  onClick: () => void
}

function SessionCard({ session, isSelected, onClick }: SessionCardProps) {
  return (
    <button
      onClick={onClick}
      className={cn(
        'w-full text-left px-3 py-3 rounded-lg border transition-colors',
        isSelected
          ? 'border-primary/50 bg-primary/5'
          : 'border-transparent hover:border-border hover:bg-accent/50',
      )}
    >
      <div className="flex items-center justify-between gap-2 mb-1">
        <span className="text-sm font-medium text-foreground truncate">{session.name}</span>
        <span className="flex items-center gap-1.5 shrink-0">
          <Circle className={cn('h-2 w-2 fill-current', statusColor(session.status))} />
          <span className="text-xs text-muted-foreground">{statusLabel(session.status)}</span>
        </span>
      </div>
      {session.workspace && (
        <p className="text-xs text-muted-foreground truncate mb-1">{session.workspace}</p>
      )}
      <div className="flex items-center gap-2 text-xs text-muted-foreground/70">
        <Clock className="h-3 w-3 shrink-0" />
        <span>Connected {formatRelativeTime(session.connected_at)}</span>
        {session.last_heartbeat && (
          <span className="ml-auto">Heartbeat {formatRelativeTime(session.last_heartbeat)}</span>
        )}
      </div>
    </button>
  )
}

// ─── Permission Banner ────────────────────────────────────────────────────────

interface PermissionBannerProps {
  request: PermissionRequest
  onApprove: () => void
  onDeny: () => void
  isResolving: boolean
}

function PermissionBanner({ request, onApprove, onDeny, isResolving }: PermissionBannerProps) {
  return (
    <div className="flex items-center gap-3 px-4 py-3 bg-amber-500/10 border-b border-amber-500/20">
      <AlertTriangle className="h-4 w-4 text-amber-500 shrink-0" />
      <div className="flex-1 min-w-0">
        <p className="text-sm font-medium text-foreground">
          Claude wants to run:{' '}
          <span className="font-mono text-amber-600 dark:text-amber-400">{request.tool_name}</span>
        </p>
        {request.description && (
          <p className="text-xs text-muted-foreground truncate mt-0.5">{request.description}</p>
        )}
      </div>
      <div className="flex items-center gap-2 shrink-0">
        <Button
          size="sm"
          className="h-7 text-xs bg-green-600 hover:bg-green-700 text-white"
          onClick={onApprove}
          disabled={isResolving}
        >
          <CheckCircle2 className="h-3.5 w-3.5 mr-1" />
          Approve
        </Button>
        <Button
          variant="outline"
          size="sm"
          className="h-7 text-xs border-destructive/50 text-destructive hover:bg-destructive/10"
          onClick={onDeny}
          disabled={isResolving}
        >
          Deny
        </Button>
      </div>
    </div>
  )
}

// ─── Chat Message ─────────────────────────────────────────────────────────────

function ChatMessage({ message }: { message: ChannelMessage }) {
  const taskId = message.metadata?.task_id
  const taskStatus = message.metadata?.task_status

  if (message.direction === 'system') {
    return (
      <div className="flex flex-col items-center gap-1 py-1">
        <span className="text-xs text-muted-foreground/60 text-center">{message.content}</span>
        {taskId && (
          <Badge variant="outline" className="text-[10px] font-mono h-4 px-1.5">
            task:{taskId.slice(0, 8)}
            {taskStatus && <span className="ml-1 text-muted-foreground">{taskStatus}</span>}
          </Badge>
        )}
      </div>
    )
  }

  const isOutbound = message.direction === 'outbound'

  return (
    <div className={cn('flex flex-col gap-1 max-w-[75%]', isOutbound ? 'self-end items-end' : 'self-start items-start')}>
      <div
        className={cn(
          'rounded-2xl px-4 py-2 text-sm leading-relaxed break-words',
          isOutbound
            ? 'bg-primary text-primary-foreground rounded-br-sm'
            : 'bg-muted text-foreground rounded-bl-sm',
        )}
      >
        {message.content}
      </div>
      <div className="flex items-center gap-1.5 px-1">
        <span className="text-xs text-muted-foreground">{formatTime(message.created_at)}</span>
        {taskId && (
          <Badge variant="outline" className="text-[10px] font-mono h-4 px-1.5">
            task:{taskId.slice(0, 8)}
            {taskStatus && <span className="ml-1 text-muted-foreground">{taskStatus}</span>}
          </Badge>
        )}
      </div>
    </div>
  )
}

// ─── Task Dispatch Dialog ─────────────────────────────────────────────────────

interface DispatchTaskDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  sessionId: string
  onDispatched: () => void
}

function DispatchTaskDialog({ open, onOpenChange, sessionId, onDispatched }: DispatchTaskDialogProps) {
  const { data: rawTasks, isLoading } = useTasks('_org')
  const dispatchTask = useDispatchTask()

  const pendingTasks = (rawTasks ?? []).filter(
    (t: Task) => t.status === 'pending' || t.status === 'in_progress',
  )

  function handleDispatch(task: Task) {
    dispatchTask.mutate(
      { session_id: sessionId, task_id: task.id },
      {
        onSuccess: () => {
          toast({ title: 'Task dispatched', description: `"${task.title}" sent to session.` })
          onDispatched()
          onOpenChange(false)
        },
        onError: (error) => {
          const msg = error instanceof Error ? error.message : 'Failed to dispatch task.'
          toast({ title: 'Error', description: msg, variant: 'destructive' })
        },
      },
    )
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>Dispatch Task</DialogTitle>
          <DialogDescription>
            Select a task to send to this Claude Code session.
          </DialogDescription>
        </DialogHeader>

        <div className="min-h-[200px] max-h-[320px] overflow-y-auto">
          {isLoading ? (
            <LoadingState message="Loading tasks..." />
          ) : pendingTasks.length === 0 ? (
            <EmptyState
              icon={ClipboardList}
              title="No pending tasks"
              description="Create tasks in the Tasks page first."
            />
          ) : (
            <div className="space-y-1 py-1">
              {pendingTasks.map((task: Task) => (
                <button
                  key={task.id}
                  onClick={() => handleDispatch(task)}
                  disabled={dispatchTask.isPending}
                  className="w-full text-left px-3 py-2.5 rounded-md hover:bg-accent transition-colors group"
                >
                  <div className="flex items-start justify-between gap-2">
                    <span className="text-sm text-foreground group-hover:text-primary transition-colors line-clamp-2">
                      {task.title}
                    </span>
                    <Badge
                      variant="outline"
                      className={cn(
                        'shrink-0 text-[10px] capitalize',
                        task.status === 'in_progress' && 'border-blue-500/50 text-blue-500',
                        task.status === 'pending' && 'border-muted-foreground/50',
                      )}
                    >
                      {task.status.replace('_', ' ')}
                    </Badge>
                  </div>
                  {task.description && (
                    <p className="text-xs text-muted-foreground mt-0.5 line-clamp-1">
                      {task.description}
                    </p>
                  )}
                </button>
              ))}
            </div>
          )}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// ─── Chat Panel ───────────────────────────────────────────────────────────────

interface ChatPanelProps {
  session: ChannelSession
  onDisconnect: () => void
}

export function ChatPanel({ session, onDisconnect }: ChatPanelProps) {
  const [input, setInput] = useState('')
  const [dispatchOpen, setDispatchOpen] = useState(false)
  const [confirmDisconnect, setConfirmDisconnect] = useState(false)
  const bottomRef = useRef<HTMLDivElement>(null)

  const { data: messages = [], isLoading: historyLoading } = useChannelHistory(session.id, 50)
  const { data: permissions = [] } = useChannelPermissions(session.id)
  const sendMessage = useSendChannelMessage()
  const resolvePermission = useResolvePermission()
  const disconnectSession = useDisconnectSession()

  // Auto-scroll to bottom when messages change
  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [messages.length])

  function handleSend() {
    const content = input.trim()
    if (!content) return
    setInput('')
    sendMessage.mutate(
      { session_id: session.id, content },
      {
        onError: (error) => {
          const msg = error instanceof Error ? error.message : 'Failed to send message.'
          toast({ title: 'Error', description: msg, variant: 'destructive' })
        },
      },
    )
  }

  function handleKeyDown(e: React.KeyboardEvent<HTMLInputElement>) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      handleSend()
    }
  }

  function handleResolve(requestId: string, behavior: 'allow' | 'deny') {
    resolvePermission.mutate(
      { request_id: requestId, behavior },
      {
        onError: (error) => {
          const msg = error instanceof Error ? error.message : 'Failed to resolve permission.'
          toast({ title: 'Error', description: msg, variant: 'destructive' })
        },
      },
    )
  }

  function handleDisconnect() {
    disconnectSession.mutate(
      { session_id: session.id },
      {
        onSuccess: () => {
          toast({ title: 'Session disconnected' })
          onDisconnect()
        },
        onError: (error) => {
          const msg = error instanceof Error ? error.message : 'Failed to disconnect.'
          toast({ title: 'Error', description: msg, variant: 'destructive' })
        },
      },
    )
    setConfirmDisconnect(false)
  }

  const pendingPermissions = permissions as PermissionRequest[]

  return (
    <div className="flex flex-col h-full">
      {/* Session header bar */}
      <div className="flex items-center justify-between gap-3 px-4 py-2.5 border-b bg-card shrink-0">
        <div className="flex items-center gap-2 min-w-0">
          <Circle className={cn('h-2.5 w-2.5 fill-current shrink-0', statusColor(session.status))} />
          <span className="text-sm font-medium text-foreground truncate">{session.name}</span>
          <Badge variant="outline" className="text-[10px] shrink-0">
            {statusLabel(session.status)}
          </Badge>
          {session.workspace && (
            <span className="text-xs text-muted-foreground truncate hidden sm:inline">
              {session.workspace}
            </span>
          )}
        </div>
        <div className="flex items-center gap-2 shrink-0">
          <Button
            variant="outline"
            size="sm"
            className="h-7 text-xs"
            onClick={() => setDispatchOpen(true)}
          >
            <ClipboardList className="h-3.5 w-3.5 mr-1.5" />
            Dispatch Task
          </Button>
          <Button
            variant="ghost"
            size="sm"
            className="h-7 text-xs text-destructive hover:text-destructive hover:bg-destructive/10"
            onClick={() => setConfirmDisconnect(true)}
            disabled={disconnectSession.isPending}
          >
            <LogOut className="h-3.5 w-3.5 mr-1.5" />
            Disconnect
          </Button>
        </div>
      </div>

      {/* Permission banners */}
      {pendingPermissions.map((req: PermissionRequest) => (
        <PermissionBanner
          key={req.id}
          request={req}
          onApprove={() => handleResolve(req.id, 'allow')}
          onDeny={() => handleResolve(req.id, 'deny')}
          isResolving={resolvePermission.isPending}
        />
      ))}

      {/* Message history */}
      <div className="flex-1 overflow-y-auto px-4">
        <div className="max-w-3xl mx-auto h-full">
          {historyLoading ? (
            <LoadingState message="Loading messages..." />
          ) : (messages as ChannelMessage[]).length === 0 ? (
            <EmptyState
              icon={Terminal}
              title="No messages yet"
              description="Send a message to start communicating with this Claude Code session."
            />
          ) : (
            <div className="flex flex-col gap-3 py-4">
              {(messages as ChannelMessage[]).map((msg: ChannelMessage) => (
                <ChatMessage key={msg.id} message={msg} />
              ))}
              <div ref={bottomRef} />
            </div>
          )}
        </div>
      </div>

      {/* Message input */}
      <div className="border-t px-4 py-3 bg-card shrink-0">
        <div className="max-w-3xl mx-auto flex gap-2">
          <Input
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder="Type a message..."
            disabled={sendMessage.isPending || session.status === 'disconnected'}
            className="flex-1"
          />
          <Button
            onClick={handleSend}
            disabled={!input.trim() || sendMessage.isPending || session.status === 'disconnected'}
            size="sm"
            className="shrink-0"
          >
            <Send className="h-4 w-4 mr-1.5" />
            Send
          </Button>
        </div>
      </div>

      {/* Dispatch task dialog */}
      <DispatchTaskDialog
        open={dispatchOpen}
        onOpenChange={setDispatchOpen}
        sessionId={session.id}
        onDispatched={() => {}}
      />

      {/* Disconnect confirmation dialog */}
      <Dialog open={confirmDisconnect} onOpenChange={setConfirmDisconnect}>
        <DialogContent className="max-w-sm">
          <DialogHeader>
            <DialogTitle>Disconnect session?</DialogTitle>
            <DialogDescription>
              This will force-disconnect <span className="font-medium">{session.name}</span>.
              The Claude Code instance will need to reconnect manually.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirmDisconnect(false)}>
              Cancel
            </Button>
            <Button
              variant="destructive"
              onClick={handleDisconnect}
              disabled={disconnectSession.isPending}
            >
              {disconnectSession.isPending ? 'Disconnecting...' : 'Disconnect'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}

// ─── Sessions Page ────────────────────────────────────────────────────────────

export function SessionsPage() {
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const { data: sessions, isLoading, error, refetch } = useChannelSessions()

  const sessionList = (sessions ?? []) as ChannelSession[]
  const selectedSession = sessionList.find((s: ChannelSession) => s.id === selectedId) ?? null

  // If selected session disappears (disconnected+removed), clear selection
  useEffect(() => {
    if (selectedId && sessionList.length > 0 && !selectedSession) {
      setSelectedId(null)
    }
  }, [sessionList, selectedId, selectedSession])

  return (
    <div className="flex h-[calc(100vh-3.5rem)]">
      {/* Left panel — session list */}
      <div className="w-72 shrink-0 flex flex-col border-r bg-card">
        <div className="px-4 py-3 border-b">
          <div className="flex items-center gap-2">
            <Terminal className="h-4 w-4 text-muted-foreground" />
            <h2 className="text-sm font-semibold">Claude Code Sessions</h2>
          </div>
          {sessionList.length > 0 && (
            <p className="text-xs text-muted-foreground mt-0.5">
              {sessionList.filter((s: ChannelSession) => s.status === 'connected').length} connected
            </p>
          )}
        </div>

        <div className="flex-1 overflow-y-auto p-2">
          {isLoading ? (
            <LoadingState message="Loading sessions..." />
          ) : error ? (
            <ErrorState error={error as Error} onRetry={() => void refetch()} />
          ) : sessionList.length === 0 ? (
            <EmptyState
              icon={Terminal}
              title="No sessions connected"
              description={`Run \`hyperax-bridge --url http://your-server:9090\` in your Claude Code session to connect.`}
            />
          ) : (
            <div className="space-y-1">
              {sessionList.map((session: ChannelSession) => (
                <SessionCard
                  key={session.id}
                  session={session}
                  isSelected={session.id === selectedId}
                  onClick={() => setSelectedId(session.id)}
                />
              ))}
            </div>
          )}
        </div>
      </div>

      {/* Right panel — chat interface */}
      <div className="flex-1 min-w-0">
        {selectedSession ? (
          <ChatPanel
            key={selectedSession.id}
            session={selectedSession}
            onDisconnect={() => setSelectedId(null)}
          />
        ) : (
          <div className="flex items-center justify-center h-full text-center">
            <EmptyState
              icon={Terminal}
              title="Select a session"
              description="Choose a Claude Code session from the list to start communicating."
            />
          </div>
        )}
      </div>
    </div>
  )
}
