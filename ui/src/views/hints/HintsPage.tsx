import { useState } from 'react'
import { Lightbulb, Search, AlertCircle } from 'lucide-react'
import {
  useHints,
  useHintProviders,
  type Hint,
} from '@/services/hintsService'
import { PageHeader } from '@/components/domain/page-header'
import { LoadingState } from '@/components/domain/loading-state'
import { ErrorState } from '@/components/domain/error-state'
import { EmptyState } from '@/components/domain/empty-state'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { cn } from '@/lib/utils'

// ─── Helpers ─────────────────────────────────────────────────────────────────

function priorityVariant(
  priority?: number,
): 'default' | 'secondary' | 'outline' {
  if (priority === undefined) return 'outline'
  if (priority >= 8) return 'default'
  if (priority >= 5) return 'secondary'
  return 'outline'
}

function groupHintsByProvider(hints: Hint[]): Map<string, Hint[]> {
  const map = new Map<string, Hint[]>()
  for (const hint of hints) {
    const provider = hint.provider || 'unknown'
    const existing = map.get(provider) ?? []
    existing.push(hint)
    map.set(provider, existing)
  }
  return map
}

// ─── HintCard ─────────────────────────────────────────────────────────────────

function HintCard({ hint }: { hint: Hint }) {
  const [expanded, setExpanded] = useState(false)
  const isLong = hint.content.length > 200

  return (
    <div className="border rounded-md p-3 space-y-2">
      <div className="flex items-start justify-between gap-2">
        <div className="flex items-center gap-2 flex-wrap">
          {hint.priority !== undefined && (
            <Badge variant={priorityVariant(hint.priority)} className="text-xs">
              P{hint.priority}
            </Badge>
          )}
          {hint.tags && hint.tags.length > 0 && (
            hint.tags.map((tag) => (
              <Badge key={tag} variant="outline" className="text-xs">{tag}</Badge>
            ))
          )}
          {hint.file_path && (
            <code className="text-xs font-mono text-muted-foreground bg-muted/50 px-1.5 py-0.5 rounded truncate max-w-xs">
              {hint.file_path}
            </code>
          )}
        </div>
      </div>
      <p
        className={cn(
          'text-sm text-foreground whitespace-pre-wrap break-words',
          !expanded && isLong ? 'line-clamp-3' : '',
        )}
      >
        {hint.content}
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
    </div>
  )
}

// ─── HintProviderGroup ────────────────────────────────────────────────────────

function HintProviderGroup({ provider, hints }: { provider: string; hints: Hint[] }) {
  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2">
        <Lightbulb className="h-3.5 w-3.5 text-muted-foreground shrink-0" />
        <h3 className="text-sm font-semibold capitalize">{provider}</h3>
        <span className="text-xs text-muted-foreground">({hints.length})</span>
      </div>
      <div className="space-y-1.5 pl-5">
        {hints.map((hint, idx) => (
          <HintCard key={hint.id ?? `${provider}-${idx}`} hint={hint} />
        ))}
      </div>
    </div>
  )
}

// ─── HintProvidersPanel ───────────────────────────────────────────────────────

function HintProvidersPanel() {
  const { data: providers, isLoading, error } = useHintProviders()

  if (isLoading) return null
  if (error) return null
  const items = Array.isArray(providers) ? providers : []
  if (items.length === 0) return null

  return (
    <div className="space-y-2">
      <h3 className="text-sm font-semibold text-muted-foreground uppercase tracking-wide text-xs">
        Configured Providers
      </h3>
      <div className="flex flex-wrap gap-2">
        {items.map((p) => (
          <div key={p.id} className="flex items-center gap-2 border rounded-md px-3 py-2 text-sm">
            <span className="font-medium">{p.name}</span>
            {p.description && (
              <span className="text-muted-foreground text-xs">{p.description}</span>
            )}
            <Badge variant={p.enabled ? 'default' : 'outline'} className="text-xs">
              {p.enabled ? 'enabled' : 'disabled'}
            </Badge>
          </div>
        ))}
      </div>
    </div>
  )
}

// ─── HintsPage ────────────────────────────────────────────────────────────────

export function HintsPage() {
  const [filePath, setFilePath] = useState('')
  const [scope, setScope] = useState('')
  const [submittedPath, setSubmittedPath] = useState('')
  const [submittedScope, setSubmittedScope] = useState('')

  const searchEnabled = submittedPath.trim().length > 0 || submittedScope.trim().length > 0

  const { data: hints, isLoading, error, refetch } = useHints(
    {
      file_path: submittedPath || undefined,
      scope: submittedScope || undefined,
    },
    searchEnabled,
  )

  function handleSearch(e: React.FormEvent) {
    e.preventDefault()
    setSubmittedPath(filePath)
    setSubmittedScope(scope)
  }

  const hintList = Array.isArray(hints) ? hints : []
  const grouped = groupHintsByProvider(hintList)

  return (
    <div className="p-6 space-y-6">
      <PageHeader
        title="Hints Configuration"
        description="Retrieve contextual hints from configured hint providers by file path or scope."
      />

      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-sm font-semibold flex items-center gap-1.5">
            <Search className="h-4 w-4 text-muted-foreground" />
            Query Hints
          </CardTitle>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleSearch} className="space-y-4">
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
              <div className="space-y-1.5">
                <Label htmlFor="hints-path">File Path</Label>
                <Input
                  id="hints-path"
                  value={filePath}
                  onChange={(e) => setFilePath(e.target.value)}
                  placeholder="src/app.ts"
                  className="font-mono"
                  autoFocus
                />
                <p className="text-xs text-muted-foreground">
                  Relative or absolute path to the file to get hints for.
                </p>
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="hints-scope">Scope</Label>
                <Input
                  id="hints-scope"
                  value={scope}
                  onChange={(e) => setScope(e.target.value)}
                  placeholder="workspace-name or global"
                />
                <p className="text-xs text-muted-foreground">
                  Optional scope to narrow hint retrieval context.
                </p>
              </div>
            </div>
            <div className="flex items-center gap-2">
              <Button type="submit" size="sm" disabled={!filePath.trim() && !scope.trim()}>
                <Search className="h-4 w-4 mr-2" />
                Get Hints
              </Button>
              {(submittedPath || submittedScope) && (
                <Button
                  type="button"
                  size="sm"
                  variant="ghost"
                  className="text-muted-foreground"
                  onClick={() => {
                    setFilePath('')
                    setScope('')
                    setSubmittedPath('')
                    setSubmittedScope('')
                  }}
                >
                  Clear
                </Button>
              )}
            </div>
          </form>
        </CardContent>
      </Card>

      <HintProvidersPanel />

      {!searchEnabled && (
        <EmptyState
          icon={Lightbulb}
          title="Enter a file path or scope"
          description="Hints will be retrieved from all configured providers for the given context."
        />
      )}

      {searchEnabled && isLoading && (
        <LoadingState message="Fetching hints..." />
      )}

      {searchEnabled && error && (
        <div className="space-y-2">
          <div className="flex items-center gap-2 text-sm text-muted-foreground border rounded-md p-3 bg-muted/20">
            <AlertCircle className="h-4 w-4 text-destructive shrink-0" />
            <span>
              The <code className="font-mono text-xs">get_hints</code> tool returned an error. The hint providers may not be configured.
            </span>
          </div>
          <ErrorState error={error as Error} onRetry={() => void refetch()} />
        </div>
      )}

      {searchEnabled && !isLoading && !error && (
        <>
          {hintList.length === 0 ? (
            <EmptyState
              icon={Lightbulb}
              title="No hints returned"
              description="No hints matched the current file path or scope. Try a different context."
            />
          ) : (
            <div className="space-y-6">
              <p className="text-sm text-muted-foreground">
                {hintList.length} hint{hintList.length !== 1 ? 's' : ''} from {grouped.size} provider{grouped.size !== 1 ? 's' : ''}
              </p>
              {Array.from(grouped.entries()).map(([provider, providerHints]) => (
                <HintProviderGroup
                  key={provider}
                  provider={provider}
                  hints={providerHints}
                />
              ))}
            </div>
          )}
        </>
      )}
    </div>
  )
}
