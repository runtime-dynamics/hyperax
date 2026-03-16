import { useState, useCallback } from 'react'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { Badge } from '@/components/ui/badge'
import { Separator } from '@/components/ui/separator'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { toast } from '@/components/ui/use-toast'
import { useWorkspaces } from '@/services/workspaceService'
import {
  useWorkspaceFiles,
  useCodeOutline,
  createAuditProject,
  parseOutlineText,
  collectSymbolKinds,
  useSymbolSearch,
  type AuditQuestion,
  type AuditUnit,
  type ParsedSymbol,
  type FileEntry,
} from '@/services/auditService'
import { mcpCall } from '@/lib/mcp-client'
import { cn } from '@/lib/utils'
import {
  Loader2,
  PlusCircle,
  X,
  FolderOpen,
  FileText,
  ChevronRight,
  ArrowLeft,
  ArrowRight,
  Check,
  Eye,
  Search,
} from 'lucide-react'

// ─── Types ───────────────────────────────────────────────────────────────────

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
}

const STEPS = ['Basics', 'Questions', 'Scope', 'Review'] as const
const PRIORITIES = ['low', 'medium', 'high', 'critical'] as const

const SOURCE_EXTENSIONS = new Set([
  '.go', '.ts', '.tsx', '.js', '.jsx', '.py', '.rs', '.c', '.cpp', '.h',
  '.hpp', '.java', '.kt', '.swift', '.rb', '.cs', '.ex', '.exs', '.zig',
  '.vue', '.svelte',
])

function isSourceFile(name: string): boolean {
  const dot = name.lastIndexOf('.')
  if (dot < 0) return false
  return SOURCE_EXTENSIONS.has(name.slice(dot))
}

// ─── WizardStepIndicator ─────────────────────────────────────────────────────

function WizardStepIndicator({ current }: { current: number }) {
  return (
    <div className="flex items-center gap-2 mb-4">
      {STEPS.map((label, i) => {
        const stepNum = i + 1
        const isActive = stepNum === current
        const isDone = stepNum < current
        return (
          <div key={label} className="flex items-center gap-2">
            {i > 0 && (
              <div
                className={cn(
                  'h-px w-6',
                  isDone ? 'bg-primary' : 'bg-muted',
                )}
              />
            )}
            <div className="flex items-center gap-1.5">
              <div
                className={cn(
                  'h-6 w-6 rounded-full flex items-center justify-center text-xs font-medium',
                  isActive && 'bg-primary text-primary-foreground',
                  isDone && 'bg-primary/20 text-primary',
                  !isActive && !isDone && 'bg-muted text-muted-foreground',
                )}
              >
                {isDone ? <Check className="h-3 w-3" /> : stepNum}
              </div>
              <span
                className={cn(
                  'text-xs hidden sm:inline',
                  isActive ? 'font-medium' : 'text-muted-foreground',
                )}
              >
                {label}
              </span>
            </div>
          </div>
        )
      })}
    </div>
  )
}

// ─── ProgressBar ─────────────────────────────────────────────────────────────

function ProgressBar({ percent }: { percent: number }) {
  const clamped = Math.min(100, Math.max(0, percent))
  return (
    <div className="w-full bg-muted rounded-full h-2 overflow-hidden">
      <div
        className={cn(
          'h-2 rounded-full transition-all',
          clamped >= 100 ? 'bg-green-500' : 'bg-primary',
        )}
        style={{ width: `${clamped}%` }}
      />
    </div>
  )
}

// ─── FileBrowser ─────────────────────────────────────────────────────────────

function FileBrowser({
  workspace,
  selectedFiles,
  onToggleFile,
  onSelectDirectory,
  onPreviewFile,
  previewFile,
}: {
  workspace: string
  selectedFiles: Set<string>
  onToggleFile: (path: string) => void
  onSelectDirectory: (dirPath: string) => void
  onPreviewFile: (path: string | null) => void
  previewFile: string | null
}) {
  const [currentPath, setCurrentPath] = useState('')
  const [filter, setFilter] = useState('')
  const { data: files, isLoading, error } = useWorkspaceFiles(workspace, currentPath)

  const entries: FileEntry[] = Array.isArray(files) ? files : []
  const filtered = filter
    ? entries.filter((f) => f.name.toLowerCase().includes(filter.toLowerCase()))
    : entries

  const pathSegments = currentPath.split('/').filter(Boolean)

  function navigateUp(toIndex: number) {
    setCurrentPath(pathSegments.slice(0, toIndex + 1).join('/'))
    setFilter('')
  }

  function navigateInto(dir: string) {
    setCurrentPath(currentPath ? `${currentPath}/${dir}` : dir)
    setFilter('')
  }

  function fullPath(name: string): string {
    return currentPath ? `${currentPath}/${name}` : name
  }

  const sourceFiles = filtered.filter((f) => !f.is_dir && isSourceFile(f.name))
  const allSourceSelected = sourceFiles.length > 0 && sourceFiles.every((f) => selectedFiles.has(fullPath(f.name)))

  function toggleAllSource() {
    for (const f of sourceFiles) {
      const fp = fullPath(f.name)
      if (allSourceSelected) {
        if (selectedFiles.has(fp)) onToggleFile(fp)
      } else {
        if (!selectedFiles.has(fp)) onToggleFile(fp)
      }
    }
  }

  return (
    <div className="space-y-2">
      {/* Breadcrumbs */}
      <div className="flex items-center gap-1 text-xs text-muted-foreground flex-wrap">
        <button
          type="button"
          className="hover:text-foreground transition-colors"
          onClick={() => { setCurrentPath(''); setFilter('') }}
        >
          root
        </button>
        {pathSegments.map((seg, i) => (
          <span key={i} className="flex items-center gap-1">
            <ChevronRight className="h-3 w-3" />
            <button
              type="button"
              className="hover:text-foreground transition-colors"
              onClick={() => navigateUp(i)}
            >
              {seg}
            </button>
          </span>
        ))}
      </div>

      {/* Filter + select all */}
      <div className="flex items-center gap-2">
        <Input
          placeholder="Filter files..."
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          className="h-7 text-xs flex-1"
        />
        {sourceFiles.length > 0 && (
          <Button
            type="button"
            size="sm"
            variant="outline"
            className="h-7 text-xs shrink-0"
            onClick={toggleAllSource}
          >
            {allSourceSelected ? 'Deselect all' : `Select all (${sourceFiles.length})`}
          </Button>
        )}
      </div>

      {/* File listing */}
      {isLoading ? (
        <div className="space-y-1">
          {[...Array(5)].map((_, i) => (
            <Skeleton key={i} className="h-7 w-full" />
          ))}
        </div>
      ) : error ? (
        <p className="text-xs text-destructive">Failed to load files: {(error as Error).message}</p>
      ) : filtered.length === 0 ? (
        <p className="text-xs text-muted-foreground italic">No files found.</p>
      ) : (
        <div className="max-h-52 overflow-y-auto border rounded-md divide-y">
          {/* Directories first */}
          {filtered
            .filter((f) => f.is_dir)
            .sort((a, b) => a.name.localeCompare(b.name))
            .map((f) => (
              <div
                key={f.name}
                className="flex items-center gap-2 px-2 py-1.5 text-xs"
              >
                <button
                  type="button"
                  className="flex items-center gap-2 flex-1 min-w-0 hover:opacity-80 transition-opacity text-left"
                  onClick={() => navigateInto(f.name)}
                >
                  <FolderOpen className="h-3.5 w-3.5 text-muted-foreground shrink-0" />
                  <span className="truncate">{f.name}/</span>
                </button>
                <button
                  type="button"
                  className="text-xs text-muted-foreground hover:text-primary transition-colors shrink-0 px-1"
                  onClick={(e) => {
                    e.stopPropagation()
                    onSelectDirectory(fullPath(f.name))
                  }}
                  title={`Select all source files in ${f.name}/`}
                >
                  + scope
                </button>
              </div>
            ))}
          {/* Files */}
          {filtered
            .filter((f) => !f.is_dir)
            .sort((a, b) => a.name.localeCompare(b.name))
            .map((f) => {
              const fp = fullPath(f.name)
              const isSource = isSourceFile(f.name)
              const isSelected = selectedFiles.has(fp)
              const isPreviewing = previewFile === fp
              return (
                <div
                  key={f.name}
                  className={cn(
                    'flex items-center gap-2 px-2 py-1.5 text-xs',
                    isPreviewing && 'bg-accent',
                  )}
                >
                  <input
                    type="checkbox"
                    checked={isSelected}
                    onChange={() => onToggleFile(fp)}
                    className="h-3.5 w-3.5 shrink-0"
                    disabled={!isSource}
                    title={isSource ? undefined : 'Non-source file — no symbols to audit'}
                  />
                  <FileText className="h-3.5 w-3.5 text-muted-foreground shrink-0" />
                  <span className={cn('truncate flex-1', !isSource && 'text-muted-foreground')}>
                    {f.name}
                  </span>
                  {isSource && (
                    <button
                      type="button"
                      className="text-muted-foreground hover:text-foreground transition-colors shrink-0"
                      onClick={() => onPreviewFile(isPreviewing ? null : fp)}
                      title="Preview symbols"
                    >
                      <Eye className="h-3.5 w-3.5" />
                    </button>
                  )}
                </div>
              )
            })}
        </div>
      )}

      {/* Selected count */}
      <p className="text-xs text-muted-foreground">
        {selectedFiles.size} file{selectedFiles.size !== 1 ? 's' : ''} selected
      </p>
    </div>
  )
}

// ─── SymbolPreview ───────────────────────────────────────────────────────────

function SymbolPreview({ workspace, filePath }: { workspace: string; filePath: string }) {
  const { data: symbols, isLoading, error } = useCodeOutline(workspace, filePath)

  if (isLoading) {
    return (
      <div className="space-y-1 p-2">
        {[...Array(4)].map((_, i) => (
          <Skeleton key={i} className="h-5 w-full" />
        ))}
      </div>
    )
  }

  if (error) {
    return <p className="text-xs text-destructive p-2">Failed to load outline.</p>
  }

  const items = symbols ?? []

  if (items.length === 0) {
    return <p className="text-xs text-muted-foreground italic p-2">No symbols found in this file.</p>
  }

  return (
    <div className="max-h-52 overflow-y-auto divide-y">
      {items.map((s, i) => (
        <div key={i} className="flex items-center gap-2 px-2 py-1 text-xs">
          <Badge variant="outline" className="text-[10px] shrink-0">{s.kind}</Badge>
          <span className="truncate">{s.name}</span>
          <span className="text-muted-foreground shrink-0 ml-auto">
            L{s.startLine}-{s.endLine}
          </span>
        </div>
      ))}
    </div>
  )
}

// ─── CreateAuditDialog ───────────────────────────────────────────────────────

export function CreateAuditDialog({ open, onOpenChange }: Props) {
  // Step tracking
  const [step, setStep] = useState(1)

  // Step 1: Basics
  const [auditName, setAuditName] = useState('')
  const [description, setDescription] = useState('')
  const [workspace, setWorkspace] = useState('')
  const [priority, setPriority] = useState('medium')
  const [nameError, setNameError] = useState('')
  const [wsError, setWsError] = useState('')

  // Step 2: Questions
  const [questions, setQuestions] = useState<AuditQuestion[]>([])
  const [newQuestion, setNewQuestion] = useState('')
  const [newQuestionPriority, setNewQuestionPriority] = useState('medium')
  const [questionError, setQuestionError] = useState('')

  // Step 3: Scope
  const [scopeMode, setScopeMode] = useState<'directory' | 'symbol'>('directory')
  const [granularity, setGranularity] = useState<'symbol' | 'file'>('symbol')
  // Directory mode
  const [selectedFiles, setSelectedFiles] = useState<Set<string>>(new Set())
  const [previewFile, setPreviewFile] = useState<string | null>(null)
  const [symbolCache, setSymbolCache] = useState<Record<string, ParsedSymbol[]>>({})
  const [selectedKinds, setSelectedKinds] = useState<Set<string>>(new Set())
  const [isDiscovering, setIsDiscovering] = useState(false)
  // Symbol search mode
  const [symbolQuery, setSymbolQuery] = useState('')
  const [symbolKindFilter, setSymbolKindFilter] = useState('__all__')
  const [selectedSymbols, setSelectedSymbols] = useState<Set<string>>(new Set()) // key: "file::name::kind::line"

  // Step 4: Review & Create
  const [isCreating, setIsCreating] = useState(false)
  const [progressStep, setProgressStep] = useState('')
  const [progressCurrent, setProgressCurrent] = useState(0)
  const [progressTotal, setProgressTotal] = useState(0)
  const [creationResult, setCreationResult] = useState<{
    projectId: string
    milestoneCount: number
    taskCount: number
  } | null>(null)
  const [creationError, setCreationError] = useState<string | null>(null)

  // Data
  const { data: workspaces } = useWorkspaces()
  const workspaceList = Array.isArray(workspaces) ? workspaces : []
  const { data: searchResults, isLoading: isSearching } = useSymbolSearch(
    workspace,
    symbolQuery,
    symbolKindFilter === '__all__' ? undefined : symbolKindFilter,
  )
  const searchSymbols = searchResults ?? []

  function symbolKey(file: string, s: ParsedSymbol): string {
    return `${file}::${s.name}::${s.kind}::${s.startLine}`
  }

  // ─── Handlers ────────────────────────────────────────────────────────

  function reset() {
    setStep(1)
    setAuditName('')
    setDescription('')
    setWorkspace('')
    setPriority('medium')
    setNameError('')
    setWsError('')
    setQuestions([])
    setNewQuestion('')
    setNewQuestionPriority('medium')
    setQuestionError('')
    setScopeMode('directory')
    setGranularity('symbol')
    setSelectedFiles(new Set())
    setPreviewFile(null)
    setSymbolCache({})
    setSelectedKinds(new Set())
    setIsDiscovering(false)
    setSymbolQuery('')
    setSymbolKindFilter('__all__')
    setSelectedSymbols(new Set())
    setIsCreating(false)
    setProgressStep('')
    setProgressCurrent(0)
    setProgressTotal(0)
    setCreationResult(null)
    setCreationError(null)
  }

  function handleOpenChange(open: boolean) {
    if (!open && isCreating) {
      toast({ title: 'Audit creation in progress', description: 'Please wait for it to complete.' })
      return
    }
    if (!open) reset()
    onOpenChange(open)
  }

  function validateStep1(): boolean {
    let valid = true
    setNameError('')
    setWsError('')
    if (!auditName.trim()) {
      setNameError('Audit name is required')
      valid = false
    }
    if (!workspace) {
      setWsError('Select a target workspace')
      valid = false
    }
    return valid
  }

  function validateStep2(): boolean {
    setQuestionError('')
    if (questions.length === 0) {
      setQuestionError('Add at least one audit question')
      return false
    }
    return true
  }

  function addQuestion() {
    if (!newQuestion.trim()) return
    setQuestions((prev) => [
      ...prev,
      {
        id: crypto.randomUUID(),
        text: newQuestion.trim(),
        priority: newQuestionPriority,
      },
    ])
    setNewQuestion('')
    setQuestionError('')
  }

  function removeQuestion(id: string) {
    setQuestions((prev) => prev.filter((q) => q.id !== id))
  }

  const toggleFile = useCallback((path: string) => {
    setSelectedFiles((prev) => {
      const next = new Set(prev)
      if (next.has(path)) next.delete(path)
      else next.add(path)
      return next
    })
  }, [])

  const selectDirectory = useCallback(
    async (dirPath: string) => {
      // Recursively collect all source files in a directory
      const collected: string[] = []
      const isRoot = dirPath === '.' || dirPath === ''
      const queue = [isRoot ? '' : dirPath]
      while (queue.length > 0) {
        const dir = queue.shift()!
        try {
          const entries = await mcpCall<FileEntry[]>('list_files_in_dir', {
            workspace_name: workspace,
            path: dir || '.',
          })
          if (!Array.isArray(entries)) continue
          for (const e of entries) {
            const fp = dir ? `${dir}/${e.name}` : e.name
            if (e.is_dir) queue.push(fp)
            else if (isSourceFile(e.name)) collected.push(fp)
          }
        } catch {
          // Skip unreadable directories
        }
      }
      if (collected.length === 0) {
        toast({ title: 'No source files found in this directory' })
        return
      }
      setSelectedFiles((prev) => {
        const next = new Set(prev)
        for (const f of collected) next.add(f)
        return next
      })
      toast({ title: `Added ${collected.length} source file${collected.length !== 1 ? 's' : ''}` })
    },
    [workspace],
  )

  async function discoverSymbols(): Promise<Record<string, ParsedSymbol[]>> {
    if (!workspace) throw new Error('No workspace selected')
    const files = Array.from(selectedFiles)
    const missing = files.filter((f) => !(f in symbolCache))
    if (missing.length === 0) return symbolCache

    setIsDiscovering(true)
    const updated = { ...symbolCache }
    let failCount = 0

    // Fetch in batches of 5
    for (let i = 0; i < missing.length; i += 5) {
      const batch = missing.slice(i, i + 5)
      const results = await Promise.allSettled(
        batch.map((f) =>
          mcpCall('code', { action: 'outline', workspace_name: workspace, path: f }),
        ),
      )
      for (let j = 0; j < batch.length; j++) {
        const result = results[j]
        if (result.status === 'fulfilled') {
          updated[batch[j]] = parseOutlineText(result.value)
        } else {
          console.warn(`Symbol discovery failed for ${batch[j]}:`, result.reason)
          updated[batch[j]] = []
          failCount++
        }
      }
    }

    if (failCount > 0) {
      toast({
        title: `Symbol discovery incomplete`,
        description: `${failCount} file${failCount > 1 ? 's' : ''} could not be parsed. They will have no symbol detail.`,
        variant: 'destructive',
      })
    }

    setSymbolCache(updated)
    setIsDiscovering(false)
    return updated
  }

  async function handleNext() {
    if (step === 1 && !validateStep1()) return
    if (step === 2 && !validateStep2()) return
    if (step === 3) {
      if (scopeMode === 'directory') {
        if (selectedFiles.size === 0) {
          toast({ title: 'Select at least one file', variant: 'destructive' })
          return
        }
        if (granularity === 'symbol') {
          const updated = await discoverSymbols()
          if (selectedKinds.size === 0) {
            const kinds = collectSymbolKinds(updated)
            if (kinds.length === 0) {
              toast({ title: 'No symbols found in selected files', variant: 'destructive' })
              return
            }
            setSelectedKinds(new Set(kinds))
          }
        }
      } else {
        // Symbol search mode
        if (selectedSymbols.size === 0) {
          toast({ title: 'Select at least one symbol', variant: 'destructive' })
          return
        }
      }
    }
    setStep((s) => Math.min(s + 1, 4))
  }

  async function handleCreate() {
    setIsCreating(true)
    setCreationError(null)
    setCreationResult(null)

    try {
      // In directory+symbol mode, ensure symbols are discovered first
      if (scopeMode === 'directory' && granularity === 'symbol') {
        await discoverSymbols()
      }
      const units = resolveAuditUnits()
      if (units.length === 0) {
        toast({ title: 'No symbols to audit', variant: 'destructive' })
        setIsCreating(false)
        return
      }
      const result = await createAuditProject({
        workspaceName: workspace,
        name: auditName,
        description,
        priority,
        questions,
        auditUnits: units,
        onProgress: (step, current, total) => {
          setProgressStep(step)
          setProgressCurrent(current)
          setProgressTotal(total)
        },
      })
      setCreationResult(result)
      toast({ title: 'Audit project created', description: `${result.milestoneCount} milestones, ${result.taskCount} tasks` })
    } catch (err) {
      setCreationError((err as Error).message)
      toast({ title: 'Audit creation failed', description: (err as Error).message, variant: 'destructive' })
    } finally {
      setIsCreating(false)
    }
  }

  // ─── Resolve audit units from either scope mode ─────────────────────

  const availableKinds = collectSymbolKinds(symbolCache)

  function resolveAuditUnits(): AuditUnit[] {
    if (scopeMode === 'directory') {
      if (granularity === 'file') {
        return Array.from(selectedFiles).map((file) => ({ file }))
      }
      const units: AuditUnit[] = []
      for (const file of selectedFiles) {
        const syms = symbolCache[file] ?? []
        for (const s of syms) {
          if (selectedKinds.has(s.kind)) units.push({ file, symbol: s })
        }
      }
      return units
    }
    // Symbol search mode: use selectedSymbols set
    const units: AuditUnit[] = []
    for (const s of searchSymbols) {
      const key = symbolKey(s.file ?? '', s)
      if (selectedSymbols.has(key)) {
        units.push({ file: s.file ?? '', symbol: s })
      }
    }
    return units
  }

  const auditUnits = resolveAuditUnits()
  const filteredSymbolCount = auditUnits.length
  const estimatedTasks = questions.length * filteredSymbolCount

  const totalSymbolsAll =
    scopeMode === 'directory'
      ? Array.from(selectedFiles).reduce((sum, f) => sum + (symbolCache[f]?.length ?? 0), 0)
      : searchSymbols.length

  // ─── Render ──────────────────────────────────────────────────────────

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="max-w-2xl max-h-[85vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Create Code Audit</DialogTitle>
          <DialogDescription>
            Define audit criteria and scope, then generate a project with milestones and tasks.
          </DialogDescription>
        </DialogHeader>

        <WizardStepIndicator current={step} />

        {/* ─── Step 1: Basics ─────────────────────────────────────────── */}
        {step === 1 && (
          <div className="space-y-4">
            <div className="space-y-1.5">
              <Label htmlFor="audit-name">Audit Name</Label>
              <Input
                id="audit-name"
                placeholder="e.g. Error Handling Review Q1"
                value={auditName}
                onChange={(e) => { setAuditName(e.target.value); setNameError('') }}
              />
              {nameError && <p className="text-xs text-destructive">{nameError}</p>}
            </div>

            <div className="space-y-1.5">
              <Label htmlFor="audit-workspace">Target Workspace</Label>
              <Select value={workspace} onValueChange={(v) => { setWorkspace(v); setWsError('') }}>
                <SelectTrigger id="audit-workspace">
                  <SelectValue placeholder="Select workspace..." />
                </SelectTrigger>
                <SelectContent>
                  {workspaceList.map((ws) => (
                    <SelectItem key={ws.id} value={ws.name}>
                      {ws.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              {wsError && <p className="text-xs text-destructive">{wsError}</p>}
            </div>

            <div className="space-y-1.5">
              <Label htmlFor="audit-description">Description (optional)</Label>
              <Textarea
                id="audit-description"
                placeholder="Describe the audit scope and goals..."
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                rows={3}
              />
            </div>

            <div className="space-y-1.5">
              <Label htmlFor="audit-priority">Priority</Label>
              <Select value={priority} onValueChange={setPriority}>
                <SelectTrigger id="audit-priority">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {PRIORITIES.map((p) => (
                    <SelectItem key={p} value={p}>
                      {p.charAt(0).toUpperCase() + p.slice(1)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>
        )}

        {/* ─── Step 2: Questions ──────────────────────────────────────── */}
        {step === 2 && (
          <div className="space-y-4">
            <p className="text-sm text-muted-foreground">
              Each question becomes a milestone. Every source file will be audited against each question.
            </p>

            {/* Add question form */}
            <div className="flex items-end gap-2">
              <div className="flex-1 space-y-1.5">
                <Label htmlFor="new-question">Question / Criterion</Label>
                <Input
                  id="new-question"
                  placeholder="e.g. Does this function handle errors properly?"
                  value={newQuestion}
                  onChange={(e) => setNewQuestion(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') { e.preventDefault(); addQuestion() }
                  }}
                />
              </div>
              <Select value={newQuestionPriority} onValueChange={setNewQuestionPriority}>
                <SelectTrigger className="w-28">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {PRIORITIES.map((p) => (
                    <SelectItem key={p} value={p}>
                      {p.charAt(0).toUpperCase() + p.slice(1)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Button type="button" size="sm" onClick={addQuestion} disabled={!newQuestion.trim()}>
                <PlusCircle className="h-4 w-4 mr-1" />
                Add
              </Button>
            </div>

            {questionError && <p className="text-xs text-destructive">{questionError}</p>}

            {/* Question list */}
            {questions.length === 0 ? (
              <p className="text-xs text-muted-foreground italic">No questions added yet.</p>
            ) : (
              <div className="space-y-1.5 max-h-52 overflow-y-auto">
                {questions.map((q, i) => (
                  <div
                    key={q.id}
                    className="flex items-center gap-2 px-3 py-2 border rounded-md text-sm"
                  >
                    <span className="text-muted-foreground text-xs w-5 shrink-0">{i + 1}.</span>
                    <span className="flex-1 truncate">{q.text}</span>
                    <Badge variant="outline" className="text-xs capitalize shrink-0">
                      {q.priority}
                    </Badge>
                    <button
                      type="button"
                      className="text-muted-foreground hover:text-destructive transition-colors shrink-0"
                      onClick={() => removeQuestion(q.id)}
                      title="Remove"
                    >
                      <X className="h-3.5 w-3.5" />
                    </button>
                  </div>
                ))}
              </div>
            )}
          </div>
        )}

        {/* ─── Step 3: Scope ──────────────────────────────────────────── */}
        {step === 3 && (
          <div className="space-y-3">
            {/* Scope mode selector */}
            <div className="flex items-center gap-2">
              <button
                type="button"
                onClick={() => setScopeMode('directory')}
                className={cn(
                  'flex items-center gap-1.5 px-3 py-1.5 rounded-md text-sm font-medium transition-colors',
                  scopeMode === 'directory'
                    ? 'bg-primary text-primary-foreground'
                    : 'text-muted-foreground hover:bg-accent',
                )}
              >
                <FolderOpen className="h-3.5 w-3.5" />
                Browse Directory
              </button>
              <button
                type="button"
                onClick={() => setScopeMode('symbol')}
                className={cn(
                  'flex items-center gap-1.5 px-3 py-1.5 rounded-md text-sm font-medium transition-colors',
                  scopeMode === 'symbol'
                    ? 'bg-primary text-primary-foreground'
                    : 'text-muted-foreground hover:bg-accent',
                )}
              >
                <Search className="h-3.5 w-3.5" />
                Search Symbols
              </button>
            </div>

            {/* ─── Directory mode ──────────────────────────────────────── */}
            {scopeMode === 'directory' && (
              <>
                <div className="flex items-center gap-2">
                  <p className="text-xs text-muted-foreground flex-1">
                    Use <strong>+ scope</strong> on a directory, or select the entire workspace.
                  </p>
                  <Button
                    type="button"
                    size="sm"
                    variant="outline"
                    className="h-7 text-xs shrink-0"
                    onClick={() => selectDirectory('.')}
                  >
                    <FolderOpen className="h-3.5 w-3.5 mr-1" />
                    Select entire workspace
                  </Button>
                  {selectedFiles.size > 0 && (
                    <Button
                      type="button"
                      size="sm"
                      variant="ghost"
                      className="h-7 text-xs shrink-0 text-destructive hover:text-destructive"
                      onClick={() => {
                        setSelectedFiles(new Set())
                        setSymbolCache({})
                        setSelectedKinds(new Set())
                      }}
                    >
                      <X className="h-3.5 w-3.5 mr-1" />
                      Clear ({selectedFiles.size})
                    </Button>
                  )}
                </div>

                <div className="grid grid-cols-1 md:grid-cols-5 gap-3">
                  <div className="md:col-span-3">
                    <FileBrowser
                      workspace={workspace}
                      selectedFiles={selectedFiles}
                      onToggleFile={toggleFile}
                      onSelectDirectory={selectDirectory}
                      onPreviewFile={setPreviewFile}
                      previewFile={previewFile}
                    />
                  </div>
                  <div className="md:col-span-2 border rounded-md">
                    <div className="px-3 py-2 border-b bg-muted/30">
                      <p className="text-xs font-medium">
                        {previewFile ? `Symbols: ${previewFile.split('/').pop()}` : 'Symbol Preview'}
                      </p>
                    </div>
                    {previewFile ? (
                      <SymbolPreview workspace={workspace} filePath={previewFile} />
                    ) : (
                      <p className="text-xs text-muted-foreground italic p-3">
                        Click the eye icon on a file to preview its symbols.
                      </p>
                    )}
                  </div>
                </div>

                {/* Granularity toggle */}
                <Separator />
                <div className="space-y-1.5">
                  <Label className="text-xs">Audit Granularity</Label>
                  <div className="flex items-center gap-2">
                    <button
                      type="button"
                      onClick={() => setGranularity('file')}
                      className={cn(
                        'px-3 py-1 rounded-md text-xs font-medium transition-colors',
                        granularity === 'file'
                          ? 'bg-primary text-primary-foreground'
                          : 'text-muted-foreground hover:bg-accent',
                      )}
                    >
                      Per File
                    </button>
                    <button
                      type="button"
                      onClick={() => setGranularity('symbol')}
                      className={cn(
                        'px-3 py-1 rounded-md text-xs font-medium transition-colors',
                        granularity === 'symbol'
                          ? 'bg-primary text-primary-foreground'
                          : 'text-muted-foreground hover:bg-accent',
                      )}
                    >
                      Per AST Symbol
                    </button>
                  </div>
                  <p className="text-xs text-muted-foreground">
                    {granularity === 'file'
                      ? 'One task per file per question — audits the file as a whole.'
                      : 'One task per symbol per question — audits each function, struct, class, etc. individually.'}
                  </p>
                </div>

                {/* AST kind selector — only shown in symbol granularity */}
                {granularity === 'symbol' && availableKinds.length > 0 && (
                  <div className="space-y-1.5">
                    <Label className="text-xs">AST Unit Types to Audit</Label>
                    <div className="flex flex-wrap gap-2 pt-1">
                      {availableKinds.map((kind) => {
                        const isOn = selectedKinds.has(kind)
                        const count = Array.from(selectedFiles).reduce(
                          (n, f) => n + (symbolCache[f]?.filter((s) => s.kind === kind).length ?? 0),
                          0,
                        )
                        return (
                          <button
                            key={kind}
                            type="button"
                            onClick={() =>
                              setSelectedKinds((prev) => {
                                const next = new Set(prev)
                                if (next.has(kind)) next.delete(kind)
                                else next.add(kind)
                                return next
                              })
                            }
                            className={cn(
                              'inline-flex items-center gap-1.5 px-2.5 py-1 rounded-md border text-xs transition-colors',
                              isOn
                                ? 'bg-primary text-primary-foreground border-primary'
                                : 'bg-background text-muted-foreground border-input hover:bg-accent',
                            )}
                          >
                            {kind}
                            <span className={cn('tabular-nums', isOn ? 'text-primary-foreground/70' : 'text-muted-foreground/60')}>
                              ({count})
                            </span>
                          </button>
                        )
                      })}
                    </div>
                  </div>
                )}

                {isDiscovering && (
                  <div className="flex items-center gap-2 text-xs text-muted-foreground">
                    <Loader2 className="h-3 w-3 animate-spin" />
                    Discovering symbols in selected files...
                  </div>
                )}
              </>
            )}

            {/* ─── Symbol search mode ──────────────────────────────────── */}
            {scopeMode === 'symbol' && (
              <>
                <p className="text-xs text-muted-foreground">
                  Search for a specific class, struct, function, or module by name.
                  Select individual symbols to include in the audit.
                </p>

                <div className="flex items-end gap-2">
                  <div className="flex-1 space-y-1">
                    <Label htmlFor="symbol-search" className="text-xs">Symbol name</Label>
                    <div className="relative">
                      <Search className="absolute left-2 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-muted-foreground" />
                      <Input
                        id="symbol-search"
                        placeholder="e.g. Router, handleRequest, UserService..."
                        value={symbolQuery}
                        onChange={(e) => setSymbolQuery(e.target.value)}
                        className="h-8 text-xs pl-7"
                      />
                    </div>
                  </div>
                  <div className="space-y-1">
                    <Label className="text-xs">Kind</Label>
                    <Select value={symbolKindFilter} onValueChange={setSymbolKindFilter}>
                      <SelectTrigger className="w-32 h-8 text-xs">
                        <SelectValue placeholder="All kinds" />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="__all__">All kinds</SelectItem>
                        <SelectItem value="function">function</SelectItem>
                        <SelectItem value="struct">struct</SelectItem>
                        <SelectItem value="method">method</SelectItem>
                        <SelectItem value="interface">interface</SelectItem>
                        <SelectItem value="class">class</SelectItem>
                        <SelectItem value="type">type</SelectItem>
                        <SelectItem value="const">const</SelectItem>
                        <SelectItem value="enum">enum</SelectItem>
                        <SelectItem value="trait">trait</SelectItem>
                        <SelectItem value="impl">impl</SelectItem>
                        <SelectItem value="module">module</SelectItem>
                      </SelectContent>
                    </Select>
                  </div>
                </div>

                {/* Search results */}
                {isSearching && (
                  <div className="flex items-center gap-2 text-xs text-muted-foreground py-2">
                    <Loader2 className="h-3 w-3 animate-spin" />
                    Searching...
                  </div>
                )}

                {!isSearching && symbolQuery.length >= 2 && searchSymbols.length === 0 && (
                  <p className="text-xs text-muted-foreground italic py-2">No symbols found for "{symbolQuery}".</p>
                )}

                {searchSymbols.length > 0 && (
                  <div className="space-y-1.5">
                    <div className="flex items-center justify-between">
                      <p className="text-xs text-muted-foreground">
                        {searchSymbols.length} result{searchSymbols.length !== 1 ? 's' : ''}
                      </p>
                      <Button
                        type="button"
                        size="sm"
                        variant="outline"
                        className="h-6 text-xs"
                        onClick={() => {
                          const allKeys = searchSymbols.map((s) => symbolKey(s.file ?? '', s))
                          const allSelected = allKeys.every((k) => selectedSymbols.has(k))
                          setSelectedSymbols((prev) => {
                            const next = new Set(prev)
                            for (const k of allKeys) {
                              if (allSelected) next.delete(k)
                              else next.add(k)
                            }
                            return next
                          })
                        }}
                      >
                        {searchSymbols.every((s) => selectedSymbols.has(symbolKey(s.file ?? '', s)))
                          ? 'Deselect all'
                          : 'Select all'}
                      </Button>
                    </div>

                    <div className="max-h-52 overflow-y-auto border rounded-md divide-y">
                      {searchSymbols.map((s, i) => {
                        const key = symbolKey(s.file ?? '', s)
                        const checked = selectedSymbols.has(key)
                        return (
                          <label
                            key={i}
                            className={cn(
                              'flex items-center gap-2 px-2 py-1.5 text-xs cursor-pointer hover:bg-accent transition-colors',
                              checked && 'bg-primary/5',
                            )}
                          >
                            <input
                              type="checkbox"
                              checked={checked}
                              onChange={() =>
                                setSelectedSymbols((prev) => {
                                  const next = new Set(prev)
                                  if (next.has(key)) next.delete(key)
                                  else next.add(key)
                                  return next
                                })
                              }
                              className="h-3.5 w-3.5 shrink-0"
                            />
                            <Badge variant="outline" className="text-[10px] shrink-0">{s.kind}</Badge>
                            <span className="font-medium truncate">{s.name}</span>
                            <span className="text-muted-foreground truncate ml-auto text-[10px]">
                              {s.file} L{s.startLine}-{s.endLine}
                            </span>
                          </label>
                        )
                      })}
                    </div>
                  </div>
                )}

                {selectedSymbols.size > 0 && (
                  <div className="flex items-center gap-2">
                    <p className="text-xs text-muted-foreground flex-1">
                      {selectedSymbols.size} symbol{selectedSymbols.size !== 1 ? 's' : ''} selected
                      {' '}= {questions.length * selectedSymbols.size} task{questions.length * selectedSymbols.size !== 1 ? 's' : ''} to create
                    </p>
                    <Button
                      type="button"
                      size="sm"
                      variant="ghost"
                      className="h-6 text-xs text-destructive hover:text-destructive"
                      onClick={() => setSelectedSymbols(new Set())}
                    >
                      <X className="h-3 w-3 mr-1" />
                      Clear
                    </Button>
                  </div>
                )}
              </>
            )}

            {/* Common footer: total count */}
            {filteredSymbolCount > 0 && (
              <p className="text-xs font-medium text-primary">
                {filteredSymbolCount} symbol{filteredSymbolCount !== 1 ? 's' : ''} in scope
              </p>
            )}
          </div>
        )}

        {/* ─── Step 4: Review ─────────────────────────────────────────── */}
        {step === 4 && !creationResult && !isCreating && (
          <div className="space-y-4">
            <div className="grid grid-cols-2 gap-3 text-sm">
              <div>
                <p className="text-muted-foreground text-xs">Audit Name</p>
                <p className="font-medium">{auditName}</p>
              </div>
              <div>
                <p className="text-muted-foreground text-xs">Workspace</p>
                <p className="font-medium">{workspace}</p>
              </div>
              <div>
                <p className="text-muted-foreground text-xs">Priority</p>
                <Badge variant="outline" className="capitalize">{priority}</Badge>
              </div>
              {description && (
                <div className="col-span-2">
                  <p className="text-muted-foreground text-xs">Description</p>
                  <p className="text-sm">{description}</p>
                </div>
              )}
            </div>

            <Separator />

            <div className="grid grid-cols-2 sm:grid-cols-4 gap-3 text-center">
              <div>
                <p className="text-2xl font-bold">{questions.length}</p>
                <p className="text-xs text-muted-foreground">Milestones</p>
              </div>
              <div>
                <p className="text-2xl font-bold">{selectedFiles.size}</p>
                <p className="text-xs text-muted-foreground">Files</p>
              </div>
              <div>
                <p className="text-2xl font-bold">{filteredSymbolCount}</p>
                <p className="text-xs text-muted-foreground">
                  Symbols ({totalSymbolsAll} total)
                </p>
              </div>
              <div>
                <p className="text-2xl font-bold text-primary">{estimatedTasks}</p>
                <p className="text-xs text-muted-foreground">Tasks</p>
              </div>
            </div>

            <Separator />

            {/* Scope details */}
            <div className="space-y-1">
              <p className="text-xs text-muted-foreground">
                Scope: <strong>
                  {scopeMode === 'symbol'
                    ? 'Symbol search'
                    : granularity === 'file'
                      ? 'Per file'
                      : 'Per AST symbol'}
                </strong>
              </p>
              {scopeMode === 'directory' && granularity === 'symbol' && selectedKinds.size > 0 && (
                <div className="flex flex-wrap gap-1">
                  <span className="text-xs text-muted-foreground">Kinds:</span>
                  {Array.from(selectedKinds).sort().map((kind) => (
                    <Badge key={kind} variant="secondary" className="text-xs">{kind}</Badge>
                  ))}
                </div>
              )}
              {scopeMode === 'symbol' && (
                <p className="text-xs text-muted-foreground">
                  {selectedSymbols.size} symbol{selectedSymbols.size !== 1 ? 's' : ''} selected via search
                </p>
              )}
            </div>

            <Separator />

            <div>
              <p className="text-sm font-medium mb-2">
                {questions.length} question{questions.length !== 1 ? 's' : ''}
                {' '}&times; {filteredSymbolCount} symbol{filteredSymbolCount !== 1 ? 's' : ''}
                {' '}= <span className="text-primary">{estimatedTasks}</span> tasks
              </p>

              <div className="space-y-1 max-h-36 overflow-y-auto text-xs">
                {questions.map((q, i) => (
                  <div key={q.id} className="flex items-center gap-2 px-2 py-1 border rounded">
                    <span className="text-muted-foreground w-4 shrink-0">{i + 1}.</span>
                    <span className="truncate flex-1">{q.text}</span>
                    <span className="text-muted-foreground shrink-0">
                      {filteredSymbolCount} task{filteredSymbolCount !== 1 ? 's' : ''}
                    </span>
                  </div>
                ))}
              </div>
            </div>
          </div>
        )}

        {/* ─── Step 4: Creating Progress ──────────────────────────────── */}
        {step === 4 && isCreating && (
          <div className="space-y-4 py-4">
            <div className="flex items-center gap-2 text-sm">
              <Loader2 className="h-4 w-4 animate-spin text-primary" />
              <span>{progressStep}</span>
            </div>
            <ProgressBar
              percent={progressTotal > 0 ? (progressCurrent / progressTotal) * 100 : 0}
            />
            <p className="text-xs text-muted-foreground text-center">
              {progressCurrent} / {progressTotal} operations
            </p>
          </div>
        )}

        {/* ─── Step 4: Result ─────────────────────────────────────────── */}
        {step === 4 && creationResult && (
          <div className="space-y-4 py-4 text-center">
            <div className="mx-auto h-12 w-12 rounded-full bg-green-500/10 flex items-center justify-center">
              <Check className="h-6 w-6 text-green-500" />
            </div>
            <div>
              <p className="text-lg font-medium">Audit Project Created</p>
              <p className="text-sm text-muted-foreground mt-1">
                {creationResult.milestoneCount} milestones and {creationResult.taskCount} tasks
                are ready for review in the Tasks page.
              </p>
            </div>
          </div>
        )}

        {/* ─── Step 4: Error ──────────────────────────────────────────── */}
        {step === 4 && creationError && !isCreating && (
          <div className="space-y-3 py-4">
            <p className="text-sm text-destructive font-medium">Creation failed</p>
            <p className="text-xs text-muted-foreground">{creationError}</p>
            <p className="text-xs text-muted-foreground">
              Any partially created project/milestones are visible in the Tasks page.
            </p>
          </div>
        )}

        {/* ─── Footer ─────────────────────────────────────────────────── */}
        <DialogFooter className="gap-2 sm:gap-0">
          {/* Back button */}
          {step > 1 && !isCreating && !creationResult && (
            <Button
              type="button"
              variant="outline"
              onClick={() => setStep((s) => Math.max(s - 1, 1))}
            >
              <ArrowLeft className="h-4 w-4 mr-1" />
              Back
            </Button>
          )}

          <div className="flex-1" />

          {/* Cancel/Close */}
          {creationResult ? (
            <Button type="button" onClick={() => handleOpenChange(false)}>
              Close
            </Button>
          ) : (
            <>
              {!isCreating && (
                <Button
                  type="button"
                  variant="ghost"
                  onClick={() => handleOpenChange(false)}
                >
                  Cancel
                </Button>
              )}

              {/* Next / Create */}
              {step < 4 && (
                <Button type="button" onClick={handleNext} disabled={isDiscovering}>
                  {isDiscovering ? (
                    <Loader2 className="h-4 w-4 mr-1 animate-spin" />
                  ) : (
                    <ArrowRight className="h-4 w-4 mr-1" />
                  )}
                  Next
                </Button>
              )}
              {step === 4 && !isCreating && !creationError && (
                <Button type="button" onClick={handleCreate}>
                  <Check className="h-4 w-4 mr-1" />
                  Create Audit
                </Button>
              )}
              {step === 4 && creationError && (
                <Button type="button" onClick={handleCreate}>
                  Retry
                </Button>
              )}
            </>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
