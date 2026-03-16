import { useState } from 'react'
import { PlusCircle, Server, Star, Globe, Lock, Pencil, Trash2, Zap, Loader2 } from 'lucide-react'
import {
  useRestProviders,
  useRestCreateProvider,
  useRestUpdateProvider,
  useRestDeleteProvider,
  useRestSetDefaultProvider,
  useTestProviderConnection,
  type Provider,
  type CreateProviderArgs,
  type TestResult,
} from '@/services/restProviderService'
import { parseModels } from '@/services/providerService'
import { providerBrandColors } from '@/lib/provider-colors'
import { cn } from '@/lib/utils'
import { PageHeader } from '@/components/domain/page-header'
import { LoadingState } from '@/components/domain/loading-state'
import { ErrorState } from '@/components/domain/error-state'
import { EmptyState } from '@/components/domain/empty-state'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { toast } from '@/components/ui/use-toast'
import { ProviderDialog } from './ProviderDialog'

// ─── Constants ─────────────────────────────────────────────────────────────

const KIND_LABELS: Record<string, string> = {
  openai: 'OpenAI',
  anthropic: 'Anthropic',
  google: 'Google Gemini',
  ollama: 'Ollama',
  azure: 'Azure OpenAI',
  bedrock: 'AWS Bedrock',
  custom: 'OpenAI Compatible API',
}

// ─── ProviderCard ────────────────────────────────────────────────────────────

interface ProviderCardProps {
  provider: Provider
  onEdit: (provider: Provider) => void
  onDelete: (id: string) => void
  onSetDefault: (id: string) => void
  onTestConnection: (id: string) => void
  isTesting: boolean
}

function ProviderCard({ provider, onEdit, onDelete, onSetDefault, onTestConnection, isTesting }: ProviderCardProps) {
  const models = parseModels(provider.models)
  const kindLabel = KIND_LABELS[provider.kind] ?? provider.kind
  const brandColors = providerBrandColors[provider.kind]

  return (
    <Card className={cn(
      brandColors?.bg,
      provider.is_default && 'ring-2 ring-primary/30',
      !provider.is_enabled && 'ring-2 ring-destructive/30 opacity-80',
    )}>
      <CardHeader className="pb-2">
        <div className="flex items-start justify-between gap-2">
          <div className="flex items-center gap-2 min-w-0">
            <div className={cn('h-8 w-8 rounded-full flex items-center justify-center shrink-0', brandColors?.icon ?? 'bg-primary/10')}>
              <Server className={cn('h-4 w-4', !brandColors && 'text-primary')} />
            </div>
            <div className="min-w-0">
              <div className="flex items-center gap-1.5">
                <p className="font-semibold text-sm truncate">{provider.name}</p>
                {provider.is_default && (
                  <Star className="h-3.5 w-3.5 text-amber-500 shrink-0" />
                )}
              </div>
              <p className="text-xs text-muted-foreground truncate">{kindLabel}</p>
            </div>
          </div>
          <div className="flex items-center gap-1 shrink-0">
            {!provider.is_default && (
              <Button
                variant="ghost"
                size="icon"
                className="h-7 w-7 text-muted-foreground hover:text-amber-500"
                onClick={() => onSetDefault(provider.id)}
                title="Set as default"
              >
                <Star className="h-3.5 w-3.5" />
                <span className="sr-only">Set {provider.name} as default</span>
              </Button>
            )}
            <Button
              variant="ghost"
              size="icon"
              className="h-7 w-7 text-muted-foreground hover:text-green-600"
              onClick={() => onTestConnection(provider.id)}
              disabled={isTesting}
              title="Test connection"
            >
              {isTesting
                ? <Loader2 className="h-3.5 w-3.5 animate-spin" />
                : <Zap className="h-3.5 w-3.5" />
              }
              <span className="sr-only">Test connection for {provider.name}</span>
            </Button>
            <Button
              variant="ghost"
              size="icon"
              className="h-7 w-7"
              onClick={() => onEdit(provider)}
            >
              <Pencil className="h-3.5 w-3.5" />
              <span className="sr-only">Edit {provider.name}</span>
            </Button>
            <Button
              variant="ghost"
              size="icon"
              className="h-7 w-7 text-muted-foreground hover:text-destructive"
              onClick={() => onDelete(provider.id)}
            >
              <Trash2 className="h-3.5 w-3.5" />
              <span className="sr-only">Delete {provider.name}</span>
            </Button>
          </div>
        </div>
      </CardHeader>
      <CardContent className="space-y-2">
        <div className="flex items-center gap-1.5 text-xs text-muted-foreground min-w-0">
          <Globe className="h-3.5 w-3.5 shrink-0" />
          <span className="truncate font-mono">{provider.base_url}</span>
        </div>
        {provider.secret_key_ref && (
          <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <Lock className="h-3.5 w-3.5 shrink-0" />
            <span className="truncate">{provider.secret_key_ref}</span>
          </div>
        )}
        {models.length > 0 && (
          <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <span className="font-medium text-foreground">{models.length} model{models.length === 1 ? '' : 's'}</span>
            {provider.updated_at && provider.updated_at !== '0001-01-01T00:00:00Z' && (
              <span>· refreshed {new Date(provider.updated_at).toLocaleDateString()}</span>
            )}
          </div>
        )}
        <div className="flex items-center gap-1.5 flex-wrap">
          <Badge variant={provider.is_enabled ? 'default' : 'destructive'} className="text-xs">
            {provider.is_enabled ? 'Enabled' : 'Disabled'}
          </Badge>
          {provider.is_default && (
            <Badge variant="outline" className="text-xs border-amber-500/50 text-amber-600">
              Default
            </Badge>
          )}
          <Badge variant="outline" className="text-xs">
            {kindLabel}
          </Badge>
        </div>
      </CardContent>
    </Card>
  )
}

// ─── ProvidersPage ───────────────────────────────────────────────────────────

export function ProvidersPage() {
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editTarget, setEditTarget] = useState<Provider | null>(null)
  const [refreshingId, setRefreshingId] = useState<string | null>(null)

  const { data: providers, isLoading, error, refetch } = useRestProviders()
  const { mutate: createProvider, isPending: isCreating } = useRestCreateProvider()
  const { mutate: updateProvider, isPending: isUpdating } = useRestUpdateProvider()
  const { mutate: deleteProvider } = useRestDeleteProvider()
  const { mutate: setDefault } = useRestSetDefaultProvider()
  const { mutate: testConnection } = useTestProviderConnection()

  const isPending = isCreating || isUpdating

  function handleEdit(provider: Provider) {
    setEditTarget(provider)
    setDialogOpen(true)
  }

  function handleDelete(id: string) {
    const provider = providers?.find((p) => p.id === id)
    if (!provider) return
    if (!confirm(`Delete provider "${provider.name}"? This cannot be undone.`)) return
    deleteProvider(id, {
      onSuccess: () =>
        toast({ title: 'Provider deleted', description: `"${provider.name}" has been removed.` }),
      onError: (err) =>
        toast({ title: 'Delete failed', description: (err as Error).message, variant: 'destructive' }),
    })
  }

  function handleSetDefault(id: string) {
    const provider = providers?.find((p) => p.id === id)
    if (!provider) return
    setDefault(id, {
      onSuccess: () =>
        toast({ title: 'Default updated', description: `"${provider.name}" is now the default provider.` }),
      onError: (err) =>
        toast({ title: 'Update failed', description: (err as Error).message, variant: 'destructive' }),
    })
  }

  function handleTestConnection(id: string) {
    const provider = providers?.find((p) => p.id === id)
    if (!provider) return
    setRefreshingId(id)
    testConnection(id, {
      onSuccess: (result: TestResult) => {
        setRefreshingId(null)
        if (result.success) {
          if (result.message) {
            toast({ title: 'Connection OK', description: result.message })
          } else {
            toast({
              title: 'Connection successful',
              description: `"${provider.name}" is working. Discovered ${result.model_count ?? 0} model${(result.model_count ?? 0) === 1 ? '' : 's'}.`,
            })
          }
          refetch()
        } else {
          toast({
            title: 'Connection failed',
            description: result.error ?? 'Unknown error',
            variant: 'destructive',
          })
        }
      },
      onError: (err) => {
        setRefreshingId(null)
        toast({ title: 'Test failed', description: (err as Error).message, variant: 'destructive' })
      },
    })
  }

  function handleDialogClose(open: boolean) {
    setDialogOpen(open)
    if (!open) setEditTarget(null)
  }

  function handleCreate(
    data: CreateProviderArgs,
    cb: { onSuccess: () => void; onError: (e: Error) => void },
  ) {
    createProvider(data, cb)
  }

  function handleUpdate(
    data: Partial<Provider> & { id: string; api_key?: string },
    cb: { onSuccess: () => void; onError: (e: Error) => void },
  ) {
    updateProvider(data, cb)
  }

  if (isLoading) return <LoadingState message="Loading providers..." />
  if (error) return <ErrorState error={error as Error} onRetry={() => void refetch()} />

  const defaultProvider = providers?.find((p) => p.is_default)

  return (
    <div className="space-y-6 p-6">
      <PageHeader
        title="Providers"
        description="Manage LLM provider configurations for your agents."
      >
        <Button onClick={() => setDialogOpen(true)} size="sm">
          <PlusCircle className="h-4 w-4 mr-2" />
          Add Provider
        </Button>
      </PageHeader>

      {defaultProvider && (
        <p className="text-sm text-muted-foreground">
          Default:{' '}
          <span className="text-foreground font-medium">{defaultProvider.name}</span>
        </p>
      )}

      {!providers || providers.length === 0 ? (
        <EmptyState
          icon={Zap}
          title="No providers configured"
          description="Add an LLM provider to enable agent model access."
          action={
            <Button onClick={() => setDialogOpen(true)} size="sm">
              Add your first provider
            </Button>
          }
        />
      ) : (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
          {providers.map((provider) => (
            <ProviderCard
              key={provider.id}
              provider={provider}
              onEdit={handleEdit}
              onDelete={handleDelete}
              onSetDefault={handleSetDefault}
              onTestConnection={handleTestConnection}
              isTesting={refreshingId === provider.id}
            />
          ))}
        </div>
      )}

      <ProviderDialog
        open={dialogOpen}
        onOpenChange={handleDialogClose}
        editTarget={editTarget}
        onCreate={handleCreate}
        onUpdate={handleUpdate}
        isPending={isPending}
      />
    </div>
  )
}
