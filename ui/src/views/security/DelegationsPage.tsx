import { useState } from 'react'
import { GitBranch, PlusCircle, Trash2, Loader2, Eye, EyeOff } from 'lucide-react'
import {
  useDelegations,
  useGrantDelegation,
  useRevokeDelegation,
  type DelegationGrant,
  type DelegationType,
} from '@/services/delegationService'
import { useAgents } from '@/services/agentService'
import { useEventStreamInvalidation } from '@/hooks/useEventStreamInvalidation'
import { PageHeader } from '@/components/domain/page-header'
import { LoadingState } from '@/components/domain/loading-state'
import { ErrorState } from '@/components/domain/error-state'
import { EmptyState } from '@/components/domain/empty-state'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
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
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '@/components/ui/dialog'
import { toast } from '@/components/ui/use-toast'

// ─── Helpers ──────────────────────────────────────────────────────────────────

function formatDate(iso?: string | null): string {
  if (!iso) return '—'
  try {
    return new Date(iso).toLocaleString()
  } catch {
    return iso
  }
}

function delegationTypeLabel(type: DelegationType): string {
  return type === 'clearance_elevation' ? 'Clearance Elevation' : 'Credential Passthrough'
}

// ─── DelegationRow ────────────────────────────────────────────────────────────

interface DelegationRowProps {
  grant: DelegationGrant
  onRevoke: (id: string) => void
  isRevokingId: string | null
}

function DelegationRow({ grant, onRevoke, isRevokingId }: DelegationRowProps) {
  const isRevoking = isRevokingId === grant.id

  return (
    <div className="flex items-start gap-3 px-3 py-2.5 border rounded-md text-sm">
      <GitBranch className="h-4 w-4 text-muted-foreground shrink-0 mt-0.5" />
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2 flex-wrap">
          <span className="font-medium">
            {grant.grantor_name ?? grant.grantor_id}
          </span>
          <span className="text-muted-foreground">→</span>
          <span className="font-medium">
            {grant.grantee_name ?? grant.grantee_id}
          </span>
          <Badge variant="outline" className="text-xs shrink-0">
            {delegationTypeLabel(grant.delegation_type)}
          </Badge>
          {!grant.is_active && (
            <Badge variant="destructive" className="text-xs shrink-0">Revoked</Badge>
          )}
        </div>
        {grant.reason && (
          <p className="mt-0.5 text-xs text-muted-foreground truncate">{grant.reason}</p>
        )}
        <div className="flex gap-3 mt-0.5 text-xs text-muted-foreground">
          <span>Granted {formatDate(grant.created_at)}</span>
          {grant.expires_at && <span>Expires {formatDate(grant.expires_at)}</span>}
        </div>
      </div>
      {grant.is_active && (
        <Button
          size="sm"
          variant="ghost"
          className="h-7 w-7 p-0 text-muted-foreground hover:text-destructive shrink-0"
          disabled={isRevoking}
          onClick={() => onRevoke(grant.id)}
          title="Revoke delegation"
        >
          {isRevoking ? (
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
          ) : (
            <Trash2 className="h-3.5 w-3.5" />
          )}
        </Button>
      )}
    </div>
  )
}

// ─── GrantDelegationDialog ────────────────────────────────────────────────────

interface GrantDelegationDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

function GrantDelegationDialog({ open, onOpenChange }: GrantDelegationDialogProps) {
  const [grantorId, setGrantorId] = useState('')
  const [granteeId, setGranteeId] = useState('')
  const [delegationType, setDelegationType] = useState<DelegationType>('clearance_elevation')
  const [credential, setCredential] = useState('')
  const [showCredential, setShowCredential] = useState(false)
  const [reason, setReason] = useState('')
  const [expiresAt, setExpiresAt] = useState('')
  const [errors, setErrors] = useState<Record<string, string>>({})

  const { data: agents } = useAgents()
  const { mutate: grantDelegation, isPending } = useGrantDelegation()

  const agentList = Array.isArray(agents) ? agents : []

  function resetForm() {
    setGrantorId('')
    setGranteeId('')
    setDelegationType('clearance_elevation')
    setCredential('')
    setShowCredential(false)
    setReason('')
    setExpiresAt('')
    setErrors({})
  }

  function handleOpenChange(next: boolean) {
    if (!next) resetForm()
    onOpenChange(next)
  }

  function validate(): boolean {
    const errs: Record<string, string> = {}
    if (!grantorId) errs.grantor = 'Select a grantor'
    if (!granteeId) errs.grantee = 'Select a grantee'
    if (grantorId && granteeId && grantorId === granteeId)
      errs.grantee = 'Grantor and grantee must be different'
    if (!reason.trim()) errs.reason = 'Reason is required'
    if (delegationType === 'credential_passthrough' && !credential.trim())
      errs.credential = 'Credential is required for passthrough'
    setErrors(errs)
    return Object.keys(errs).length === 0
  }

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (!validate()) return

    grantDelegation(
      {
        grantor_id: grantorId,
        grantee_id: granteeId,
        delegation_type: delegationType,
        reason,
        credential: delegationType === 'credential_passthrough' ? credential : undefined,
        expires_at: expiresAt || undefined,
      },
      {
        onSuccess: () => {
          toast({ title: 'Delegation granted', description: 'The delegation grant is now active.' })
          handleOpenChange(false)
        },
        onError: (err) =>
          toast({
            title: 'Grant failed',
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
          <DialogTitle>Grant Delegation</DialogTitle>
          <DialogDescription>
            Allow one agent to act on behalf of another, either by elevating clearance or passing through credentials.
          </DialogDescription>
        </DialogHeader>

        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1.5">
              <Label htmlFor="del-grantor">Grantor *</Label>
              <Select value={grantorId} onValueChange={setGrantorId}>
                <SelectTrigger id="del-grantor">
                  <SelectValue placeholder="Select..." />
                </SelectTrigger>
                <SelectContent>
                  {agentList.map((a) => (
                    <SelectItem key={a.id} value={a.id}>{a.name}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
              {errors.grantor && <p className="text-xs text-destructive">{errors.grantor}</p>}
            </div>

            <div className="space-y-1.5">
              <Label htmlFor="del-grantee">Grantee *</Label>
              <Select value={granteeId} onValueChange={setGranteeId}>
                <SelectTrigger id="del-grantee">
                  <SelectValue placeholder="Select..." />
                </SelectTrigger>
                <SelectContent>
                  {agentList.map((a) => (
                    <SelectItem key={a.id} value={a.id}>{a.name}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
              {errors.grantee && <p className="text-xs text-destructive">{errors.grantee}</p>}
            </div>
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="del-type">Delegation Type *</Label>
            <Select value={delegationType} onValueChange={(v) => setDelegationType(v as DelegationType)}>
              <SelectTrigger id="del-type">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="clearance_elevation">Clearance Elevation</SelectItem>
                <SelectItem value="credential_passthrough">Credential Passthrough</SelectItem>
              </SelectContent>
            </Select>
          </div>

          {delegationType === 'credential_passthrough' && (
            <div className="space-y-1.5">
              <Label htmlFor="del-cred">Credential *</Label>
              <div className="relative">
                <Input
                  id="del-cred"
                  type={showCredential ? 'text' : 'password'}
                  value={credential}
                  onChange={(e) => setCredential(e.target.value)}
                  placeholder="Token or password to pass through"
                  className="font-mono pr-10"
                />
                <button
                  type="button"
                  tabIndex={-1}
                  className="absolute right-3 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground transition-colors"
                  onClick={() => setShowCredential((p) => !p)}
                  aria-label={showCredential ? 'Hide credential' : 'Show credential'}
                >
                  {showCredential ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                </button>
              </div>
              <p className="text-xs text-muted-foreground">
                The credential is encrypted at rest and never shown after submission.
              </p>
              {errors.credential && <p className="text-xs text-destructive">{errors.credential}</p>}
            </div>
          )}

          <div className="space-y-1.5">
            <Label htmlFor="del-reason">Reason *</Label>
            <Textarea
              id="del-reason"
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              placeholder="Why is this delegation needed?"
              rows={2}
            />
            {errors.reason && <p className="text-xs text-destructive">{errors.reason}</p>}
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="del-expiry">Expiry (optional)</Label>
            <Input
              id="del-expiry"
              type="datetime-local"
              value={expiresAt}
              onChange={(e) => setExpiresAt(e.target.value)}
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
                  Granting...
                </>
              ) : (
                'Grant Delegation'
              )}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

// ─── DelegationsPage ──────────────────────────────────────────────────────────

export function DelegationsPage() {
  const [grantOpen, setGrantOpen] = useState(false)
  const [revokingId, setRevokingId] = useState<string | null>(null)

  useEventStreamInvalidation()

  const { data: grants, isLoading, error, refetch } = useDelegations()
  const { mutate: revokeDelegation } = useRevokeDelegation()

  function handleRevoke(id: string) {
    if (!confirm('Revoke this delegation? The grantee will immediately lose delegated access.')) return
    setRevokingId(id)
    revokeDelegation(
      { delegation_id: id },
      {
        onSuccess: () =>
          toast({ title: 'Delegation revoked', description: 'The grant has been revoked.' }),
        onError: (err) =>
          toast({ title: 'Revoke failed', description: (err as Error).message, variant: 'destructive' }),
        onSettled: () => setRevokingId(null),
      },
    )
  }

  if (isLoading)
    return (
      <div className="p-6 space-y-6">
        <PageHeader title="Delegations" description="Manage on-behalf-of delegation grants between agents." />
        <LoadingState message="Loading delegations..." />
      </div>
    )

  if (error)
    return (
      <div className="p-6 space-y-6">
        <PageHeader title="Delegations" description="Manage on-behalf-of delegation grants between agents." />
        <ErrorState error={error as Error} onRetry={() => void refetch()} />
      </div>
    )

  const items = Array.isArray(grants) ? grants : []
  const active = items.filter((g) => g.is_active)
  const inactive = items.filter((g) => !g.is_active)

  return (
    <div className="p-6 space-y-6">
      <PageHeader
        title="Delegations"
        description="Grant agents the ability to act on behalf of other agents with elevated clearance or passed credentials."
      >
        <Button size="sm" onClick={() => setGrantOpen(true)}>
          <PlusCircle className="h-4 w-4 mr-2" />
          Grant Delegation
        </Button>
      </PageHeader>

      {items.length === 0 ? (
        <EmptyState
          icon={GitBranch}
          title="No delegations"
          description="Grant a delegation to allow one agent to act on behalf of another."
          action={
            <Button size="sm" onClick={() => setGrantOpen(true)}>
              Create first delegation
            </Button>
          }
        />
      ) : (
        <div className="space-y-4">
          {active.length > 0 && (
            <div className="space-y-2">
              <p className="text-xs font-medium text-muted-foreground uppercase tracking-wide px-1">
                Active — {active.length}
              </p>
              {active.map((g) => (
                <DelegationRow key={g.id} grant={g} onRevoke={handleRevoke} isRevokingId={revokingId} />
              ))}
            </div>
          )}

          {inactive.length > 0 && (
            <div className="space-y-2">
              <p className="text-xs font-medium text-muted-foreground uppercase tracking-wide px-1">
                Revoked — {inactive.length}
              </p>
              {inactive.map((g) => (
                <DelegationRow key={g.id} grant={g} onRevoke={handleRevoke} isRevokingId={revokingId} />
              ))}
            </div>
          )}
        </div>
      )}

      <GrantDelegationDialog open={grantOpen} onOpenChange={setGrantOpen} />
    </div>
  )
}
