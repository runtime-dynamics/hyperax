import { useState } from 'react'
import { Lock, PlusCircle, Trash2, Loader2, Shield, Globe, User, Puzzle, Server, Pencil } from 'lucide-react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { mcpCall } from '@/lib/mcp-client'
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

// ─── Types ────────────────────────────────────────────────────────────────────

interface SecretEntry {
  key: string
  scope: string
  access_scope: string
  created_at: string
  updated_at: string
}

interface AgentInfo {
  id: string
  name: string
}

interface PluginInfo {
  id: string
  name: string
  source_hash?: string
}

// ─── Data fetching ─────────────────────────────────────────────────────────────

function useSecretEntries() {
  return useQuery({
    queryKey: ['secret-entries'],
    queryFn: async () => {
      const result = await mcpCall<{ entries: SecretEntry[]; count: number }>(
        'secret',
        { action: 'list_entries', scope: 'global' },
      )
      return result?.entries ?? []
    },
  })
}

function useAgentList() {
  return useQuery({
    queryKey: ['agents-for-secrets'],
    queryFn: async () => {
      const result = await mcpCall<AgentInfo[]>('agent', { action: 'list' })
      return Array.isArray(result) ? result : []
    },
  })
}

function usePluginList() {
  return useQuery({
    queryKey: ['plugins-for-secrets'],
    queryFn: async () => {
      const result = await mcpCall<{ plugins: PluginInfo[] } | PluginInfo[]>(
        'plugin',
        { action: 'list' },
      )
      if (Array.isArray(result)) return result
      return result?.plugins ?? []
    },
  })
}

interface ProviderInfo {
  id: string
  name: string
  kind: string
}

function useProviderList() {
  return useQuery({
    queryKey: ['providers-for-secrets'],
    queryFn: async () => {
      const result = await mcpCall<ProviderInfo[] | { providers: ProviderInfo[] }>(
        'config',
        { action: 'list_providers' },
      )
      if (Array.isArray(result)) return result
      return (result as { providers?: ProviderInfo[] })?.providers ?? []
    },
  })
}

// ─── Access Scope Display ──────────────────────────────────────────────────────

function AccessScopeBadge({ accessScope }: { accessScope: string }) {
  if (!accessScope || accessScope === 'global') {
    return (
      <Badge variant="outline" className="gap-1 text-xs">
        <Globe className="h-3 w-3" />
        Global
      </Badge>
    )
  }
  if (accessScope.startsWith('persona:')) {
    const id = accessScope.replace('persona:', '')
    return (
      <Badge variant="outline" className="gap-1 text-xs">
        <User className="h-3 w-3" />
        Persona: {id}
      </Badge>
    )
  }
  if (accessScope.startsWith('plugin:')) {
    const hash = accessScope.replace('plugin:', '')
    return (
      <Badge variant="outline" className="gap-1 text-xs">
        <Puzzle className="h-3 w-3" />
        Plugin: {hash}
      </Badge>
    )
  }
  if (accessScope.startsWith('provider:')) {
    const id = accessScope.replace('provider:', '')
    return (
      <Badge variant="outline" className="gap-1 text-xs">
        <Shield className="h-3 w-3" />
        Provider: {id}
      </Badge>
    )
  }
  if (accessScope === 'system') {
    return (
      <Badge variant="secondary" className="gap-1 text-xs">
        <Server className="h-3 w-3" />
        System
      </Badge>
    )
  }
  return <Badge variant="outline" className="text-xs">{accessScope}</Badge>
}

// ─── SecretRow ─────────────────────────────────────────────────────────────────

interface SecretRowProps {
  entry: SecretEntry
  onDelete: (key: string) => void
  onEditScope: (entry: SecretEntry) => void
  isDeleting: boolean
}

function SecretRow({ entry, onDelete, onEditScope, isDeleting }: SecretRowProps) {
  return (
    <div className="flex items-center gap-3 px-3 py-2.5 border rounded-md text-sm">
      <Lock className="h-4 w-4 text-muted-foreground shrink-0" />
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2 flex-wrap">
          <span className="font-mono font-medium truncate">{entry.key}</span>
          <AccessScopeBadge accessScope={entry.access_scope} />
        </div>
        <div className="flex gap-3 mt-0.5 text-xs text-muted-foreground">
          <span>Created {formatDate(entry.created_at)}</span>
          <span>Updated {formatDate(entry.updated_at)}</span>
        </div>
      </div>
      <Button
        size="sm"
        variant="ghost"
        className="h-7 w-7 p-0 text-muted-foreground hover:text-foreground shrink-0"
        onClick={() => onEditScope(entry)}
        title="Edit access scope"
      >
        <Pencil className="h-3.5 w-3.5" />
      </Button>
      <Button
        size="sm"
        variant="ghost"
        className="h-7 w-7 p-0 text-muted-foreground hover:text-destructive shrink-0"
        disabled={isDeleting}
        onClick={() => onDelete(entry.key)}
        title="Delete secret"
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

function formatDate(iso?: string | null): string {
  if (!iso) return '—'
  try {
    return new Date(iso).toLocaleString()
  } catch {
    return iso
  }
}

// ─── AddSecretDialog ───────────────────────────────────────────────────────────

interface AddSecretDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

function AddSecretDialog({ open, onOpenChange }: AddSecretDialogProps) {
  const queryClient = useQueryClient()
  const [key, setKey] = useState('')
  const [value, setValue] = useState('')
  const [accessType, setAccessType] = useState('global')
  const [accessTarget, setAccessTarget] = useState('')
  const [keyError, setKeyError] = useState('')
  const [valueError, setValueError] = useState('')

  const { data: personas = [] } = useAgentList()
  const { data: plugins = [] } = usePluginList()
  const { data: providers = [] } = useProviderList()

  const { mutate: setSecret, isPending } = useMutation({
    mutationFn: (args: { key: string; value: string; access_scope: string }) =>
      mcpCall('secret', { action: 'set', key: args.key, value: args.value, scope: 'global', access_scope: args.access_scope }),
  })

  function resetForm() {
    setKey('')
    setValue('')
    setAccessType('global')
    setAccessTarget('')
    setKeyError('')
    setValueError('')
  }

  function handleOpenChange(next: boolean) {
    if (!next) resetForm()
    onOpenChange(next)
  }

  function buildAccessScope(): string {
    switch (accessType) {
      case 'persona':
        return `persona:${accessTarget}`
      case 'plugin':
        return `plugin:${accessTarget}`
      case 'provider':
        return `provider:${accessTarget}`
      case 'system':
        return 'system'
      default:
        return 'global'
    }
  }

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    let valid = true

    if (!key.trim()) {
      setKeyError('Secret key is required')
      valid = false
    } else if (!/^[a-zA-Z0-9_.-]+$/.test(key)) {
      setKeyError('Key must be alphanumeric (with _ . -)')
      valid = false
    } else {
      setKeyError('')
    }

    if (!value.trim()) {
      setValueError('Secret value is required')
      valid = false
    } else {
      setValueError('')
    }

    if ((accessType === 'persona' || accessType === 'plugin' || accessType === 'provider') && !accessTarget) {
      toast({ title: 'Select a target', description: `Select a ${accessType} for access control.`, variant: 'destructive' })
      return
    }

    if (!valid) return

    setSecret(
      { key: key.trim(), value: value.trim(), access_scope: buildAccessScope() },
      {
        onSuccess: () => {
          toast({ title: 'Secret stored', description: `Secret "${key}" has been saved.` })
          void queryClient.invalidateQueries({ queryKey: ['secret-entries'] })
          handleOpenChange(false)
        },
        onError: (err) =>
          toast({ title: 'Save failed', description: (err as Error).message, variant: 'destructive' }),
      },
    )
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>Add Secret</DialogTitle>
          <DialogDescription>
            Store an encrypted secret. Choose who can access it.
          </DialogDescription>
        </DialogHeader>

        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="space-y-1.5">
            <Label htmlFor="sec-key">Key *</Label>
            <Input
              id="sec-key"
              value={key}
              onChange={(e) => setKey(e.target.value)}
              placeholder="e.g. DISCORD_BOT_TOKEN"
              className="font-mono"
              autoFocus
            />
            {keyError && <p className="text-xs text-destructive">{keyError}</p>}
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="sec-value">Value *</Label>
            <Input
              id="sec-value"
              type="password"
              value={value}
              onChange={(e) => setValue(e.target.value)}
              placeholder="Secret value..."
              className="font-mono"
            />
            {valueError && <p className="text-xs text-destructive">{valueError}</p>}
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="sec-access">Access Scope</Label>
            <Select value={accessType} onValueChange={(v) => { setAccessType(v); setAccessTarget('') }}>
              <SelectTrigger id="sec-access">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="global">
                  <div className="flex items-center gap-2">
                    <Globe className="h-3.5 w-3.5" />
                    Global — Any persona or plugin
                  </div>
                </SelectItem>
                <SelectItem value="persona">
                  <div className="flex items-center gap-2">
                    <User className="h-3.5 w-3.5" />
                    Persona — Specific agent only
                  </div>
                </SelectItem>
                <SelectItem value="plugin">
                  <div className="flex items-center gap-2">
                    <Puzzle className="h-3.5 w-3.5" />
                    Plugin — Specific plugin only
                  </div>
                </SelectItem>
                <SelectItem value="provider">
                  <div className="flex items-center gap-2">
                    <Shield className="h-3.5 w-3.5" />
                    Provider — Specific LLM provider only
                  </div>
                </SelectItem>
                <SelectItem value="system">
                  <div className="flex items-center gap-2">
                    <Server className="h-3.5 w-3.5" />
                    System — Internal use only
                  </div>
                </SelectItem>
              </SelectContent>
            </Select>
            <p className="text-xs text-muted-foreground">
              {accessType === 'global' && 'Accessible by all agents and plugins.'}
              {accessType === 'persona' && 'Only the selected persona can read this secret.'}
              {accessType === 'plugin' && 'Only the selected plugin can access this secret. Identified by source hash.'}
              {accessType === 'provider' && 'Only the selected LLM provider can use this secret (e.g., API keys).'}
              {accessType === 'system' && 'Reserved for internal Hyperax operations. Not accessible via MCP tools.'}
            </p>
          </div>

          {accessType === 'persona' && (
            <div className="space-y-1.5">
              <Label htmlFor="sec-persona">Persona</Label>
              <Select value={accessTarget} onValueChange={setAccessTarget}>
                <SelectTrigger id="sec-persona">
                  <SelectValue placeholder="Select persona..." />
                </SelectTrigger>
                <SelectContent>
                  {personas.map((p) => (
                    <SelectItem key={p.id} value={p.id}>{p.name}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          )}

          {accessType === 'plugin' && (
            <div className="space-y-1.5">
              <Label htmlFor="sec-plugin">Plugin</Label>
              <Select value={accessTarget} onValueChange={setAccessTarget}>
                <SelectTrigger id="sec-plugin">
                  <SelectValue placeholder="Select plugin..." />
                </SelectTrigger>
                <SelectContent>
                  {plugins.map((p) => (
                    <SelectItem key={p.id} value={p.source_hash || p.name}>
                      {p.name}{p.source_hash && <span className="text-muted-foreground ml-1 font-mono text-xs">({p.source_hash})</span>}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          )}

          {accessType === 'provider' && (
            <div className="space-y-1.5">
              <Label htmlFor="sec-provider">Provider</Label>
              <Select value={accessTarget} onValueChange={setAccessTarget}>
                <SelectTrigger id="sec-provider">
                  <SelectValue placeholder="Select provider..." />
                </SelectTrigger>
                <SelectContent>
                  {providers.map((p) => (
                    <SelectItem key={p.id} value={p.id}>
                      {p.name} <span className="text-muted-foreground ml-1 text-xs">({p.kind})</span>
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          )}

          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => handleOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={isPending}>
              {isPending ? (
                <>
                  <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                  Saving...
                </>
              ) : (
                'Store Secret'
              )}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

// ─── EditScopeDialog ───────────────────────────────────────────────────────────

interface EditScopeDialogProps {
  entry: SecretEntry | null
  open: boolean
  onOpenChange: (open: boolean) => void
}

function EditScopeDialog({ entry, open, onOpenChange }: EditScopeDialogProps) {
  const queryClient = useQueryClient()
  const [accessType, setAccessType] = useState('global')
  const [accessTarget, setAccessTarget] = useState('')

  const { data: personas = [] } = useAgentList()
  const { data: plugins = [] } = usePluginList()
  const { data: providers = [] } = useProviderList()

  const { mutate: updateScope, isPending } = useMutation({
    mutationFn: (args: { key: string; scope: string; access_scope: string }) =>
      mcpCall('secret', { action: 'update_scope', ...args }),
  })

  function parseAccessScope(scope: string): [string, string] {
    if (!scope || scope === 'global') return ['global', '']
    if (scope === 'system') return ['system', '']
    if (scope.startsWith('persona:')) return ['persona', scope.replace('persona:', '')]
    if (scope.startsWith('plugin:')) return ['plugin', scope.replace('plugin:', '')]
    if (scope.startsWith('provider:')) return ['provider', scope.replace('provider:', '')]
    return ['global', '']
  }

  function handleOpenChange(next: boolean) {
    if (next && entry) {
      const [type, target] = parseAccessScope(entry.access_scope)
      setAccessType(type)
      setAccessTarget(target)
    }
    onOpenChange(next)
  }

  function buildAccessScope(): string {
    switch (accessType) {
      case 'persona':
        return `persona:${accessTarget}`
      case 'plugin':
        return `plugin:${accessTarget}`
      case 'provider':
        return `provider:${accessTarget}`
      case 'system':
        return 'system'
      default:
        return 'global'
    }
  }

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (!entry) return

    if ((accessType === 'persona' || accessType === 'plugin' || accessType === 'provider') && !accessTarget) {
      toast({ title: 'Select a target', description: `Select a ${accessType} for access control.`, variant: 'destructive' })
      return
    }

    const newScope = buildAccessScope()
    updateScope(
      { key: entry.key, scope: entry.scope || 'global', access_scope: newScope },
      {
        onSuccess: () => {
          toast({ title: 'Access scope updated', description: `Secret "${entry.key}" now has access scope: ${newScope}` })
          void queryClient.invalidateQueries({ queryKey: ['secret-entries'] })
          onOpenChange(false)
        },
        onError: (err) =>
          toast({ title: 'Update failed', description: (err as Error).message, variant: 'destructive' }),
      },
    )
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>Edit Access Scope</DialogTitle>
          <DialogDescription>
            Change who can access the secret <span className="font-mono font-medium">{entry?.key}</span>.
          </DialogDescription>
        </DialogHeader>

        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="space-y-1.5">
            <Label htmlFor="edit-access">Access Scope</Label>
            <Select value={accessType} onValueChange={(v) => { setAccessType(v); setAccessTarget('') }}>
              <SelectTrigger id="edit-access">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="global">
                  <div className="flex items-center gap-2">
                    <Globe className="h-3.5 w-3.5" />
                    Global — Any persona or plugin
                  </div>
                </SelectItem>
                <SelectItem value="persona">
                  <div className="flex items-center gap-2">
                    <User className="h-3.5 w-3.5" />
                    Persona — Specific agent only
                  </div>
                </SelectItem>
                <SelectItem value="plugin">
                  <div className="flex items-center gap-2">
                    <Puzzle className="h-3.5 w-3.5" />
                    Plugin — Specific plugin only
                  </div>
                </SelectItem>
                <SelectItem value="provider">
                  <div className="flex items-center gap-2">
                    <Shield className="h-3.5 w-3.5" />
                    Provider — Specific LLM provider only
                  </div>
                </SelectItem>
                <SelectItem value="system">
                  <div className="flex items-center gap-2">
                    <Server className="h-3.5 w-3.5" />
                    System — Internal use only
                  </div>
                </SelectItem>
              </SelectContent>
            </Select>
            <p className="text-xs text-muted-foreground">
              {accessType === 'global' && 'Accessible by all agents and plugins.'}
              {accessType === 'persona' && 'Only the selected persona can read this secret.'}
              {accessType === 'plugin' && 'Only the selected plugin can access this secret.'}
              {accessType === 'provider' && 'Only the selected LLM provider can use this secret (e.g., API keys).'}
              {accessType === 'system' && 'Reserved for internal Hyperax operations.'}
            </p>
          </div>

          {accessType === 'persona' && (
            <div className="space-y-1.5">
              <Label htmlFor="edit-persona">Persona</Label>
              <Select value={accessTarget} onValueChange={setAccessTarget}>
                <SelectTrigger id="edit-persona">
                  <SelectValue placeholder="Select persona..." />
                </SelectTrigger>
                <SelectContent>
                  {personas.map((p) => (
                    <SelectItem key={p.id} value={p.id}>{p.name}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          )}

          {accessType === 'plugin' && (
            <div className="space-y-1.5">
              <Label htmlFor="edit-plugin">Plugin</Label>
              <Select value={accessTarget} onValueChange={setAccessTarget}>
                <SelectTrigger id="edit-plugin">
                  <SelectValue placeholder="Select plugin..." />
                </SelectTrigger>
                <SelectContent>
                  {plugins.map((p) => (
                    <SelectItem key={p.id} value={p.source_hash || p.name}>
                      {p.name}{p.source_hash && <span className="text-muted-foreground ml-1 font-mono text-xs">({p.source_hash})</span>}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          )}

          {accessType === 'provider' && (
            <div className="space-y-1.5">
              <Label htmlFor="edit-provider">Provider</Label>
              <Select value={accessTarget} onValueChange={setAccessTarget}>
                <SelectTrigger id="edit-provider">
                  <SelectValue placeholder="Select provider..." />
                </SelectTrigger>
                <SelectContent>
                  {providers.map((p) => (
                    <SelectItem key={p.id} value={p.id}>
                      {p.name} <span className="text-muted-foreground ml-1 text-xs">({p.kind})</span>
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          )}

          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={isPending}>
              {isPending ? (
                <>
                  <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                  Updating...
                </>
              ) : (
                'Update Scope'
              )}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

// ─── SecretsPage ───────────────────────────────────────────────────────────────

export function SecretsPage() {
  const [addOpen, setAddOpen] = useState(false)
  const [editEntry, setEditEntry] = useState<SecretEntry | null>(null)
  const [deletingKey, setDeletingKey] = useState<string | null>(null)
  const queryClient = useQueryClient()

  const { data: entries, isLoading, error, refetch } = useSecretEntries()

  const { mutate: deleteSecret } = useMutation({
    mutationFn: (key: string) => mcpCall('secret', { action: 'delete', key, scope: 'global' }),
  })

  function handleDelete(key: string) {
    setDeletingKey(key)
    deleteSecret(key, {
      onSuccess: () => {
        toast({ title: 'Secret deleted', description: `Secret "${key}" has been removed.` })
        void queryClient.invalidateQueries({ queryKey: ['secret-entries'] })
      },
      onError: (err) =>
        toast({ title: 'Delete failed', description: (err as Error).message, variant: 'destructive' }),
      onSettled: () => setDeletingKey(null),
    })
  }

  if (isLoading) {
    return (
      <div className="p-6 space-y-6">
        <PageHeader title="Secrets" description="Manage encrypted secrets with access control." />
        <LoadingState message="Loading secrets..." />
      </div>
    )
  }

  if (error) {
    return (
      <div className="p-6 space-y-6">
        <PageHeader title="Secrets" description="Manage encrypted secrets with access control." />
        <ErrorState error={error as Error} onRetry={() => void refetch()} />
      </div>
    )
  }

  const secretList = entries ?? []

  return (
    <div className="p-6 space-y-6">
      <PageHeader
        title="Secrets"
        description="Store and manage encrypted secrets. Each secret can be scoped to a specific persona, plugin, or made globally available."
      >
        <Button size="sm" onClick={() => setAddOpen(true)}>
          <PlusCircle className="h-4 w-4 mr-2" />
          Add Secret
        </Button>
      </PageHeader>

      {secretList.length === 0 ? (
        <EmptyState
          icon={Shield}
          title="No secrets stored"
          description="Add your first secret to get started. Secrets are encrypted at rest and scoped to specific consumers."
          action={
            <Button size="sm" onClick={() => setAddOpen(true)}>
              Add your first secret
            </Button>
          }
        />
      ) : (
        <div className="space-y-2">
          {secretList.map((entry) => (
            <SecretRow
              key={`${entry.key}-${entry.scope}`}
              entry={entry}
              onDelete={handleDelete}
              onEditScope={setEditEntry}
              isDeleting={deletingKey === entry.key}
            />
          ))}
        </div>
      )}

      <AddSecretDialog open={addOpen} onOpenChange={setAddOpen} />
      <EditScopeDialog
        entry={editEntry}
        open={editEntry !== null}
        onOpenChange={(open) => { if (!open) setEditEntry(null) }}
      />
    </div>
  )
}
