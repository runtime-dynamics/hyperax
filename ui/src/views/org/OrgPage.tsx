import { useState, useCallback, useRef, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { z } from 'zod'
import { Network, RefreshCw, X, ChevronDown, ChevronRight, UserPlus, Pencil, Check, MousePointerClick, Inbox, ArrowUpFromLine, Clock, AlertTriangle, RotateCcw, Ban, CheckCircle2, Loader2, MessageSquare, ArrowRight } from 'lucide-react'

import {
  useAgentHierarchy,
  useAgentStates,
  useAgentDetail,
  useAgentInboxes,
  useAgentCommLog,
  type RuntimeStateSummary,
  type CommLogEntry,
} from '@/services/orgService'
import { useAgents, useCreateAgent, useUpdateAgent, useDeleteAgent } from '@/services/agentService'
import { useTasks, type Task } from '@/services/taskService'
import { useRoleTemplates } from '@/services/roleTemplateService'
import { useEffectiveEngagementRules, type EngagementRule } from '@/services/engagementService'
import { useProviders, parseModels } from '@/services/providerService'
import {
  usePostboxStatus,
  useDeadLetters,
  useRetryDeadLetter,
  useDiscardDeadLetter,
  type DeadLetter,
} from '@/services/channelService'

import { useEventStreamInvalidation } from '@/hooks/useEventStreamInvalidation'
import { useFavorites } from '@/hooks/useFavorites'
import { PageHeader } from '@/components/domain/page-header'
import { LoadingState } from '@/components/domain/loading-state'
import { ErrorState } from '@/components/domain/error-state'
import { EmptyState } from '@/components/domain/empty-state'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
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
import { cn } from '@/lib/utils'

import { OrgFlowChart } from './OrgFlowChart'
import { AgentActivityDrawer } from './AgentActivityDrawer'
import { type FsmState, getFsmState, fsmStyles } from './fsm'
import { useAgentActivity } from '@/hooks/useAgentActivity'

function formatDate(iso?: string | null): string {
  if (!iso) return '—'
  try {
    return new Date(iso).toLocaleString()
  } catch {
    return iso
  }
}

// ─── Quick-create panel ───────────────────────────────────────────────────────

const createSchema = z.object({
  name: z.string().min(1, 'Name is required'),
})

type CreateForm = z.infer<typeof createSchema>
type CreateErrors = Partial<Record<keyof CreateForm, string>>

const CLEARANCE_LABELS: Record<number, string> = {
  0: 'Observer',
  1: 'Operator',
  2: 'Admin',
  3: 'Chief of Staff',
}

interface QuickCreatePanelProps {
  parentId: string | null
  onClose: () => void
  onCreated: () => void
}

function QuickCreatePanel({ parentId, onClose, onCreated }: QuickCreatePanelProps) {
  const [name, setName] = useState('')
  const [roleTemplateId, setRoleTemplateId] = useState<string>('__none__')
  const [personality, setPersonality] = useState('')
  const [clearanceLevel, setClearanceLevel] = useState<string>('1')
  const [providerId, setProviderId] = useState<string>('__none__')
  const [defaultModel, setDefaultModel] = useState<string>('__none__')
  const [errors, setErrors] = useState<CreateErrors>({})

  const { mutate: createAgent, isPending } = useCreateAgent()
  const { data: roleTemplates } = useRoleTemplates()
  const { data: providers } = useProviders()

  const templateList = Array.isArray(roleTemplates) ? roleTemplates : []
  const providerList = Array.isArray(providers) ? providers : []
  const selectedProvider = providerList.find((p) => p.id === (providerId === '__none__' ? '' : providerId)) ?? null
  const availableModels = selectedProvider ? parseModels(selectedProvider.models) : []

  function handleRoleTemplateChange(value: string) {
    setRoleTemplateId(value)
    if (value !== '__none__') {
      const tpl = templateList.find((t) => t.id === value)
      if (tpl) {
        setClearanceLevel(String(tpl.clearance_level))
        setPersonality(tpl.description)
      }
    }
  }

  function handleProviderChange(value: string) {
    setProviderId(value)
    setDefaultModel('__none__')
  }

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    const result = createSchema.safeParse({ name })
    if (!result.success) {
      const fe: CreateErrors = {}
      for (const issue of result.error.issues) {
        const f = issue.path[0] as keyof CreateErrors
        fe[f] = issue.message
      }
      setErrors(fe)
      return
    }
    setErrors({})

    createAgent(
      {
        name: result.data.name,
        persona_id: '',
        parent_agent_id: parentId ?? undefined,
        role_template_id: roleTemplateId === '__none__' ? undefined : roleTemplateId,
        personality: personality || undefined,
        clearance_level: Number(clearanceLevel),
        provider_id: providerId === '__none__' ? undefined : providerId,
        default_model: defaultModel === '__none__' ? undefined : defaultModel,
      },
      {
        onSuccess: () => {
          toast({ title: 'Agent created', description: `"${name}" added to the organization.` })
          onCreated()
          onClose()
        },
        onError: (err) =>
          toast({ title: 'Create failed', description: (err as Error).message, variant: 'destructive' }),
      },
    )
  }

  return (
    <Card className="border-primary/30">
      <CardHeader className="pb-3">
        <div className="flex items-center justify-between">
          <CardTitle className="text-sm flex items-center gap-2">
            <UserPlus className="h-4 w-4" />
            {parentId ? `Add agent under "${parentId}"` : 'Add root agent'}
          </CardTitle>
          <Button size="sm" variant="ghost" onClick={onClose} aria-label="Close quick-create panel">
            <X className="h-3.5 w-3.5" />
          </Button>
        </div>
      </CardHeader>
      <CardContent>
        <form onSubmit={handleSubmit} className="space-y-3 text-sm">
          <div className="space-y-1">
            <Label htmlFor="qc-name" className="text-xs">Agent Name *</Label>
            <Input id="qc-name" className="h-8 text-xs" value={name} onChange={(e) => setName(e.target.value)} placeholder="e.g. CTO, backend-guardian" autoFocus />
            {errors.name && <p className="text-xs text-destructive">{errors.name}</p>}
          </div>

          <div className="space-y-1">
            <Label htmlFor="qc-role-template" className="text-xs">Role Template</Label>
            <Select value={roleTemplateId} onValueChange={handleRoleTemplateChange}>
              <SelectTrigger id="qc-role-template" className="h-8 text-xs">
                <SelectValue placeholder="Select a role template…" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="__none__" className="text-xs text-muted-foreground">None</SelectItem>
                {templateList.map((t) => (
                  <SelectItem key={t.id} value={t.id} className="text-xs">
                    {t.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div className="space-y-1">
            <Label htmlFor="qc-personality" className="text-xs">Personality</Label>
            <Textarea
              id="qc-personality"
              className="text-xs resize-none"
              rows={2}
              value={personality}
              onChange={(e) => setPersonality(e.target.value)}
              placeholder="Describe this agent's personality and behavioral style…"
            />
          </div>

          <div className="space-y-1">
            <Label htmlFor="qc-clearance" className="text-xs">Clearance Level</Label>
            <Select value={clearanceLevel} onValueChange={setClearanceLevel}>
              <SelectTrigger id="qc-clearance" className="h-8 text-xs">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {Object.entries(CLEARANCE_LABELS).map(([level, label]) => (
                  <SelectItem key={level} value={level} className="text-xs">
                    {level} — {label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div className="grid grid-cols-2 gap-2">
            <div className="space-y-1">
              <Label htmlFor="qc-provider" className="text-xs">Provider</Label>
              <Select value={providerId} onValueChange={handleProviderChange}>
                <SelectTrigger id="qc-provider" className="h-8 text-xs">
                  <SelectValue placeholder="Inherit default" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="__none__" className="text-xs text-muted-foreground">Inherit default</SelectItem>
                  {providerList.map((p) => (
                    <SelectItem key={p.id} value={p.id} className="text-xs">
                      {p.name}{p.is_default ? ' (default)' : ''}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            <div className="space-y-1">
              <Label htmlFor="qc-model" className="text-xs">Model</Label>
              <Select value={defaultModel} onValueChange={setDefaultModel} disabled={availableModels.length === 0}>
                <SelectTrigger id="qc-model" className="h-8 text-xs">
                  <SelectValue placeholder="Inherit" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="__none__" className="text-xs text-muted-foreground">Inherit</SelectItem>
                  {availableModels.map((m) => (
                    <SelectItem key={m} value={m} className="text-xs font-mono">
                      {m}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>

          <div className="flex gap-2 pt-1">
            <Button type="submit" size="sm" disabled={isPending} className="flex-1">
              {isPending ? 'Creating…' : 'Create Agent'}
            </Button>
            <Button type="button" size="sm" variant="outline" onClick={onClose}>
              Cancel
            </Button>
          </div>
        </form>
      </CardContent>
    </Card>
  )
}

// ─── Reparent dialog ──────────────────────────────────────────────────────────

interface ReparentDialogProps {
  agentId: string
  agents: RuntimeStateSummary[]
  onConfirm: (newParentId: string | null) => void
  onClose: () => void
}

function ReparentDialog({ agentId, agents, onConfirm, onClose }: ReparentDialogProps) {
  const [selectedParent, setSelectedParent] = useState<string>('__root__')

  const candidates = agents.filter((a) => a.agent_id !== agentId)

  return (
    <Dialog open onOpenChange={(open) => { if (!open) onClose() }}>
      <DialogContent className="max-w-sm">
        <DialogHeader>
          <DialogTitle>Move Agent</DialogTitle>
          <DialogDescription>
            Select the new parent for <span className="font-medium">{agentId}</span>.
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-3 py-2">
          <Label htmlFor="reparent-select" className="text-xs">New Parent</Label>
          <Select value={selectedParent} onValueChange={setSelectedParent}>
            <SelectTrigger id="reparent-select">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="__root__">No parent (make root)</SelectItem>
              {candidates.map((a) => (
                <SelectItem key={a.agent_id} value={a.agent_id}>
                  {a.name ?? a.agent_id}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <DialogFooter>
          <Button variant="outline" size="sm" onClick={onClose}>Cancel</Button>
          <Button size="sm" onClick={() => onConfirm(selectedParent === '__root__' ? null : selectedParent)}>
            Move
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// ─── FlatGrid (fallback when no hierarchy data) ──────────────────────────────

interface FlatGridProps {
  agents: RuntimeStateSummary[]
  selectedId: string | null
  onSelect: (agentId: string) => void
  roleNameMap: Map<string, string>
  tasksByAgent: Map<string, Task[]>
  inboxByAgent: Map<string, number>
}

function FlatGrid({
  agents,
  selectedId,
  onSelect,
  roleNameMap,
  tasksByAgent,
  inboxByAgent,
}: FlatGridProps) {
  return (
    <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 gap-4 p-4">
      {agents.map((agent) => {
        const state = getFsmState(agent.status)
        const styles = fsmStyles[state]
        const displayName = agent.name ?? agent.agent_id
        const roleName = agent.role_template_id ? roleNameMap.get(agent.role_template_id) : undefined
        const agentTasks = tasksByAgent.get(agent.agent_id) ?? []
        const inboxSize = inboxByAgent.get(agent.agent_id) ?? 0

        return (
          <div
            key={agent.agent_id}
            className={cn(
              'w-full rounded-md border-l-4 border border-border text-left transition-all duration-150 shadow-sm cursor-pointer',
              styles.border,
              styles.bg,
              selectedId === agent.agent_id && 'ring-2 ring-primary ring-offset-1',
            )}
            onClick={() => onSelect(agent.agent_id)}
          >
            <div className="px-3 py-2.5 space-y-1">
              <p className="text-xs font-semibold truncate leading-tight" title={displayName}>
                {displayName}
              </p>
              {roleName && (
                <p className="text-[10px] text-muted-foreground truncate leading-tight">{roleName}</p>
              )}
              <div className="flex items-center gap-1.5">
                <span className={cn('inline-flex items-center rounded-full px-1.5 py-0.5 text-[10px] font-medium capitalize', styles.badge)}>
                  {agent.status}
                </span>
                {inboxSize > 0 && (
                  <span className="inline-flex items-center rounded-full px-1.5 py-0.5 text-[10px] font-medium bg-blue-100 text-blue-700 dark:bg-blue-900/50 dark:text-blue-300">
                    {inboxSize} msg
                  </span>
                )}
              </div>
              {agentTasks.length > 0 && (
                <p className="text-[10px] text-muted-foreground truncate">
                  {agentTasks.filter((t) => t.status === 'in_progress').length} active / {agentTasks.length} tasks
                </p>
              )}
            </div>
          </div>
        )
      })}
    </div>
  )
}

// ─── EngagementRulesPanel ─────────────────────────────────────────────────────

interface EngagementRulesPanelProps {
  agentId: string
}

function EngagementRulesPanel({ agentId }: EngagementRulesPanelProps) {
  const { data, isLoading, error } = useEffectiveEngagementRules(agentId)
  const [open, setOpen] = useState(true)

  const rules: EngagementRule[] = Array.isArray(data) ? data : []

  return (
    <div className="border rounded-md overflow-hidden">
      <button
        type="button"
        className="w-full flex items-center justify-between px-3 py-2 text-xs font-medium text-muted-foreground uppercase tracking-wide hover:bg-muted/40 transition-colors"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
      >
        Rules of Engagement
        {open ? (
          <ChevronDown className="h-3.5 w-3.5" />
        ) : (
          <ChevronRight className="h-3.5 w-3.5" />
        )}
      </button>

      {open && (
        <div className="px-3 py-2 space-y-2">
          {isLoading && (
            <div className="space-y-1.5">
              {[1, 2].map((i) => (
                <div key={i} className="h-10 rounded bg-muted/50 animate-pulse" />
              ))}
            </div>
          )}

          {!isLoading && error && (
            <p className="text-xs text-muted-foreground italic">Could not load engagement rules</p>
          )}

          {!isLoading && !error && rules.length === 0 && (
            <p className="text-xs text-muted-foreground italic">
              No engagement rules defined for this role
            </p>
          )}

          {!isLoading &&
            !error &&
            rules.map((rule) => (
              <div
                key={rule.id}
                className={cn(
                  'rounded-md border-l-2 pl-2.5 pr-2 py-2 bg-muted/20 space-y-1.5',
                  rule.disabled && 'opacity-50',
                )}
                style={{ borderLeftColor: rule.color }}
              >
                {/* Header row: trigger + source badge */}
                <div className="flex items-center justify-between gap-2 flex-wrap">
                  <span className="text-sm font-medium leading-tight">{rule.trigger}</span>
                  <span
                    className={cn(
                      'inline-flex items-center rounded-full px-1.5 py-0.5 text-xs font-medium shrink-0',
                      rule.source === 'custom'
                        ? 'bg-primary/15 text-primary'
                        : 'bg-muted text-muted-foreground',
                    )}
                  >
                    {rule.source === 'custom' ? 'Custom' : 'Role Default'}
                  </span>
                </div>

                {/* Chain steps */}
                {Array.isArray(rule.chain) && rule.chain.length > 0 && (
                  <div className="flex items-start flex-wrap gap-1">
                    {rule.chain.map((step, idx) => (
                      <div key={idx} className="flex items-center gap-1">
                        {idx > 0 && (
                          <span className="text-muted-foreground text-xs select-none">→</span>
                        )}
                        <div className="flex flex-col gap-0.5">
                          <span className="text-xs text-foreground/80">{step.action}</span>
                          {step.unassigned ? (
                            <span className="inline-flex items-center rounded-full px-1.5 py-0 text-xs font-medium bg-amber-100 text-amber-800 dark:bg-amber-900/40 dark:text-amber-300">
                              Unassigned
                            </span>
                          ) : step.agent_name ? (
                            <span className="inline-flex items-center rounded-full px-1.5 py-0 text-xs font-medium bg-muted text-muted-foreground">
                              {step.agent_name}
                            </span>
                          ) : (
                            <span className="inline-flex items-center rounded-full px-1.5 py-0 text-xs font-medium bg-muted/50 text-muted-foreground italic">
                              {step.role}
                            </span>
                          )}
                        </div>
                      </div>
                    ))}
                  </div>
                )}
              </div>
            ))}
        </div>
      )}
    </div>
  )
}

// ─── AgentMessagesPanel ──────────────────────────────────────────────────────

interface AgentMessagesPanelProps {
  entries: CommLogEntry[]
  isLoading: boolean
  agentName: string
}

function formatMsgTime(iso: string): string {
  try {
    return new Date(iso).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })
  } catch {
    return iso
  }
}

function AgentMessagesPanel({ entries, isLoading, agentName }: AgentMessagesPanelProps) {
  const [open, setOpen] = useState(true)

  // Sort newest-first for display
  const sorted = [...entries].sort(
    (a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime(),
  )

  return (
    <div className="border rounded-md overflow-hidden">
      <button
        type="button"
        className="w-full flex items-center justify-between px-3 py-2 text-xs font-medium text-muted-foreground uppercase tracking-wide hover:bg-muted/40 transition-colors"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
      >
        <span className="flex items-center gap-1.5">
          <MessageSquare className="h-3.5 w-3.5" />
          Recent Messages
          {sorted.length > 0 && (
            <span className="text-muted-foreground/70 normal-case font-normal">({sorted.length})</span>
          )}
        </span>
        {open ? <ChevronDown className="h-3.5 w-3.5" /> : <ChevronRight className="h-3.5 w-3.5" />}
      </button>

      {open && (
        <div className="max-h-[280px] overflow-y-auto">
          {isLoading && (
            <div className="px-3 py-4 space-y-2">
              {[1, 2, 3].map((i) => (
                <div key={i} className="h-10 rounded bg-muted/50 animate-pulse" />
              ))}
            </div>
          )}

          {!isLoading && sorted.length === 0 && (
            <div className="px-3 py-4 text-center text-xs text-muted-foreground/70">
              No messages yet
            </div>
          )}

          {!isLoading && sorted.length > 0 && (
            <div className="divide-y divide-border/40">
              {sorted.map((entry) => {
                const isFromAgent = entry.from_agent === agentName
                const truncatedContent =
                  entry.content.length > 200
                    ? entry.content.slice(0, 200) + '…'
                    : entry.content

                return (
                  <div key={entry.id} className="px-3 py-2 space-y-1">
                    <div className="flex items-center justify-between gap-2">
                      <div className="flex items-center gap-1 text-[10px] text-muted-foreground min-w-0">
                        <span className={cn('font-medium truncate', isFromAgent ? 'text-blue-600 dark:text-blue-400' : 'text-foreground')}>
                          {entry.from_agent}
                        </span>
                        <ArrowRight className="h-2.5 w-2.5 shrink-0" />
                        <span className={cn('font-medium truncate', !isFromAgent ? 'text-blue-600 dark:text-blue-400' : 'text-foreground')}>
                          {entry.to_agent}
                        </span>
                      </div>
                      <span className="text-[10px] text-muted-foreground tabular-nums shrink-0">
                        {formatMsgTime(entry.created_at)}
                      </span>
                    </div>
                    <p className={cn(
                      'text-xs break-words whitespace-pre-wrap',
                      isFromAgent ? 'text-muted-foreground' : 'text-foreground',
                    )}>
                      {truncatedContent}
                    </p>
                    {entry.session_id && (
                      <p className="text-[10px] font-mono text-muted-foreground/50 truncate" title={entry.session_id}>
                        session: {entry.session_id.slice(0, 8)}…
                      </p>
                    )}
                  </div>
                )
              })}
            </div>
          )}
        </div>
      )}
    </div>
  )
}

// ─── AgentDetailPanel ─────────────────────────────────────────────────────────

interface AgentDetailPanelProps {
  agentId: string
  onClose: () => void
  onReparent: (agentId: string) => void
}

function AgentDetailPanel({ agentId, onClose, onReparent }: AgentDetailPanelProps) {
  const { data: detail, isLoading, error, refetch } = useAgentDetail(agentId)
  const { data: commLog, isLoading: commLogLoading } = useAgentCommLog(agentId)
  const [expandedSection, setExpandedSection] = useState<string | null>(null)
  const [editingName, setEditingName] = useState(false)
  const [nameValue, setNameValue] = useState('')
  const nameInputRef = useRef<HTMLInputElement>(null)
  const { data: providers } = useProviders()
  const updateAgent = useUpdateAgent()

  const providerList = Array.isArray(providers) ? providers : []
  const currentProviderId = (detail?.config?.provider_id as string) ?? ''
  const currentModel = (detail?.config?.default_model as string) ?? ''
  const currentChatModel = (detail?.config?.chat_model as string) ?? ''
  const selectedProvider = providerList.find((p) => p.id === currentProviderId) ?? null
  const availableModels = selectedProvider ? parseModels(selectedProvider.models) : []

  function handleProviderChange(value: string) {
    const newProviderId = value === '__none__' ? '' : value
    updateAgent.mutate(
      { agent_id: agentId, provider_id: newProviderId, default_model: '', chat_model: '' },
      {
        onSuccess: () => {
          toast({ title: 'Provider updated' })
          void refetch()
        },
        onError: (err) =>
          toast({ title: 'Update failed', description: (err as Error).message, variant: 'destructive' }),
      },
    )
  }

  function handleModelChange(field: 'default_model' | 'chat_model', value: string) {
    const newModel = value === '__none__' ? '' : value
    updateAgent.mutate(
      { agent_id: agentId, [field]: newModel },
      {
        onSuccess: () => {
          toast({ title: field === 'default_model' ? 'Work model updated' : 'Chat model updated' })
          void refetch()
        },
        onError: (err) =>
          toast({ title: 'Update failed', description: (err as Error).message, variant: 'destructive' }),
      },
    )
  }

  function startRename() {
    setNameValue(detail?.name ?? agentId)
    setEditingName(true)
    setTimeout(() => nameInputRef.current?.select(), 0)
  }

  function submitRename() {
    const trimmed = nameValue.trim()
    if (!trimmed || trimmed === (detail?.name ?? agentId)) {
      setEditingName(false)
      return
    }
    updateAgent.mutate(
      { agent_id: agentId, name: trimmed },
      {
        onSuccess: () => {
          toast({ title: 'Agent renamed', description: `Now "${trimmed}".` })
          setEditingName(false)
          void refetch()
        },
        onError: (err) =>
          toast({ title: 'Rename failed', description: (err as Error).message, variant: 'destructive' }),
      },
    )
  }

  function toggleSection(label: string) {
    setExpandedSection((prev) => (prev === label ? null : label))
  }

  const state = detail ? getFsmState(detail.status) : 'unknown'
  const styles = fsmStyles[state]

  return (
    <Card className="h-full overflow-auto">
      <CardHeader className="pb-3">
        <div className="flex items-center justify-between">
          {editingName ? (
            <div className="flex items-center gap-1.5">
              <Input
                ref={nameInputRef}
                className="h-8 text-base font-semibold w-48"
                value={nameValue}
                onChange={(e) => setNameValue(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') submitRename()
                  if (e.key === 'Escape') setEditingName(false)
                }}
                autoFocus
                disabled={updateAgent.isPending}
              />
              <Button
                size="sm"
                variant="ghost"
                onClick={submitRename}
                disabled={updateAgent.isPending}
                className="h-7 w-7 p-0"
                aria-label="Confirm rename"
              >
                <Check className="h-3.5 w-3.5" />
              </Button>
              <Button
                size="sm"
                variant="ghost"
                onClick={() => setEditingName(false)}
                className="h-7 w-7 p-0"
                aria-label="Cancel rename"
              >
                <X className="h-3.5 w-3.5" />
              </Button>
            </div>
          ) : (
            <CardTitle
              className="text-base flex items-center gap-1.5 cursor-pointer group"
              onClick={startRename}
              title="Click to rename"
            >
              {detail?.name ?? agentId}
              <Pencil className="h-3 w-3 text-muted-foreground opacity-0 group-hover:opacity-100 transition-opacity" />
            </CardTitle>
          )}
          <div className="flex items-center gap-2">
            <Button
              size="sm"
              variant="outline"
              onClick={() => onReparent(agentId)}
              className="h-7 text-xs"
              aria-label="Move agent"
            >
              Move
            </Button>
            <Button
              size="sm"
              variant="ghost"
              onClick={() => void refetch()}
              disabled={isLoading}
              aria-label="Refresh agent details"
            >
              <RefreshCw className={cn('h-3.5 w-3.5', isLoading && 'animate-spin')} />
            </Button>
            <Button size="sm" variant="ghost" onClick={onClose} aria-label="Close detail panel">
              <X className="h-3.5 w-3.5" />
            </Button>
          </div>
        </div>
        <code className="text-xs font-mono text-muted-foreground">{agentId}</code>
      </CardHeader>

      <CardContent className="text-xs space-y-4">
        {isLoading && <LoadingState message="Loading agent details…" className="py-4" />}
        {error && <ErrorState error={error as Error} onRetry={() => void refetch()} className="py-4" />}

        {detail && (
          <>
            <div className="grid grid-cols-2 sm:grid-cols-3 gap-x-6 gap-y-2 text-muted-foreground">
              <div>
                <span className="font-medium text-foreground">Status: </span>
                <span
                  className={cn(
                    'inline-flex items-center rounded-full px-1.5 py-0.5 text-xs font-medium capitalize',
                    styles.badge,
                  )}
                >
                  {detail.status}
                </span>
              </div>
              <div>
                <span className="font-medium text-foreground">Workspace: </span>
                {detail.workspace ?? '—'}
              </div>
              <div>
                <span className="font-medium text-foreground">Tool Calls: </span>
                {detail.tool_calls?.toLocaleString() ?? '—'}
              </div>
              <div>
                <span className="font-medium text-foreground">Started: </span>
                {formatDate(detail.started_at)}
              </div>
              <div>
                <span className="font-medium text-foreground">Last Active: </span>
                {formatDate(detail.last_active_at)}
              </div>
              {(detail.status_reason || detail.error) && (
                <div className="col-span-full text-destructive">
                  <span className="font-medium">Error: </span>
                  {detail.status_reason || detail.error}
                </div>
              )}
            </div>

            {/* Inline provider / model editing */}
            <div className="border rounded-md p-3 space-y-3">
              <p className="text-xs font-medium text-muted-foreground uppercase tracking-wide">Provider & Models</p>
              <div className="space-y-3">
                <div className="space-y-1">
                  <Label htmlFor="detail-provider" className="text-xs">Provider</Label>
                  <Select
                    value={currentProviderId || '__none__'}
                    onValueChange={handleProviderChange}
                    disabled={updateAgent.isPending}
                  >
                    <SelectTrigger id="detail-provider" className="h-8 text-xs">
                      <SelectValue placeholder="Inherit default" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="__none__" className="text-xs text-muted-foreground">Inherit default</SelectItem>
                      {providerList.map((p) => (
                        <SelectItem key={p.id} value={p.id} className={cn('text-xs', !p.is_enabled && 'text-muted-foreground/50')}>
                          {p.name}{p.is_default ? ' (default)' : ''}{!p.is_enabled ? ' (disabled)' : ''}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <div className="grid grid-cols-2 gap-3">
                  <div className="space-y-1">
                    <Label htmlFor="detail-work-model" className="text-xs">Work Model</Label>
                    <Select
                      value={currentModel || '__none__'}
                      onValueChange={(v) => handleModelChange('default_model', v)}
                      disabled={availableModels.length === 0 || updateAgent.isPending}
                    >
                      <SelectTrigger id="detail-work-model" className="h-8 text-xs">
                        <SelectValue placeholder="Inherit" />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="__none__" className="text-xs text-muted-foreground">Inherit</SelectItem>
                        {availableModels.map((m) => (
                          <SelectItem key={m} value={m} className="text-xs font-mono">
                            {m}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </div>
                  <div className="space-y-1">
                    <Label htmlFor="detail-chat-model" className="text-xs">Chat Model</Label>
                    <Select
                      value={currentChatModel || '__none__'}
                      onValueChange={(v) => handleModelChange('chat_model', v)}
                      disabled={availableModels.length === 0 || updateAgent.isPending}
                    >
                      <SelectTrigger id="detail-chat-model" className="h-8 text-xs">
                        <SelectValue placeholder="Same as work" />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="__none__" className="text-xs text-muted-foreground">Same as work model</SelectItem>
                        {availableModels.map((m) => (
                          <SelectItem key={m} value={m} className="text-xs font-mono">
                            {m}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </div>
                </div>
              </div>
              {selectedProvider && !selectedProvider.is_enabled && (
                <div className="flex items-center gap-2 px-3 py-2 rounded-md bg-destructive/10 border border-destructive/20 text-destructive text-xs">
                  <AlertTriangle className="h-3.5 w-3.5 shrink-0" />
                  <span>This provider is disabled. The agent cannot process messages until it is re-enabled in Settings.</span>
                </div>
              )}
            </div>

            {/* Recent Messages */}
            <AgentMessagesPanel
              entries={commLog ?? []}
              isLoading={commLogLoading}
              agentName={detail?.name ?? agentId}
            />

            {/* Rules of Engagement */}
            <EngagementRulesPanel agentId={agentId} />

            {(['Config', 'Metrics', 'Metadata'] as const).map((label) => {
              const key = label.toLowerCase() as 'config' | 'metrics' | 'metadata'
              const data = detail[key]
              if (!data || Object.keys(data).length === 0) return null
              const isOpen = expandedSection === label

              return (
                <div key={label} className="border rounded-md overflow-hidden">
                  <button
                    type="button"
                    className="w-full flex items-center justify-between px-3 py-2 text-xs font-medium text-muted-foreground uppercase tracking-wide hover:bg-muted/40 transition-colors"
                    onClick={() => toggleSection(label)}
                    aria-expanded={isOpen}
                  >
                    {label}
                    {isOpen ? (
                      <ChevronDown className="h-3.5 w-3.5" />
                    ) : (
                      <ChevronRight className="h-3.5 w-3.5" />
                    )}
                  </button>
                  {isOpen && (
                    <pre className="text-xs font-mono bg-muted/30 px-3 py-2 overflow-auto whitespace-pre-wrap break-all max-h-48">
                      {JSON.stringify(data, null, 2)}
                    </pre>
                  )}
                </div>
              )
            })}
          </>
        )}
      </CardContent>
    </Card>
  )
}

// ─── Mail Health ──────────────────────────────────────────────────────────────

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

function MailHealthPanel() {
  const [actingId, setActingId] = useState<string | null>(null)
  const { data: postbox, refetch: refetchPostbox } = usePostboxStatus()
  const { data: dlData, refetch: refetchDL } = useDeadLetters()
  const { mutate: retry } = useRetryDeadLetter()
  const { mutate: discard } = useDiscardDeadLetter()

  const status = postbox ?? { inbound_count: 0, outbound_count: 0, last_poll: null }
  const deadLetters: DeadLetter[] = Array.isArray(dlData?.dead_letters) ? dlData.dead_letters : []

  function handleRefresh() {
    void refetchPostbox()
    void refetchDL()
  }

  function handleRetry(id: string) {
    setActingId(id)
    retry({ dead_letter_id: id }, {
      onSuccess: () => setActingId(null),
      onError: () => setActingId(null),
    })
  }

  function handleDiscard(id: string) {
    setActingId(id)
    discard({ dead_letter_id: id }, {
      onSuccess: () => setActingId(null),
      onError: () => setActingId(null),
    })
  }

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-semibold">Mail Health</h3>
        <Button variant="ghost" size="sm" className="h-7 text-xs text-muted-foreground" onClick={handleRefresh}>
          <RefreshCw className="h-3.5 w-3.5 mr-1.5" />
          Refresh
        </Button>
      </div>

      <div className="grid grid-cols-3 gap-3">
        <Card>
          <CardContent className="px-3 py-3 flex items-center gap-2.5">
            <div className="h-7 w-7 rounded-md bg-blue-500/10 text-blue-600 dark:text-blue-400 flex items-center justify-center shrink-0">
              <Inbox className="h-3.5 w-3.5" />
            </div>
            <div>
              <p className="text-lg font-semibold tabular-nums leading-tight">{status.inbound_count}</p>
              <p className="text-xs text-muted-foreground">Inbound</p>
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="px-3 py-3 flex items-center gap-2.5">
            <div className="h-7 w-7 rounded-md bg-purple-500/10 text-purple-600 dark:text-purple-400 flex items-center justify-center shrink-0">
              <ArrowUpFromLine className="h-3.5 w-3.5" />
            </div>
            <div>
              <p className="text-lg font-semibold tabular-nums leading-tight">{status.outbound_count}</p>
              <p className="text-xs text-muted-foreground">Outbound</p>
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="px-3 py-3 flex items-center gap-2.5">
            <div className="h-7 w-7 rounded-md bg-muted text-muted-foreground flex items-center justify-center shrink-0">
              <Clock className="h-3.5 w-3.5" />
            </div>
            <div>
              <p className="text-sm font-semibold leading-tight">
                {status.last_poll ? formatRelativeTime(status.last_poll) : 'Never'}
              </p>
              <p className="text-xs text-muted-foreground">Last poll</p>
            </div>
          </CardContent>
        </Card>
      </div>

      {deadLetters.length > 0 && (
        <div className="rounded-md border overflow-hidden">
          <div className="px-3 py-2 bg-muted/40 border-b flex items-center justify-between">
            <span className="text-xs font-medium text-muted-foreground flex items-center gap-1.5">
              <AlertTriangle className="h-3.5 w-3.5 text-yellow-500" />
              Dead Letters ({deadLetters.length})
            </span>
          </div>
          <div className="divide-y max-h-[200px] overflow-y-auto">
            {deadLetters.map((dl) => (
              <div key={dl.id} className="px-3 py-2 flex items-center justify-between gap-3 text-sm">
                <div className="min-w-0 flex-1">
                  <p className="text-xs font-mono text-muted-foreground truncate">{dl.mail_id}</p>
                  <p className="text-xs text-foreground truncate">{dl.reason}</p>
                </div>
                <div className="flex items-center gap-1 shrink-0">
                  <Button size="sm" variant="ghost" className="h-6 w-6 p-0" onClick={() => handleRetry(dl.id)} disabled={actingId === dl.id} title="Retry">
                    {actingId === dl.id ? <Loader2 className="h-3 w-3 animate-spin" /> : <RotateCcw className="h-3 w-3" />}
                  </Button>
                  <Button size="sm" variant="ghost" className="h-6 w-6 p-0 text-muted-foreground hover:text-destructive" onClick={() => handleDiscard(dl.id)} disabled={actingId === dl.id} title="Discard">
                    {actingId === dl.id ? <Loader2 className="h-3 w-3 animate-spin" /> : <Ban className="h-3 w-3" />}
                  </Button>
                </div>
              </div>
            ))}
          </div>
        </div>
      )}

      {deadLetters.length === 0 && (
        <div className="flex items-center gap-2 text-xs text-muted-foreground px-1">
          <CheckCircle2 className="h-3.5 w-3.5 text-green-500" />
          All messages delivered — no quarantined items
        </div>
      )}
    </div>
  )
}

// ─── OrgPage ──────────────────────────────────────────────────────────────────

export default function OrgPage() {
  const navigate = useNavigate()
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [quickCreateParentId, setQuickCreateParentId] = useState<string | null | undefined>(
    undefined,
  ) // undefined = panel closed
  const [reparentTargetId, setReparentTargetId] = useState<string | null>(null)
  const [activityAgentId, setActivityAgentId] = useState<string | null>(null)
  const [activityDrawerOpen, setActivityDrawerOpen] = useState(false)

  // Track active agent thinking state via event stream for node indicators
  // useAgentActivity subscribes to tooluse.* and chat.completion.* events
  // We pass activityAgentId to track a specific agent; null = no focused agent
  const drawerActivity = useAgentActivity(activityDrawerOpen ? activityAgentId : null)

  // Per-node thinking indicators: track the most recently active agent
  const [thinkingAgentId, setThinkingAgentId] = useState<string | null>(null)
  const [thinkingTool, setThinkingTool] = useState<string | null>(null)

  // Watch drawer activity to also update per-node thinking indicators
  useEffect(() => {
    if (drawerActivity.isActive && activityAgentId) {
      setThinkingAgentId(activityAgentId)
      setThinkingTool(drawerActivity.currentTool)
    } else if (!drawerActivity.isActive) {
      setThinkingAgentId(null)
      setThinkingTool(null)
    }
  }, [drawerActivity.isActive, drawerActivity.currentTool, activityAgentId])

  const handleOpenActivity = useCallback((agentId: string) => {
    setActivityAgentId(agentId)
    setActivityDrawerOpen(true)
  }, [])
  const handleCloseActivity = useCallback(() => {
    setActivityDrawerOpen(false)
  }, [])

  // Favorites management
  const { favoriteIds, toggleFavorite } = useFavorites()

  // Navigate to chat page with an agent pre-selected
  const handleStartChat = useCallback(
    (agentId: string) => navigate(`/chat?agent=${agentId}`),
    [navigate],
  )

  const {
    data: hierarchy,
    isLoading: hierarchyLoading,
    error: hierarchyError,
    refetch: refetchHierarchy,
  } = useAgentHierarchy()

  const {
    data: agentStates,
    isLoading: statesLoading,
    error: statesError,
    refetch: refetchStates,
  } = useAgentStates()

  const updateAgent = useUpdateAgent()

  useEventStreamInvalidation()

  // Fetch agents from the new agents table
  const { data: agentList } = useAgents()

  // Fetch providers for disabled-state detection on nodes
  const { data: providersData } = useProviders()
  const orgProviderList = Array.isArray(providersData) ? providersData : []

  // Fetch tasks and inbox sizes for hover cards
  const { data: allTasks } = useTasks('hyperax')
  const { data: agentInboxes } = useAgentInboxes()

  const handleSelect = useCallback((agentId: string) => {
    setSelectedId((prev) => (prev === agentId ? null : agentId))
    setQuickCreateParentId(undefined)
  }, [])

  const handleRefreshAll = useCallback(() => {
    void refetchHierarchy()
    void refetchStates()
  }, [refetchHierarchy, refetchStates])

  const handleAddChild = useCallback((parentId: string) => {
    setQuickCreateParentId(parentId)
    setSelectedId(null)
  }, [])

  const handleAddRoot = useCallback(() => {
    setQuickCreateParentId(null)
    setSelectedId(null)
  }, [])

  // React Flow reparent via node drag-and-drop
  const handleFlowDrop = useCallback(
    (draggedId: string, targetId: string) => {
      if (draggedId === targetId) return
      updateAgent.mutate(
        { agent_id: draggedId, parent_agent_id: targetId },
        {
          onSuccess: () => {
            toast({ title: 'Agent moved', description: `Moved "${draggedId}" under "${targetId}".` })
            void refetchHierarchy()
          },
          onError: (err) =>
            toast({ title: 'Move failed', description: (err as Error).message, variant: 'destructive' }),
        },
      )
    },
    [updateAgent, refetchHierarchy],
  )

  const handleResetState = useCallback(
    (agentId: string) => {
      updateAgent.mutate(
        { agent_id: agentId, status: 'idle' },
        {
          onSuccess: () => {
            toast({ title: 'Agent reset', description: `Agent state reset to idle.` })
            void refetchStates()
          },
          onError: (err) =>
            toast({ title: 'Reset failed', description: (err as Error).message, variant: 'destructive' }),
        },
      )
    },
    [updateAgent, refetchStates],
  )

  const deleteAgent = useDeleteAgent()
  const handleDeleteAgent = useCallback(
    (agentId: string) => {
      deleteAgent.mutate(agentId, {
        onSuccess: () => {
          toast({ title: 'Agent deleted' })
          if (selectedId === agentId) setSelectedId(null)
          void refetchHierarchy()
          void refetchStates()
        },
        onError: (err) =>
          toast({ title: 'Delete failed', description: (err as Error).message, variant: 'destructive' }),
      })
    },
    [deleteAgent, selectedId, refetchHierarchy, refetchStates],
  )

  const handleReparentConfirm = useCallback(
    (newParentId: string | null) => {
      if (!reparentTargetId) return
      updateAgent.mutate(
        { agent_id: reparentTargetId, parent_agent_id: newParentId ?? '' },
        {
          onSuccess: () => {
            toast({ title: 'Agent moved', description: newParentId ? `Now reporting to "${newParentId}".` : 'Moved to root.' })
            void refetchHierarchy()
            setReparentTargetId(null)
          },
          onError: (err) =>
            toast({ title: 'Move failed', description: (err as Error).message, variant: 'destructive' }),
        },
      )
    },
    [reparentTargetId, updateAgent, refetchHierarchy],
  )

  const isLoading = hierarchyLoading || statesLoading
  const runtimeAgents = Array.isArray(agentStates) ? agentStates : []

  // Fall back to agent instances when no runtime agents have connected
  const agentsFromDb: RuntimeStateSummary[] = (Array.isArray(agentList) ? agentList : []).map((a) => ({
    agent_id: a.id,
    name: a.name,
    status: a.status || 'idle',
    status_reason: a.status_reason,
    error: a.status_reason,
    workspace: a.workspace_id,
    role_template_id: a.role_template_id,
    default_model: a.default_model,
    provider_id: a.provider_id,
  }))

  const { data: roleTemplates } = useRoleTemplates()
  const roleNameMap = new Map<string, string>(
    (Array.isArray(roleTemplates) ? roleTemplates : []).map((t) => [t.id, t.name]),
  )

  const agents = runtimeAgents.length > 0 ? runtimeAgents : agentsFromDb

  // Enrich with role_template_id and default_model from DB agents (runtime states don't carry them)
  const dbAgentMap = new Map(
    (Array.isArray(agentList) ? agentList : []).map((a) => [a.id, a]),
  )
  for (const agent of agents) {
    const dbAgent = dbAgentMap.get(agent.agent_id)
    if (dbAgent) {
      if (!agent.role_template_id) agent.role_template_id = dbAgent.role_template_id
      if (!agent.default_model) agent.default_model = dbAgent.default_model
      if (!agent.provider_id) agent.provider_id = dbAgent.provider_id
    }
  }

  const stateMap = new Map<string, RuntimeStateSummary>(agents.map((a) => [a.agent_id, a]))

  // Build set of agent IDs whose assigned provider is disabled
  const disabledProviderAgentIds = new Set<string>(
    agents
      .filter((a) => {
        if (!a.provider_id) return false
        const provider = orgProviderList.find((p) => p.id === a.provider_id)
        return !!provider && !provider.is_enabled
      })
      .map((a) => a.agent_id),
  )

  // Build map of agentId → provider kind for node color-coding
  const providerKindByAgentId = new Map<string, string>()
  for (const a of agents) {
    if (!a.provider_id) continue
    const provider = orgProviderList.find((p) => p.id === a.provider_id)
    if (provider) providerKindByAgentId.set(a.agent_id, provider.kind)
  }

  // Build per-agent task map. The normalized `assignee` field contains the
  // agent UUID (mapped from backend `assignee_agent_id` by normalizeTask).
  const tasksByAgent = new Map<string, Task[]>()
  for (const t of Array.isArray(allTasks) ? allTasks : []) {
    const assignee = t.assignee || ''
    if (!assignee) continue
    const existing = tasksByAgent.get(assignee)
    if (existing) existing.push(t)
    else tasksByAgent.set(assignee, [t])
  }

  // Build per-agent inbox size map
  const inboxByAgent = new Map<string, number>()
  for (const inbox of Array.isArray(agentInboxes) ? agentInboxes : []) {
    inboxByAgent.set(inbox.agent_id, inbox.size)
  }

  const hierarchyRoots = Array.isArray(hierarchy) && hierarchy.length > 0 ? hierarchy : null
  const useHierarchyView = hierarchyRoots !== null && !hierarchyError
  const hasAgents = agents.length > 0

  const fsmLegend: { state: FsmState; label: string }[] = [
    { state: 'active', label: 'Active' },
    { state: 'onboarding', label: 'Onboarding' },
    { state: 'suspended', label: 'Suspended' },
    { state: 'error', label: 'Error' },
    { state: 'halted', label: 'Halted' },
    { state: 'draining', label: 'Draining' },
    { state: 'decommissioned', label: 'Decommissioned' },
  ]

  return (
    <div className="p-6 space-y-6">
      <PageHeader
        title="Organization"
        description="Interactive agent hierarchy with zoom, pan, and minimap. Click + to add children."
      >
        <div className="flex items-center gap-2">
          <Button size="sm" variant="outline" onClick={handleAddRoot}>
            <UserPlus className="h-4 w-4 mr-1.5" />
            Add Agent
          </Button>
          <Button size="sm" variant="outline" onClick={handleRefreshAll} disabled={isLoading}>
            <RefreshCw className={cn('h-4 w-4 mr-2', isLoading && 'animate-spin')} />
            Refresh
          </Button>
        </div>
      </PageHeader>

      {/* FSM Legend */}
      <div className="flex items-center gap-1.5 flex-wrap">
        {fsmLegend.map(({ state, label }) => {
          const s = fsmStyles[state]
          return (
            <span
              key={state}
              className={cn(
                'inline-flex items-center gap-1 rounded-full px-2.5 py-0.5 text-xs font-medium',
                s.badge,
              )}
            >
              <span className={cn('h-1.5 w-1.5 rounded-full shrink-0', s.dot)} />
              {label}
            </span>
          )
        })}
      </div>

      {/* Main content: chart + side panel */}
      {statesError && !hasAgents ? (
        <ErrorState error={statesError as Error} onRetry={() => void refetchStates()} />
      ) : isLoading && !hasAgents ? (
        <LoadingState message="Loading agent hierarchy…" />
      ) : !hasAgents ? (
        <EmptyState
          icon={Network}
          title="No agents registered"
          description="Add your first agent or wait for agents to connect."
          action={
            <Button size="sm" onClick={handleAddRoot}>
              <UserPlus className="h-4 w-4 mr-1.5" />
              Add First Agent
            </Button>
          }
        />
      ) : (
        <div className="flex gap-4" style={{ height: 'clamp(400px, 50vh, 600px)' }}>
          {/* Chart area */}
          <div className="flex-1 border rounded-lg bg-card overflow-hidden min-w-0">
            {useHierarchyView ? (
              <OrgFlowChart
                roots={hierarchyRoots}
                stateMap={stateMap}
                selectedId={selectedId}
                onSelect={handleSelect}
                onAddChild={handleAddChild}
                onDrop={handleFlowDrop}
                roleNameMap={roleNameMap}
                tasksByAgent={tasksByAgent}
                inboxByAgent={inboxByAgent}
                onToggleFavorite={toggleFavorite}
                favoriteIds={favoriteIds}
                onStartChat={handleStartChat}
                onResetState={handleResetState}
                onDeleteAgent={handleDeleteAgent}
                disabledProviderAgentIds={disabledProviderAgentIds}
                providerKindByAgentId={providerKindByAgentId}
                onOpenActivity={handleOpenActivity}
                activeAgentId={thinkingAgentId}
                activeToolName={thinkingTool}
              />
            ) : (
              <>
                <div className="px-4 py-2 border-b text-xs text-muted-foreground flex items-center justify-between">
                  <span>Flat view — {agents.length} agent{agents.length !== 1 ? 's' : ''}</span>
                  {hierarchyError && (
                    <span className="text-yellow-600 dark:text-yellow-400">Hierarchy data unavailable</span>
                  )}
                </div>
                <FlatGrid
                  agents={agents}
                  selectedId={selectedId}
                  onSelect={handleSelect}
                  roleNameMap={roleNameMap}
                  tasksByAgent={tasksByAgent}
                  inboxByAgent={inboxByAgent}
                />
              </>
            )}
          </div>

          {/* Right side panel */}
          <div className="w-80 shrink-0">
            {quickCreateParentId !== undefined ? (
              <QuickCreatePanel
                parentId={quickCreateParentId}
                onClose={() => setQuickCreateParentId(undefined)}
                onCreated={handleRefreshAll}
              />
            ) : selectedId ? (
              <AgentDetailPanel
                agentId={selectedId}
                onClose={() => setSelectedId(null)}
                onReparent={(id) => setReparentTargetId(id)}
              />
            ) : (
              <div className="h-full border rounded-lg bg-card flex flex-col items-center justify-center text-center p-6 gap-3 text-muted-foreground">
                <MousePointerClick className="h-8 w-8 opacity-30" />
                <p className="text-sm">Select an agent to view details</p>
                <p className="text-xs opacity-60">Click any node in the hierarchy to inspect and edit it.</p>
              </div>
            )}
          </div>
        </div>
      )}

      {/* Mail Health — postbox queue stats and dead letter management */}
      <MailHealthPanel />

      {/* Reparent dialog */}
      {reparentTargetId && (
        <ReparentDialog
          agentId={reparentTargetId}
          agents={agents}
          onConfirm={handleReparentConfirm}
          onClose={() => setReparentTargetId(null)}
        />
      )}

      {/* Agent activity drawer */}
      {activityAgentId && (
        <AgentActivityDrawer
          agentId={activityAgentId}
          open={activityDrawerOpen}
          onClose={handleCloseActivity}
          activity={drawerActivity}
        />
      )}
    </div>
  )
}
