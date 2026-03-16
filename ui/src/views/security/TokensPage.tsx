import { useState } from 'react'
import { KeyRound, PlusCircle, Trash2, Loader2, Copy, Check, TriangleAlert } from 'lucide-react'
import {
  useMCPTokensForPersonas,
  useCreateMCPToken,
  useRevokeMCPToken,
  type MCPToken,
  type CreateTokenResult,
} from '@/services/tokenService'
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

function isExpired(expires_at?: string | null): boolean {
  if (!expires_at) return false
  return new Date(expires_at) < new Date()
}

// ─── TokenRow ─────────────────────────────────────────────────────────────────

interface TokenRowProps {
  token: MCPToken
  onRevoke: (id: string) => void
  isRevokingId: string | null
}

function TokenRow({ token, onRevoke, isRevokingId }: TokenRowProps) {
  const isRevoking = isRevokingId === token.id
  const expired = isExpired(token.expires_at)

  return (
    <div className="flex items-center gap-3 px-3 py-2.5 border rounded-md text-sm">
      <KeyRound className="h-4 w-4 text-muted-foreground shrink-0" />
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2 flex-wrap">
          <span className="font-medium truncate">{token.description}</span>
          {token.persona_name && (
            <Badge variant="outline" className="text-xs shrink-0">{token.persona_name}</Badge>
          )}
          {!token.is_active && (
            <Badge variant="destructive" className="text-xs shrink-0">Revoked</Badge>
          )}
          {expired && token.is_active && (
            <Badge variant="secondary" className="text-xs shrink-0">Expired</Badge>
          )}
        </div>
        <div className="flex gap-3 mt-0.5 text-xs text-muted-foreground">
          <span>Created {formatDate(token.created_at)}</span>
          {token.expires_at && (
            <span className={expired ? 'text-destructive' : ''}>
              Expires {formatDate(token.expires_at)}
            </span>
          )}
          {token.last_used_at && <span>Last used {formatDate(token.last_used_at)}</span>}
        </div>
      </div>
      {token.is_active && (
        <Button
          size="sm"
          variant="ghost"
          className="h-7 w-7 p-0 text-muted-foreground hover:text-destructive shrink-0"
          disabled={isRevoking}
          onClick={() => onRevoke(token.id)}
          title="Revoke token"
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

// ─── CreatedTokenDisplay ──────────────────────────────────────────────────────

interface CreatedTokenDisplayProps {
  result: CreateTokenResult
  onDone: () => void
}

function CreatedTokenDisplay({ result, onDone }: CreatedTokenDisplayProps) {
  const [copied, setCopied] = useState(false)

  function handleCopy() {
    void navigator.clipboard.writeText(result.token).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    })
  }

  return (
    <div className="space-y-4">
      <div className="flex items-start gap-2 rounded-md border border-amber-500/50 bg-amber-500/10 px-3 py-2.5 text-sm">
        <TriangleAlert className="h-4 w-4 text-amber-500 shrink-0 mt-0.5" />
        <p className="text-amber-700 dark:text-amber-400">
          Copy this token now. It will <strong>never be shown again</strong>.
        </p>
      </div>

      <div className="space-y-1.5">
        <Label>Your new token</Label>
        <div className="flex gap-2">
          <Input
            readOnly
            value={result.token}
            className="font-mono text-xs bg-muted"
            onFocus={(e) => e.target.select()}
          />
          <Button
            type="button"
            variant="outline"
            size="sm"
            className="shrink-0"
            onClick={handleCopy}
          >
            {copied ? (
              <Check className="h-4 w-4 text-green-500" />
            ) : (
              <Copy className="h-4 w-4" />
            )}
          </Button>
        </div>
      </div>

      <DialogFooter>
        <Button onClick={onDone}>Done</Button>
      </DialogFooter>
    </div>
  )
}

// ─── CreateTokenDialog ────────────────────────────────────────────────────────

interface CreateTokenDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

function CreateTokenDialog({ open, onOpenChange }: CreateTokenDialogProps) {
  const [agentId, setAgentId] = useState('')
  const [description, setDescription] = useState('')
  const [expiresAt, setExpiresAt] = useState('')
  const [agentError, setAgentError] = useState('')
  const [descError, setDescError] = useState('')
  const [createdResult, setCreatedResult] = useState<CreateTokenResult | null>(null)

  const { data: agents } = useAgents()
  const { mutate: createToken, isPending } = useCreateMCPToken()

  const agentList = Array.isArray(agents) ? agents : []

  function resetForm() {
    setAgentId('')
    setDescription('')
    setExpiresAt('')
    setAgentError('')
    setDescError('')
    setCreatedResult(null)
  }

  function handleOpenChange(next: boolean) {
    if (!next) resetForm()
    onOpenChange(next)
  }

  function validate(): boolean {
    let valid = true
    if (!agentId) { setAgentError('Select an agent'); valid = false } else setAgentError('')
    if (!description.trim()) { setDescError('Description is required'); valid = false } else setDescError('')
    return valid
  }

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (!validate()) return

    createToken(
      {
        persona_id: agentId,
        description,
        expires_at: expiresAt || undefined,
      },
      {
        onSuccess: (result) => {
          setCreatedResult(result)
        },
        onError: (err) =>
          toast({
            title: 'Create failed',
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
          <DialogTitle>Create MCP Token</DialogTitle>
          <DialogDescription>
            Issue a bearer token for a persona. The plaintext token is shown only once at creation.
          </DialogDescription>
        </DialogHeader>

        {createdResult ? (
          <CreatedTokenDisplay result={createdResult} onDone={() => handleOpenChange(false)} />
        ) : (
          <form onSubmit={handleSubmit} className="space-y-4">
            <div className="space-y-1.5">
              <Label htmlFor="tok-agent">Agent *</Label>
              <Select value={agentId} onValueChange={setAgentId}>
                <SelectTrigger id="tok-agent">
                  <SelectValue placeholder="Select agent..." />
                </SelectTrigger>
                <SelectContent>
                  {agentList.map((a) => (
                    <SelectItem key={a.id} value={a.id}>
                      {a.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              {agentError && <p className="text-xs text-destructive">{agentError}</p>}
            </div>

            <div className="space-y-1.5">
              <Label htmlFor="tok-desc">Description *</Label>
              <Input
                id="tok-desc"
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                placeholder="e.g. CI pipeline token"
                autoFocus
              />
              {descError && <p className="text-xs text-destructive">{descError}</p>}
            </div>

            <div className="space-y-1.5">
              <Label htmlFor="tok-expiry">Expiry (optional)</Label>
              <Input
                id="tok-expiry"
                type="datetime-local"
                value={expiresAt}
                onChange={(e) => setExpiresAt(e.target.value)}
              />
              <p className="text-xs text-muted-foreground">Leave blank for a non-expiring token.</p>
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
                  'Create Token'
                )}
              </Button>
            </DialogFooter>
          </form>
        )}
      </DialogContent>
    </Dialog>
  )
}

// ─── TokensPage ───────────────────────────────────────────────────────────────

export function TokensPage() {
  const [createOpen, setCreateOpen] = useState(false)
  const [revokingId, setRevokingId] = useState<string | null>(null)

  useEventStreamInvalidation()

  const { data: agentsData, isLoading: isLoadingAgents, error: agentsError, refetch: refetchAgents } = useAgents()
  const agentList = Array.isArray(agentsData) ? agentsData : []
  const agentIds = agentList.map((a) => a.id)

  const { tokens, isLoading: isLoadingTokens, errors: tokenErrors } = useMCPTokensForPersonas(agentIds)

  const { mutate: revokeToken } = useRevokeMCPToken()

  const isLoading = isLoadingAgents || isLoadingTokens

  function handleRevoke(id: string) {
    if (!confirm('Revoke this token? Any clients using it will immediately lose access.')) return
    setRevokingId(id)
    revokeToken(
      { token_id: id },
      {
        onSuccess: () => toast({ title: 'Token revoked', description: 'The token has been revoked.' }),
        onError: (err) =>
          toast({ title: 'Revoke failed', description: (err as Error).message, variant: 'destructive' }),
        onSettled: () => setRevokingId(null),
      },
    )
  }

  if (isLoading)
    return (
      <div className="p-6 space-y-6">
        <PageHeader title="API Tokens" description="Manage bearer tokens for MCP client authentication." />
        <LoadingState message="Loading tokens..." />
      </div>
    )

  if (agentsError)
    return (
      <div className="p-6 space-y-6">
        <PageHeader title="API Tokens" description="Manage bearer tokens for MCP client authentication." />
        <ErrorState error={agentsError as Error} onRetry={() => void refetchAgents()} />
      </div>
    )

  const active = tokens.filter((t) => t.is_active)
  const inactive = tokens.filter((t) => !t.is_active)

  return (
    <div className="p-6 space-y-6">
      <PageHeader
        title="API Tokens"
        description="Issue bearer tokens for external MCP client access. Tokens are scoped to an agent."
      >
        <Button size="sm" onClick={() => setCreateOpen(true)}>
          <PlusCircle className="h-4 w-4 mr-2" />
          Create Token
        </Button>
      </PageHeader>

      {agentList.length === 0 ? (
        <EmptyState
          icon={KeyRound}
          title="No agents found"
          description="Create an agent before issuing MCP tokens."
        />
      ) : tokens.length === 0 ? (
        <EmptyState
          icon={KeyRound}
          title="No tokens issued"
          description="Create a token to allow external MCP clients to authenticate on behalf of an agent."
          action={
            <Button size="sm" onClick={() => setCreateOpen(true)}>
              Create your first token
            </Button>
          }
        />
      ) : (
        <div className="space-y-4">
          {tokenErrors.length > 0 && (
            <div className="text-xs text-amber-600 dark:text-amber-400 px-1">
              Some token data may be unavailable.
            </div>
          )}
          {active.length > 0 && (
            <div className="space-y-2">
              <p className="text-xs font-medium text-muted-foreground uppercase tracking-wide px-1">
                Active — {active.length}
              </p>
              {active.map((t) => (
                <TokenRow key={t.id} token={t} onRevoke={handleRevoke} isRevokingId={revokingId} />
              ))}
            </div>
          )}

          {inactive.length > 0 && (
            <div className="space-y-2">
              <p className="text-xs font-medium text-muted-foreground uppercase tracking-wide px-1">
                Revoked — {inactive.length}
              </p>
              {inactive.map((t) => (
                <TokenRow key={t.id} token={t} onRevoke={handleRevoke} isRevokingId={revokingId} />
              ))}
            </div>
          )}
        </div>
      )}

      <CreateTokenDialog open={createOpen} onOpenChange={setCreateOpen} />
    </div>
  )
}
