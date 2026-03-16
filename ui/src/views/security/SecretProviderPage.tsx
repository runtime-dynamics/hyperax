import { useState, useEffect } from 'react'
import { Loader2, CheckCircle2, XCircle, TriangleAlert } from 'lucide-react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { mcpCall } from '@/lib/mcp-client'
import { PageHeader } from '@/components/domain/page-header'
import { LoadingState } from '@/components/domain/loading-state'
import { ErrorState } from '@/components/domain/error-state'
import { Button } from '@/components/ui/button'
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
import { Badge } from '@/components/ui/badge'
import { toast } from '@/components/ui/use-toast'

// ─── Data fetching ─────────────────────────────────────────────────────────────

interface ProviderListResult {
  providers: string[]
  active: string
}

function useSecretProviders() {
  return useQuery({
    queryKey: ['secret-providers'],
    queryFn: () =>
      mcpCall<ProviderListResult>('configure_secret_provider', { action: 'list' }),
    retry: false,
  })
}

function useSwitchProvider() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (provider: string) =>
      mcpCall<{ status: string } | string>('configure_secret_provider', {
        action: 'switch',
        provider,
      }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['secret-providers'] }),
  })
}

function useTestProvider() {
  return useMutation({
    mutationFn: async (provider: string) => {
      const result = await mcpCall<{ provider: string; status: string; error?: string }>(
        'configure_secret_provider',
        { action: 'health', provider },
      )
      return {
        healthy: result?.status === 'healthy',
        message: result?.error ?? (result?.status === 'healthy' ? 'Connected' : 'Connection failed'),
      }
    },
  })
}

// ─── HealthIndicator ──────────────────────────────────────────────────────────

function HealthIndicator({ healthy, message, testing }: { healthy?: boolean; message?: string; testing?: boolean }) {
  if (testing) {
    return (
      <div className="flex items-center gap-2 text-sm text-muted-foreground">
        <Loader2 className="h-4 w-4 animate-spin" />
        Testing connection...
      </div>
    )
  }
  if (healthy === undefined) return null
  return (
    <div className={`flex items-center gap-2 text-sm ${healthy ? 'text-green-600 dark:text-green-400' : 'text-destructive'}`}>
      {healthy ? <CheckCircle2 className="h-4 w-4 shrink-0" /> : <XCircle className="h-4 w-4 shrink-0" />}
      <span>{message ?? (healthy ? 'Connected' : 'Connection failed')}</span>
    </div>
  )
}

// ─── ProviderSwitchDialog ─────────────────────────────────────────────────────

function ProviderSwitchDialog({
  open, from, to, onConfirm, onCancel, isPending,
}: {
  open: boolean; from: string; to: string; onConfirm: () => void; onCancel: () => void; isPending: boolean
}) {
  return (
    <Dialog open={open} onOpenChange={(o) => { if (!o) onCancel() }}>
      <DialogContent className="max-w-sm">
        <DialogHeader>
          <DialogTitle>Switch Secret Provider?</DialogTitle>
          <DialogDescription>
            You are switching from <strong>{from}</strong> to <strong>{to}</strong>.
            Existing secrets stored in the current provider will not be automatically migrated.
          </DialogDescription>
        </DialogHeader>
        <div className="flex items-start gap-2 rounded-md border border-amber-500/50 bg-amber-500/10 px-3 py-2.5 text-sm">
          <TriangleAlert className="h-4 w-4 text-amber-500 shrink-0 mt-0.5" />
          <p className="text-amber-700 dark:text-amber-400">
            This is a critical change. Ensure the new provider is reachable and your secrets are
            backed up before switching.
          </p>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onCancel} disabled={isPending}>Cancel</Button>
          <Button variant="destructive" onClick={onConfirm} disabled={isPending}>
            {isPending ? <><Loader2 className="h-4 w-4 mr-2 animate-spin" />Switching...</> : 'Switch Provider'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// ─── SecretProviderPage ───────────────────────────────────────────────────────

export function SecretProviderPage() {
  const { data, isLoading, error, refetch } = useSecretProviders()
  const { mutate: switchProvider, isPending: isSwitching } = useSwitchProvider()
  const { mutate: testProvider, isPending: isTesting } = useTestProvider()

  const [selected, setSelected] = useState<string>('')
  const [healthResult, setHealthResult] = useState<{ healthy: boolean; message: string } | null>(null)
  const [switchDialog, setSwitchDialog] = useState(false)

  const providers = data?.providers ?? []
  const active = data?.active ?? 'local'

  useEffect(() => {
    if (data) setSelected(data.active ?? 'local')
  }, [data])

  function handleTest() {
    testProvider(selected, {
      onSuccess: (result) => setHealthResult(result),
      onError: (err) => setHealthResult({ healthy: false, message: (err as Error).message }),
    })
  }

  function handleSave() {
    if (selected !== active) {
      setSwitchDialog(true)
      return
    }
    toast({ title: 'No change', description: 'The selected provider is already active.' })
  }

  function doSwitch() {
    setSwitchDialog(false)
    switchProvider(selected, {
      onSuccess: () => {
        toast({ title: 'Provider switched', description: `Active provider is now "${selected}".` })
        setHealthResult(null)
        void refetch()
      },
      onError: (err) =>
        toast({ title: 'Switch failed', description: (err as Error).message, variant: 'destructive' }),
    })
  }

  if (isLoading) {
    return (
      <div className="p-6 space-y-6">
        <PageHeader title="Secret Provider" description="Configure the backend used to store encrypted secrets." />
        <LoadingState message="Loading providers..." />
      </div>
    )
  }

  if (error) {
    return (
      <div className="p-6 space-y-6">
        <PageHeader title="Secret Provider" description="Configure the backend used to store encrypted secrets." />
        <ErrorState error={error as Error} onRetry={() => void refetch()} />
      </div>
    )
  }

  return (
    <div className="p-6 space-y-6 max-w-xl">
      <PageHeader
        title="Secret Provider"
        description="Choose the backend used to store encrypted secrets. Additional providers are registered via plugins."
      />

      <div className="space-y-1.5">
        <Label htmlFor="prov-select">Provider</Label>
        <div className="flex items-center gap-3">
          <Select value={selected} onValueChange={(v) => { setSelected(v); setHealthResult(null) }}>
            <SelectTrigger id="prov-select" className="w-[220px]">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {providers.map((p) => (
                <SelectItem key={p} value={p}>
                  {p === 'local' ? 'Local (SQLite)' : p}
                  {p === active ? ' (active)' : ''}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>

          <Badge variant="outline" className="gap-1.5">
            <span className="h-1.5 w-1.5 rounded-full bg-green-500" />
            Active: {active}
          </Badge>
        </div>
      </div>

      {selected === 'local' && (
        <p className="text-sm text-muted-foreground">
          The local provider uses SQLite with AES-GCM encryption. No additional configuration is required.
        </p>
      )}

      {selected !== 'local' && (
        <p className="text-sm text-muted-foreground">
          This provider is managed by a plugin. Configuration is handled through the plugin's settings.
        </p>
      )}

      {providers.length <= 1 && (
        <div className="rounded-md border border-muted bg-muted/50 px-4 py-3 text-sm text-muted-foreground">
          Only the built-in local provider is available. Install a secret provider plugin
          (e.g., Vault or 1Password) to add more options.
        </div>
      )}

      <div className="flex items-center gap-3 pt-2">
        <Button variant="outline" size="sm" onClick={handleTest} disabled={isTesting || !selected}>
          {isTesting ? <><Loader2 className="h-4 w-4 mr-2 animate-spin" />Testing...</> : 'Test Connection'}
        </Button>

        {selected !== active && (
          <Button size="sm" onClick={handleSave} disabled={isSwitching}>
            {isSwitching ? <><Loader2 className="h-4 w-4 mr-2 animate-spin" />Switching...</> : 'Switch Provider'}
          </Button>
        )}
      </div>

      <HealthIndicator healthy={healthResult?.healthy} message={healthResult?.message} testing={isTesting} />

      <ProviderSwitchDialog
        open={switchDialog}
        from={active}
        to={selected}
        onConfirm={doSwitch}
        onCancel={() => setSwitchDialog(false)}
        isPending={isSwitching}
      />
    </div>
  )
}
