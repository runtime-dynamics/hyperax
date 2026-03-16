import { useState } from 'react'
import { Brain, Search, PlusCircle, Trash2, Loader2, Tag } from 'lucide-react'
import {
  useMemoryRecall,
  useMemoryStore,
  useMemoryForget,
  type MemoryEntry,
  type MemoryStoreArgs,
} from '@/services/memoryService'
import { PageHeader } from '@/components/domain/page-header'
import { LoadingState } from '@/components/domain/loading-state'
import { ErrorState } from '@/components/domain/error-state'
import { EmptyState } from '@/components/domain/empty-state'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
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

function scopeVariant(scope: string): 'default' | 'secondary' | 'outline' {
  switch (scope) {
    case 'global': return 'default'
    case 'project': return 'secondary'
    default: return 'outline'
  }
}

function typeVariant(type: string): 'default' | 'secondary' | 'outline' {
  switch (type) {
    case 'procedural': return 'default'
    case 'semantic': return 'secondary'
    default: return 'outline'
  }
}

// ─── MemoryCard ──────────────────────────────────────────────────────────────

interface MemoryCardProps {
  entry: MemoryEntry
  onForget: (id: string) => void
  isDeletingId: string | null
}

function MemoryCard({ entry, onForget, isDeletingId }: MemoryCardProps) {
  const [expanded, setExpanded] = useState(false)
  const isDeleting = isDeletingId === entry.id
  const isLong = entry.content.length > 300

  return (
    <div className="border rounded-lg p-3 space-y-2">
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0 flex-1 space-y-1.5">
          <div className="flex items-center gap-1.5 flex-wrap">
            <Badge variant={scopeVariant(entry.scope)} className="text-xs capitalize">
              {entry.scope}
            </Badge>
            <Badge variant={typeVariant(entry.type)} className="text-xs capitalize">
              {entry.type}
            </Badge>
            {entry.score !== undefined && (
              <span className="text-xs text-muted-foreground">
                score: {entry.score.toFixed(3)}
              </span>
            )}
            {entry.workspace_id && (
              <Badge variant="outline" className="text-xs font-mono">
                ws: {entry.workspace_id.slice(0, 8)}
              </Badge>
            )}
          </div>
          <p
            className={cn(
              'text-sm text-muted-foreground break-words',
              !expanded && isLong ? 'line-clamp-3' : '',
            )}
          >
            {entry.content}
          </p>
          {isLong && (
            <button
              type="button"
              className="text-xs text-primary hover:underline"
              onClick={() => setExpanded((p) => !p)}
            >
              {expanded ? 'Show less' : 'Show more'}
            </button>
          )}
          <p className="text-xs text-muted-foreground font-mono opacity-60">{entry.id}</p>
        </div>
        <Button
          size="sm"
          variant="ghost"
          className="h-7 w-7 p-0 text-muted-foreground hover:text-destructive shrink-0"
          disabled={isDeleting}
          onClick={() => onForget(entry.id)}
          title="Delete memory"
        >
          {isDeleting ? (
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
          ) : (
            <Trash2 className="h-3.5 w-3.5" />
          )}
        </Button>
      </div>
    </div>
  )
}

// ─── StoreMemoryDialog ────────────────────────────────────────────────────────

interface StoreMemoryDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onStore: (
    args: MemoryStoreArgs,
    cb: { onSuccess: () => void; onError: (e: Error) => void },
  ) => void
  isPending: boolean
}

function StoreMemoryDialog({ open, onOpenChange, onStore, isPending }: StoreMemoryDialogProps) {
  const [content, setContent] = useState('')
  const [scope, setScope] = useState('project')
  const [type, setType] = useState('episodic')
  const [tagsRaw, setTagsRaw] = useState('')
  const [contentError, setContentError] = useState('')

  function resetForm() {
    setContent('')
    setScope('project')
    setType('episodic')
    setTagsRaw('')
    setContentError('')
  }

  function handleOpenChange(next: boolean) {
    if (!next) resetForm()
    onOpenChange(next)
  }

  function validate(): boolean {
    if (!content.trim()) { setContentError('Content is required'); return false }
    setContentError('')
    return true
  }

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (!validate()) return
    const tags = tagsRaw
      .split(',')
      .map((t) => t.trim())
      .filter(Boolean)
    onStore(
      {
        content,
        scope,
        type,
        tags: tags.length > 0 ? tags : undefined,
      },
      {
        onSuccess: () => {
          toast({ title: 'Memory stored', description: 'Memory saved successfully.' })
          handleOpenChange(false)
        },
        onError: (err) =>
          toast({ title: 'Store failed', description: err.message, variant: 'destructive' }),
      },
    )
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>Store Memory</DialogTitle>
          <DialogDescription>
            Save a new memory to the agent knowledge system with scope and type classification.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="space-y-1.5">
            <Label htmlFor="mem-content">Content *</Label>
            <textarea
              id="mem-content"
              value={content}
              onChange={(e) => setContent(e.target.value)}
              placeholder="Describe the memory to store..."
              rows={4}
              autoFocus
              className="w-full rounded-md border border-input bg-background text-foreground px-3 py-2 text-sm resize-none focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
            />
            {contentError && <p className="text-xs text-destructive">{contentError}</p>}
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1.5">
              <Label htmlFor="mem-scope">Scope</Label>
              <Select value={scope} onValueChange={setScope}>
                <SelectTrigger id="mem-scope">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="global">Global</SelectItem>
                  <SelectItem value="project">Project</SelectItem>
                  <SelectItem value="persona">Persona</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="mem-type">Type</Label>
              <Select value={type} onValueChange={setType}>
                <SelectTrigger id="mem-type">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="episodic">Episodic</SelectItem>
                  <SelectItem value="semantic">Semantic</SelectItem>
                  <SelectItem value="procedural">Procedural</SelectItem>
                </SelectContent>
              </Select>
            </div>
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="mem-tags">
              <Tag className="h-3 w-3 inline mr-1" />
              Tags (comma-separated)
            </Label>
            <Input
              id="mem-tags"
              value={tagsRaw}
              onChange={(e) => setTagsRaw(e.target.value)}
              placeholder="config, ui, preference"
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
                  Storing...
                </>
              ) : (
                'Store Memory'
              )}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

// ─── MemoryPage ───────────────────────────────────────────────────────────────

export function MemoryPage() {
  const [query, setQuery] = useState('')
  const [submittedQuery, setSubmittedQuery] = useState('')
  const [storeOpen, setStoreOpen] = useState(false)
  const [deletingId, setDeletingId] = useState<string | null>(null)

  const searchEnabled = submittedQuery.trim().length > 0
  const { data: memories, isLoading, error, refetch } = useMemoryRecall(
    { query: submittedQuery, max_results: 50 },
    searchEnabled,
  )
  const { mutate: storeMemory, isPending: isStoring } = useMemoryStore()
  const { mutate: forgetMemory } = useMemoryForget()

  function handleSearch(e: React.FormEvent) {
    e.preventDefault()
    setSubmittedQuery(query)
  }

  function handleForget(id: string) {
    if (!confirm('Delete this memory? This cannot be undone.')) return
    setDeletingId(id)
    forgetMemory(
      { id },
      {
        onSuccess: () => toast({ title: 'Memory deleted' }),
        onError: (err) =>
          toast({ title: 'Delete failed', description: (err as Error).message, variant: 'destructive' }),
        onSettled: () => setDeletingId(null),
      },
    )
  }

  function handleStore(
    args: MemoryStoreArgs,
    cb: { onSuccess: () => void; onError: (e: Error) => void },
  ) {
    storeMemory(args, cb)
  }

  const entries = Array.isArray(memories) ? memories : []

  return (
    <div className="p-6 space-y-6">
      <PageHeader
        title="Memory Management"
        description="Search, store, and manage agent memory entries across scopes."
      >
        <Button size="sm" onClick={() => setStoreOpen(true)}>
          <PlusCircle className="h-4 w-4 mr-2" />
          Store Memory
        </Button>
      </PageHeader>

      <form onSubmit={handleSearch} className="flex items-center gap-2">
        <div className="relative flex-1">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground pointer-events-none" />
          <Input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Recall memories by natural language query..."
            className="pl-9"
          />
        </div>
        <Button type="submit" size="sm" disabled={!query.trim()}>
          Recall
        </Button>
        {submittedQuery && (
          <Button
            type="button"
            size="sm"
            variant="ghost"
            className="text-muted-foreground"
            onClick={() => { setQuery(''); setSubmittedQuery('') }}
          >
            Clear
          </Button>
        )}
      </form>

      {!searchEnabled && (
        <EmptyState
          icon={Brain}
          title="Search your memory store"
          description="Enter a natural language query above to recall matching memory entries."
          action={
            <Button size="sm" variant="outline" onClick={() => setStoreOpen(true)}>
              Store a new memory
            </Button>
          }
        />
      )}

      {searchEnabled && isLoading && (
        <LoadingState message="Recalling memories..." />
      )}

      {searchEnabled && error && (
        <ErrorState error={error as Error} onRetry={() => void refetch()} />
      )}

      {searchEnabled && !isLoading && !error && (
        <>
          {entries.length === 0 ? (
            <EmptyState
              icon={Brain}
              title="No memories found"
              description={`No results matched "${submittedQuery}". Try a different query or store a new memory.`}
              action={
                <Button size="sm" onClick={() => setStoreOpen(true)}>
                  Store Memory
                </Button>
              }
            />
          ) : (
            <div className="space-y-2">
              <p className="text-xs text-muted-foreground">
                {entries.length} result{entries.length !== 1 ? 's' : ''} for &quot;{submittedQuery}&quot;
              </p>
              {entries.map((entry) => (
                <MemoryCard
                  key={entry.id}
                  entry={entry}
                  onForget={handleForget}
                  isDeletingId={deletingId}
                />
              ))}
            </div>
          )}
        </>
      )}

      <StoreMemoryDialog
        open={storeOpen}
        onOpenChange={setStoreOpen}
        onStore={handleStore}
        isPending={isStoring}
      />
    </div>
  )
}
