import { useState, useMemo, useEffect } from 'react'
import {
  FileText,
  ChevronRight,
  ChevronDown,
  FolderOpen,
  Folder,
  BookOpen,
  Lock,
  Plus,
  Trash2,
  Tag,
  AlertCircle,
  ExternalLink,
  ClipboardList,
  CheckCircle2,
  Clock,
  FileCheck,
  Archive,
} from 'lucide-react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { useLocation } from 'react-router-dom'
import { useWorkspaces } from '@/services/workspaceService'
import {
  useDocs,
  useDocContent,
  useExternalDocSources,
  useAddExternalDocSource,
  useRemoveExternalDocSource,
  useDocTags,
  useTagDocument,
  useUntagDocument,
  useWorkspaceDocStatus,
  type DocFile,
} from '@/services/docService'
import {
  useSpecs,
  useSpecDetail,
  useUpdateSpecStatus,
  type SpecSummary,
} from '@/services/specService'
import { PageHeader } from '@/components/domain/page-header'
import { LoadingState } from '@/components/domain/loading-state'
import { EmptyState } from '@/components/domain/empty-state'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { cn } from '@/lib/utils'

// ── Tree building utilities ─────────────────────────────────────────────────

interface TreeNode {
  name: string
  path: string
  isDir: boolean
  readonly: boolean
  source: string
  children: TreeNode[]
}

function buildTree(files: DocFile[]): TreeNode[] {
  const root: TreeNode[] = []

  for (const file of files) {
    const parts = file.path.split('/')
    let current = root

    for (let i = 0; i < parts.length; i++) {
      const part = parts[i]
      const isLast = i === parts.length - 1
      const existing = current.find((n) => n.name === part)

      if (existing) {
        current = existing.children
      } else {
        const node: TreeNode = {
          name: part,
          path: isLast ? file.path : parts.slice(0, i + 1).join('/'),
          isDir: !isLast,
          readonly: isLast ? !!file.readonly : false,
          source: isLast ? (file.source ?? 'internal') : '',
          children: [],
        }
        current.push(node)
        current = node.children
      }
    }
  }

  const sortNodes = (nodes: TreeNode[]) => {
    nodes.sort((a, b) => {
      if (a.isDir !== b.isDir) return a.isDir ? -1 : 1
      return a.name.localeCompare(b.name)
    })
    nodes.forEach((n) => sortNodes(n.children))
  }
  sortNodes(root)
  return root
}

// ── Tree item component ─────────────────────────────────────────────────────

function TreeItem({
  node,
  selectedPath,
  onSelect,
  depth = 0,
}: {
  node: TreeNode
  selectedPath: string
  onSelect: (path: string) => void
  depth?: number
}) {
  const [expanded, setExpanded] = useState(true)
  const isSelected = node.path === selectedPath
  const isExtDir = node.isDir && node.name === '@ext'

  if (node.isDir) {
    return (
      <div>
        <button
          onClick={() => setExpanded(!expanded)}
          className="flex items-center gap-1 w-full py-1 px-1 text-sm text-muted-foreground hover:text-foreground hover:bg-accent rounded-sm transition-colors"
          style={{ paddingLeft: `${depth * 12 + 4}px` }}
        >
          {expanded ? (
            <ChevronDown className="h-3.5 w-3.5 shrink-0" />
          ) : (
            <ChevronRight className="h-3.5 w-3.5 shrink-0" />
          )}
          {expanded ? (
            <FolderOpen className={cn('h-3.5 w-3.5 shrink-0', isExtDir ? 'text-blue-500' : 'text-amber-500')} />
          ) : (
            <Folder className={cn('h-3.5 w-3.5 shrink-0', isExtDir ? 'text-blue-500' : 'text-amber-500')} />
          )}
          <span className="truncate">{node.name}</span>
          {isExtDir && (
            <ExternalLink className="h-3 w-3 shrink-0 text-blue-500 ml-auto" />
          )}
        </button>
        {expanded && (
          <div>
            {node.children.map((child) => (
              <TreeItem
                key={child.path}
                node={child}
                selectedPath={selectedPath}
                onSelect={onSelect}
                depth={depth + 1}
              />
            ))}
          </div>
        )}
      </div>
    )
  }

  return (
    <button
      onClick={() => onSelect(node.path)}
      className={cn(
        'flex items-center gap-1.5 w-full py-1 px-1 text-sm rounded-sm transition-colors',
        isSelected
          ? 'bg-primary text-primary-foreground'
          : 'text-muted-foreground hover:text-foreground hover:bg-accent',
      )}
      style={{ paddingLeft: `${depth * 12 + 4}px` }}
    >
      <FileText className="h-3.5 w-3.5 shrink-0" />
      <span className="truncate">{node.name}</span>
      {node.readonly && (
        <Lock className="h-3 w-3 shrink-0 ml-auto opacity-50" />
      )}
    </button>
  )
}

// ── Document viewer ─────────────────────────────────────────────────────────

function DocViewer({
  workspaceName,
  path,
}: {
  workspaceName: string
  path: string
}) {
  const { data: content, isLoading, error } = useDocContent(workspaceName, path)

  if (isLoading) return <LoadingState message="Loading document..." />
  if (error) return <p className="text-sm text-destructive">Failed to load document.</p>
  if (!content) return null

  const markdown = typeof content === 'object' && content !== null
    ? (content as { content?: string }).content || JSON.stringify(content)
    : String(content)

  return (
    <div className="doc-markdown text-sm">
      <ReactMarkdown remarkPlugins={[remarkGfm]}>{markdown}</ReactMarkdown>
    </div>
  )
}

// ── Add external source dialog ──────────────────────────────────────────────

function AddExternalSourceDialog({ workspaceName }: { workspaceName: string }) {
  const [open, setOpen] = useState(false)
  const [name, setName] = useState('')
  const [path, setPath] = useState('')
  const addSource = useAddExternalDocSource()

  const handleSubmit = () => {
    if (!name.trim() || !path.trim()) return
    addSource.mutate(
      { workspace_name: workspaceName, name: name.trim(), path: path.trim() },
      {
        onSuccess: () => {
          setName('')
          setPath('')
          setOpen(false)
        },
      },
    )
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button variant="ghost" size="sm" className="h-7 px-2 text-xs">
          <Plus className="h-3.5 w-3.5 mr-1" />
          External Source
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Add External Documentation Source</DialogTitle>
          <DialogDescription>
            Add a read-only directory containing markdown documentation. Files
            will be indexed and searchable but cannot be edited.
          </DialogDescription>
        </DialogHeader>
        <div className="grid gap-4 py-4">
          <div className="grid gap-2">
            <Label htmlFor="source-name">Display Name</Label>
            <Input
              id="source-name"
              placeholder="e.g. company-wiki"
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </div>
          <div className="grid gap-2">
            <Label htmlFor="source-path">Directory Path</Label>
            <Input
              id="source-path"
              placeholder="/path/to/docs"
              value={path}
              onChange={(e) => setPath(e.target.value)}
            />
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => setOpen(false)}>
            Cancel
          </Button>
          <Button onClick={handleSubmit} disabled={addSource.isPending || !name.trim() || !path.trim()}>
            {addSource.isPending ? 'Adding...' : 'Add Source'}
          </Button>
        </DialogFooter>
        {addSource.isError && (
          <p className="text-sm text-destructive mt-2">
            {addSource.error instanceof Error ? addSource.error.message : 'Failed to add source'}
          </p>
        )}
      </DialogContent>
    </Dialog>
  )
}

// ── External sources list ───────────────────────────────────────────────────

function ExternalSourcesList({ workspaceName }: { workspaceName: string }) {
  const { data: sources } = useExternalDocSources(workspaceName)
  const removeSource = useRemoveExternalDocSource()

  if (!sources || sources.length === 0) return null

  return (
    <div className="px-2 py-1.5 border-b">
      <div className="text-[10px] uppercase text-muted-foreground font-medium mb-1">
        External Sources
      </div>
      {sources.map((src) => (
        <div
          key={src.id}
          className="flex items-center gap-1.5 text-xs text-muted-foreground py-0.5 group"
        >
          <ExternalLink className="h-3 w-3 shrink-0 text-blue-500" />
          <span className="truncate flex-1" title={src.path}>
            {src.name}
          </span>
          <button
            onClick={() =>
              removeSource.mutate({
                workspace_name: workspaceName,
                source_id: src.id,
              })
            }
            className="opacity-0 group-hover:opacity-100 transition-opacity"
            title="Remove source"
          >
            <Trash2 className="h-3 w-3 text-destructive" />
          </button>
        </div>
      ))}
    </div>
  )
}

// ── Workspace doc status indicator ──────────────────────────────────────────

function DocStatusIndicator({ workspaceName }: { workspaceName: string }) {
  const { data: status } = useWorkspaceDocStatus(workspaceName)

  if (!status) return null

  const missing = !status.has_architecture || !status.has_standards

  if (!missing) return null

  return (
    <div className="px-2 py-1.5 border-b">
      <div className="flex items-start gap-1.5 text-xs">
        <AlertCircle className="h-3.5 w-3.5 shrink-0 text-destructive mt-0.5" />
        <div>
          {!status.has_architecture && (
            <p className="text-destructive">No architecture doc tagged</p>
          )}
          {!status.has_standards && (
            <p className="text-destructive">No standards doc tagged</p>
          )}
        </div>
      </div>
    </div>
  )
}

// ── Tag management toolbar ──────────────────────────────────────────────────

function TagToolbar({
  workspaceName,
  selectedPath,
}: {
  workspaceName: string
  selectedPath: string
}) {
  const { data: tags } = useDocTags(workspaceName)
  const tagDoc = useTagDocument()
  const untagDoc = useUntagDocument()

  const isReadonly = selectedPath.startsWith('@ext/')

  // Find existing tags for the selected path
  const archTag = tags?.find((t) => t.tag === 'architecture')
  const stdTag = tags?.find((t) => t.tag === 'standards')
  const isTaggedArch = archTag?.file_path === selectedPath
  const isTaggedStd = stdTag?.file_path === selectedPath

  // Show which doc currently holds each tag (if not this one)
  const archOtherDoc = archTag && !isTaggedArch ? archTag.file_path : null
  const stdOtherDoc = stdTag && !isTaggedStd ? stdTag.file_path : null

  return (
    <div className="flex items-center gap-2">
      {isReadonly && (
        <Badge variant="secondary" className="text-[10px] gap-1">
          <Lock className="h-3 w-3" />
          Read-only
        </Badge>
      )}

      <Button
        variant={isTaggedArch ? 'default' : 'outline'}
        size="sm"
        className={cn(
          'h-7 px-2.5 text-[11px] gap-1.5',
          isTaggedArch && 'bg-blue-600 hover:bg-blue-700 text-white',
        )}
        title={
          isTaggedArch
            ? 'Click to remove architecture tag from this document'
            : archOtherDoc
              ? `Currently tagged: ${archOtherDoc}. Click to reassign to this document.`
              : 'Click to tag this document as the architecture doc'
        }
        disabled={tagDoc.isPending || untagDoc.isPending}
        onClick={() => {
          if (isTaggedArch) {
            untagDoc.mutate({ workspace_name: workspaceName, tag: 'architecture' })
          } else {
            tagDoc.mutate({
              workspace_name: workspaceName,
              file_path: selectedPath,
              tag: 'architecture',
            })
          }
        }}
      >
        <Tag className="h-3 w-3" />
        {isTaggedArch ? 'Architecture' : 'Set as Architecture'}
      </Button>

      <Button
        variant={isTaggedStd ? 'default' : 'outline'}
        size="sm"
        className={cn(
          'h-7 px-2.5 text-[11px] gap-1.5',
          isTaggedStd && 'bg-green-600 hover:bg-green-700 text-white',
        )}
        title={
          isTaggedStd
            ? 'Click to remove standards tag from this document'
            : stdOtherDoc
              ? `Currently tagged: ${stdOtherDoc}. Click to reassign to this document.`
              : 'Click to tag this document as the standards doc'
        }
        disabled={tagDoc.isPending || untagDoc.isPending}
        onClick={() => {
          if (isTaggedStd) {
            untagDoc.mutate({ workspace_name: workspaceName, tag: 'standards' })
          } else {
            tagDoc.mutate({
              workspace_name: workspaceName,
              file_path: selectedPath,
              tag: 'standards',
            })
          }
        }}
      >
        <Tag className="h-3 w-3" />
        {isTaggedStd ? 'Standards' : 'Set as Standards'}
      </Button>
    </div>
  )
}

// ── Spec status helpers ──────────────────────────────────────────────────────

const specStatusConfig: Record<string, { label: string; color: string; icon: typeof Clock }> = {
  draft: { label: 'Draft', color: 'bg-muted text-muted-foreground', icon: Clock },
  approved: { label: 'Approved', color: 'bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200', icon: FileCheck },
  in_progress: { label: 'In Progress', color: 'bg-amber-100 text-amber-800 dark:bg-amber-900 dark:text-amber-200', icon: Clock },
  completed: { label: 'Completed', color: 'bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200', icon: CheckCircle2 },
  archived: { label: 'Archived', color: 'bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-400', icon: Archive },
}

function SpecStatusBadge({ status }: { status: string }) {
  const cfg = specStatusConfig[status] ?? specStatusConfig.draft
  const Icon = cfg.icon
  return (
    <Badge variant="outline" className={cn('text-[10px] gap-1', cfg.color)}>
      <Icon className="h-3 w-3" />
      {cfg.label}
    </Badge>
  )
}

// ── Spec list panel ──────────────────────────────────────────────────────────

function SpecListPanel({
  workspaceName,
  selectedSpecId,
  onSelect,
}: {
  workspaceName: string
  selectedSpecId: string
  onSelect: (id: string) => void
}) {
  const { data: specs, isLoading } = useSpecs(workspaceName)

  if (!workspaceName) {
    return (
      <EmptyState
        icon={FolderOpen}
        title="Select a project"
        description="Choose a project to browse its specifications"
      />
    )
  }

  if (isLoading) return <LoadingState message="Loading specs..." />

  if (!specs || specs.length === 0) {
    return (
      <EmptyState
        icon={ClipboardList}
        title="No specifications"
        description="Use create_spec to create a specification"
      />
    )
  }

  return (
    <div className="space-y-0.5">
      {specs.map((s: SpecSummary) => (
        <button
          key={s.id}
          onClick={() => onSelect(s.id)}
          className={cn(
            'flex items-center gap-2 w-full py-2 px-3 text-sm rounded-sm transition-colors text-left',
            s.id === selectedSpecId
              ? 'bg-primary text-primary-foreground'
              : 'text-muted-foreground hover:text-foreground hover:bg-accent',
          )}
        >
          <ClipboardList className="h-3.5 w-3.5 shrink-0" />
          <div className="flex-1 min-w-0">
            <div className="flex items-center gap-1.5">
              <span className="font-mono text-[10px] opacity-70">{s.label}</span>
              <span className="truncate">{s.title}</span>
            </div>
          </div>
          {s.id !== selectedSpecId && <SpecStatusBadge status={s.status} />}
        </button>
      ))}
    </div>
  )
}

// ── Spec detail viewer ───────────────────────────────────────────────────────

function SpecDetailViewer({ specId }: { specId: string }) {
  const { data: spec, isLoading } = useSpecDetail(specId)
  const updateStatus = useUpdateSpecStatus()

  if (isLoading) return <LoadingState message="Loading specification..." />
  if (!spec) return null

  const validTransitions: Record<string, string[]> = {
    draft: ['approved'],
    approved: ['in_progress'],
    in_progress: ['completed'],
    completed: ['archived'],
  }
  const nextStatuses = validTransitions[spec.status] ?? []

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-start justify-between gap-4">
        <div>
          <div className="flex items-center gap-2 mb-1">
            <span className="font-mono text-xs text-muted-foreground">{spec.label}</span>
            <SpecStatusBadge status={spec.status} />
          </div>
          <h2 className="text-lg font-semibold">{spec.title}</h2>
          {spec.created_by && (
            <p className="text-xs text-muted-foreground mt-1">Created by {spec.created_by}</p>
          )}
        </div>
        <div className="flex gap-2 shrink-0">
          {nextStatuses.map((status: string) => (
            <Button
              key={status}
              variant="outline"
              size="sm"
              className="text-xs"
              disabled={updateStatus.isPending}
              onClick={() => updateStatus.mutate({ spec_id: specId, status })}
            >
              {specStatusConfig[status]?.label ?? status}
            </Button>
          ))}
        </div>
      </div>

      {/* Description */}
      <div className="text-sm text-muted-foreground">
        <ReactMarkdown remarkPlugins={[remarkGfm]}>{spec.description}</ReactMarkdown>
      </div>

      {/* Milestones & Tasks */}
      {spec.milestones.map((ms, msIdx) => (
        <div key={ms.id} className="border rounded-md">
          <div className="px-4 py-3 bg-muted/50 border-b">
            <h3 className="text-sm font-medium">
              <span className="text-muted-foreground mr-1.5">Milestone {msIdx + 1}:</span>
              {ms.title}
            </h3>
            {ms.description && (
              <p className="text-xs text-muted-foreground mt-1">{ms.description}</p>
            )}
          </div>
          {ms.tasks.length > 0 && (
            <div className="divide-y">
              {ms.tasks.map((task, taskIdx) => (
                <div key={task.id} className="px-4 py-3">
                  <div className="flex items-start gap-2">
                    <span className="font-mono text-[10px] text-muted-foreground mt-0.5 shrink-0">
                      {msIdx + 1}.{taskIdx + 1}
                    </span>
                    <div className="flex-1 min-w-0">
                      <p className="text-sm font-medium">{task.title}</p>
                      {task.requirement && (
                        <div className="mt-1.5">
                          <span className="text-[10px] uppercase text-muted-foreground font-medium">Requirement</span>
                          <p className="text-xs text-muted-foreground mt-0.5">{task.requirement}</p>
                        </div>
                      )}
                      {task.acceptance_criteria && (
                        <div className="mt-1.5">
                          <span className="text-[10px] uppercase text-muted-foreground font-medium">Acceptance Criteria</span>
                          <p className="text-xs text-muted-foreground mt-0.5">{task.acceptance_criteria}</p>
                        </div>
                      )}
                    </div>
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      ))}

      {/* Amendments */}
      {spec.amendments && spec.amendments.length > 0 && (
        <div>
          <h3 className="text-sm font-medium mb-3">Amendments</h3>
          <div className="space-y-2">
            {spec.amendments.map((a) => (
              <div key={a.id} className="border rounded-md px-4 py-3">
                <div className="flex items-center gap-2 mb-1">
                  <span className="text-sm font-medium">{a.title}</span>
                  {a.author && (
                    <span className="text-[10px] text-muted-foreground">by {a.author}</span>
                  )}
                  <span className="text-[10px] text-muted-foreground ml-auto">{a.created_at}</span>
                </div>
                <p className="text-xs text-muted-foreground">{a.description}</p>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}

// ── Main page ───────────────────────────────────────────────────────────────

export function DocsPage() {
  const { data: workspaces, isLoading: wsLoading } = useWorkspaces()
  const [selectedWorkspace, setSelectedWorkspace] = useState('')
  const [selectedPath, setSelectedPath] = useState('')
  const location = useLocation()

  // Accept navigation from global search
  useEffect(() => {
    const state = location.state as { workspace?: string; path?: string } | null
    if (state?.workspace) {
      setSelectedWorkspace(state.workspace)
      if (state.path) setSelectedPath(state.path)
      // Clear the state so refreshing doesn't re-trigger
      window.history.replaceState({}, '')
    }
  }, [location.state])

  const { data: docs, isLoading: docsLoading } = useDocs(selectedWorkspace)
  const { data: status } = useWorkspaceDocStatus(selectedWorkspace)

  const tree = useMemo(() => {
    if (!docs || docs.length === 0) return []
    return buildTree(docs)
  }, [docs])

  const [selectedSpecId, setSelectedSpecId] = useState('')
  const [activeTab, setActiveTab] = useState('docs')

  const filteredWorkspaces = (workspaces ?? []).filter((w) => w.name !== '_org')

  // Determine if selected workspace is missing required docs
  const wsMissingDocs = status && (!status.has_architecture || !status.has_standards)

  if (wsLoading) return <LoadingState message="Loading workspaces..." />

  return (
    <div className="flex flex-col h-[calc(100vh-3.5rem)]">
      <div className="px-6 py-4 border-b bg-card shrink-0">
        <div className="flex items-center justify-between">
          <PageHeader
            title="Documentation"
            description="Browse documentation and specifications across all projects"
          />
          <div className="flex items-center gap-3">
            {/* Workspace selector — shared across tabs */}
            <Select
              value={selectedWorkspace || undefined}
              onValueChange={(name) => {
                setSelectedWorkspace(name)
                setSelectedPath('')
                setSelectedSpecId('')
              }}
            >
              <SelectTrigger
                className={cn(
                  'w-52 text-xs h-8',
                  wsMissingDocs && 'border-destructive text-destructive',
                )}
              >
                <SelectValue placeholder="Select project..." />
              </SelectTrigger>
              <SelectContent>
                {filteredWorkspaces.map((w) => (
                  <SelectItem key={w.name} value={w.name}>
                    {w.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        </div>
      </div>
      <div className="flex-1 overflow-hidden px-6 py-4">
        <Tabs value={activeTab} onValueChange={setActiveTab} className="h-full flex flex-col">
          <TabsList className="shrink-0 w-fit">
            <TabsTrigger value="docs" className="gap-1.5">
              <BookOpen className="h-3.5 w-3.5" />
              Documents
            </TabsTrigger>
            <TabsTrigger value="specs" className="gap-1.5">
              <ClipboardList className="h-3.5 w-3.5" />
              Specifications
            </TabsTrigger>
          </TabsList>

          {/* Documents tab */}
          <TabsContent value="docs" className="flex-1 overflow-hidden mt-3">
            <div className="flex h-full gap-0 overflow-hidden rounded-md border bg-card">
              {/* Left: file tree */}
              <div className="w-64 shrink-0 border-r flex flex-col">
                {/* Doc status indicator */}
                {selectedWorkspace && <DocStatusIndicator workspaceName={selectedWorkspace} />}

                {/* External sources list */}
                {selectedWorkspace && <ExternalSourcesList workspaceName={selectedWorkspace} />}

                {/* Add external source button */}
                {selectedWorkspace && (
                  <div className="px-2 py-1 border-b flex justify-end">
                    <AddExternalSourceDialog workspaceName={selectedWorkspace} />
                  </div>
                )}

                <div className="flex-1 overflow-y-auto p-1.5">
                  {!selectedWorkspace && (
                    <EmptyState
                      icon={FolderOpen}
                      title="Select a project"
                      description="Choose a project to browse its documentation"
                    />
                  )}
                  {selectedWorkspace && docsLoading && <LoadingState message="Loading files..." />}
                  {selectedWorkspace && !docsLoading && tree.length === 0 && (
                    <EmptyState
                      icon={FileText}
                      title="No docs found"
                      description="No markdown files in the docs/ directory"
                    />
                  )}
                  {tree.map((node) => (
                    <TreeItem
                      key={node.path}
                      node={node}
                      selectedPath={selectedPath}
                      onSelect={setSelectedPath}
                    />
                  ))}
                </div>
              </div>

              {/* Right: document content */}
              <div className="flex-1 overflow-y-auto p-6">
                {!selectedPath ? (
                  <EmptyState
                    icon={BookOpen}
                    title="Select a document"
                    description="Choose a file from the tree to read its content"
                  />
                ) : (
                  <div>
                    <div className="mb-4 pb-3 border-b flex items-center justify-between">
                      <h3 className="text-sm font-medium text-muted-foreground">{selectedPath}</h3>
                      <TagToolbar workspaceName={selectedWorkspace} selectedPath={selectedPath} />
                    </div>
                    <DocViewer workspaceName={selectedWorkspace} path={selectedPath} />
                  </div>
                )}
              </div>
            </div>
          </TabsContent>

          {/* Specifications tab */}
          <TabsContent value="specs" className="flex-1 overflow-hidden mt-3">
            <div className="flex h-full gap-0 overflow-hidden rounded-md border bg-card">
              {/* Left: spec list */}
              <div className="w-72 shrink-0 border-r flex flex-col">
                <div className="flex-1 overflow-y-auto p-1.5">
                  <SpecListPanel
                    workspaceName={selectedWorkspace}
                    selectedSpecId={selectedSpecId}
                    onSelect={setSelectedSpecId}
                  />
                </div>
              </div>

              {/* Right: spec detail */}
              <div className="flex-1 overflow-y-auto p-6">
                {!selectedSpecId ? (
                  <EmptyState
                    icon={ClipboardList}
                    title="Select a specification"
                    description="Choose a spec from the list to view its details"
                  />
                ) : (
                  <SpecDetailViewer specId={selectedSpecId} />
                )}
              </div>
            </div>
          </TabsContent>
        </Tabs>
      </div>
    </div>
  )
}
