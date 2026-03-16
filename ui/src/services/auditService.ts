import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { mcpCall } from '@/lib/mcp-client'

// ─── Audit Creation Interfaces ───────────────────────────────────────────────

export interface ParsedSymbol {
  name: string
  kind: string
  startLine: number
  endLine: number
  file?: string
}

export interface CodeSearchResult {
  name: string
  kind: string
  file: string
  start_line: number
  end_line: number
}

export interface FileEntry {
  name: string
  is_dir: boolean
  size?: number
}

export interface AuditQuestion {
  id: string
  text: string
  description?: string
  priority: string
}

// ─── Symbol Parsing ──────────────────────────────────────────────────────────

export function parseOutlineText(raw: unknown): ParsedSymbol[] {
  const text = typeof raw === 'string' ? raw : JSON.stringify(raw)
  const lines = text.split('\n')
  const symbols: ParsedSymbol[] = []
  // Match patterns like: "  funcName [function] L10-30" or structured output
  const regex = /^\s+(.+?)\s+\[(\w+)]\s+L(\d+)-(\d+)$/
  for (const line of lines) {
    const match = regex.exec(line)
    if (match) {
      symbols.push({
        name: match[1].trim(),
        kind: match[2],
        startLine: parseInt(match[3], 10),
        endLine: parseInt(match[4], 10),
      })
    }
  }
  // If regex didn't match, try parsing as JSON array of symbol objects
  if (symbols.length === 0) {
    try {
      const parsed = typeof raw === 'string' ? JSON.parse(raw) : raw
      if (Array.isArray(parsed)) {
        for (const s of parsed) {
          if (s.name && s.kind) {
            symbols.push({
              name: s.name,
              kind: s.kind,
              startLine: s.start_line ?? s.startLine ?? 0,
              endLine: s.end_line ?? s.endLine ?? 0,
            })
          }
        }
      }
    } catch {
      // Not JSON, leave symbols empty
    }
  }
  return symbols
}

// ─── Workspace File/Symbol Hooks ─────────────────────────────────────────────

export function useWorkspaceFiles(workspaceName: string, path: string) {
  return useQuery({
    queryKey: ['workspace-files', workspaceName, path],
    queryFn: () =>
      mcpCall<FileEntry[]>('workspace', {
        action: 'list_files',
        workspace_name: workspaceName,
        path: path || '.',
      }),
    enabled: !!workspaceName,
    retry: false,
  })
}

export function useCodeOutline(workspaceName: string, path: string, enabled = true) {
  return useQuery({
    queryKey: ['code-outline', workspaceName, path],
    queryFn: async () => {
      const raw = await mcpCall('code', {
        action: 'outline',
        workspace_name: workspaceName,
        path,
      })
      return parseOutlineText(raw)
    },
    enabled: enabled && !!workspaceName && !!path,
    retry: false,
  })
}

export function useSymbolSearch(workspaceName: string, query: string, kind?: string) {
  return useQuery({
    queryKey: ['symbol-search', workspaceName, query, kind],
    queryFn: async () => {
      const raw = await mcpCall<{ symbols?: CodeSearchResult[]; content_matches?: unknown[] }>(
        'code',
        {
          action: 'search',
          workspace_name: workspaceName,
          query,
          ...(kind ? { kind } : {}),
          limit: 100,
        },
      )
      // search_code returns { symbols: [...], content_matches: [...] }
      const symbols = raw?.symbols ?? []
      return symbols.map(
        (s): ParsedSymbol => ({
          name: s.name,
          kind: s.kind,
          startLine: s.start_line ?? 0,
          endLine: s.end_line ?? 0,
          file: s.file,
        }),
      )
    },
    enabled: !!workspaceName && query.length >= 2,
    retry: false,
  })
}

// ─── Audit Project Creation Orchestration ────────────────────────────────────

export interface AuditUnit {
  file: string
  /** When absent, the audit task covers the entire file. */
  symbol?: ParsedSymbol
}

export interface CreateAuditParams {
  workspaceName: string
  name: string
  description: string
  priority: string
  questions: AuditQuestion[]
  /** Pre-resolved list of symbols to audit. Each becomes a task per question. */
  auditUnits: AuditUnit[]
  onProgress: (step: string, current: number, total: number) => void
}

export interface CreateAuditResult {
  projectId: string
  milestoneCount: number
  taskCount: number
}

/** Filter symbols by the user-selected AST unit kinds. */
export function filterSymbols(
  symbolsByFile: Record<string, ParsedSymbol[]>,
  selectedKinds: Set<string>,
): ParsedSymbol[] {
  const out: ParsedSymbol[] = []
  for (const symbols of Object.values(symbolsByFile)) {
    for (const s of symbols) {
      if (selectedKinds.has(s.kind)) out.push(s)
    }
  }
  return out
}

/** Collect unique symbol kinds across all discovered files. */
export function collectSymbolKinds(
  symbolsByFile: Record<string, ParsedSymbol[]>,
): string[] {
  const kinds = new Set<string>()
  for (const symbols of Object.values(symbolsByFile)) {
    for (const s of symbols) kinds.add(s.kind)
  }
  return Array.from(kinds).sort()
}

export async function createAuditProject(
  params: CreateAuditParams,
): Promise<CreateAuditResult> {
  const {
    workspaceName, name, description, priority,
    questions, auditUnits, onProgress,
  } = params

  const totalTasks = questions.length * auditUnits.length
  const totalCalls = 1 + questions.length + totalTasks
  let current = 0

  // Step 1: Create project
  onProgress('Creating audit project...', ++current, totalCalls)
  const project = await mcpCall<{ id: string }>('project', {
    action: 'create',
    workspace_name: workspaceName,
    name: `[Audit] ${name}`,
    description: description || `Code audit: ${name}`,
    priority,
  })
  const projectId = project.id

  // Step 2: Create milestones (one per question)
  const milestoneIds: string[] = []
  for (let i = 0; i < questions.length; i++) {
    const q = questions[i]
    onProgress(`Creating milestone ${i + 1}/${questions.length}...`, ++current, totalCalls)
    const ms = await mcpCall<{ id: string }>('project', {
      action: 'add_milestone',
      project_id: projectId,
      name: q.text,
      description: q.description || `Audit criterion: ${q.text}`,
      priority: q.priority,
    })
    milestoneIds.push(ms.id)
  }

  // Step 3: Create tasks (one per unit per milestone)
  let taskCount = 0
  for (let i = 0; i < milestoneIds.length; i++) {
    const q = questions[i]
    for (const { file, symbol } of auditUnits) {
      onProgress(
        `Creating task ${taskCount + 1}/${totalTasks}...`,
        ++current,
        totalCalls,
      )
      const taskName = symbol ? `${symbol.name} [${symbol.kind}]` : file
      const taskDesc = symbol
        ? [
            `**Question:** ${q.text}`,
            `**File:** ${file}`,
            `**Symbol:** ${symbol.name}`,
            `**Kind:** ${symbol.kind}`,
            `**Lines:** ${symbol.startLine}-${symbol.endLine}`,
          ].join('\n')
        : [`**Question:** ${q.text}`, `**File:** ${file}`].join('\n')
      await mcpCall('project', {
        action: 'add_task',
        milestone_id: milestoneIds[i],
        name: taskName,
        description: taskDesc,
        priority: q.priority,
      })
      taskCount++
    }
  }

  return { projectId, milestoneCount: milestoneIds.length, taskCount }
}

// ─── Existing Audit Run Interfaces ───────────────────────────────────────────

export interface AuditRun {
  id: string
  name: string
  workspace?: string
  status: string
  created_at: string
  completed_at?: string
  total_items: number
  completed_items: number
}

export interface AuditProgress {
  audit_id: string
  total: number
  completed: number
  pending: number
  failed: number
  percent: number
}

export interface AuditItem {
  id: string
  audit_id: string
  title: string
  description?: string
  status: string
  notes?: string
  created_at: string
  updated_at?: string
}

export interface AuditItemDetail extends AuditItem {
  metadata?: Record<string, unknown>
}

export interface CompleteAuditItemArgs {
  item_id: string
  notes?: string
}

export interface UpdateAuditItemArgs {
  item_id: string
  status: string
  notes?: string
}

// ─── Hooks ────────────────────────────────────────────────────────────────────

export function useAudits(workspaceName = 'hyperax') {
  return useQuery({
    queryKey: ['audits', workspaceName],
    queryFn: () => mcpCall<AuditRun[]>('audit', { action: 'list', workspace_name: workspaceName }),
    retry: false,
  })
}

export function useAuditProgress(auditId: string | null) {
  return useQuery({
    queryKey: ['audit-progress', auditId],
    queryFn: () => mcpCall<AuditProgress>('audit', { action: 'get_progress', audit_id: auditId! }),
    enabled: !!auditId,
    retry: false,
  })
}

export function useAuditItems(auditId: string | null) {
  return useQuery({
    queryKey: ['audit-items', auditId],
    queryFn: () => mcpCall<AuditItem[]>('audit', { action: 'get_items', audit_id: auditId! }),
    enabled: !!auditId,
    retry: false,
  })
}

export function useAuditItemDetail(itemId: string | null) {
  return useQuery({
    queryKey: ['audit-item-detail', itemId],
    queryFn: () => mcpCall<AuditItemDetail>('audit', { action: 'get_detail', item_id: itemId! }),
    enabled: !!itemId,
    retry: false,
  })
}

export function useCompleteAuditItem() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (args: CompleteAuditItemArgs) =>
      mcpCall<{ id: string; status: string }>('audit', { action: 'complete_item', ...(args as unknown as Record<string, unknown>) }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['audit-items'] })
      void qc.invalidateQueries({ queryKey: ['audit-progress'] })
      void qc.invalidateQueries({ queryKey: ['audit-item-detail'] })
    },
  })
}

export function useUpdateAuditItem() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (args: UpdateAuditItemArgs) =>
      mcpCall<{ id: string; status: string }>('audit', { action: 'update_item', ...(args as unknown as Record<string, unknown>) }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['audit-items'] })
      void qc.invalidateQueries({ queryKey: ['audit-progress'] })
      void qc.invalidateQueries({ queryKey: ['audit-item-detail'] })
    },
  })
}
