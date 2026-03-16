import { useState, useMemo, useEffect } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { useEventStream } from '@/hooks/useEventStream'
import {
  Puzzle,
  PlusCircle,
  Loader2,
  ToggleLeft,
  ToggleRight,
  ChevronDown,
  ChevronRight,
  Link as LinkIcon,
  FolderOpen,
  Globe2,
  Key,
  Check,
  Trash2,
  ShieldCheck,
  Github,
  Radio,
  Wrench,
  Lock,
  Activity,
  Save,
  Plus,
  X,
  Search,
  RefreshCw,
  Download,
  ArrowUpCircle,
  Store,
} from 'lucide-react'
import {
  usePlugins,
  usePluginInfo,
  useEnablePlugin,
  useDisablePlugin,
  useInstallPlugin,
  useUninstallPlugin,
  useUpgradePlugin,
  useConfigurePlugin,
  useLinkPluginSecret,
  useApprovePlugin,
  useRequestPluginApproval,
  type PluginSummary,
  type PluginVariable,
  type InstallPluginParams,
} from '@/services/pluginService'
import {
  useCatalog,
  useRefreshCatalog,
  type CatalogEntryWithStatus,
} from '@/services/catalogService'
import { useSecretEntries, useUpdateSecretAccessScope, type SecretEntry } from '@/services/secretService'
import { PageHeader } from '@/components/domain/page-header'
import { LoadingState } from '@/components/domain/loading-state'
import { ErrorState } from '@/components/domain/error-state'
import { EmptyState } from '@/components/domain/empty-state'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
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
import { toast } from '@/components/ui/use-toast'
import {
  Tabs,
  TabsList,
  TabsTrigger,
  TabsContent,
} from '@/components/ui/tabs'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
// Switch not available — using toggle button approach for bool variables.

// ─── Helpers ─────────────────────────────────────────────────────────────────

function statusVariant(
  status?: string,
): 'default' | 'secondary' | 'destructive' | 'outline' {
  switch ((status ?? '').toLowerCase()) {
    case 'active':
    case 'healthy':
    case 'enabled':
      return 'default'
    case 'loading':
    case 'loaded':
      return 'secondary'
    case 'error':
    case 'unhealthy':
      return 'destructive'
    default:
      return 'outline'
  }
}

function PluginStatusDot({ status }: { status?: string }) {
  const s = (status ?? '').toLowerCase()
  if (s === 'running' || s === 'enabled') {
    return (
      <span
        className="inline-block h-2 w-2 rounded-full bg-green-500 shrink-0"
        title="Running"
        aria-label="Status: Running"
      />
    )
  }
  if (s.includes('error')) {
    return (
      <span
        className="inline-block h-2 w-2 rounded-full bg-yellow-500 shrink-0"
        title="Error"
        aria-label="Status: Error"
      />
    )
  }
  if (s === 'disabled' || s === 'stopped') {
    return (
      <span
        className="inline-block h-2 w-2 rounded-full bg-red-500 shrink-0"
        title="Stopped"
        aria-label="Status: Stopped"
      />
    )
  }
  // installed, empty, or unknown
  return (
    <span
      className="inline-block h-2 w-2 rounded-full bg-muted-foreground/40 shrink-0"
      title="Installed"
      aria-label="Status: Installed"
    />
  )
}

const integrationIcons: Record<string, typeof Radio> = {
  channel: Radio,
  tooling: Wrench,
  secret_provider: Lock,
  sensor: Activity,
}

const integrationLabels: Record<string, string> = {
  channel: 'Channel',
  tooling: 'Tooling',
  secret_provider: 'Secret Provider',
  sensor: 'Sensor',
}

// ─── VariableEditor ──────────────────────────────────────────────────────────

function VariableEditor({
  variable,
  pluginName,
  currentValue,
  secrets: secretList,
}: {
  variable: PluginVariable
  pluginName: string
  currentValue?: string
  secrets: SecretEntry[]
}) {
  const [value, setValue] = useState(currentValue ?? (variable.default != null ? String(variable.default) : ''))
  const [arrayItems, setArrayItems] = useState<string[]>(() => {
    if (variable.type.startsWith('array_') && currentValue) {
      try { return JSON.parse(currentValue) } catch { return [] }
    }
    return Array.isArray(variable.default) ? variable.default.map(String) : []
  })
  const [newItem, setNewItem] = useState('')
  const { mutate: configure, isPending: isConfiguring } = useConfigurePlugin()
  const { mutate: linkSecret, isPending: isLinking } = useLinkPluginSecret()
  const [selectedSecret, setSelectedSecret] = useState('')

  function handleSave() {
    const saveValue = variable.type.startsWith('array_') ? JSON.stringify(arrayItems) : value
    configure(
      { name: pluginName, variable: variable.name, value: saveValue },
      {
        onSuccess: () => toast({ title: 'Saved', description: `${variable.name} updated.` }),
        onError: (err) => toast({ title: 'Error', description: (err as Error).message, variant: 'destructive' }),
      },
    )
  }

  function handleLinkSecret() {
    if (!selectedSecret) return
    linkSecret(
      { plugin_name: pluginName, variable: variable.name, secret_key: selectedSecret },
      {
        onSuccess: () => toast({ title: 'Secret linked', description: `${variable.name} linked to ${selectedSecret}.` }),
        onError: (err) => toast({ title: 'Error', description: (err as Error).message, variant: 'destructive' }),
      },
    )
  }

  function addArrayItem() {
    if (!newItem.trim()) return
    setArrayItems((prev) => [...prev, newItem.trim()])
    setNewItem('')
  }

  function removeArrayItem(idx: number) {
    setArrayItems((prev) => prev.filter((_, i) => i !== idx))
  }

  // Secret variable — show secret linking UI.
  if (variable.secret) {
    const isLinked = currentValue?.startsWith('secret:')
    return (
      <div className="border rounded-md p-2.5 space-y-1.5">
        <div className="flex items-center gap-2">
          <Key className="h-3.5 w-3.5 text-amber-500" />
          <code className="font-mono text-xs font-medium">{variable.name}</code>
          {variable.required && <Badge variant="destructive" className="text-[10px] px-1 py-0">required</Badge>}
          {isLinked ? (
            <Badge variant="default" className="text-[10px] px-1 py-0 gap-0.5"><Check className="h-2.5 w-2.5" /> linked</Badge>
          ) : (
            <Badge variant="outline" className="text-[10px] px-1 py-0">unlinked</Badge>
          )}
        </div>
        {variable.description && <p className="text-muted-foreground text-[11px]">{variable.description}</p>}
        {isLinked ? (
          <p className="text-[11px] text-muted-foreground">Linked: <code className="font-mono">{currentValue}</code></p>
        ) : (
          <div className="flex items-center gap-2 mt-1">
            <Select value={selectedSecret} onValueChange={setSelectedSecret}>
              <SelectTrigger className="h-7 text-xs flex-1">
                <SelectValue placeholder="Select a secret..." />
              </SelectTrigger>
              <SelectContent>
                {secretList.map((s) => (
                  <SelectItem key={s.key} value={s.key} className="text-xs">{s.key}</SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Button size="sm" variant="outline" className="h-7 text-xs" disabled={!selectedSecret || isLinking} onClick={handleLinkSecret}>
              {isLinking ? <Loader2 className="h-3 w-3 animate-spin" /> : 'Link'}
            </Button>
          </div>
        )}
      </div>
    )
  }

  // Array variable — show list editor.
  if (variable.type.startsWith('array_')) {
    return (
      <div className="border rounded-md p-2.5 space-y-1.5">
        <div className="flex items-center gap-2">
          <code className="font-mono text-xs font-medium">{variable.name}</code>
          {variable.required && <Badge variant="destructive" className="text-[10px] px-1 py-0">required</Badge>}
          {variable.dynamic && <Badge variant="secondary" className="text-[10px] px-1 py-0">dynamic</Badge>}
          <span className="text-[10px] text-muted-foreground">{variable.type}</span>
        </div>
        {variable.description && <p className="text-muted-foreground text-[11px]">{variable.description}</p>}
        <div className="space-y-1">
          {arrayItems.map((item, idx) => (
            <div key={idx} className="flex items-center gap-1">
              <code className="text-xs bg-muted/50 px-1.5 py-0.5 rounded flex-1 truncate">{item}</code>
              <button type="button" className="text-muted-foreground hover:text-destructive" onClick={() => removeArrayItem(idx)}>
                <X className="h-3 w-3" />
              </button>
            </div>
          ))}
          <div className="flex items-center gap-1">
            <Input className="h-7 text-xs flex-1" value={newItem} onChange={(e) => setNewItem(e.target.value)} placeholder="Add item..." onKeyDown={(e) => e.key === 'Enter' && addArrayItem()} />
            <Button size="sm" variant="ghost" className="h-7 px-2" onClick={addArrayItem}><Plus className="h-3 w-3" /></Button>
          </div>
        </div>
        <Button size="sm" variant="outline" className="h-7 text-xs gap-1" disabled={isConfiguring} onClick={handleSave}>
          {isConfiguring ? <Loader2 className="h-3 w-3 animate-spin" /> : <Save className="h-3 w-3" />} Save
        </Button>
      </div>
    )
  }

  // Bool variable — show switch.
  if (variable.type === 'bool') {
    return (
      <div className="border rounded-md p-2.5 space-y-1.5">
        <div className="flex items-center gap-2">
          <code className="font-mono text-xs font-medium">{variable.name}</code>
          {variable.required && <Badge variant="destructive" className="text-[10px] px-1 py-0">required</Badge>}
        </div>
        {variable.description && <p className="text-muted-foreground text-[11px]">{variable.description}</p>}
        <div className="flex items-center gap-2">
          <button
            type="button"
            className="text-muted-foreground hover:text-foreground transition-colors"
            onClick={() => {
              const next = value === 'true' ? 'false' : 'true'
              setValue(next)
              configure(
                { name: pluginName, variable: variable.name, value: next },
                { onError: (err: Error) => toast({ title: 'Error', description: err.message, variant: 'destructive' }) },
              )
            }}
          >
            {value === 'true' ? (
              <ToggleRight className="h-5 w-5 text-green-500" />
            ) : (
              <ToggleLeft className="h-5 w-5" />
            )}
          </button>
          <span className="text-xs text-muted-foreground">{value === 'true' ? 'Enabled' : 'Disabled'}</span>
        </div>
      </div>
    )
  }

  // String/number variable — show text/number input.
  const inputType = variable.type === 'int' || variable.type === 'float' ? 'number' : 'text'

  return (
    <div className="border rounded-md p-2.5 space-y-1.5">
      <div className="flex items-center gap-2">
        <code className="font-mono text-xs font-medium">{variable.name}</code>
        {variable.required && <Badge variant="destructive" className="text-[10px] px-1 py-0">required</Badge>}
        <span className="text-[10px] text-muted-foreground">{variable.type}</span>
      </div>
      {variable.description && <p className="text-muted-foreground text-[11px]">{variable.description}</p>}
      <div className="flex items-center gap-1">
        <Input className="h-7 text-xs flex-1" type={inputType} value={value} onChange={(e) => setValue(e.target.value)} />
        <Button size="sm" variant="outline" className="h-7 text-xs gap-1" disabled={isConfiguring} onClick={handleSave}>
          {isConfiguring ? <Loader2 className="h-3 w-3 animate-spin" /> : <Save className="h-3 w-3" />} Save
        </Button>
      </div>
    </div>
  )
}

// ─── PluginDetailPanel ────────────────────────────────────────────────────────

function PluginDetailPanel({ pluginId }: { pluginId: string }) {
  const { data: info, isLoading, error, refetch } = usePluginInfo(pluginId)
  const { data: secrets } = useSecretEntries()
  const { mutate: updateScope, isPending: isScoping } = useUpdateSecretAccessScope()
  const { mutate: approve, isPending: isApproving } = useApprovePlugin()
  const { mutate: requestApproval, isPending: isRequesting } = useRequestPluginApproval()
  const { mutate: reinstall, isPending: isReinstalling } = useUpgradePlugin()
  const [approvalStep, setApprovalStep] = useState<'idle' | 'pending' | 'verify'>('idle')
  const [approvalCode, setApprovalCode] = useState('')
  const [approvalChannelId, setApprovalChannelId] = useState('')

  if (isLoading) return <LoadingState message="Loading plugin details..." className="py-4" />
  if (error) return <ErrorState error={error as Error} onRetry={() => void refetch()} className="py-4" />
  if (!info) return null

  const secretList: SecretEntry[] = secrets ?? []
  const secretMap = new Map<string, SecretEntry>()
  for (const s of secretList) {
    secretMap.set(s.key.toLowerCase(), s)
  }

  function findMatchingSecret(envName: string): SecretEntry | undefined {
    const lower = envName.toLowerCase()
    if (secretMap.has(lower)) return secretMap.get(lower)
    for (const [k, v] of secretMap) {
      if (k.includes(lower) || lower.includes(k)) return v
    }
    return undefined
  }

  function handleScopeToPlugin(secret: SecretEntry) {
    if (!info?.source_hash) return
    updateScope(
      { key: secret.key, scope: secret.scope ?? 'global', access_scope: `plugin:${info.source_hash}` },
      {
        onSuccess: () => toast({ title: 'Secret scoped', description: `"${secret.key}" is now scoped to this plugin.` }),
        onError: (err) => toast({ title: 'Scope update failed', description: (err as Error).message, variant: 'destructive' }),
      },
    )
  }

  function handleRequestApproval() {
    if (!info || !approvalChannelId.trim()) return
    requestApproval(
      { name: info.name, channel_id: approvalChannelId.trim() },
      {
        onSuccess: (result) => {
          if (result.status === 'already_approved') {
            toast({ title: 'Already approved', description: result.message ?? '' })
            void refetch()
          } else {
            setApprovalStep('verify')
            toast({ title: 'Code sent', description: result.message ?? 'Check your channel for the verification code.' })
          }
        },
        onError: (err) => toast({ title: 'Request failed', description: (err as Error).message, variant: 'destructive' }),
      },
    )
  }

  function handleVerifyCode() {
    if (!info || !approvalCode.trim()) return
    approve(
      { name: info.name, code: approvalCode.trim() },
      {
        onSuccess: () => {
          toast({ title: 'Plugin approved', description: `"${info.name}" is now approved for event processing.` })
          setApprovalStep('idle')
          setApprovalCode('')
          void refetch()
        },
        onError: (err) => toast({ title: 'Verification failed', description: (err as Error).message, variant: 'destructive' }),
      },
    )
  }

  const IntIcon = info.integration ? integrationIcons[info.integration] ?? Puzzle : Puzzle

  return (
    <div className="space-y-3 text-xs">
      {/* Integration + Approval badges */}
      <div className="flex items-center gap-2 flex-wrap">
        {info.integration && (
          <Badge variant="secondary" className="text-[10px] gap-1">
            <IntIcon className="h-3 w-3" />
            {integrationLabels[info.integration] ?? info.integration}
          </Badge>
        )}
        {info.approval_required && (
          info.approved ? (
            <Badge variant="default" className="text-[10px] gap-0.5"><ShieldCheck className="h-2.5 w-2.5" /> Approved</Badge>
          ) : (
            <Badge variant="destructive" className="text-[10px]">Approval Required</Badge>
          )
        )}
      </div>

      {/* ── Challenge-Response Approval Flow ── */}
      {info.approval_required && !info.approved && (
        <div className="border border-amber-500/30 rounded-md p-3 bg-amber-500/5 space-y-2">
          <p className="text-xs font-medium text-amber-600">Challenge-Response Verification</p>
          {approvalStep === 'idle' && (
            <div className="space-y-1.5">
              <p className="text-[11px] text-muted-foreground">
                Enter a channel or DM ID to receive a verification code, proving the plugin connection works.
              </p>
              <div className="flex items-center gap-1.5">
                <Input
                  className="h-7 text-xs flex-1"
                  value={approvalChannelId}
                  onChange={(e) => setApprovalChannelId(e.target.value)}
                  placeholder="Channel or DM ID..."
                />
                <Button size="sm" variant="outline" className="h-7 text-xs gap-1" disabled={isRequesting || !approvalChannelId.trim()} onClick={handleRequestApproval}>
                  {isRequesting ? <Loader2 className="h-3 w-3 animate-spin" /> : <ShieldCheck className="h-3 w-3" />}
                  Send Code
                </Button>
              </div>
            </div>
          )}
          {approvalStep === 'verify' && (
            <div className="space-y-1.5">
              <p className="text-[11px] text-muted-foreground">
                A verification code was sent to your channel. Enter it below to approve the plugin.
              </p>
              <div className="flex items-center gap-1.5">
                <Input
                  className="h-7 text-xs flex-1 font-mono tracking-wider"
                  value={approvalCode}
                  onChange={(e) => setApprovalCode(e.target.value)}
                  placeholder="Enter 8-character code..."
                  maxLength={8}
                  autoFocus
                  onKeyDown={(e) => e.key === 'Enter' && handleVerifyCode()}
                />
                <Button size="sm" variant="default" className="h-7 text-xs gap-1" disabled={isApproving || approvalCode.length < 8} onClick={handleVerifyCode}>
                  {isApproving ? <Loader2 className="h-3 w-3 animate-spin" /> : <Check className="h-3 w-3" />}
                  Verify
                </Button>
                <Button size="sm" variant="ghost" className="h-7 text-xs" onClick={() => { setApprovalStep('idle'); setApprovalCode('') }}>
                  Retry
                </Button>
              </div>
            </div>
          )}
        </div>
      )}

      {info.author && (
        <p className="text-muted-foreground">
          <span className="font-medium text-foreground">Author: </span>
          {info.author}
        </p>
      )}
      {info.source_repo && (
        <div className="flex items-center gap-2">
          <p className="text-muted-foreground flex items-center gap-1 min-w-0">
            <LinkIcon className="h-3 w-3 shrink-0" />
            <span className="truncate">{info.source_repo}</span>
          </p>
          {info.source_repo.startsWith('github.com/') && (
            <Button
              size="sm"
              variant="outline"
              className="h-6 text-[11px] px-2 gap-1 shrink-0"
              disabled={isReinstalling}
              onClick={() =>
                reinstall(
                  { name: info.name, mode: 'github', value: info.source_repo! },
                  {
                    onSuccess: (result) => {
                      toast({
                        title: 'Plugin re-installed',
                        description: result.message || `${info.name} re-installed successfully.`,
                      })
                      void refetch()
                    },
                    onError: (err) =>
                      toast({
                        title: 'Re-install failed',
                        description: (err as Error).message,
                        variant: 'destructive',
                      }),
                  },
                )
              }
            >
              {isReinstalling ? <Loader2 className="h-3 w-3 animate-spin" /> : <RefreshCw className="h-3 w-3" />}
              Re-install
            </Button>
          )}
        </div>
      )}

      {/* ── Variables Section ── */}
      {info.variables && info.variables.length > 0 && (
        <div>
          <p className="font-medium text-foreground mb-2 flex items-center gap-1.5">
            <Key className="h-3.5 w-3.5" />
            Variables ({info.variables.length})
          </p>
          <div className="space-y-2">
            {info.variables.map((v) => (
              <VariableEditor
                key={v.name}
                variable={v}
                pluginName={info.name}
                currentValue={info.config?.[v.name]}
                secrets={secretList}
              />
            ))}
          </div>
        </div>
      )}

      {/* ── Legacy Env Vars / Secret Linking (for plugins without variables) ── */}
      {(!info.variables || info.variables.length === 0) && info.env && info.env.length > 0 && (
        <div>
          <p className="font-medium text-foreground mb-2 flex items-center gap-1.5">
            <Key className="h-3.5 w-3.5" />
            Required Secrets ({info.env.length})
          </p>
          <div className="space-y-2">
            {info.env.map((envVar) => {
              const match = findMatchingSecret(envVar.name)
              const isAlreadyScoped = match?.access_scope?.startsWith('plugin:')
              return (
                <div key={envVar.name} className="border rounded-md p-2.5 space-y-1">
                  <div className="flex items-center gap-2">
                    <code className="font-mono text-xs font-medium">{envVar.name}</code>
                    {envVar.required && <Badge variant="destructive" className="text-[10px] px-1 py-0">required</Badge>}
                    {match ? (
                      <Badge variant="default" className="text-[10px] px-1 py-0 gap-0.5"><Check className="h-2.5 w-2.5" /> linked</Badge>
                    ) : (
                      <Badge variant="outline" className="text-[10px] px-1 py-0">unlinked</Badge>
                    )}
                  </div>
                  {envVar.description && <p className="text-muted-foreground text-[11px]">{envVar.description}</p>}
                  {match ? (
                    <div className="flex items-center gap-2 text-[11px] mt-1">
                      <span className="text-muted-foreground">
                        Secret: <code className="font-mono">{match.key}</code>
                        {match.access_scope && <span className="ml-1">({match.access_scope})</span>}
                      </span>
                      {info.source_hash && !isAlreadyScoped && (
                        <Button size="sm" variant="outline" className="h-5 text-[10px] px-1.5" disabled={isScoping} onClick={() => handleScopeToPlugin(match)}>
                          {isScoping ? <Loader2 className="h-3 w-3 animate-spin" /> : 'Scope to this plugin'}
                        </Button>
                      )}
                      {isAlreadyScoped && <span className="text-green-600 text-[10px]">plugin-scoped</span>}
                    </div>
                  ) : (
                    <p className="text-muted-foreground text-[11px] mt-1">
                      No matching secret found. Add a secret named <code className="font-mono">{envVar.name}</code> in Settings &rarr; Secrets.
                    </p>
                  )}
                </div>
              )
            })}
          </div>
        </div>
      )}

      {/* ── Resources Section ── */}
      {info.resources && info.resources.length > 0 && (
        <div>
          <p className="font-medium text-foreground mb-1">Auto-Created Resources ({info.resources.length})</p>
          <div className="space-y-1">
            {info.resources.map((r) => (
              <div key={r.name} className="flex items-center gap-2 text-[11px] text-muted-foreground">
                <Badge variant="outline" className="text-[10px]">{r.type}</Badge>
                <span>{r.name}</span>
              </div>
            ))}
          </div>
        </div>
      )}

      {info.tools && info.tools.length > 0 && (
        <div>
          <p className="font-medium text-foreground mb-1">Tools ({info.tools.length})</p>
          <div className="flex flex-wrap gap-1">
            {info.tools.map((t) => (
              <code key={t} className="bg-muted/50 px-1.5 py-0.5 rounded text-xs font-mono">{t}</code>
            ))}
          </div>
        </div>
      )}
      {info.permissions && info.permissions.length > 0 && (
        <div>
          <p className="font-medium text-foreground mb-1">Permissions</p>
          <div className="flex flex-wrap gap-1">
            {info.permissions.map((p) => (
              <Badge key={p} variant="outline" className="text-xs">{p}</Badge>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}

// ─── PluginCard ───────────────────────────────────────────────────────────────

interface PluginCardProps {
  plugin: PluginSummary
  onToggle: (plugin: PluginSummary) => void
  onUninstall: (plugin: PluginSummary) => void
  isTogglingId: string | null
}

function PluginCard({ plugin, onToggle, onUninstall, isTogglingId }: PluginCardProps) {
  const [expanded, setExpanded] = useState(false)
  const isToggling = isTogglingId === plugin.id

  return (
    <div className="border rounded-lg overflow-hidden">
      <div className="flex items-center gap-2 px-4 py-3 flex-wrap sm:flex-nowrap">
        <button
          type="button"
          className="flex items-center gap-1.5 text-left hover:opacity-80 transition-opacity shrink-0"
          onClick={() => setExpanded((p) => !p)}
          aria-expanded={expanded}
          aria-label={`${expanded ? 'Collapse' : 'Expand'} ${plugin.name}`}
        >
          {expanded ? (
            <ChevronDown className="h-4 w-4 text-muted-foreground" />
          ) : (
            <ChevronRight className="h-4 w-4 text-muted-foreground" />
          )}
        </button>

        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2">
            <PluginStatusDot status={plugin.status} />
            <p className="text-sm font-medium truncate">{plugin.name}</p>
            {plugin.version && (
              <span className="text-xs text-muted-foreground shrink-0">v{plugin.version}</span>
            )}
          </div>
          {plugin.description && (
            <p className="text-xs text-muted-foreground mt-0.5 truncate">{plugin.description}</p>
          )}
        </div>

        <div className="flex items-center gap-2 shrink-0">
          {plugin.status && (
            <Badge variant={statusVariant(plugin.status)} className="text-xs capitalize">
              {plugin.status}
            </Badge>
          )}
          <button
            type="button"
            title={plugin.enabled ? 'Disable plugin' : 'Enable plugin'}
            disabled={isToggling}
            className="text-muted-foreground hover:text-foreground transition-colors disabled:opacity-50"
            onClick={() => onToggle(plugin)}
          >
            {isToggling ? (
              <Loader2 className="h-4 w-4 animate-spin" />
            ) : plugin.enabled ? (
              <ToggleRight className="h-5 w-5 text-green-500" />
            ) : (
              <ToggleLeft className="h-5 w-5" />
            )}
          </button>
          <button
            type="button"
            title="Uninstall plugin"
            className="text-muted-foreground hover:text-destructive transition-colors"
            onClick={() => onUninstall(plugin)}
          >
            <Trash2 className="h-4 w-4" />
          </button>
        </div>
      </div>

      {expanded && (
        <div className="border-t bg-muted/10 px-4 py-3">
          <PluginDetailPanel pluginId={plugin.id} />
        </div>
      )}
    </div>
  )
}

// ─── UninstallDialog ──────────────────────────────────────────────────────────

function UninstallDialog({
  plugin,
  open,
  onOpenChange,
  onConfirm,
  isPending,
}: {
  plugin: PluginSummary | null
  open: boolean
  onOpenChange: (open: boolean) => void
  onConfirm: () => void
  isPending: boolean
}) {
  if (!plugin) return null
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-sm">
        <DialogHeader>
          <DialogTitle>Uninstall {plugin.name}?</DialogTitle>
          <DialogDescription>
            This will remove the plugin, deregister all its tools, and clean up any auto-created resources (cron jobs, event handlers). This action cannot be undone.
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>Cancel</Button>
          <Button variant="destructive" disabled={isPending} onClick={onConfirm}>
            {isPending ? <><Loader2 className="h-4 w-4 mr-2 animate-spin" />Removing...</> : 'Uninstall'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// ─── InstallPluginDialog ──────────────────────────────────────────────────────

interface InstallPluginDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onInstall: (
    params: InstallPluginParams,
    cb: { onSuccess: () => void; onError: (e: Error) => void },
  ) => void
  isPending: boolean
  initialSource?: string
}

function InstallPluginDialog({ open, onOpenChange, onInstall, isPending, initialSource }: InstallPluginDialogProps) {
  const [mode, setMode] = useState<'remote' | 'local' | 'github'>(initialSource ? 'github' : 'github')
  const [value, setValue] = useState(initialSource ?? '')
  const [error, setError] = useState('')

  function resetForm() {
    setValue('')
    setError('')
    setMode('github')
  }

  function handleOpenChange(next: boolean) {
    if (!next) resetForm()
    onOpenChange(next)
  }

  function validate(): boolean {
    if (!value.trim()) {
      const labels: Record<string, string> = { local: 'Plugin directory path', remote: 'Manifest URL', github: 'GitHub source' }
      setError(`${labels[mode]} is required`)
      return false
    }
    if (mode === 'remote' && !value.trim().startsWith('http')) {
      setError('URL must start with http:// or https://')
      return false
    }
    if (mode === 'local' && !value.trim().startsWith('/')) {
      setError('Path must be absolute (start with /)')
      return false
    }
    if (mode === 'github' && !value.trim().startsWith('github.com/')) {
      setError('Source must start with github.com/')
      return false
    }
    setError('')
    return true
  }

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (!validate()) return
    onInstall(
      { mode, value: value.trim() },
      {
        onSuccess: () => {
          toast({ title: 'Plugin installed', description: 'Plugin has been installed and registered.' })
          handleOpenChange(false)
        },
        onError: (err) =>
          toast({ title: 'Install failed', description: err.message, variant: 'destructive' }),
      },
    )
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>Install Plugin</DialogTitle>
          <DialogDescription>
            Install a plugin from GitHub, a remote manifest URL, or a local directory.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="space-y-4">
          <Tabs value={mode} onValueChange={(v) => { setMode(v as typeof mode); setValue(''); setError('') }}>
            <TabsList className="w-full">
              <TabsTrigger value="github" className="flex-1 gap-1.5">
                <Github className="h-3.5 w-3.5" />
                GitHub
              </TabsTrigger>
              <TabsTrigger value="remote" className="flex-1 gap-1.5">
                <Globe2 className="h-3.5 w-3.5" />
                Remote URL
              </TabsTrigger>
              <TabsTrigger value="local" className="flex-1 gap-1.5">
                <FolderOpen className="h-3.5 w-3.5" />
                Local Path
              </TabsTrigger>
            </TabsList>
            <TabsContent value="github" className="mt-3 space-y-1.5">
              <Label htmlFor="plugin-source">GitHub Source *</Label>
              <Input
                id="plugin-source"
                value={value}
                onChange={(e) => setValue(e.target.value)}
                placeholder="github.com/org/plugin-name@v1.0.0"
                autoFocus
              />
              <p className="text-xs text-muted-foreground">
                GitHub owner/repo with optional @version. Downloads manifest and platform binary from the GitHub release.
              </p>
            </TabsContent>
            <TabsContent value="remote" className="mt-3 space-y-1.5">
              <Label htmlFor="plugin-url">Manifest URL *</Label>
              <Input
                id="plugin-url"
                value={value}
                onChange={(e) => setValue(e.target.value)}
                placeholder="https://raw.githubusercontent.com/org/repo/main/hyperax-plugin.yaml"
                autoFocus
              />
              <p className="text-xs text-muted-foreground">
                Direct URL to a <code className="text-xs">hyperax-plugin.yaml</code> manifest file.
              </p>
            </TabsContent>
            <TabsContent value="local" className="mt-3 space-y-1.5">
              <Label htmlFor="plugin-path">Plugin Directory *</Label>
              <Input
                id="plugin-path"
                value={value}
                onChange={(e) => setValue(e.target.value)}
                placeholder="/path/to/your/plugin"
                className="font-mono text-sm"
                autoFocus
              />
              <p className="text-xs text-muted-foreground">
                Absolute path to a directory containing <code className="text-xs">hyperax-plugin.yaml</code>. Ideal for local plugin development.
              </p>
            </TabsContent>
          </Tabs>

          {error && <p className="text-xs text-destructive">{error}</p>}

          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => handleOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={isPending}>
              {isPending ? (
                <>
                  <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                  Installing...
                </>
              ) : (
                'Install Plugin'
              )}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

// ─── CatalogCard ──────────────────────────────────────────────────────────────

const CATEGORY_LABELS: Record<string, string> = {
  channel: 'Channel',
  tooling: 'Tooling',
  secret_provider: 'Secret Provider',
  sensor: 'Sensor',
}

const CATEGORY_ICONS: Record<string, typeof Radio> = {
  channel: Radio,
  tooling: Wrench,
  secret_provider: Lock,
  sensor: Activity,
}

interface CatalogCardProps {
  entry: CatalogEntryWithStatus
  onInstall: (source: string) => void
  onUpgrade: (name: string, source: string) => void
  isUpgrading?: boolean
}

function CatalogCard({ entry, onInstall, onUpgrade, isUpgrading }: CatalogCardProps) {
  const CatIcon = CATEGORY_ICONS[entry.category] ?? Puzzle
  const catLabel = CATEGORY_LABELS[entry.category] ?? entry.category
  const hasUpdate =
    entry.installed &&
    entry.installed_version != null &&
    entry.installed_version !== entry.latest_version

  return (
    <div className="border rounded-lg p-4 flex flex-col gap-3 hover:border-border/80 transition-colors">
      <div className="flex items-start justify-between gap-2">
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2 flex-wrap">
            <p className="text-sm font-semibold truncate">{entry.display_name}</p>
            {entry.verified && (
              <ShieldCheck className="h-3.5 w-3.5 text-blue-500 shrink-0" aria-label="Verified" />
            )}
          </div>
          <p className="text-xs text-muted-foreground mt-0.5 line-clamp-2">{entry.description}</p>
        </div>
      </div>

      <div className="flex items-center gap-1.5 flex-wrap">
        <Badge variant="secondary" className="text-[10px] gap-1 px-1.5 py-0">
          <CatIcon className="h-2.5 w-2.5" />
          {catLabel}
        </Badge>
        <Badge variant="outline" className="text-[10px] px-1.5 py-0">
          v{entry.latest_version}
        </Badge>
        {entry.installed && (
          <Badge variant="default" className="text-[10px] px-1.5 py-0 gap-1">
            <Check className="h-2.5 w-2.5" />
            Installed{entry.installed_version ? ` v${entry.installed_version}` : ''}
          </Badge>
        )}
        {entry.installed && (
          <Badge
            variant={entry.enabled ? 'default' : 'outline'}
            className="text-[10px] px-1.5 py-0"
          >
            {entry.enabled ? 'Enabled' : 'Disabled'}
          </Badge>
        )}
      </div>

      <div className="flex items-center justify-between gap-2 mt-auto">
        <p className="text-[11px] text-muted-foreground truncate">{entry.author}</p>
        <div className="flex items-center gap-1.5 shrink-0">
          {hasUpdate && (
            <Button
              size="sm"
              variant="default"
              className="h-7 text-xs gap-1.5"
              disabled={isUpgrading}
              onClick={() => onUpgrade(entry.name, entry.source)}
            >
              {isUpgrading ? (
                <Loader2 className="h-3 w-3 animate-spin" />
              ) : (
                <ArrowUpCircle className="h-3 w-3" />
              )}
              Upgrade to v{entry.latest_version}
            </Button>
          )}
          {!entry.installed && (
            <Button
              size="sm"
              variant="outline"
              className="h-7 text-xs gap-1.5"
              onClick={() => onInstall(entry.source)}
            >
              <Download className="h-3 w-3" />
              Install
            </Button>
          )}
        </div>
      </div>
    </div>
  )
}

// ─── CatalogBrowser ───────────────────────────────────────────────────────────

const CATEGORY_FILTERS = [
  { value: '', label: 'All' },
  { value: 'channel', label: 'Channel' },
  { value: 'tooling', label: 'Tooling' },
  { value: 'secret_provider', label: 'Secret Provider' },
  { value: 'sensor', label: 'Sensor' },
]

interface CatalogBrowserProps {
  onInstallFromCatalog: (source: string) => void
}

function CatalogBrowser({ onInstallFromCatalog }: CatalogBrowserProps) {
  const [searchQuery, setSearchQuery] = useState('')
  const [categoryFilter, setCategoryFilter] = useState('')

  const { data: catalog, isLoading, error, refetch } = useCatalog()
  const { mutate: refreshCatalog, isPending: isRefreshing } = useRefreshCatalog()
  const { mutate: upgradePlugin, isPending: isUpgrading } = useUpgradePlugin()
  const items = Array.isArray(catalog) ? catalog : []

  const filtered = useMemo(() => {
    let result = items
    if (categoryFilter) {
      result = result.filter((e) => e.category === categoryFilter)
    }
    if (searchQuery.trim()) {
      const q = searchQuery.trim().toLowerCase()
      result = result.filter(
        (e) =>
          e.display_name.toLowerCase().includes(q) ||
          e.description.toLowerCase().includes(q) ||
          e.author.toLowerCase().includes(q) ||
          (e.tags ?? []).some((t) => t.toLowerCase().includes(q)),
      )
    }
    return result
  }, [items, categoryFilter, searchQuery])

  function handleRefresh() {
    refreshCatalog(undefined, {
      onSuccess: (result) =>
        toast({
          title: 'Catalog refreshed',
          description: result.message || `${result.added} added, ${result.updated} updated.`,
        }),
      onError: (err) =>
        toast({ title: 'Refresh failed', description: (err as Error).message, variant: 'destructive' }),
    })
  }

  function handleUpgrade(name: string, source: string) {
    upgradePlugin(
      { name, mode: 'github', value: source },
      {
        onSuccess: (result) =>
          toast({
            title: 'Plugin upgraded',
            description: result.message || `${name} upgraded to v${result.new_version}.`,
          }),
        onError: (err) =>
          toast({ title: 'Upgrade failed', description: (err as Error).message, variant: 'destructive' }),
      },
    )
  }

  if (isLoading) return <LoadingState message="Loading catalog..." />
  if (error) return <ErrorState error={error as Error} onRetry={() => void refetch()} />

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-2">
        <div className="relative flex-1">
          <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-muted-foreground pointer-events-none" />
          <Input
            className="pl-8 h-8 text-sm"
            placeholder="Search plugins..."
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
          />
        </div>
        <Button
          size="sm"
          variant="ghost"
          className="h-8 gap-1.5 text-xs text-muted-foreground shrink-0"
          disabled={isRefreshing}
          onClick={handleRefresh}
        >
          <RefreshCw className={`h-3.5 w-3.5 ${isRefreshing ? 'animate-spin' : ''}`} />
          Refresh
        </Button>
      </div>

      <div className="flex items-center gap-1.5 flex-wrap">
        {CATEGORY_FILTERS.map((f) => (
          <button
            key={f.value}
            type="button"
            onClick={() => setCategoryFilter(f.value)}
            className={`text-xs px-3 py-1 rounded-full border transition-colors ${
              categoryFilter === f.value
                ? 'bg-primary text-primary-foreground border-primary'
                : 'border-border text-muted-foreground hover:text-foreground hover:border-foreground/40'
            }`}
          >
            {f.label}
          </button>
        ))}
      </div>

      {filtered.length === 0 ? (
        <EmptyState
          icon={Store}
          title={items.length === 0 ? 'Catalog is empty' : 'No results'}
          description={
            items.length === 0
              ? 'Refresh the catalog to fetch the latest available plugins.'
              : 'No plugins match your search or filter.'
          }
          action={
            items.length === 0 ? (
              <Button size="sm" variant="outline" disabled={isRefreshing} onClick={handleRefresh}>
                {isRefreshing ? <Loader2 className="h-4 w-4 animate-spin mr-2" /> : <RefreshCw className="h-4 w-4 mr-2" />}
                Refresh Catalog
              </Button>
            ) : undefined
          }
        />
      ) : (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
          {filtered.map((entry) => (
            <CatalogCard key={entry.name} entry={entry} onInstall={onInstallFromCatalog} onUpgrade={handleUpgrade} isUpgrading={isUpgrading} />
          ))}
        </div>
      )}
    </div>
  )
}

// ─── PluginsPage ──────────────────────────────────────────────────────────────

export function PluginsPage() {
  const [installOpen, setInstallOpen] = useState(false)
  const [installSource, setInstallSource] = useState<string | undefined>(undefined)
  const [togglingId, setTogglingId] = useState<string | null>(null)
  const [uninstallTarget, setUninstallTarget] = useState<PluginSummary | null>(null)

  const { data: plugins, isLoading, error, refetch } = usePlugins()
  const { mutate: enablePlugin } = useEnablePlugin()
  const { mutate: disablePlugin } = useDisablePlugin()
  const { mutate: installPlugin, isPending: isInstalling } = useInstallPlugin()
  const { mutate: uninstallPlugin, isPending: isUninstalling } = useUninstallPlugin()

  // Subscribe to plugin.* WebSocket events and invalidate the plugins query
  // so status indicators update in real-time without waiting for the 10s polling interval.
  const wsQc = useQueryClient()
  const { events: wsEvents } = useEventStream({ patterns: ['plugin.*'] })

  useEffect(() => {
    if (wsEvents.length === 0) return
    void wsQc.invalidateQueries({ queryKey: ['plugins'] })
  }, [wsEvents, wsQc])

  function handleToggle(p: PluginSummary) {
    setTogglingId(p.id)
    const mutate = p.enabled ? disablePlugin : enablePlugin
    mutate(p.name, {
      onSuccess: () =>
        toast({
          title: p.enabled ? 'Plugin disabled' : 'Plugin enabled',
          description: `"${p.name}" is now ${p.enabled ? 'disabled' : 'enabled'}.`,
        }),
      onError: (err) =>
        toast({ title: 'Toggle failed', description: (err as Error).message, variant: 'destructive' }),
      onSettled: () => setTogglingId(null),
    })
  }

  function handleInstall(
    params: InstallPluginParams,
    cb: { onSuccess: () => void; onError: (e: Error) => void },
  ) {
    installPlugin(params, cb)
  }

  function handleUninstallConfirm() {
    if (!uninstallTarget) return
    uninstallPlugin(uninstallTarget.name, {
      onSuccess: () => {
        toast({ title: 'Plugin uninstalled', description: `"${uninstallTarget.name}" has been removed.` })
        setUninstallTarget(null)
      },
      onError: (err) =>
        toast({ title: 'Uninstall failed', description: (err as Error).message, variant: 'destructive' }),
    })
  }

  function handleInstallFromCatalog(source: string) {
    setInstallSource(source)
    setInstallOpen(true)
  }

  function handleInstallOpenChange(open: boolean) {
    setInstallOpen(open)
    if (!open) setInstallSource(undefined)
  }

  if (isLoading)
    return (
      <div className="p-6 space-y-6">
        <PageHeader title="Plugin Manager" description="Install and manage Hyperax plugins." />
        <LoadingState message="Loading plugins..." />
      </div>
    )

  if (error)
    return (
      <div className="p-6 space-y-6">
        <PageHeader title="Plugin Manager" description="Install and manage Hyperax plugins." />
        <ErrorState error={error as Error} onRetry={() => void refetch()} />
      </div>
    )

  const items = Array.isArray(plugins) ? plugins : []
  const enabledCount = items.filter((p) => p.enabled).length

  return (
    <div className="p-6 space-y-6">
      <PageHeader
        title="Plugin Manager"
        description="Install, enable, and configure Hyperax extension plugins."
      >
        <Button size="sm" onClick={() => { setInstallSource(undefined); setInstallOpen(true) }}>
          <PlusCircle className="h-4 w-4 mr-2" />
          Install Plugin
        </Button>
      </PageHeader>

      <Tabs defaultValue="installed">
        <TabsList>
          <TabsTrigger value="installed">
            Installed ({items.length})
          </TabsTrigger>
          <TabsTrigger value="catalog" className="gap-1.5">
            <Store className="h-3.5 w-3.5" />
            Catalog
          </TabsTrigger>
        </TabsList>

        <TabsContent value="installed" className="mt-4 space-y-4">
          {items.length > 0 && (
            <p className="text-sm text-muted-foreground">
              {enabledCount} of {items.length} plugin{items.length !== 1 ? 's' : ''} enabled
            </p>
          )}

          {items.length === 0 ? (
            <EmptyState
              icon={Puzzle}
              title="No plugins installed"
              description="Install a plugin from GitHub, a remote URL, or local directory to extend Hyperax."
              action={
                <Button size="sm" onClick={() => { setInstallSource(undefined); setInstallOpen(true) }}>
                  Install your first plugin
                </Button>
              }
            />
          ) : (
            <div className="space-y-2">
              {items.map((p) => (
                <PluginCard
                  key={p.id}
                  plugin={p}
                  onToggle={handleToggle}
                  onUninstall={setUninstallTarget}
                  isTogglingId={togglingId}
                />
              ))}
            </div>
          )}
        </TabsContent>

        <TabsContent value="catalog" className="mt-4">
          <CatalogBrowser onInstallFromCatalog={handleInstallFromCatalog} />
        </TabsContent>
      </Tabs>

      <InstallPluginDialog
        key={installSource ?? '__manual__'}
        open={installOpen}
        onOpenChange={handleInstallOpenChange}
        onInstall={handleInstall}
        isPending={isInstalling}
        initialSource={installSource}
      />

      <UninstallDialog
        plugin={uninstallTarget}
        open={!!uninstallTarget}
        onOpenChange={(open) => { if (!open) setUninstallTarget(null) }}
        onConfirm={handleUninstallConfirm}
        isPending={isUninstalling}
      />
    </div>
  )
}
