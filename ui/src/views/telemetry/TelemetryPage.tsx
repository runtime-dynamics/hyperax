import { useState } from 'react'
import { Activity, BarChart3, Bell, Clock, Trash2, PlusCircle, DollarSign, Cpu } from 'lucide-react'
import {
  useSessions,
  useMetricsSummary,
  useCostReport,
  useAlerts,
  useCreateAlert,
  useDeleteAlert,
  type Alert,
  type CreateAlertArgs,
  type Session,
} from '@/services/telemetryService'
import { useProviders, type Provider } from '@/services/providerService'
import { PageHeader } from '@/components/domain/page-header'
import { LoadingState } from '@/components/domain/loading-state'
import { ErrorState } from '@/components/domain/error-state'
import { EmptyState } from '@/components/domain/empty-state'
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
import { toast } from '@/components/ui/use-toast'
import { cn } from '@/lib/utils'

// ─── Time Range ───────────────────────────────────────────────────────────────

const TIME_RANGES = [
  { label: '1h', hours: 1 },
  { label: '24h', hours: 24 },
  { label: '7d', hours: 24 * 7 },
  { label: '30d', hours: 24 * 30 },
] as const

type TimeRangeLabel = (typeof TIME_RANGES)[number]['label']

function sinceFromHours(hours: number): string {
  const d = new Date(Date.now() - hours * 60 * 60 * 1000)
  return d.toISOString()
}

// ─── Known Alert Metrics ──────────────────────────────────────────────────────

// Valid metrics as defined in the backend (pkg/types/telemetry.go + handler validation)
const KNOWN_METRICS = [
  { value: 'session_cost', label: 'Session Cost (USD)' },
  { value: 'tool_calls', label: 'Tool Calls' },
  { value: 'error_rate', label: 'Error Rate' },
  { value: 'duration', label: 'Duration (ms)' },
] as const

// Backend operators: gt, lt, gte, lte, eq — displayed as symbols
const OPERATOR_OPTIONS: { value: string; label: string }[] = [
  { value: 'gt', label: '> (greater than)' },
  { value: 'lt', label: '< (less than)' },
  { value: 'gte', label: '>= (greater or equal)' },
  { value: 'lte', label: '<= (less or equal)' },
  { value: 'eq', label: '== (equal)' },
]

function operatorSymbol(op: string): string {
  switch (op) {
    case 'gt': return '>'
    case 'lt': return '<'
    case 'gte': return '>='
    case 'lte': return '<='
    case 'eq': return '=='
    default: return op
  }
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

function formatDate(iso: string | undefined): string {
  if (!iso) return '—'
  return new Date(iso).toLocaleString()
}

function formatDurationMs(ms: number | undefined): string {
  if (ms === undefined || ms === null) return '—'
  if (ms < 1000) return `${Math.round(ms)}ms`
  return `${(ms / 1000).toFixed(2)}s`
}

function formatCost(usd: number): string {
  if (usd === 0) return '$0.00'
  if (usd < 0.001) return `$${usd.toFixed(6)}`
  return `$${usd.toFixed(4)}`
}

function sessionStatusVariant(status: string): 'default' | 'secondary' | 'destructive' | 'outline' {
  switch (status?.toLowerCase()) {
    case 'active':
    case 'running':
      return 'default'
    case 'completed':
    case 'closed':
      return 'secondary'
    case 'error':
    case 'failed':
      return 'destructive'
    default:
      return 'outline'
  }
}

function kindBadgeVariant(kind: string): 'default' | 'secondary' | 'outline' {
  switch (kind.toLowerCase()) {
    case 'anthropic':
      return 'default'
    case 'openai':
    case 'google':
    case 'gemini':
      return 'secondary'
    default:
      return 'outline'
  }
}

// ─── TimeRangeSelector ────────────────────────────────────────────────────────

interface TimeRangeSelectorProps {
  value: TimeRangeLabel
  onChange: (value: TimeRangeLabel) => void
}

function TimeRangeSelector({ value, onChange }: TimeRangeSelectorProps) {
  return (
    <div className="flex items-center gap-1 rounded-md border bg-muted/40 p-0.5">
      {TIME_RANGES.map((range) => (
        <button
          key={range.label}
          type="button"
          onClick={() => onChange(range.label)}
          className={cn(
            'px-3 py-1 rounded text-xs font-medium transition-colors',
            value === range.label
              ? 'bg-background text-foreground shadow-sm'
              : 'text-muted-foreground hover:text-foreground',
          )}
        >
          {range.label}
        </button>
      ))}
    </div>
  )
}

// ─── MetricCard ───────────────────────────────────────────────────────────────

interface MetricCardProps {
  title: string
  value: string
  subtitle?: string
  icon: React.ComponentType<{ className?: string }>
  className?: string
}

function MetricCard({ title, value, subtitle, icon: Icon, className }: MetricCardProps) {
  return (
    <Card className={className}>
      <CardHeader className="flex flex-row items-center justify-between pb-2 space-y-0">
        <CardTitle className="text-sm font-medium text-muted-foreground">{title}</CardTitle>
        <Icon className="h-4 w-4 text-muted-foreground" />
      </CardHeader>
      <CardContent>
        <p className="text-2xl font-bold tracking-tight">{value}</p>
        {subtitle && <p className="text-xs text-muted-foreground mt-1">{subtitle}</p>}
      </CardContent>
    </Card>
  )
}

// ─── MetricsSummarySection ────────────────────────────────────────────────────

function MetricsSummarySection() {
  const { data, isLoading, error, refetch } = useMetricsSummary()

  if (isLoading) return <LoadingState message="Loading metrics..." className="py-8" />
  if (error) return <ErrorState error={error as Error} onRetry={() => void refetch()} className="py-8" />

  const metrics = data

  return (
    <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-5 gap-4">
      <MetricCard
        title="Total Calls"
        value={metrics?.total_calls !== undefined ? String(metrics.total_calls) : '—'}
        icon={Activity}
      />
      <MetricCard
        title="Avg Duration"
        value={formatDurationMs(metrics?.avg_duration_ms)}
        subtitle="mean latency"
        icon={Clock}
      />
      <MetricCard
        title="p50 Latency"
        value={formatDurationMs(metrics?.p50_ms)}
        subtitle="median"
        icon={BarChart3}
      />
      <MetricCard
        title="p95 Latency"
        value={formatDurationMs(metrics?.p95_ms)}
        subtitle="95th percentile"
        icon={BarChart3}
      />
      <MetricCard
        title="p99 Latency"
        value={formatDurationMs(metrics?.p99_ms)}
        subtitle="99th percentile"
        icon={BarChart3}
      />
    </div>
  )
}

// ─── SessionsTable ────────────────────────────────────────────────────────────

interface SessionsTableProps {
  since?: string
  providerMap: Map<string, Provider>
}

function SessionsTable({ since, providerMap }: SessionsTableProps) {
  const { data: sessions, isLoading, error, refetch } = useSessions(50)

  if (isLoading) return <LoadingState message="Loading sessions..." className="py-8" />
  if (error) return <ErrorState error={error as Error} onRetry={() => void refetch()} className="py-8" />

  const list = Array.isArray(sessions) ? (sessions as Session[]) : []

  // Filter by time range if provided
  const filtered = since
    ? list.filter((s) => s.started_at && new Date(s.started_at) >= new Date(since))
    : list

  if (filtered.length === 0) {
    return (
      <EmptyState
        icon={Cpu}
        title="No sessions recorded"
        description="Session telemetry will appear here once agent sessions have been tracked."
      />
    )
  }

  return (
    <div className="rounded-lg border overflow-hidden">
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b bg-muted/40">
              <th className="text-left px-4 py-3 font-medium text-muted-foreground">Agent</th>
              <th className="text-left px-4 py-3 font-medium text-muted-foreground">Provider</th>
              <th className="text-left px-4 py-3 font-medium text-muted-foreground">Started</th>
              <th className="text-left px-4 py-3 font-medium text-muted-foreground">Ended</th>
              <th className="text-right px-4 py-3 font-medium text-muted-foreground">Tool Calls</th>
              <th className="text-right px-4 py-3 font-medium text-muted-foreground">Cost</th>
              <th className="text-left px-4 py-3 font-medium text-muted-foreground">Status</th>
            </tr>
          </thead>
          <tbody>
            {filtered.map((session, idx) => {
              const provider = session.agent_id ? providerMap.get(session.agent_id) : undefined
              return (
                <tr
                  key={session.id}
                  className={cn(
                    'border-b last:border-0 hover:bg-muted/30 transition-colors',
                    idx % 2 === 0 ? '' : 'bg-muted/10',
                  )}
                >
                  <td className="px-4 py-3 font-mono text-xs text-foreground truncate max-w-[12rem]">
                    {session.agent_id || '—'}
                  </td>
                  <td className="px-4 py-3 text-xs">
                    {provider ? (
                      <Badge variant={kindBadgeVariant(provider.kind)} className="capitalize text-xs">
                        {provider.kind}
                      </Badge>
                    ) : (
                      <span className="text-muted-foreground">—</span>
                    )}
                  </td>
                  <td className="px-4 py-3 text-xs text-muted-foreground whitespace-nowrap">
                    {formatDate(session.started_at)}
                  </td>
                  <td className="px-4 py-3 text-xs text-muted-foreground whitespace-nowrap">
                    {formatDate(session.ended_at ?? undefined)}
                  </td>
                  <td className="px-4 py-3 text-right tabular-nums text-xs">
                    {session.tool_calls ?? 0}
                  </td>
                  <td className="px-4 py-3 text-right font-mono text-xs">
                    {formatCost(session.total_cost ?? 0)}
                  </td>
                  <td className="px-4 py-3">
                    <Badge variant={sessionStatusVariant(session.status)} className="capitalize text-xs">
                      {session.status || 'unknown'}
                    </Badge>
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
    </div>
  )
}

// ─── CostReportSection ────────────────────────────────────────────────────────

interface CostReportSectionProps {
  since?: string
  providerMap: Map<string, Provider>
}

function CostReportSection({ since, providerMap }: CostReportSectionProps) {
  const { data: entries, isLoading, error, refetch } = useCostReport(since)

  if (isLoading) return <LoadingState message="Loading cost report..." className="py-8" />
  if (error) return <ErrorState error={error as Error} onRetry={() => void refetch()} className="py-8" />

  const list = Array.isArray(entries) ? entries : []

  if (list.length === 0) {
    return (
      <EmptyState
        icon={DollarSign}
        title="No cost data"
        description="Cost entries will appear here as tools are invoked."
      />
    )
  }

  // total_cost is the correct field; fall back to cost_usd for legacy shape
  const total = list.reduce((sum, e) => sum + (e.total_cost ?? e.cost_usd ?? 0), 0)

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between">
        <p className="text-sm text-muted-foreground">{list.length} tool{list.length !== 1 ? 's' : ''}</p>
        <p className="text-sm font-semibold">
          Total: <span className="font-mono">{formatCost(total)}</span>
        </p>
      </div>
      <div className="rounded-lg border overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b bg-muted/40">
                <th className="text-left px-4 py-3 font-medium text-muted-foreground">Tool</th>
                <th className="text-left px-4 py-3 font-medium text-muted-foreground">Provider</th>
                <th className="text-right px-4 py-3 font-medium text-muted-foreground">Calls</th>
                <th className="text-right px-4 py-3 font-medium text-muted-foreground">Avg Cost</th>
                <th className="text-right px-4 py-3 font-medium text-muted-foreground">Total Cost</th>
              </tr>
            </thead>
            <tbody>
              {list.map((entry, idx) => {
                const provider = entry.provider_id ? providerMap.get(entry.provider_id) : undefined
                const cost = entry.total_cost ?? entry.cost_usd ?? 0
                const avgCost = entry.avg_cost ?? (entry.call_count ? cost / entry.call_count : 0)
                return (
                  <tr
                    key={entry.tool_name ?? String(idx)}
                    className={cn(
                      'border-b last:border-0 hover:bg-muted/30 transition-colors',
                      idx % 2 === 0 ? '' : 'bg-muted/10',
                    )}
                  >
                    <td className="px-4 py-3 font-mono text-xs">{entry.tool_name || '—'}</td>
                    <td className="px-4 py-3 text-xs">
                      {provider ? (
                        <Badge variant={kindBadgeVariant(provider.kind)} className="capitalize text-xs">
                          {provider.kind}
                        </Badge>
                      ) : (
                        <span className="text-muted-foreground">—</span>
                      )}
                    </td>
                    <td className="px-4 py-3 text-right tabular-nums text-xs">
                      {(entry.call_count ?? 0).toLocaleString()}
                    </td>
                    <td className="px-4 py-3 text-right font-mono text-xs">
                      {formatCost(avgCost)}
                    </td>
                    <td className="px-4 py-3 text-right font-mono text-xs font-medium">
                      {formatCost(cost)}
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  )
}

// ─── AddAlertDialog ───────────────────────────────────────────────────────────

interface AddAlertDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

function AddAlertDialog({ open, onOpenChange }: AddAlertDialogProps) {
  const [name, setName] = useState('')
  const [selectedMetric, setSelectedMetric] = useState<string>('session_cost')
  const [operator, setOperator] = useState<string>('gt')
  const [threshold, setThreshold] = useState('')
  const [nameError, setNameError] = useState('')
  const [thresholdError, setThresholdError] = useState('')

  const { mutate: createAlert, isPending } = useCreateAlert()

  function resetForm() {
    setName('')
    setSelectedMetric('session_cost')
    setOperator('gt')
    setThreshold('')
    setNameError('')
    setThresholdError('')
  }

  function handleOpenChange(next: boolean) {
    if (!next) resetForm()
    onOpenChange(next)
  }

  function validate(): boolean {
    let valid = true
    if (!name.trim()) {
      setNameError('Alert name is required')
      valid = false
    } else {
      setNameError('')
    }
    const num = Number(threshold)
    if (threshold.trim() === '' || isNaN(num)) {
      setThresholdError('Threshold must be a number')
      valid = false
    } else {
      setThresholdError('')
    }
    return valid
  }

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (!validate()) return

    const args: CreateAlertArgs = {
      name: name.trim(),
      metric: selectedMetric,
      operator,
      threshold: Number(threshold),
    }

    createAlert(args, {
      onSuccess: () => {
        toast({ title: 'Alert created', description: `Alert "${name}" on "${selectedMetric}" has been added.` })
        handleOpenChange(false)
      },
      onError: (err) =>
        toast({
          title: 'Failed to create alert',
          description: (err as Error).message,
          variant: 'destructive',
        }),
    })
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>Add Alert</DialogTitle>
          <DialogDescription>
            Define a threshold alert. Hyperax will fire when the metric crosses the threshold.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="space-y-1.5">
            <Label htmlFor="al-name">Alert Name *</Label>
            <Input
              id="al-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g. High session cost"
              autoFocus
            />
            {nameError && <p className="text-xs text-destructive">{nameError}</p>}
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="al-metric">Metric</Label>
            <Select value={selectedMetric} onValueChange={setSelectedMetric}>
              <SelectTrigger id="al-metric">
                <SelectValue placeholder="Select a metric" />
              </SelectTrigger>
              <SelectContent>
                {KNOWN_METRICS.map((m) => (
                  <SelectItem key={m.value} value={m.value}>
                    {m.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1.5">
              <Label htmlFor="al-operator">Condition</Label>
              <Select value={operator} onValueChange={setOperator}>
                <SelectTrigger id="al-operator">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {OPERATOR_OPTIONS.map((op) => (
                    <SelectItem key={op.value} value={op.value}>{op.label}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="al-threshold">Threshold *</Label>
              <Input
                id="al-threshold"
                type="number"
                value={threshold}
                onChange={(e) => setThreshold(e.target.value)}
                placeholder="100"
              />
              {thresholdError && <p className="text-xs text-destructive">{thresholdError}</p>}
            </div>
          </div>

          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => handleOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={isPending}>
              {isPending ? 'Creating...' : 'Create Alert'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

// ─── AlertsSection ────────────────────────────────────────────────────────────

function AlertsSection() {
  const [dialogOpen, setDialogOpen] = useState(false)
  const { data: alerts, isLoading, error, refetch } = useAlerts()
  const { mutate: deleteAlert } = useDeleteAlert()

  function handleDelete(alert: Alert) {
    if (!confirm(`Delete alert on metric "${alert.metric}"? This cannot be undone.`)) return
    deleteAlert(alert.id, {
      onSuccess: () =>
        toast({ title: 'Alert deleted', description: `Alert on "${alert.metric}" has been removed.` }),
      onError: (err) =>
        toast({ title: 'Delete failed', description: (err as Error).message, variant: 'destructive' }),
    })
  }

  const list = Array.isArray(alerts) ? alerts : []

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div className="space-y-0.5">
          <h3 className="text-base font-semibold">Alerts</h3>
          <p className="text-sm text-muted-foreground">Threshold alerts that trigger on metric conditions.</p>
        </div>
        <Button size="sm" onClick={() => setDialogOpen(true)}>
          <PlusCircle className="h-4 w-4 mr-2" />
          Add Alert
        </Button>
      </div>

      {isLoading && <LoadingState message="Loading alerts..." className="py-8" />}
      {error && <ErrorState error={error as Error} onRetry={() => void refetch()} className="py-8" />}

      {!isLoading && !error && list.length === 0 && (
        <EmptyState
          icon={Bell}
          title="No alerts configured"
          description="Create an alert to get notified when a metric crosses a threshold."
          action={
            <Button size="sm" onClick={() => setDialogOpen(true)}>
              Add your first alert
            </Button>
          }
        />
      )}

      {!isLoading && !error && list.length > 0 && (
        <div className="space-y-2">
          {list.map((alert) => (
            <div
              key={alert.id}
              className="flex items-center gap-3 rounded-lg border px-4 py-3 bg-card hover:bg-muted/30 transition-colors"
            >
              <Bell className="h-4 w-4 text-muted-foreground shrink-0" />
              <div className="flex-1 min-w-0 space-y-0.5">
                <p className="text-sm font-medium">{alert.name}</p>
                <div className="flex items-center gap-2 flex-wrap">
                  <code className="text-xs font-mono bg-muted/50 px-1.5 py-0.5 rounded">
                    {alert.metric} {operatorSymbol(alert.operator)} {alert.threshold}
                  </code>
                  {alert.window && (
                    <Badge variant="outline" className="text-xs">{alert.window}</Badge>
                  )}
                  {alert.severity && alert.severity !== 'info' && (
                    <Badge variant={alert.severity === 'critical' ? 'destructive' : 'secondary'} className="text-xs capitalize">
                      {alert.severity}
                    </Badge>
                  )}
                  <span className="text-xs text-muted-foreground">{formatDate(alert.created_at)}</span>
                </div>
              </div>
              <Button
                variant="ghost"
                size="icon"
                className="h-7 w-7 text-muted-foreground hover:text-destructive shrink-0"
                onClick={() => handleDelete(alert)}
              >
                <Trash2 className="h-3.5 w-3.5" />
                <span className="sr-only">Delete alert on {alert.metric}</span>
              </Button>
            </div>
          ))}
        </div>
      )}

      <AddAlertDialog open={dialogOpen} onOpenChange={setDialogOpen} />
    </div>
  )
}

// ─── TelemetryPage ────────────────────────────────────────────────────────────

export function TelemetryPage() {
  const [timeRange, setTimeRange] = useState<TimeRangeLabel>('24h')
  const { data: providers } = useProviders()

  const enabledProviders = Array.isArray(providers)
    ? (providers as Provider[]).filter((p) => p.is_enabled)
    : []

  // Build a map from agent_id → Provider. The agent_id is not the same as provider.id,
  // so this map is keyed by provider.id for cost/session scope lookups.
  const providerMap = new Map<string, Provider>()
  for (const p of enabledProviders) {
    providerMap.set(p.id, p)
  }

  const selectedRange = TIME_RANGES.find((r) => r.label === timeRange) ?? TIME_RANGES[1]
  const since = sinceFromHours(selectedRange.hours)

  return (
    <div className="p-6 space-y-8">
      <PageHeader
        title="Telemetry"
        description="Monitor session activity, tool usage, costs, and alert thresholds."
      >
        <TimeRangeSelector value={timeRange} onChange={setTimeRange} />
      </PageHeader>

      {/* Metrics Summary */}
      <section className="space-y-3">
        <div className="flex items-center gap-2">
          <Activity className="h-4 w-4 text-muted-foreground" />
          <h3 className="text-base font-semibold">Metrics Summary</h3>
          <Badge variant="outline" className="text-xs">live · 10s</Badge>
        </div>
        <MetricsSummarySection />
      </section>

      {/* Sessions */}
      <section className="space-y-3">
        <div className="space-y-0.5">
          <h3 className="text-base font-semibold">Sessions</h3>
          <p className="text-sm text-muted-foreground">Recent agent sessions with tool call and token usage.</p>
        </div>
        <SessionsTable since={since} providerMap={providerMap} />
      </section>

      {/* Cost Report */}
      <section className="space-y-3">
        <div className="flex items-center gap-2">
          <DollarSign className="h-4 w-4 text-muted-foreground" />
          <div className="space-y-0.5">
            <h3 className="text-base font-semibold">Cost Report</h3>
            <p className="text-sm text-muted-foreground">Per-call cost breakdown across all tools and agents.</p>
          </div>
        </div>
        <CostReportSection since={since} providerMap={providerMap} />
      </section>

      {/* Alerts */}
      <section>
        <AlertsSection />
      </section>
    </div>
  )
}
