import { useState } from 'react'
import {
  ShieldAlert,
  ShieldCheck,
  AlertTriangle,
  CheckCircle2,
  XCircle,
  ChevronDown,
  ChevronUp,
  Zap,
  Clock,
  RefreshCw,
} from 'lucide-react'
import {
  useActiveInterjections,
  useSafeModeStatus,
  useInterjectionHistory,
  usePullAndonCord,
  useResolveInterjection,
  type Interjection,
} from '@/services/interjectionService'
import { PageHeader } from '@/components/domain/page-header'
import { LoadingState } from '@/components/domain/loading-state'
import { ErrorState } from '@/components/domain/error-state'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader } from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { toast } from '@/components/ui/use-toast'
import { cn } from '@/lib/utils'

// ─── Helpers ─────────────────────────────────────────────────────────────────

function severityVariant(severity: string): 'default' | 'secondary' | 'destructive' | 'outline' {
  switch (severity) {
    case 'fatal':
      return 'destructive'
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
      return <XCircle className="h-4 w-4" />
    case 'critical':
      return <AlertTriangle className="h-4 w-4" />
    case 'warning':
      return <ShieldAlert className="h-4 w-4" />
    default:
      return <AlertTriangle className="h-4 w-4" />
  }
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

// ─── Safe Mode Banner ─────────────────────────────────────────────────────────

interface SafeModeBannerProps {
  active: boolean
  scopeCount: number
}

function SafeModeBanner({ active, scopeCount }: SafeModeBannerProps) {
  return (
    <div
      className={cn(
        'flex items-center gap-3 rounded-lg border px-4 py-3 text-sm font-medium',
        active
          ? 'border-destructive/50 bg-destructive/10 text-destructive'
          : 'border-green-500/30 bg-green-500/10 text-green-700 dark:text-green-400',
      )}
      role="status"
      aria-live="polite"
    >
      {active ? (
        <ShieldAlert className="h-5 w-5 shrink-0" />
      ) : (
        <ShieldCheck className="h-5 w-5 shrink-0" />
      )}
      <span>
        {active
          ? `Safe mode is ACTIVE across ${scopeCount} scope${scopeCount !== 1 ? 's' : ''} — agent execution is halted`
          : 'Safe mode is clear — all systems operating normally'}
      </span>
    </div>
  )
}

// ─── Pull Andon Cord Dialog ───────────────────────────────────────────────────

interface PullAndonCordDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

function PullAndonCordDialog({ open, onOpenChange }: PullAndonCordDialogProps) {
  const [scope, setScope] = useState('global')
  const [severity, setSeverity] = useState('critical')
  const [reason, setReason] = useState('')
  const [source, setSource] = useState('')
  const [reasonError, setReasonError] = useState('')

  const { mutate: pullCord, isPending } = usePullAndonCord()

  function resetForm() {
    setScope('global')
    setSeverity('critical')
    setReason('')
    setSource('')
    setReasonError('')
  }

  function handleOpenChange(next: boolean) {
    if (!next) resetForm()
    onOpenChange(next)
  }

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (!reason.trim()) {
      setReasonError('Reason is required')
      return
    }
    setReasonError('')

    const args: { scope: string; severity: string; reason: string; source?: string } = {
      scope,
      severity,
      reason: reason.trim(),
    }
    if (source.trim()) args.source = source.trim()

    pullCord(args, {
      onSuccess: (result) => {
        toast({
          title: 'Andon cord pulled',
          description: result.message,
        })
        handleOpenChange(false)
      },
      onError: (err) =>
        toast({
          title: 'Failed to pull andon cord',
          description: (err as Error).message,
          variant: 'destructive',
        }),
    })
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2 text-destructive">
            <ShieldAlert className="h-5 w-5" />
            Pull Andon Cord
          </DialogTitle>
          <DialogDescription>
            Trigger a safety interjection to halt agent execution in the specified scope.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1.5">
              <Label htmlFor="ac-scope">Scope</Label>
              <Select value={scope} onValueChange={setScope}>
                <SelectTrigger id="ac-scope">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="global">Global</SelectItem>
                  <SelectItem value="workspace">Workspace</SelectItem>
                  <SelectItem value="pipeline">Pipeline</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="ac-severity">Severity</Label>
              <Select value={severity} onValueChange={setSeverity}>
                <SelectTrigger id="ac-severity">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="warning">Warning</SelectItem>
                  <SelectItem value="critical">Critical</SelectItem>
                  <SelectItem value="fatal">Fatal</SelectItem>
                </SelectContent>
              </Select>
            </div>
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="ac-reason">Reason *</Label>
            <Textarea
              id="ac-reason"
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              placeholder="Describe why this interjection is being triggered..."
              rows={3}
              autoFocus
            />
            {reasonError && <p className="text-xs text-destructive">{reasonError}</p>}
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="ac-source">Source (optional)</Label>
            <Input
              id="ac-source"
              value={source}
              onChange={(e) => setSource(e.target.value)}
              placeholder="e.g. pipeline:build-prod, agent:deployer"
            />
          </div>

          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => handleOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" variant="destructive" disabled={isPending}>
              {isPending ? 'Triggering...' : 'Pull Andon Cord'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

// ─── Resolve Interjection Dialog ──────────────────────────────────────────────

interface ResolveDialogProps {
  interjection: Interjection | null
  onOpenChange: (open: boolean) => void
}

function ResolveDialog({ interjection, onOpenChange }: ResolveDialogProps) {
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
            description: `Interjection ${interjection.id.slice(0, 8)}… resolved with action "${action}".`,
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
            Resolve Interjection
          </DialogTitle>
          {interjection && (
            <DialogDescription>
              Resolving "{interjection.reason.slice(0, 80)}{interjection.reason.length > 80 ? '…' : ''}"
            </DialogDescription>
          )}
        </DialogHeader>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="space-y-1.5">
            <Label htmlFor="rv-action">Action</Label>
            <Select value={action} onValueChange={setAction}>
              <SelectTrigger id="rv-action">
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
            <Label htmlFor="rv-resolution">Resolution Notes *</Label>
            <Textarea
              id="rv-resolution"
              value={resolution}
              onChange={(e) => setResolution(e.target.value)}
              placeholder="Describe how this was resolved and what action was taken..."
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

// ─── Interjection Card ────────────────────────────────────────────────────────

interface InterjectionCardProps {
  interjection: Interjection
  onResolve?: (interjection: Interjection) => void
  showResolve?: boolean
}

function InterjectionCard({ interjection, onResolve, showResolve = true }: InterjectionCardProps) {
  const isActive = interjection.status === 'active'

  return (
    <Card
      className={cn(
        'border transition-colors',
        isActive && interjection.severity === 'fatal' && 'border-destructive/60',
        isActive && interjection.severity === 'critical' && 'border-destructive/40',
      )}
    >
      <CardHeader className="pb-2">
        <div className="flex items-start justify-between gap-2">
          <div className="flex items-center gap-2 min-w-0">
            <div
              className={cn(
                'h-8 w-8 rounded-full flex items-center justify-center shrink-0',
                interjection.severity === 'fatal' && 'bg-destructive/20 text-destructive',
                interjection.severity === 'critical' && 'bg-destructive/15 text-destructive',
                interjection.severity === 'warning' && 'bg-yellow-500/15 text-yellow-600 dark:text-yellow-400',
              )}
            >
              {severityIcon(interjection.severity)}
            </div>
            <div className="min-w-0">
              <p className="font-semibold text-sm truncate">{interjection.source || 'Unknown source'}</p>
              <p className="text-xs text-muted-foreground">{interjection.scope} scope</p>
            </div>
          </div>
          <div className="flex items-center gap-1.5 shrink-0 flex-wrap justify-end">
            <Badge variant={severityVariant(interjection.severity)} className="text-xs capitalize">
              {interjection.severity}
            </Badge>
            <Badge
              variant={isActive ? 'destructive' : 'secondary'}
              className="text-xs capitalize"
            >
              {interjection.status}
            </Badge>
          </div>
        </div>
      </CardHeader>
      <CardContent className="space-y-2">
        <p className="text-sm">{interjection.reason}</p>

        <div className="flex items-center gap-1 text-xs text-muted-foreground">
          <Clock className="h-3.5 w-3.5 shrink-0" />
          <span>{formatRelativeTime(interjection.created_at)}</span>
          {interjection.created_by && (
            <>
              <span className="mx-1">·</span>
              <span>by {interjection.created_by}</span>
            </>
          )}
        </div>

        {interjection.trace_id && (
          <p className="text-xs text-muted-foreground font-mono truncate">
            trace: {interjection.trace_id}
          </p>
        )}

        {interjection.resolution && (
          <div className="rounded-md bg-muted px-3 py-2 text-xs text-muted-foreground">
            <span className="font-medium text-foreground">Resolution: </span>
            {interjection.resolution}
            {interjection.action && (
              <span className="ml-2">
                <Badge variant="outline" className="text-xs capitalize">{interjection.action}</Badge>
              </span>
            )}
          </div>
        )}

        {showResolve && isActive && onResolve && (
          <div className="pt-1">
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
        )}
      </CardContent>
    </Card>
  )
}

// ─── InterjectionsPage ────────────────────────────────────────────────────────

export function InterjectionsPage() {
  const [andonOpen, setAndonOpen] = useState(false)
  const [resolveTarget, setResolveTarget] = useState<Interjection | null>(null)
  const [historyExpanded, setHistoryExpanded] = useState(false)

  const {
    data: activeData,
    isLoading: activeLoading,
    error: activeError,
    refetch: refetchActive,
  } = useActiveInterjections()

  const {
    data: safeMode,
    isLoading: safeModeLoading,
  } = useSafeModeStatus()

  const {
    data: historyData,
    isLoading: historyLoading,
    refetch: refetchHistory,
  } = useInterjectionHistory()

  const activeInterjections = activeData?.interjections ?? []
  const safeModeActive = safeMode?.active ?? false
  const safeModeScopes = safeMode?.count ?? 0

  if (activeLoading || safeModeLoading) {
    return <LoadingState message="Loading safety status..." />
  }

  if (activeError) {
    return <ErrorState error={activeError as Error} onRetry={() => void refetchActive()} />
  }

  return (
    <div className="space-y-6 p-6">
      <PageHeader
        title="Safety"
        description="Monitor active interjections, safe mode status, and system halts."
      >
        <Button
          variant="destructive"
          size="sm"
          onClick={() => setAndonOpen(true)}
        >
          <Zap className="h-4 w-4 mr-2" />
          Pull Andon Cord
        </Button>
      </PageHeader>

      {/* Safe Mode Banner */}
      <SafeModeBanner active={safeModeActive} scopeCount={safeModeScopes} />

      {/* Active Interjections */}
      <section aria-labelledby="active-heading">
        <div className="flex items-center justify-between mb-3">
          <h2 id="active-heading" className="text-sm font-semibold text-foreground">
            Active Interjections
            {activeInterjections.length > 0 && (
              <Badge variant="destructive" className="ml-2 text-xs">
                {activeInterjections.length}
              </Badge>
            )}
          </h2>
          <Button
            variant="ghost"
            size="sm"
            className="h-7 text-xs text-muted-foreground"
            onClick={() => void refetchActive()}
          >
            <RefreshCw className="h-3.5 w-3.5 mr-1.5" />
            Refresh
          </Button>
        </div>

        {activeInterjections.length === 0 ? (
          <div className="flex flex-col items-center justify-center rounded-lg border border-dashed py-10 text-center">
            <ShieldCheck className="h-8 w-8 text-green-500 mb-2" />
            <p className="text-sm font-medium">No active interjections</p>
            <p className="text-xs text-muted-foreground mt-1">
              All systems are running without safety halts.
            </p>
          </div>
        ) : (
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
            {activeInterjections.map((interjection) => (
              <InterjectionCard
                key={interjection.id}
                interjection={interjection}
                onResolve={setResolveTarget}
                showResolve
              />
            ))}
          </div>
        )}
      </section>

      {/* History Section */}
      <section aria-labelledby="history-heading">
        <button
          id="history-heading"
          className="flex w-full items-center justify-between rounded-lg border px-4 py-3 text-sm font-semibold hover:bg-accent transition-colors"
          onClick={() => {
            setHistoryExpanded((prev) => {
              if (!prev) void refetchHistory()
              return !prev
            })
          }}
          aria-expanded={historyExpanded}
        >
          <span className="flex items-center gap-2">
            <Clock className="h-4 w-4 text-muted-foreground" />
            Interjection History
            {historyData && (
              <Badge variant="secondary" className="text-xs">
                {historyData.count}
              </Badge>
            )}
          </span>
          {historyExpanded ? (
            <ChevronUp className="h-4 w-4 text-muted-foreground" />
          ) : (
            <ChevronDown className="h-4 w-4 text-muted-foreground" />
          )}
        </button>

        {historyExpanded && (
          <div className="mt-3">
            {historyLoading ? (
              <LoadingState message="Loading history..." />
            ) : !historyData || historyData.interjections.length === 0 ? (
              <div className="flex flex-col items-center justify-center rounded-lg border border-dashed py-8 text-center">
                <Clock className="h-7 w-7 text-muted-foreground mb-2" />
                <p className="text-sm text-muted-foreground">No interjection history found.</p>
              </div>
            ) : (
              <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
                {historyData.interjections.map((interjection) => (
                  <InterjectionCard
                    key={interjection.id}
                    interjection={interjection}
                    showResolve={false}
                  />
                ))}
              </div>
            )}
          </div>
        )}
      </section>

      {/* Dialogs */}
      <PullAndonCordDialog open={andonOpen} onOpenChange={setAndonOpen} />
      <ResolveDialog interjection={resolveTarget} onOpenChange={(open) => { if (!open) setResolveTarget(null) }} />
    </div>
  )
}

