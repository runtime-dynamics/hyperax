import { useState } from 'react'
import {
  PlusCircle,
  Loader2,
  Settings2,
  CheckCircle2,
  AlertTriangle,
  XCircle,
  RefreshCw,
  ShieldAlert,
  Flame,
  Cpu,
} from 'lucide-react'
import {
  useAllBudgetStatuses,
  useProviderBudgets,
  useBudgetScopes,
  useSetBudgetThreshold,
  type BudgetStatus,
  type ProviderBudget,
  type SetBudgetThresholdArgs,
  type Provider,
} from '@/services/budgetService'
import { useProviders } from '@/services/providerService'
import { useSessions, type Session } from '@/services/telemetryService'
import {
  useActiveInterjections,
  useResolveInterjection,
  type Interjection,
} from '@/services/interjectionService'
import { PageHeader } from '@/components/domain/page-header'
import { LoadingState } from '@/components/domain/loading-state'
import { ErrorState } from '@/components/domain/error-state'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '@/components/ui/dialog'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { toast } from '@/components/ui/use-toast'
import { cn } from '@/lib/utils'

// ─── Constants ────────────────────────────────────────────────────────────────

const SYSTEM_SCOPES = ['global', 'workspace']
const BUDGET_MONITOR_SOURCE = 'budget.monitor'

// ─── Helpers ─────────────────────────────────────────────────────────────────

function progressBarColor(percent: number): string {
  if (percent >= 95) return 'bg-red-500'
  if (percent >= 80) return 'bg-orange-500'
  if (percent >= 60) return 'bg-yellow-500'
  return 'bg-green-500'
}

function formatUsd(amount: number): string {
  return `$${amount.toFixed(4)}`
}

function formatRelativeTime(iso: string): string {
  const diffMs = Date.now() - new Date(iso).getTime()
  const diffSecs = Math.floor(diffMs / 1000)
  if (diffSecs < 60) return `${diffSecs}s ago`
  const diffMins = Math.floor(diffSecs / 60)
  if (diffMins < 60) return `${diffMins}m ago`
  const diffHours = Math.floor(diffMins / 60)
  if (diffHours < 24) return `${diffHours}h ago`
  return `${Math.floor(diffHours / 24)}d ago`
}

function severityVariant(severity: string): 'default' | 'secondary' | 'destructive' | 'outline' {
  switch (severity) {
    case 'fatal':
    case 'critical':
      return 'destructive'
    case 'warning':
      return 'secondary'
    default:
      return 'outline'
  }
}

function severityIcon(severity: string) {
  switch (severity) {
    case 'fatal':
      return <XCircle className="h-3.5 w-3.5" />
    case 'critical':
      return <AlertTriangle className="h-3.5 w-3.5" />
    default:
      return <ShieldAlert className="h-3.5 w-3.5" />
  }
}

function kindBadgeVariant(kind: string): 'default' | 'secondary' | 'outline' {
  switch (kind.toLowerCase()) {
    case 'anthropic':
      return 'default'
    case 'openai':
      return 'secondary'
    case 'google':
    case 'gemini':
      return 'secondary'
    default:
      return 'outline'
  }
}

// ─── BudgetProgressBar ────────────────────────────────────────────────────────

function BudgetProgressBar({ percent }: { percent: number }) {
  const clamped = Math.min(100, Math.max(0, percent))
  return (
    <div className="w-full bg-muted rounded-full h-2 overflow-hidden">
      <div
        className={cn('h-2 rounded-full transition-all', progressBarColor(clamped))}
        style={{ width: `${clamped}%` }}
        role="progressbar"
        aria-valuenow={clamped}
        aria-valuemin={0}
        aria-valuemax={100}
      />
    </div>
  )
}

// ─── BudgetCard ──────────────────────────────────────────────────────────────

interface BudgetCardProps {
  budget: BudgetStatus
  onSetThreshold: (scope: string) => void
}

function BudgetCard({ budget, onSetThreshold }: BudgetCardProps) {
  const percent = budget.percent_used ?? 0
  const isExhausted = percent >= 100

  return (
    <Card
      className={cn(
        'border transition-colors',
        isExhausted && 'border-red-500/50',
        !isExhausted && percent >= 80 && 'border-orange-500/40',
      )}
    >
      <CardHeader className="pb-2 pt-4 px-4 flex flex-row items-start justify-between gap-2">
        <div className="flex items-center gap-2 min-w-0">
          <CardTitle className="text-sm font-semibold capitalize truncate">{budget.scope}</CardTitle>
          {isExhausted && (
            <Badge variant="destructive" className="text-xs shrink-0">
              Exhausted
            </Badge>
          )}
        </div>
        <Button
          size="sm"
          variant="ghost"
          className="h-6 w-6 p-0 text-muted-foreground hover:text-foreground shrink-0"
          onClick={() => onSetThreshold(budget.scope)}
          title="Set budget threshold"
        >
          <Settings2 className="h-3.5 w-3.5" />
        </Button>
      </CardHeader>
      <CardContent className="px-4 pb-4 space-y-3">
        <div className="flex items-end justify-between gap-2">
          <div>
            <p className="text-2xl font-semibold tabular-nums">{formatUsd(budget.cost)}</p>
            <p className="text-xs text-muted-foreground">used</p>
          </div>
          {budget.threshold > 0 && (
            <div className="text-right">
              <p className="text-sm font-medium tabular-nums">{formatUsd(budget.threshold)}</p>
              <p className="text-xs text-muted-foreground">limit</p>
            </div>
          )}
        </div>

        {budget.threshold > 0 ? (
          <div className="space-y-1">
            <BudgetProgressBar percent={percent} />
            <div className="flex items-center justify-between text-xs text-muted-foreground">
              <span>{Math.round(percent)}% used</span>
              <span>{formatUsd(budget.remaining)} remaining</span>
            </div>
          </div>
        ) : (
          <p className="text-xs text-muted-foreground italic">No limit set</p>
        )}
      </CardContent>
    </Card>
  )
}

// ─── ProviderBudgetCard ───────────────────────────────────────────────────────

interface ProviderBudgetCardProps {
  providerBudget: ProviderBudget
  onSetThreshold: (scope: string) => void
}

function ProviderBudgetCard({ providerBudget, onSetThreshold }: ProviderBudgetCardProps) {
  const { provider, budget } = providerBudget
  const percent = budget.percent_used ?? 0
  const isExhausted = percent >= 100

  return (
    <Card
      className={cn(
        'border transition-colors',
        isExhausted && 'border-red-500/50',
        !isExhausted && percent >= 80 && 'border-orange-500/40',
      )}
    >
      <CardHeader className="pb-2 pt-4 px-4 flex flex-row items-start justify-between gap-2">
        <div className="flex flex-col gap-1 min-w-0">
          <div className="flex items-center gap-2">
            <CardTitle className="text-sm font-semibold truncate">{provider.name}</CardTitle>
            {isExhausted && (
              <Badge variant="destructive" className="text-xs shrink-0">
                Exhausted
              </Badge>
            )}
          </div>
          <div className="flex items-center gap-1.5 flex-wrap">
            <Badge variant={kindBadgeVariant(provider.kind)} className="text-xs capitalize">
              {provider.kind}
            </Badge>
            {provider.is_default && (
              <Badge variant="outline" className="text-xs">
                default
              </Badge>
            )}
          </div>
        </div>
        <Button
          size="sm"
          variant="ghost"
          className="h-6 w-6 p-0 text-muted-foreground hover:text-foreground shrink-0"
          onClick={() => onSetThreshold(budget.scope)}
          title="Set budget threshold"
        >
          <Settings2 className="h-3.5 w-3.5" />
        </Button>
      </CardHeader>
      <CardContent className="px-4 pb-4 space-y-3">
        <div className="flex items-end justify-between gap-2">
          <div>
            <p className="text-2xl font-semibold tabular-nums">{formatUsd(budget.cost)}</p>
            <p className="text-xs text-muted-foreground">used</p>
          </div>
          {budget.threshold > 0 && (
            <div className="text-right">
              <p className="text-sm font-medium tabular-nums">{formatUsd(budget.threshold)}</p>
              <p className="text-xs text-muted-foreground">limit</p>
            </div>
          )}
        </div>

        {budget.threshold > 0 ? (
          <div className="space-y-1">
            <BudgetProgressBar percent={percent} />
            <div className="flex items-center justify-between text-xs text-muted-foreground">
              <span>{Math.round(percent)}% used</span>
              <span>{formatUsd(budget.remaining)} remaining</span>
            </div>
          </div>
        ) : (
          <p className="text-xs text-muted-foreground italic">No limit set</p>
        )}
      </CardContent>
    </Card>
  )
}

// ─── TotalAcrossProviders ─────────────────────────────────────────────────────

function TotalAcrossProviders({ providerBudgets }: { providerBudgets: ProviderBudget[] }) {
  const totalCost = providerBudgets.reduce((sum, pb) => sum + (pb.budget.cost ?? 0), 0)
  const totalThreshold = providerBudgets.reduce((sum, pb) => sum + (pb.budget.threshold ?? 0), 0)
  const remaining = totalThreshold > 0 ? totalThreshold - totalCost : 0
  const percent = totalThreshold > 0 ? (totalCost / totalThreshold) * 100 : 0

  return (
    <Card className="border-dashed">
      <CardHeader className="pb-2 pt-4 px-4">
        <div className="flex items-center gap-2">
          <Cpu className="h-4 w-4 text-muted-foreground" />
          <CardTitle className="text-sm font-semibold">Total Across Providers</CardTitle>
        </div>
      </CardHeader>
      <CardContent className="px-4 pb-4 space-y-3">
        <div className="flex items-end justify-between gap-2">
          <div>
            <p className="text-2xl font-semibold tabular-nums">{formatUsd(totalCost)}</p>
            <p className="text-xs text-muted-foreground">combined spend</p>
          </div>
          {totalThreshold > 0 && (
            <div className="text-right">
              <p className="text-sm font-medium tabular-nums">{formatUsd(totalThreshold)}</p>
              <p className="text-xs text-muted-foreground">combined limit</p>
            </div>
          )}
        </div>
        {totalThreshold > 0 ? (
          <div className="space-y-1">
            <BudgetProgressBar percent={percent} />
            <div className="flex items-center justify-between text-xs text-muted-foreground">
              <span>{Math.round(percent)}% used</span>
              <span>{formatUsd(remaining)} remaining</span>
            </div>
          </div>
        ) : (
          <p className="text-xs text-muted-foreground italic">No combined limit set</p>
        )}
      </CardContent>
    </Card>
  )
}

// ─── SetThresholdDialog ───────────────────────────────────────────────────────

interface SetThresholdDialogProps {
  open: boolean
  initialScope: string
  providers: Provider[]
  onOpenChange: (open: boolean) => void
  onSet: (
    args: SetBudgetThresholdArgs,
    cb: { onSuccess: () => void; onError: (e: Error) => void },
  ) => void
  isPending: boolean
}

function SetThresholdDialog({
  open,
  initialScope,
  providers,
  onOpenChange,
  onSet,
  isPending,
}: SetThresholdDialogProps) {
  const [scopeMode, setScopeMode] = useState<'provider' | 'custom'>(() =>
    initialScope.startsWith('provider:') ? 'provider' : 'custom',
  )
  const [selectedProvider, setSelectedProvider] = useState<string>(() => {
    if (initialScope.startsWith('provider:')) return initialScope.slice('provider:'.length)
    return providers[0]?.id ?? ''
  })
  const [customScope, setCustomScope] = useState(() =>
    initialScope.startsWith('provider:') ? '' : initialScope,
  )
  const [threshold, setThreshold] = useState('')
  const [thresholdError, setThresholdError] = useState('')
  const [scopeError, setScopeError] = useState('')

  function resetForm() {
    setScopeMode(initialScope.startsWith('provider:') ? 'provider' : 'custom')
    setSelectedProvider(
      initialScope.startsWith('provider:') ? initialScope.slice('provider:'.length) : (providers[0]?.id ?? ''),
    )
    setCustomScope(initialScope.startsWith('provider:') ? '' : initialScope)
    setThreshold('')
    setThresholdError('')
    setScopeError('')
  }

  function handleOpenChange(next: boolean) {
    if (!next) resetForm()
    onOpenChange(next)
  }

  function resolvedScope(): string {
    if (scopeMode === 'provider') return `provider:${selectedProvider}`
    return customScope.trim()
  }

  function validate(): boolean {
    let valid = true
    if (scopeMode === 'provider' && !selectedProvider) {
      setScopeError('Select a provider')
      valid = false
    } else if (scopeMode === 'custom' && !customScope.trim()) {
      setScopeError('Scope is required')
      valid = false
    } else {
      setScopeError('')
    }
    const parsed = parseFloat(threshold)
    if (!threshold.trim() || isNaN(parsed) || parsed <= 0) {
      setThresholdError('Enter a valid positive number')
      valid = false
    } else {
      setThresholdError('')
    }
    return valid
  }

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (!validate()) return
    const scope = resolvedScope()
    onSet(
      { scope, threshold: parseFloat(threshold) },
      {
        onSuccess: () => {
          toast({ title: 'Budget threshold set', description: `Threshold for "${scope}" updated.` })
          handleOpenChange(false)
        },
        onError: (err) =>
          toast({
            title: 'Failed to set threshold',
            description: err.message,
            variant: 'destructive',
          }),
      },
    )
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="max-w-sm">
        <DialogHeader>
          <DialogTitle>Set Budget Threshold</DialogTitle>
          <DialogDescription>
            Define a spending threshold in USD. The budget monitor will trigger an interjection when
            the limit is exceeded.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="space-y-4">
          {/* Scope mode toggle */}
          <div className="space-y-1.5">
            <Label>Scope Type</Label>
            <div className="flex gap-2">
              <Button
                type="button"
                size="sm"
                variant={scopeMode === 'provider' ? 'default' : 'outline'}
                onClick={() => setScopeMode('provider')}
                disabled={providers.length === 0}
              >
                Provider
              </Button>
              <Button
                type="button"
                size="sm"
                variant={scopeMode === 'custom' ? 'default' : 'outline'}
                onClick={() => setScopeMode('custom')}
              >
                Custom
              </Button>
            </div>
          </div>

          {scopeMode === 'provider' && providers.length > 0 ? (
            <div className="space-y-1.5">
              <Label htmlFor="bt-provider">Provider *</Label>
              <Select value={selectedProvider} onValueChange={setSelectedProvider}>
                <SelectTrigger id="bt-provider">
                  <SelectValue placeholder="Select a provider" />
                </SelectTrigger>
                <SelectContent>
                  {providers.map((p) => (
                    <SelectItem key={p.id} value={p.id}>
                      <span className="flex items-center gap-2">
                        <span>{p.name}</span>
                        <span className="text-muted-foreground text-xs capitalize">({p.kind})</span>
                      </span>
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              {scopeError && <p className="text-xs text-destructive">{scopeError}</p>}
            </div>
          ) : (
            <div className="space-y-1.5">
              <Label htmlFor="bt-scope">Scope *</Label>
              <Input
                id="bt-scope"
                value={customScope}
                onChange={(e) => setCustomScope(e.target.value)}
                placeholder="global"
                autoFocus
              />
              {scopeError && <p className="text-xs text-destructive">{scopeError}</p>}
            </div>
          )}

          <div className="space-y-1.5">
            <Label htmlFor="bt-threshold">Threshold (USD) *</Label>
            <Input
              id="bt-threshold"
              type="number"
              min="0.01"
              step="0.01"
              value={threshold}
              onChange={(e) => setThreshold(e.target.value)}
              placeholder="10.00"
            />
            {thresholdError && <p className="text-xs text-destructive">{thresholdError}</p>}
          </div>
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
                'Set Threshold'
              )}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

// ─── ResolveInterjectionDialog ────────────────────────────────────────────────

interface ResolveInterjectionDialogProps {
  interjection: Interjection | null
  onOpenChange: (open: boolean) => void
}

function ResolveInterjectionDialog({
  interjection,
  onOpenChange,
}: ResolveInterjectionDialogProps) {
  const open = !!interjection
  const [action, setAction] = useState('resume')
  const [resolution, setResolution] = useState('')
  const [resolutionError, setResolutionError] = useState('')

  const { mutate: resolve, isPending } = useResolveInterjection()

  function handleOpenChange(next: boolean) {
    if (!next) {
      setAction('resume')
      setResolution('')
      setResolutionError('')
    }
    onOpenChange(next)
  }

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (!interjection) return
    if (!resolution.trim()) {
      setResolutionError('Resolution notes are required')
      return
    }
    setResolutionError('')

    resolve(
      { id: interjection.id, action, resolution: resolution.trim() },
      {
        onSuccess: () => {
          toast({
            title: 'Interjection resolved',
            description: `Budget interjection for "${interjection.scope}" resolved.`,
          })
          handleOpenChange(false)
        },
        onError: (err) =>
          toast({
            title: 'Resolution failed',
            description: (err as Error).message,
            variant: 'destructive',
          }),
      },
    )
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <CheckCircle2 className="h-5 w-5 text-green-500" />
            Resolve Budget Interjection
          </DialogTitle>
          {interjection && (
            <DialogDescription>
              Resolving "{interjection.reason.slice(0, 80)}
              {interjection.reason.length > 80 ? '…' : ''}"
            </DialogDescription>
          )}
        </DialogHeader>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="space-y-1.5">
            <Label htmlFor="bri-action">Action</Label>
            <Select value={action} onValueChange={setAction}>
              <SelectTrigger id="bri-action">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="resume">Resume</SelectItem>
                <SelectItem value="abort">Abort</SelectItem>
                <SelectItem value="retry">Retry</SelectItem>
              </SelectContent>
            </Select>
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="bri-resolution">Resolution Notes *</Label>
            <Textarea
              id="bri-resolution"
              value={resolution}
              onChange={(e) => setResolution(e.target.value)}
              placeholder="Describe how the budget issue was addressed..."
              rows={3}
              autoFocus
            />
            {resolutionError && <p className="text-xs text-destructive">{resolutionError}</p>}
          </div>

          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => handleOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={isPending}>
              {isPending ? 'Resolving...' : 'Confirm Resolution'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

// ─── BudgetInterjectionCard ───────────────────────────────────────────────────

interface BudgetInterjectionCardProps {
  interjection: Interjection
  onResolve: (interjection: Interjection) => void
}

function BudgetInterjectionCard({ interjection, onResolve }: BudgetInterjectionCardProps) {
  return (
    <Card
      className={cn(
        'border transition-colors',
        interjection.severity === 'fatal' && 'border-destructive/60',
        interjection.severity === 'critical' && 'border-destructive/40',
        interjection.severity === 'warning' && 'border-yellow-500/40',
      )}
    >
      <CardContent className="px-4 py-3 space-y-2">
        <div className="flex items-start justify-between gap-2">
          <div className="flex items-center gap-2 min-w-0">
            <div
              className={cn(
                'h-7 w-7 rounded-full flex items-center justify-center shrink-0',
                interjection.severity === 'fatal' && 'bg-destructive/20 text-destructive',
                interjection.severity === 'critical' && 'bg-destructive/15 text-destructive',
                interjection.severity === 'warning' &&
                  'bg-yellow-500/15 text-yellow-600 dark:text-yellow-400',
              )}
            >
              {severityIcon(interjection.severity)}
            </div>
            <div className="min-w-0">
              <p className="text-sm font-medium truncate">{interjection.scope}</p>
              <p className="text-xs text-muted-foreground truncate">{interjection.reason}</p>
            </div>
          </div>
          <div className="flex items-center gap-1.5 shrink-0 flex-wrap justify-end">
            <Badge variant={severityVariant(interjection.severity)} className="text-xs capitalize">
              {interjection.severity}
            </Badge>
          </div>
        </div>

        <div className="flex items-center justify-between gap-2">
          <p className="text-xs text-muted-foreground">{formatRelativeTime(interjection.created_at)}</p>
          <Button
            size="sm"
            variant="outline"
            className="h-7 text-xs"
            onClick={() => onResolve(interjection)}
          >
            <CheckCircle2 className="h-3.5 w-3.5 mr-1.5" />
            Resolve
          </Button>
        </div>
      </CardContent>
    </Card>
  )
}

// ─── BudgetGovernanceSection ──────────────────────────────────────────────────

interface BudgetGovernanceSectionProps {
  onResolve: (interjection: Interjection) => void
}

function BudgetGovernanceSection({ onResolve }: BudgetGovernanceSectionProps) {
  const { data, isLoading, error, refetch } = useActiveInterjections()

  const budgetInterjections = (data?.interjections ?? []).filter(
    (i) => i.source === BUDGET_MONITOR_SOURCE,
  )

  if (isLoading) {
    return (
      <section aria-labelledby="budget-governance-heading">
        <h2 id="budget-governance-heading" className="text-sm font-semibold mb-3">
          Active Budget Interjections
        </h2>
        <LoadingState message="Checking budget interjections..." />
      </section>
    )
  }

  if (error) {
    return (
      <section aria-labelledby="budget-governance-heading">
        <h2 id="budget-governance-heading" className="text-sm font-semibold mb-3">
          Active Budget Interjections
        </h2>
        <ErrorState error={error as Error} onRetry={() => void refetch()} />
      </section>
    )
  }

  return (
    <section aria-labelledby="budget-governance-heading">
      <div className="flex items-center justify-between mb-3">
        <h2 id="budget-governance-heading" className="text-sm font-semibold">
          Active Budget Interjections
          {budgetInterjections.length > 0 && (
            <Badge variant="destructive" className="ml-2 text-xs">
              {budgetInterjections.length}
            </Badge>
          )}
        </h2>
        <Button
          variant="ghost"
          size="sm"
          className="h-7 text-xs text-muted-foreground"
          onClick={() => void refetch()}
        >
          <RefreshCw className="h-3.5 w-3.5 mr-1.5" />
          Refresh
        </Button>
      </div>

      {budgetInterjections.length === 0 ? (
        <div className="flex flex-col items-center justify-center rounded-lg border border-dashed py-8 text-center">
          <CheckCircle2 className="h-7 w-7 text-green-500 mb-2" />
          <p className="text-sm font-medium">No budget interjections</p>
          <p className="text-xs text-muted-foreground mt-1">
            All scopes are within their spending thresholds.
          </p>
        </div>
      ) : (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
          {budgetInterjections.map((interjection) => (
            <BudgetInterjectionCard
              key={interjection.id}
              interjection={interjection}
              onResolve={onResolve}
            />
          ))}
        </div>
      )}
    </section>
  )
}

// ─── ContextHeatmap ───────────────────────────────────────────────────────────

function heatmapColor(ratio: number): string {
  const r = Math.min(1, ratio)
  if (r < 0.33) return 'bg-green-500/20 border-green-500/30 text-green-700 dark:text-green-400'
  if (r < 0.66) return 'bg-yellow-500/20 border-yellow-500/30 text-yellow-700 dark:text-yellow-400'
  if (r < 0.85) return 'bg-orange-500/20 border-orange-500/30 text-orange-700 dark:text-orange-400'
  return 'bg-red-500/20 border-red-500/30 text-red-700 dark:text-red-400'
}

function heatmapDotColor(ratio: number): string {
  const r = Math.min(1, ratio)
  if (r < 0.33) return 'bg-green-500'
  if (r < 0.66) return 'bg-yellow-500'
  if (r < 0.85) return 'bg-orange-500'
  return 'bg-red-500'
}

interface AgentCostRow {
  agent_id: string
  total_cost: number
  tool_calls: number
  session_count: number
  provider_ids: Set<string>
}

interface ContextHeatmapProps {
  providerMap: Map<string, Provider>
}

function ContextHeatmap({ providerMap }: ContextHeatmapProps) {
  const { data: rawSessions, isLoading, error, refetch } = useSessions(200)
  const [filterProvider, setFilterProvider] = useState<string | null>(null)

  const sessions = Array.isArray(rawSessions) ? (rawSessions as Session[]) : []

  // Aggregate cost and tool calls per agent, also track provider IDs
  const agentMap = new Map<string, AgentCostRow>()
  for (const s of sessions) {
    if (!s.agent_id) continue
    const existing = agentMap.get(s.agent_id)
    if (existing) {
      existing.total_cost += s.total_cost ?? 0
      existing.tool_calls += s.tool_calls ?? 0
      existing.session_count += 1
      if (s.provider_id) existing.provider_ids.add(s.provider_id)
    } else {
      agentMap.set(s.agent_id, {
        agent_id: s.agent_id,
        total_cost: s.total_cost ?? 0,
        tool_calls: s.tool_calls ?? 0,
        session_count: 1,
        provider_ids: new Set(s.provider_id ? [s.provider_id] : []),
      })
    }
  }

  let rows = Array.from(agentMap.values()).sort((a, b) => b.total_cost - a.total_cost)

  if (filterProvider) {
    rows = rows.filter((r) => r.provider_ids.has(filterProvider))
  }

  const maxCost = rows.length > 0 ? rows[0].total_cost : 1

  // Build provider filter options from providerMap
  const providerOptions = Array.from(providerMap.values())

  return (
    <section aria-labelledby="context-heatmap-heading">
      <div className="flex items-center justify-between mb-3 flex-wrap gap-2">
        <div className="flex items-center gap-2">
          <Flame className="h-4 w-4 text-muted-foreground" />
          <h2 id="context-heatmap-heading" className="text-sm font-semibold">
            Context Budget Heatmap
          </h2>
          <Badge variant="outline" className="text-xs">per-agent tokens</Badge>
        </div>
        <div className="flex items-center gap-2">
          {providerOptions.length > 0 && (
            <div className="flex items-center gap-1 flex-wrap">
              <Button
                type="button"
                size="sm"
                variant={filterProvider === null ? 'default' : 'outline'}
                className="h-6 text-xs px-2"
                onClick={() => setFilterProvider(null)}
              >
                All
              </Button>
              {providerOptions.map((p) => (
                <Button
                  key={p.id}
                  type="button"
                  size="sm"
                  variant={filterProvider === p.id ? 'default' : 'outline'}
                  className="h-6 text-xs px-2 capitalize"
                  onClick={() => setFilterProvider(filterProvider === p.id ? null : p.id)}
                >
                  {p.kind}
                </Button>
              ))}
            </div>
          )}
          <Button
            variant="ghost"
            size="sm"
            className="h-7 text-xs text-muted-foreground"
            onClick={() => void refetch()}
            disabled={isLoading}
          >
            <RefreshCw className={cn('h-3.5 w-3.5 mr-1.5', isLoading && 'animate-spin')} />
            Refresh
          </Button>
        </div>
      </div>

      {isLoading && <LoadingState message="Loading session data..." className="py-6" />}
      {error && <ErrorState error={error as Error} onRetry={() => void refetch()} className="py-6" />}

      {!isLoading && !error && rows.length === 0 && (
        <div className="flex flex-col items-center justify-center rounded-lg border border-dashed py-8 text-center">
          <Flame className="h-7 w-7 text-muted-foreground/40 mb-2" />
          <p className="text-sm font-medium text-muted-foreground">No session data yet</p>
          <p className="text-xs text-muted-foreground mt-1">
            Token usage will appear here once agents have completed sessions.
          </p>
        </div>
      )}

      {!isLoading && !error && rows.length > 0 && (
        <>
          {/* Legend */}
          <div className="flex items-center gap-3 mb-3 text-xs text-muted-foreground">
            <span>Low</span>
            <div className="flex gap-1">
              {['bg-green-500', 'bg-yellow-500', 'bg-orange-500', 'bg-red-500'].map((c) => (
                <span key={c} className={cn('h-3 w-6 rounded-sm inline-block', c)} />
              ))}
            </div>
            <span>High</span>
          </div>

          <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 gap-2">
            {rows.map((row) => {
              const ratio = maxCost > 0 ? row.total_cost / maxCost : 0
              return (
                <div
                  key={row.agent_id}
                  className={cn(
                    'rounded-md border px-3 py-2.5 space-y-1 text-xs',
                    heatmapColor(ratio),
                  )}
                  title={`${row.agent_id}: $${row.total_cost.toFixed(4)} across ${row.session_count} session${row.session_count !== 1 ? 's' : ''}`}
                >
                  <div className="flex items-center gap-1.5">
                    <span className={cn('h-2 w-2 rounded-full shrink-0', heatmapDotColor(ratio))} />
                    <p className="font-mono truncate leading-tight" title={row.agent_id}>
                      {row.agent_id}
                    </p>
                  </div>
                  <p className="font-semibold tabular-nums">
                    ${row.total_cost.toFixed(4)}
                  </p>
                  <p className="opacity-60">{row.tool_calls.toLocaleString()} calls · {row.session_count} sess.</p>
                </div>
              )
            })}
          </div>
        </>
      )}
    </section>
  )
}

// ─── ProviderBudgetsSection ───────────────────────────────────────────────────

interface ProviderBudgetsSectionProps {
  onSetThreshold: (scope: string) => void
}

function ProviderBudgetsSection({ onSetThreshold }: ProviderBudgetsSectionProps) {
  const { data: providerBudgets, isLoading, error, refetch } = useProviderBudgets()
  const { data: providers } = useProviders()

  const enabledProviders = Array.isArray(providers)
    ? (providers as Provider[]).filter((p) => p.is_enabled)
    : []

  if (enabledProviders.length === 0 && !isLoading) {
    return null
  }

  return (
    <section aria-labelledby="provider-budgets-heading">
      <div className="flex items-center justify-between mb-3">
        <h2 id="provider-budgets-heading" className="text-sm font-semibold">
          Provider Budgets
        </h2>
        <Button
          variant="ghost"
          size="sm"
          className="h-7 text-xs text-muted-foreground"
          onClick={() => void refetch()}
          disabled={isLoading}
        >
          <RefreshCw className={cn('h-3.5 w-3.5 mr-1.5', isLoading && 'animate-spin')} />
          Refresh
        </Button>
      </div>

      {isLoading && <LoadingState message="Loading provider budgets..." className="py-6" />}
      {error && <ErrorState error={error as Error} onRetry={() => void refetch()} className="py-6" />}

      {!isLoading && !error && (
        <>
          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4 mb-4">
            {(providerBudgets ?? []).map((pb) => (
              <ProviderBudgetCard
                key={pb.provider.id}
                providerBudget={pb}
                onSetThreshold={onSetThreshold}
              />
            ))}
          </div>
          {(providerBudgets ?? []).length > 1 && (
            <TotalAcrossProviders providerBudgets={providerBudgets ?? []} />
          )}
        </>
      )}
    </section>
  )
}

// ─── SystemBudgetsSection ─────────────────────────────────────────────────────
// Uses useAllBudgetStatuses (batch) — no per-scope calls.
// Filters to non-provider scopes; merges system defaults with any known custom scopes.

interface SystemBudgetsSectionProps {
  knownScopes: string[]
  allStatuses: BudgetStatus[]
  isLoading: boolean
  error: Error | null
  onRefetch: () => void
  onSetThreshold: (scope: string) => void
}

function SystemBudgetsSection({
  knownScopes,
  allStatuses,
  isLoading,
  error,
  onRefetch,
  onSetThreshold,
}: SystemBudgetsSectionProps) {
  // Show SYSTEM_SCOPES plus any non-provider scopes returned by the backend.
  // A scope is "system" if it does not start with "provider:".
  const systemScopeSet = new Set([
    ...SYSTEM_SCOPES,
    ...knownScopes.filter((s) => !s.startsWith('provider:')),
  ])

  // Build a status lookup from the batch data
  const statusMap = new Map<string, BudgetStatus>()
  for (const s of allStatuses) {
    statusMap.set(s.scope, s)
  }

  // Show all system scopes; use a zero-state fallback for ones without data yet
  const items: BudgetStatus[] = Array.from(systemScopeSet).map(
    (scope) =>
      statusMap.get(scope) ?? {
        scope,
        cost: 0,
        threshold: 0,
        remaining: 0,
        percent_used: 0,
      },
  )

  return (
    <section aria-labelledby="system-budgets-heading">
      <div className="flex items-center justify-between mb-3">
        <h2 id="system-budgets-heading" className="text-sm font-semibold">
          System Budgets
        </h2>
        <Button
          variant="ghost"
          size="sm"
          className="h-7 text-xs text-muted-foreground"
          onClick={onRefetch}
          disabled={isLoading}
        >
          <RefreshCw className={cn('h-3.5 w-3.5 mr-1.5', isLoading && 'animate-spin')} />
          Refresh
        </Button>
      </div>

      {isLoading && <LoadingState message="Loading system budgets..." className="py-6" />}
      {error && <ErrorState error={error} onRetry={onRefetch} className="py-6" />}

      {!isLoading && !error && (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
          {items.map((budget) => (
            <BudgetCard key={budget.scope} budget={budget} onSetThreshold={onSetThreshold} />
          ))}
        </div>
      )}
    </section>
  )
}

// ─── BudgetPage ───────────────────────────────────────────────────────────────

export function BudgetPage() {
  const [thresholdDialogOpen, setThresholdDialogOpen] = useState(false)
  const [targetScope, setTargetScope] = useState('global')
  const [resolveTarget, setResolveTarget] = useState<Interjection | null>(null)

  const { data: providers } = useProviders()
  const { data: knownScopes } = useBudgetScopes()
  const {
    data: allStatuses,
    isLoading: allStatusesLoading,
    error: allStatusesError,
    refetch: refetchAllStatuses,
  } = useAllBudgetStatuses()
  const { mutate: setThreshold, isPending: isSettingThreshold } = useSetBudgetThreshold()

  const enabledProviders = Array.isArray(providers)
    ? (providers as Provider[]).filter((p) => p.is_enabled)
    : []

  const providerMap = new Map<string, Provider>()
  for (const p of enabledProviders) {
    providerMap.set(p.id, p)
  }

  const scopesList = Array.isArray(knownScopes) ? knownScopes : []
  const statusesList = Array.isArray(allStatuses) ? allStatuses : []

  function openSetThreshold(scope: string) {
    setTargetScope(scope)
    setThresholdDialogOpen(true)
  }

  function handleSetThreshold(
    args: SetBudgetThresholdArgs,
    cb: { onSuccess: () => void; onError: (e: Error) => void },
  ) {
    setThreshold(args, cb)
  }

  return (
    <div className="p-6 space-y-8">
      <PageHeader
        title="Budget Governance"
        description="Monitor spending limits and budget-triggered interjections across all scopes."
      >
        <Button size="sm" onClick={() => openSetThreshold('global')}>
          <PlusCircle className="h-4 w-4 mr-2" />
          Set Threshold
        </Button>
      </PageHeader>

      {/* Provider Budgets */}
      <ProviderBudgetsSection onSetThreshold={openSetThreshold} />

      {/* System Budgets — global, workspace, and any custom scopes (uses batch data) */}
      <SystemBudgetsSection
        knownScopes={scopesList}
        allStatuses={statusesList}
        isLoading={allStatusesLoading}
        error={allStatusesError as Error | null}
        onRefetch={() => void refetchAllStatuses()}
        onSetThreshold={openSetThreshold}
      />

      {/* Context Budget Heatmap — per-agent token consumption */}
      <ContextHeatmap providerMap={providerMap} />

      {/* Budget Governance — active interjections from budget.monitor */}
      <BudgetGovernanceSection onResolve={setResolveTarget} />

      {/* Dialogs */}
      <SetThresholdDialog
        open={thresholdDialogOpen}
        initialScope={targetScope}
        providers={enabledProviders}
        onOpenChange={setThresholdDialogOpen}
        onSet={handleSetThreshold}
        isPending={isSettingThreshold}
      />
      <ResolveInterjectionDialog
        interjection={resolveTarget}
        onOpenChange={(open) => {
          if (!open) setResolveTarget(null)
        }}
      />
    </div>
  )
}
