import { useState } from 'react'
import {
  Activity,
  PlusCircle,
  Trash2,
  Zap,
  Filter,
  Loader2,
  Radio,
} from 'lucide-react'
import {
  useEventStats,
  useEventHandlers,
  useDomainEvents,
  useCreateEventHandler,
  useFireEvent,
  useDeleteEventHandler,
  type EventHandler,
  type DomainEvent,
  type CreateEventHandlerArgs,
  type FireEventArgs,
} from '@/services/eventService'
import { PageHeader } from '@/components/domain/page-header'
import { LoadingState } from '@/components/domain/loading-state'
import { ErrorState } from '@/components/domain/error-state'
import { EmptyState } from '@/components/domain/empty-state'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '@/components/ui/dialog'
import { toast } from '@/components/ui/use-toast'
import { cn } from '@/lib/utils'

// ─── Helpers ─────────────────────────────────────────────────────────────────

function formatDate(iso?: string): string {
  if (!iso) return '—'
  try {
    return new Date(iso).toLocaleString()
  } catch {
    return iso
  }
}


// ─── EventStatsCards ─────────────────────────────────────────────────────────

function EventStatsCards() {
  const { data: stats, isLoading, error } = useEventStats()

  if (isLoading)
    return (
      <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 gap-3">
        {[1, 2, 3, 4].map((i) => (
          <Card key={i} className="animate-pulse">
            <CardContent className="pt-4 pb-3">
              <div className="h-4 bg-muted rounded w-2/3 mb-2" />
              <div className="h-6 bg-muted rounded w-1/3" />
            </CardContent>
          </Card>
        ))}
      </div>
    )

  if (error || !stats || Object.keys(stats).length === 0)
    return (
      <Card className="border-dashed">
        <CardContent className="flex items-center gap-2 py-4 text-sm text-muted-foreground">
          <Activity className="h-4 w-4" />
          <span>No event statistics available yet.</span>
        </CardContent>
      </Card>
    )

  const entries = Object.entries(stats).sort(([, a], [, b]) => b - a)

  return (
    <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 gap-3">
      {entries.map(([eventType, count]) => (
        <Card key={eventType}>
          <CardHeader className="pb-1 pt-3 px-4">
            <CardTitle className="text-xs font-mono text-muted-foreground truncate" title={eventType}>
              {eventType}
            </CardTitle>
          </CardHeader>
          <CardContent className="pb-3 px-4">
            <p className="text-2xl font-semibold tabular-nums">
              {count.toLocaleString()}
            </p>
          </CardContent>
        </Card>
      ))}
    </div>
  )
}

// ─── DomainEventRow ───────────────────────────────────────────────────────────

function DomainEventRow({ event }: { event: DomainEvent }) {
  const [expanded, setExpanded] = useState(false)

  return (
    <div
      className={cn(
        'border rounded-md overflow-hidden',
        expanded ? 'bg-muted/10' : '',
      )}
    >
      <button
        type="button"
        className="w-full flex items-center gap-3 px-3 py-2 text-xs hover:bg-muted/30 transition-colors text-left"
        onClick={() => setExpanded((p) => !p)}
      >
        <span className="text-muted-foreground tabular-nums shrink-0 w-12 text-right">
          #{event.sequence_id}
        </span>
        <code className="font-mono text-xs shrink-0 text-foreground">{event.type}</code>
        <Badge variant="outline" className="text-xs shrink-0">{event.source}</Badge>
        {event.target && (
          <span className="text-muted-foreground shrink-0">→ {event.target}</span>
        )}
        <span className="flex-1" />
        <span className="text-muted-foreground shrink-0">{formatDate(event.timestamp)}</span>
      </button>
      {expanded && !!event.data && (
        <div className="border-t px-3 py-2 bg-muted/20">
          <pre className="text-xs font-mono text-muted-foreground whitespace-pre-wrap break-all">
            {JSON.stringify(event.data, null, 2)}
          </pre>
        </div>
      )}
    </div>
  )
}

// ─── DomainEventLog ──────────────────────────────────────────────────────────

function DomainEventLog() {
  const [typeFilter, setTypeFilter] = useState('')
  const [sourceFilter, setSourceFilter] = useState('')

  const { data: events, isLoading, error, refetch } = useDomainEvents({
    event_type: typeFilter || undefined,
    source: sourceFilter || undefined,
    limit: 50,
  })

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between gap-2">
        <h3 className="text-sm font-semibold flex items-center gap-1.5">
          <Radio className="h-3.5 w-3.5 text-muted-foreground" />
          Domain Events
        </h3>
        <div className="flex items-center gap-2">
          <div className="flex items-center gap-1.5">
            <Filter className="h-3.5 w-3.5 text-muted-foreground shrink-0" />
            <Input
              value={typeFilter}
              onChange={(e) => setTypeFilter(e.target.value)}
              placeholder="Event type..."
              className="h-7 w-36 text-xs font-mono"
            />
            <Input
              value={sourceFilter}
              onChange={(e) => setSourceFilter(e.target.value)}
              placeholder="Source..."
              className="h-7 w-28 text-xs"
            />
          </div>
          {(typeFilter || sourceFilter) && (
            <Button
              size="sm"
              variant="ghost"
              className="h-7 px-2 text-xs text-muted-foreground"
              onClick={() => { setTypeFilter(''); setSourceFilter('') }}
            >
              Clear
            </Button>
          )}
        </div>
      </div>

      {isLoading && <LoadingState message="Loading events..." className="py-6" />}
      {error && <ErrorState error={error as Error} onRetry={() => void refetch()} className="py-6" />}

      {!isLoading && !error && (
        <>
          {(events ?? []).length === 0 ? (
            <EmptyState
              icon={Activity}
              title="No events recorded"
              description={
                typeFilter || sourceFilter
                  ? 'No events match the current filter.'
                  : 'Events will appear here as the system generates them.'
              }
            />
          ) : (
            <div className="space-y-1">
              {(events ?? []).map((event, idx) => (
                <DomainEventRow key={event.id ?? `${event.sequence_id}-${idx}`} event={event} />
              ))}
            </div>
          )}
        </>
      )}
    </div>
  )
}

// ─── EventHandlerRow ─────────────────────────────────────────────────────────

interface EventHandlerRowProps {
  handler: EventHandler
  onDelete: (id: string) => void
  isDeletingId: string | null
}

function EventHandlerRow({ handler, onDelete, isDeletingId }: EventHandlerRowProps) {
  const isDeleting = isDeletingId === handler.id

  return (
    <div className="flex items-center gap-3 px-3 py-2.5 border rounded-md text-sm">
      <code className="font-mono text-xs bg-muted/50 px-1.5 py-0.5 rounded shrink-0">
        {handler.event_filter}
      </code>
      <span className="text-muted-foreground shrink-0">→</span>
      <span className="flex-1 min-w-0 truncate">
        <span className="font-medium">{handler.action}</span>
        {handler.target && (
          <span className="text-muted-foreground ml-1.5 text-xs">{handler.target}</span>
        )}
      </span>
      <span className="text-xs text-muted-foreground shrink-0 hidden sm:inline">
        {formatDate(handler.created_at)}
      </span>
      <Button
        size="sm"
        variant="ghost"
        className="h-7 w-7 p-0 text-muted-foreground hover:text-destructive shrink-0"
        disabled={isDeleting}
        onClick={() => onDelete(handler.id)}
        title="Delete handler"
      >
        {isDeleting ? (
          <Loader2 className="h-3.5 w-3.5 animate-spin" />
        ) : (
          <Trash2 className="h-3.5 w-3.5" />
        )}
      </Button>
    </div>
  )
}

// ─── CreateHandlerDialog ──────────────────────────────────────────────────────

interface CreateHandlerDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onCreate: (args: CreateEventHandlerArgs, cb: { onSuccess: () => void; onError: (e: Error) => void }) => void
  isPending: boolean
}

function CreateHandlerDialog({ open, onOpenChange, onCreate, isPending }: CreateHandlerDialogProps) {
  const [filter, setFilter] = useState('')
  const [action, setAction] = useState('')
  const [target, setTarget] = useState('')
  const [filterError, setFilterError] = useState('')
  const [actionError, setActionError] = useState('')

  function resetForm() {
    setFilter('')
    setAction('')
    setTarget('')
    setFilterError('')
    setActionError('')
  }

  function handleOpenChange(next: boolean) {
    if (!next) resetForm()
    onOpenChange(next)
  }

  function validate(): boolean {
    let valid = true
    if (!filter.trim()) { setFilterError('Event filter is required'); valid = false } else setFilterError('')
    if (!action.trim()) { setActionError('Action is required'); valid = false } else setActionError('')
    return valid
  }

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (!validate()) return
    onCreate(
      { event_filter: filter, action, target: target || undefined },
      {
        onSuccess: () => {
          toast({ title: 'Handler created', description: `Filter "${filter}" is now active.` })
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
          <DialogTitle>Create Event Handler</DialogTitle>
          <DialogDescription>
            Define a glob-pattern filter to match event types and specify what action to take.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="space-y-1.5">
            <Label htmlFor="eh-filter">Event Filter *</Label>
            <Input
              id="eh-filter"
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
              placeholder="event.* or nervous.system.*"
              className="font-mono"
              autoFocus
            />
            {filterError && <p className="text-xs text-destructive">{filterError}</p>}
            <p className="text-xs text-muted-foreground">Glob pattern matching event types.</p>
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="eh-action">Action *</Label>
            <Input
              id="eh-action"
              value={action}
              onChange={(e) => setAction(e.target.value)}
              placeholder="log or notify or pipeline"
            />
            {actionError && <p className="text-xs text-destructive">{actionError}</p>}
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="eh-target">Target (optional)</Label>
            <Input
              id="eh-target"
              value={target}
              onChange={(e) => setTarget(e.target.value)}
              placeholder="pipeline-id or agent-id"
            />
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
                'Create Handler'
              )}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

// ─── FireEventDialog ──────────────────────────────────────────────────────────

interface FireEventDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onFire: (args: FireEventArgs, cb: { onSuccess: () => void; onError: (e: Error) => void }) => void
  isPending: boolean
}

function FireEventDialog({ open, onOpenChange, onFire, isPending }: FireEventDialogProps) {
  const [eventType, setEventType] = useState('')
  const [source, setSource] = useState('')
  const [target, setTarget] = useState('')
  const [dataRaw, setDataRaw] = useState('')
  const [eventTypeError, setEventTypeError] = useState('')
  const [sourceError, setSourceError] = useState('')
  const [dataError, setDataError] = useState('')

  function resetForm() {
    setEventType('')
    setSource('')
    setTarget('')
    setDataRaw('')
    setEventTypeError('')
    setSourceError('')
    setDataError('')
  }

  function handleOpenChange(next: boolean) {
    if (!next) resetForm()
    onOpenChange(next)
  }

  function validate(): boolean {
    let valid = true
    if (!eventType.trim()) { setEventTypeError('Event type is required'); valid = false } else setEventTypeError('')
    if (!source.trim()) { setSourceError('Source is required'); valid = false } else setSourceError('')
    if (dataRaw.trim()) {
      try { JSON.parse(dataRaw) } catch {
        setDataError('Must be valid JSON or empty')
        valid = false
      }
    }
    if (valid) setDataError('')
    return valid
  }

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (!validate()) return

    let parsedData: Record<string, unknown> | undefined
    if (dataRaw.trim()) {
      try { parsedData = JSON.parse(dataRaw) as Record<string, unknown> } catch { return }
    }

    onFire(
      { event_type: eventType, source, target: target || undefined, data: parsedData },
      {
        onSuccess: () => {
          toast({ title: 'Event fired', description: `"${eventType}" dispatched to the nervous system.` })
          handleOpenChange(false)
        },
        onError: (err) =>
          toast({ title: 'Fire failed', description: err.message, variant: 'destructive' }),
      },
    )
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>Fire Event</DialogTitle>
          <DialogDescription>
            Manually dispatch an event into the nervous system event bus.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="space-y-1.5">
            <Label htmlFor="fe-type">Event Type *</Label>
            <Input
              id="fe-type"
              value={eventType}
              onChange={(e) => setEventType(e.target.value)}
              placeholder="system.test"
              className="font-mono"
              autoFocus
            />
            {eventTypeError && <p className="text-xs text-destructive">{eventTypeError}</p>}
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1.5">
              <Label htmlFor="fe-source">Source *</Label>
              <Input
                id="fe-source"
                value={source}
                onChange={(e) => setSource(e.target.value)}
                placeholder="ui"
              />
              {sourceError && <p className="text-xs text-destructive">{sourceError}</p>}
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="fe-target">Target</Label>
              <Input
                id="fe-target"
                value={target}
                onChange={(e) => setTarget(e.target.value)}
                placeholder="optional"
              />
            </div>
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="fe-data">Data (JSON)</Label>
            <textarea
              id="fe-data"
              value={dataRaw}
              onChange={(e) => setDataRaw(e.target.value)}
              placeholder='{"key": "value"}'
              rows={3}
              className="w-full rounded-md border border-input bg-background text-foreground px-3 py-2 text-sm font-mono resize-none focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
            />
            {dataError && <p className="text-xs text-destructive">{dataError}</p>}
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => handleOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={isPending}>
              {isPending ? (
                <>
                  <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                  Firing...
                </>
              ) : (
                <>
                  <Zap className="h-4 w-4 mr-2" />
                  Fire Event
                </>
              )}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

// ─── EventHandlersSection ────────────────────────────────────────────────────

function EventHandlersSection() {
  const [createOpen, setCreateOpen] = useState(false)
  const [deletingId, setDeletingId] = useState<string | null>(null)

  const { data: handlers, isLoading, error, refetch } = useEventHandlers()
  const { mutate: createHandler, isPending: isCreating } = useCreateEventHandler()
  const { mutate: deleteHandler } = useDeleteEventHandler()

  function handleDelete(id: string) {
    const handler = handlers?.find((h) => h.id === id)
    if (!handler) return
    if (!confirm(`Delete handler for "${handler.event_filter}"? This cannot be undone.`)) return
    setDeletingId(id)
    deleteHandler(id, {
      onSuccess: () => toast({ title: 'Handler deleted' }),
      onError: (err) =>
        toast({ title: 'Delete failed', description: (err as Error).message, variant: 'destructive' }),
      onSettled: () => setDeletingId(null),
    })
  }

  function handleCreate(
    args: CreateEventHandlerArgs,
    cb: { onSuccess: () => void; onError: (e: Error) => void },
  ) {
    createHandler(args, cb)
  }

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between gap-2">
        <h3 className="text-sm font-semibold flex items-center gap-1.5">
          <Filter className="h-3.5 w-3.5 text-muted-foreground" />
          Event Handlers
        </h3>
        <Button size="sm" variant="outline" className="h-7 px-2.5 text-xs" onClick={() => setCreateOpen(true)}>
          <PlusCircle className="h-3.5 w-3.5 mr-1" />
          Add Handler
        </Button>
      </div>

      {isLoading && <LoadingState message="Loading handlers..." className="py-4" />}
      {error && <ErrorState error={error as Error} onRetry={() => void refetch()} className="py-4" />}

      {!isLoading && !error && (
        <>
          {(handlers ?? []).length === 0 ? (
            <EmptyState
              icon={Filter}
              title="No event handlers configured"
              description="Add a handler to react to nervous system events automatically."
              action={
                <Button size="sm" onClick={() => setCreateOpen(true)}>
                  Add your first handler
                </Button>
              }
            />
          ) : (
            <div className="space-y-1.5">
              {(handlers ?? []).map((handler) => (
                <EventHandlerRow
                  key={handler.id}
                  handler={handler}
                  onDelete={handleDelete}
                  isDeletingId={deletingId}
                />
              ))}
            </div>
          )}
        </>
      )}

      <CreateHandlerDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        onCreate={handleCreate}
        isPending={isCreating}
      />
    </div>
  )
}

// ─── EventsPage ──────────────────────────────────────────────────────────────

export function EventsPage() {
  const [fireOpen, setFireOpen] = useState(false)
  const { mutate: fireEvent, isPending: isFiring } = useFireEvent()

  function handleFire(
    args: FireEventArgs,
    cb: { onSuccess: () => void; onError: (e: Error) => void },
  ) {
    fireEvent(args, cb)
  }

  return (
    <div className="p-6 space-y-8">
      <PageHeader
        title="Events"
        description="Monitor the nervous system event bus, domain events, and reactive handlers."
      >
        <Button size="sm" onClick={() => setFireOpen(true)}>
          <Zap className="h-4 w-4 mr-2" />
          Fire Event
        </Button>
      </PageHeader>

      <section className="space-y-3">
        <h3 className="text-sm font-semibold flex items-center gap-1.5">
          <Activity className="h-3.5 w-3.5 text-muted-foreground" />
          Event Statistics
        </h3>
        <EventStatsCards />
      </section>

      <section>
        <DomainEventLog />
      </section>

      <section>
        <EventHandlersSection />
      </section>

      <FireEventDialog
        open={fireOpen}
        onOpenChange={setFireOpen}
        onFire={handleFire}
        isPending={isFiring}
      />
    </div>
  )
}
