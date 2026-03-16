import { useState } from 'react'
import {
  ClipboardList,
  PlusCircle,
  Loader2,
  ChevronDown,
  ChevronRight,
  MessageSquare,
  User,
  UserMinus,
  Trash2,
  Archive,
  ArrowUpDown,
  EyeOff,
  CheckCircle2,
  MoreHorizontal,
  Recycle,
} from 'lucide-react'
import {
  useProjects,
  useProjectDetails,
  useTasks,
  useCreateProject,
  useDeleteProject,
  useArchiveProject,
  useAddMilestone,
  useAddTask,
  useUpdateTaskStatus,
  useAssignTask,
  useUnassignTask,
  useAddComment,
  useDeleteTask,
  useDeleteMilestone,
  usePurgeOrphans,
  useBulkUpdateTaskStatus,
  type Project,
  type Milestone,
  type Task,
  type Comment,
  type CreateProjectArgs,
  type AddMilestoneArgs,
  type AddTaskArgs,
} from '@/services/taskService'
import { useAgents } from '@/services/agentService'
import { PageHeader } from '@/components/domain/page-header'
import { LoadingState } from '@/components/domain/loading-state'
import { ErrorState } from '@/components/domain/error-state'
import { EmptyState } from '@/components/domain/empty-state'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectSeparator,
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
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { toast } from '@/components/ui/use-toast'
import { cn } from '@/lib/utils'

// ─── Constants ────────────────────────────────────────────────────────────────

const TASK_STATUSES = ['pending', 'in_progress', 'blocked', 'completed'] as const
const PRIORITY_OPTIONS = ['low', 'medium', 'high', 'critical'] as const

// ─── Helpers ──────────────────────────────────────────────────────────────────

function statusVariant(status: string): 'default' | 'secondary' | 'destructive' | 'outline' {
  switch (status.toLowerCase()) {
    case 'completed':
      return 'default'
    case 'in_progress':
      return 'secondary'
    case 'blocked':
      return 'destructive'
    default:
      return 'outline'
  }
}

function priorityVariant(priority: string): 'default' | 'secondary' | 'destructive' | 'outline' {
  switch (priority.toLowerCase()) {
    case 'critical':
      return 'destructive'
    case 'high':
      return 'default'
    case 'medium':
      return 'secondary'
    default:
      return 'outline'
  }
}

function statusColumnBg(status: string): string {
  switch (status.toLowerCase()) {
    case 'in_progress':
      return 'bg-blue-950/20 border-blue-800/30'
    case 'completed':
      return 'bg-green-950/20 border-green-800/30'
    case 'blocked':
      return 'bg-red-950/20 border-red-800/30'
    default:
      return 'bg-muted/20 border-border'
  }
}

function formatStatus(s: string): string {
  return s.replace(/_/g, ' ').replace(/\b\w/g, (c) => c.toUpperCase())
}

function formatDate(iso: string | null | undefined): string {
  if (!iso) return '—'
  try {
    return new Date(iso).toLocaleDateString()
  } catch {
    return iso
  }
}

function collectAllTasks(project: Project | null | undefined): Task[] {
  if (!project) return []
  const fromMilestones: Task[] = []
  const milestones = Array.isArray(project.milestones) ? project.milestones : []
  for (const m of milestones) {
    const mTasks = Array.isArray(m.tasks) ? m.tasks : []
    fromMilestones.push(...mTasks)
  }
  const directTasks = Array.isArray(project.tasks) ? project.tasks : []
  return [...fromMilestones, ...directTasks]
}

function milestoneForTask(task: Task, project: Project | null | undefined): string {
  if (!project) return '—'
  const milestones = Array.isArray(project.milestones) ? project.milestones : []
  const found = milestones.find((m) => m.id === task.milestone_id)
  return found ? found.name : '—'
}

// ─── Create Project Dialog ────────────────────────────────────────────────────

interface CreateProjectDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

function CreateProjectDialog({ open, onOpenChange }: CreateProjectDialogProps) {
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [priority, setPriority] = useState<string>('medium')
  const [nameError, setNameError] = useState('')

  const { mutate: createProject, isPending } = useCreateProject()

  function reset() {
    setName('')
    setDescription('')
    setPriority('medium')
    setNameError('')
  }

  function handleOpenChange(next: boolean) {
    if (!next) reset()
    onOpenChange(next)
  }

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (!name.trim()) {
      setNameError('Name is required')
      return
    }
    setNameError('')
    const args: CreateProjectArgs = {
      workspace_name: 'hyperax',
      name: name.trim(),
      priority,
    }
    if (description.trim()) args.description = description.trim()

    createProject(args, {
      onSuccess: () => {
        toast({ title: 'Project created', description: `"${name}" has been created.` })
        handleOpenChange(false)
      },
      onError: (err) =>
        toast({ title: 'Create failed', description: (err as Error).message, variant: 'destructive' }),
    })
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>Create Project</DialogTitle>
          <DialogDescription>Create a new project plan in the hyperax workspace.</DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="space-y-1.5">
            <Label htmlFor="project-name">Name *</Label>
            <Input
              id="project-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="My Project"
              autoFocus
            />
            {nameError && <p className="text-xs text-destructive">{nameError}</p>}
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="project-desc">Description</Label>
            <Textarea
              id="project-desc"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="Optional description..."
              rows={3}
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="project-priority">Priority</Label>
            <Select value={priority} onValueChange={setPriority}>
              <SelectTrigger id="project-priority" className="h-9">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {PRIORITY_OPTIONS.map((p) => (
                  <SelectItem key={p} value={p} className="capitalize">
                    {p.charAt(0).toUpperCase() + p.slice(1)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
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
                'Create Project'
              )}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

// ─── Add Milestone Dialog ─────────────────────────────────────────────────────

interface AddMilestoneDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  projectId: string
}

function AddMilestoneDialog({ open, onOpenChange, projectId }: AddMilestoneDialogProps) {
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [priority, setPriority] = useState('medium')
  const [dueDate, setDueDate] = useState('')
  const [nameError, setNameError] = useState('')

  const { mutate: addMilestone, isPending } = useAddMilestone()

  function reset() {
    setName('')
    setDescription('')
    setPriority('medium')
    setDueDate('')
    setNameError('')
  }

  function handleOpenChange(next: boolean) {
    if (!next) reset()
    onOpenChange(next)
  }

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (!name.trim()) {
      setNameError('Name is required')
      return
    }
    setNameError('')
    const args: AddMilestoneArgs = {
      project_id: projectId,
      name: name.trim(),
      priority,
    }
    if (description.trim()) args.description = description.trim()
    if (dueDate) args.due_date = dueDate

    addMilestone(args, {
      onSuccess: () => {
        toast({ title: 'Milestone added', description: `"${name}" has been added.` })
        handleOpenChange(false)
      },
      onError: (err) =>
        toast({ title: 'Failed', description: (err as Error).message, variant: 'destructive' }),
    })
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>Add Milestone</DialogTitle>
          <DialogDescription>Add a new milestone to this project.</DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="space-y-1.5">
            <Label htmlFor="ms-name">Name *</Label>
            <Input
              id="ms-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="Milestone name"
              autoFocus
            />
            {nameError && <p className="text-xs text-destructive">{nameError}</p>}
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="ms-desc">Description</Label>
            <Textarea
              id="ms-desc"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="Optional description..."
              rows={2}
            />
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1.5">
              <Label htmlFor="ms-priority">Priority</Label>
              <Select value={priority} onValueChange={setPriority}>
                <SelectTrigger id="ms-priority" className="h-9">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {PRIORITY_OPTIONS.map((p) => (
                    <SelectItem key={p} value={p} className="capitalize">
                      {p.charAt(0).toUpperCase() + p.slice(1)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="ms-due">Due Date</Label>
              <Input
                id="ms-due"
                type="date"
                value={dueDate}
                onChange={(e) => setDueDate(e.target.value)}
                className="h-9"
              />
            </div>
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => handleOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={isPending}>
              {isPending ? (
                <>
                  <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                  Adding...
                </>
              ) : (
                'Add Milestone'
              )}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

// ─── Add Task Dialog ──────────────────────────────────────────────────────────

interface AddTaskDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  milestones: Milestone[]
  defaultMilestoneId?: string
}

function AddTaskDialog({ open, onOpenChange, milestones, defaultMilestoneId }: AddTaskDialogProps) {
  const [title, setTitle] = useState('')
  const [description, setDescription] = useState('')
  const [priority, setPriority] = useState('medium')
  const [milestoneId, setMilestoneId] = useState(defaultMilestoneId ?? (milestones[0]?.id ?? ''))
  const [assignee, setAssignee] = useState('')
  const [titleError, setTitleError] = useState('')
  const [milestoneError, setMilestoneError] = useState('')

  const { mutate: addTask, isPending } = useAddTask()

  function reset() {
    setTitle('')
    setDescription('')
    setPriority('medium')
    setMilestoneId(defaultMilestoneId ?? (milestones[0]?.id ?? ''))
    setAssignee('')
    setTitleError('')
    setMilestoneError('')
  }

  function handleOpenChange(next: boolean) {
    if (!next) reset()
    onOpenChange(next)
  }

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    let valid = true
    if (!title.trim()) { setTitleError('Title is required'); valid = false } else setTitleError('')
    if (!milestoneId) { setMilestoneError('Milestone is required'); valid = false } else setMilestoneError('')
    if (!valid) return

    const args: AddTaskArgs = {
      milestone_id: milestoneId,
      title: title.trim(),
      priority,
    }
    if (description.trim()) args.description = description.trim()
    if (assignee.trim()) args.assignee = assignee.trim()

    addTask(args, {
      onSuccess: () => {
        toast({ title: 'Task added', description: `"${title}" has been added.` })
        handleOpenChange(false)
      },
      onError: (err) =>
        toast({ title: 'Failed', description: (err as Error).message, variant: 'destructive' }),
    })
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>Add Task</DialogTitle>
          <DialogDescription>Add a new task to a milestone.</DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="space-y-1.5">
            <Label htmlFor="task-title">Title *</Label>
            <Input
              id="task-title"
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              placeholder="Task title"
              autoFocus
            />
            {titleError && <p className="text-xs text-destructive">{titleError}</p>}
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="task-desc">Description</Label>
            <Textarea
              id="task-desc"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="Optional description..."
              rows={2}
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="task-milestone">Milestone *</Label>
            <Select value={milestoneId} onValueChange={setMilestoneId}>
              <SelectTrigger id="task-milestone" className="h-9">
                <SelectValue placeholder="Select milestone..." />
              </SelectTrigger>
              <SelectContent>
                {milestones.map((m) => (
                  <SelectItem key={m.id} value={m.id}>
                    {m.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            {milestoneError && <p className="text-xs text-destructive">{milestoneError}</p>}
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1.5">
              <Label htmlFor="task-priority">Priority</Label>
              <Select value={priority} onValueChange={setPriority}>
                <SelectTrigger id="task-priority" className="h-9">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {PRIORITY_OPTIONS.map((p) => (
                    <SelectItem key={p} value={p} className="capitalize">
                      {p.charAt(0).toUpperCase() + p.slice(1)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="task-assignee">Assignee</Label>
              <Input
                id="task-assignee"
                value={assignee}
                onChange={(e) => setAssignee(e.target.value)}
                placeholder="agent-id or name"
                className="h-9"
              />
            </div>
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => handleOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={isPending}>
              {isPending ? (
                <>
                  <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                  Adding...
                </>
              ) : (
                'Add Task'
              )}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

// ─── Task Detail Panel ────────────────────────────────────────────────────────

interface TaskDetailPanelProps {
  task: Task
  onClose: () => void
}

function TaskDetailPanel({ task, onClose }: TaskDetailPanelProps) {
  const [commentText, setCommentText] = useState('')
  const [assigneeText, setAssigneeText] = useState(task.assignee ?? '')
  const [showAssignInput, setShowAssignInput] = useState(false)

  const { mutate: updateStatus, isPending: isUpdating } = useUpdateTaskStatus()
  const { mutate: assignTask, isPending: isAssigning } = useAssignTask()
  const { mutate: unassignTask, isPending: isUnassigning } = useUnassignTask()
  const { mutate: addComment, isPending: isCommenting } = useAddComment()

  const comments = Array.isArray(task.comments) ? task.comments : []

  function handleStatusChange(newStatus: string) {
    updateStatus(
      { task_id: task.id, status: newStatus },
      {
        onSuccess: () => toast({ title: 'Status updated', description: `Task marked as ${formatStatus(newStatus)}.` }),
        onError: (err) =>
          toast({ title: 'Update failed', description: (err as Error).message, variant: 'destructive' }),
      },
    )
  }

  function handleAssign() {
    if (!assigneeText.trim()) return
    assignTask(
      { task_id: task.id, assignee: assigneeText.trim() },
      {
        onSuccess: () => {
          toast({ title: 'Task assigned', description: `Assigned to ${assigneeText}.` })
          setShowAssignInput(false)
        },
        onError: (err) =>
          toast({ title: 'Assign failed', description: (err as Error).message, variant: 'destructive' }),
      },
    )
  }

  function handleUnassign() {
    unassignTask(task.id, {
      onSuccess: () => {
        toast({ title: 'Task unassigned' })
        setAssigneeText('')
      },
      onError: (err) =>
        toast({ title: 'Unassign failed', description: (err as Error).message, variant: 'destructive' }),
    })
  }

  function handleAddComment(e: React.FormEvent) {
    e.preventDefault()
    if (!commentText.trim()) return
    addComment(
      { task_id: task.id, content: commentText.trim() },
      {
        onSuccess: () => {
          toast({ title: 'Comment added' })
          setCommentText('')
        },
        onError: (err) =>
          toast({ title: 'Comment failed', description: (err as Error).message, variant: 'destructive' }),
      },
    )
  }

  return (
    <div className="border rounded-lg bg-card overflow-hidden flex flex-col h-full">
      {/* Header */}
      <div className="flex items-start justify-between px-4 py-3 border-b">
        <div className="flex-1 min-w-0 pr-3">
          <p className="text-sm font-semibold leading-snug">{task.title}</p>
          {task.description && (
            <p className="text-xs text-muted-foreground mt-1 leading-relaxed">{task.description}</p>
          )}
        </div>
        <button
          type="button"
          className="text-xs text-muted-foreground hover:text-foreground transition-colors shrink-0 mt-0.5"
          onClick={onClose}
        >
          Close
        </button>
      </div>

      <div className="overflow-auto flex-1 px-4 py-3 space-y-4">
        {/* Meta */}
        <div className="flex flex-wrap gap-2">
          <Badge variant={statusVariant(task.status)} className="capitalize text-xs">
            {formatStatus(task.status)}
          </Badge>
          <Badge variant={priorityVariant(task.priority)} className="capitalize text-xs">
            {task.priority}
          </Badge>
          {(task.displayAssignee || task.assignee) && (
            <Badge variant="outline" className="text-xs gap-1">
              <User className="h-3 w-3" />
              {task.displayAssignee || task.assignee}
            </Badge>
          )}
        </div>

        {/* Status change */}
        <div className="space-y-1.5">
          <p className="text-xs font-medium text-muted-foreground uppercase tracking-wide">Change Status</p>
          <div className="flex flex-wrap gap-1.5">
            {TASK_STATUSES.map((s) => (
              <Button
                key={s}
                size="sm"
                variant={task.status === s ? 'default' : 'outline'}
                className="h-7 text-xs capitalize"
                disabled={isUpdating || task.status === s}
                onClick={() => handleStatusChange(s)}
              >
                {formatStatus(s)}
              </Button>
            ))}
          </div>
        </div>

        {/* Assign */}
        <div className="space-y-1.5">
          <p className="text-xs font-medium text-muted-foreground uppercase tracking-wide">Assignment</p>
          {task.assignee ? (
            <div className="flex items-center gap-2">
              <span className="text-xs text-muted-foreground">Assigned to: <span className="text-foreground font-medium">{task.displayAssignee || task.assignee}</span></span>
              <Button
                size="sm"
                variant="ghost"
                className="h-6 px-2 text-xs text-muted-foreground hover:text-destructive"
                disabled={isUnassigning}
                onClick={handleUnassign}
              >
                <UserMinus className="h-3 w-3 mr-1" />
                Unassign
              </Button>
            </div>
          ) : showAssignInput ? (
            <div className="flex gap-2">
              <Input
                value={assigneeText}
                onChange={(e) => setAssigneeText(e.target.value)}
                placeholder="agent-id or name"
                className="h-8 text-xs flex-1"
                autoFocus
                onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); handleAssign() } }}
              />
              <Button size="sm" className="h-8 text-xs" disabled={isAssigning} onClick={handleAssign}>
                {isAssigning ? <Loader2 className="h-3 w-3 animate-spin" /> : 'Assign'}
              </Button>
              <Button
                size="sm"
                variant="ghost"
                className="h-8 text-xs"
                onClick={() => setShowAssignInput(false)}
              >
                Cancel
              </Button>
            </div>
          ) : (
            <Button
              size="sm"
              variant="outline"
              className="h-7 text-xs"
              onClick={() => setShowAssignInput(true)}
            >
              <User className="h-3 w-3 mr-1" />
              Assign
            </Button>
          )}
        </div>

        {/* Comments */}
        <div className="space-y-2">
          <p className="text-xs font-medium text-muted-foreground uppercase tracking-wide">
            Comments {comments.length > 0 && `(${comments.length})`}
          </p>
          {comments.length > 0 && (
            <div className="space-y-2 max-h-48 overflow-auto">
              {comments.map((c: Comment) => (
                <div key={c.id} className="rounded-md border bg-muted/20 px-3 py-2">
                  <div className="flex items-center justify-between mb-1">
                    <span className="text-xs font-medium">{c.author ?? 'Unknown'}</span>
                    <span className="text-xs text-muted-foreground">{formatDate(c.created_at)}</span>
                  </div>
                  <p className="text-xs text-foreground leading-relaxed">{c.content}</p>
                </div>
              ))}
            </div>
          )}
          <form onSubmit={handleAddComment} className="flex gap-2">
            <Input
              value={commentText}
              onChange={(e) => setCommentText(e.target.value)}
              placeholder="Add a comment..."
              className="h-8 text-xs flex-1"
            />
            <Button
              type="submit"
              size="sm"
              className="h-8 text-xs"
              disabled={isCommenting || !commentText.trim()}
            >
              {isCommenting ? (
                <Loader2 className="h-3 w-3 animate-spin" />
              ) : (
                <MessageSquare className="h-3 w-3" />
              )}
            </Button>
          </form>
        </div>

        <div className="text-xs text-muted-foreground">
          Created: {formatDate(task.created_at)}
          {task.updated_at && task.updated_at !== task.created_at && (
            <> · Updated: {formatDate(task.updated_at)}</>
          )}
        </div>
      </div>
    </div>
  )
}

// ─── Task Card (Board) ────────────────────────────────────────────────────────

interface TaskCardProps {
  task: Task
  milestoneName: string
  onSelect: (task: Task) => void
  onStatusChange: (taskId: string, status: string) => void
  isUpdating: boolean
  selected?: boolean
  onToggleSelect?: (taskId: string) => void
  onDelete?: (taskId: string) => void
}

function TaskCard({ task, milestoneName, onSelect, onStatusChange, isUpdating, selected, onToggleSelect, onDelete }: TaskCardProps) {
  return (
    <div
      className={cn(
        'border rounded-md bg-card p-3 space-y-2 cursor-pointer hover:border-primary/50 transition-colors',
        selected && 'border-primary bg-primary/5',
      )}
      onClick={() => onSelect(task)}
      role="button"
      tabIndex={0}
      onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') onSelect(task) }}
      aria-label={`Task: ${task.title}`}
    >
      <div className="flex items-start gap-2">
        {onToggleSelect && (
          <div onClick={(e) => e.stopPropagation()} onKeyDown={(e) => e.stopPropagation()} role="none">
            <input
              type="checkbox"
              checked={selected ?? false}
              onChange={() => onToggleSelect(task.id)}
              className="mt-0.5 h-3.5 w-3.5 cursor-pointer accent-primary"
              aria-label={`Select task: ${task.title}`}
            />
          </div>
        )}
        <p className="flex-1 text-sm font-medium leading-snug line-clamp-2">{task.title}</p>
        {onDelete && (
          <div onClick={(e) => e.stopPropagation()} onKeyDown={(e) => e.stopPropagation()} role="none">
            <Button
              size="sm"
              variant="ghost"
              className="h-5 w-5 p-0 text-muted-foreground hover:text-destructive shrink-0"
              onClick={() => onDelete(task.id)}
              title="Delete task"
            >
              <Trash2 className="h-3 w-3" />
            </Button>
          </div>
        )}
      </div>

      <div className="flex flex-wrap gap-1.5">
        <Badge variant={priorityVariant(task.priority)} className="text-xs capitalize">
          {task.priority}
        </Badge>
        {(task.displayAssignee || task.assignee) && (
          <Badge variant="outline" className="text-xs gap-1">
            <User className="h-2.5 w-2.5" />
            {task.displayAssignee || task.assignee}
          </Badge>
        )}
      </div>

      {milestoneName !== '—' && (
        <p className="text-xs text-muted-foreground truncate">{milestoneName}</p>
      )}

      <div
        onClick={(e) => e.stopPropagation()}
        onKeyDown={(e) => e.stopPropagation()}
        role="none"
      >
        <Select
          value={task.status}
          onValueChange={(s) => onStatusChange(task.id, s)}
          disabled={isUpdating}
        >
          <SelectTrigger className="h-6 w-full text-xs mt-1" onClick={(e) => e.stopPropagation()}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {TASK_STATUSES.map((s) => (
              <SelectItem key={s} value={s} className="text-xs capitalize">
                {formatStatus(s)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
    </div>
  )
}

// ─── Board View ───────────────────────────────────────────────────────────────

interface BoardViewProps {
  tasks: Task[]
  project: Project | null
  onSelectTask: (task: Task) => void
  hideCompleted?: boolean
  selectedIds?: Set<string>
  onToggleSelect?: (taskId: string) => void
  onSelectAll?: (taskIds: string[]) => void
  onDeleteTask?: (taskId: string) => void
  onCompleteMilestoneTasks?: (milestoneId: string) => void
  onDeleteMilestone?: (milestoneId: string) => void
}

function BoardView({ tasks, project, onSelectTask, hideCompleted = false, selectedIds, onToggleSelect, onSelectAll, onDeleteTask, onCompleteMilestoneTasks, onDeleteMilestone }: BoardViewProps) {
  const { mutate: updateStatus, isPending: isUpdating } = useUpdateTaskStatus()

  function handleStatusChange(taskId: string, status: string) {
    updateStatus(
      { task_id: taskId, status },
      {
        onSuccess: () => toast({ title: 'Status updated', description: `Task moved to ${formatStatus(status)}.` }),
        onError: (err) =>
          toast({ title: 'Update failed', description: (err as Error).message, variant: 'destructive' }),
      },
    )
  }

  const visibleStatuses = hideCompleted
    ? TASK_STATUSES.filter((s) => s !== 'completed')
    : TASK_STATUSES

  const colCount = visibleStatuses.length
  const gridClass =
    colCount === 4
      ? 'grid-cols-1 sm:grid-cols-2 xl:grid-cols-4'
      : colCount === 3
        ? 'grid-cols-1 sm:grid-cols-3'
        : 'grid-cols-1 sm:grid-cols-2'

  const milestones = project && Array.isArray(project.milestones) ? project.milestones : []

  function renderStatusColumns(columnTasks: Task[], msName: string, minH = 'min-h-[120px]') {
    return (
      <div className={cn('grid gap-3', gridClass)}>
        {visibleStatuses.map((status) => {
          const colTasks = columnTasks.filter((t) => t.status === status)
          return (
            <div
              key={status}
              className={cn('rounded-lg border p-3 space-y-2', minH, statusColumnBg(status))}
            >
              <div className="flex items-center justify-between pb-1">
                <span className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
                  {formatStatus(status)}
                </span>
                <span className="text-xs text-muted-foreground bg-muted/40 rounded px-1.5 py-0.5">
                  {colTasks.length}
                </span>
              </div>
              {colTasks.length === 0 && (
                <p className="text-xs text-muted-foreground/60 italic text-center pt-3">No tasks</p>
              )}
              {colTasks.map((task) => (
                <TaskCard
                  key={task.id}
                  task={task}
                  milestoneName={msName}
                  onSelect={onSelectTask}
                  onStatusChange={handleStatusChange}
                  isUpdating={isUpdating}
                  selected={selectedIds?.has(task.id)}
                  onToggleSelect={onToggleSelect}
                  onDelete={onDeleteTask}
                />
              ))}
            </div>
          )
        })}
      </div>
    )
  }

  // Milestone-grouped view for single-project mode
  if (milestones.length > 0) {
    const milestonedIds = new Set(milestones.map((m) => m.id))
    const ungrouped = tasks.filter((t) => !t.milestone_id || !milestonedIds.has(t.milestone_id))

    return (
      <div className="space-y-6">
        {milestones.map((milestone) => {
          const mTasks = tasks.filter((t) => t.milestone_id === milestone.id)
          const completedCount = mTasks.filter((t) => t.status === 'completed').length
          const visibleMTasks = hideCompleted ? mTasks.filter((t) => t.status !== 'completed') : mTasks

          if (hideCompleted && visibleMTasks.length === 0) return null

          return (
            <div key={milestone.id}>
              <div className="flex items-center gap-3 mb-3">
                <h3 className="text-sm font-semibold text-foreground">{milestone.name}</h3>
                <span className="text-xs text-muted-foreground tabular-nums">
                  {completedCount}/{mTasks.length} completed
                </span>
                <Badge variant={priorityVariant(milestone.priority)} className="text-xs capitalize">
                  {milestone.priority}
                </Badge>
                {(onSelectAll || onCompleteMilestoneTasks || onDeleteMilestone) && (
                  <DropdownMenu>
                    <DropdownMenuTrigger asChild>
                      <Button size="sm" variant="ghost" className="h-6 w-6 p-0 text-muted-foreground">
                        <MoreHorizontal className="h-3.5 w-3.5" />
                      </Button>
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="start">
                      {onSelectAll && visibleMTasks.length > 0 && (
                        <DropdownMenuItem onClick={() => onSelectAll(visibleMTasks.map((t) => t.id))}>
                          <CheckCircle2 className="h-3.5 w-3.5 mr-2" />
                          Select All ({visibleMTasks.length})
                        </DropdownMenuItem>
                      )}
                      {onCompleteMilestoneTasks && (
                        <DropdownMenuItem onClick={() => onCompleteMilestoneTasks(milestone.id)}>
                          <CheckCircle2 className="h-3.5 w-3.5 mr-2" />
                          Complete All Tasks
                        </DropdownMenuItem>
                      )}
                      {onDeleteMilestone && (
                        <>
                          <DropdownMenuSeparator />
                          <DropdownMenuItem
                            className="text-destructive focus:text-destructive"
                            onClick={() => onDeleteMilestone(milestone.id)}
                          >
                            <Trash2 className="h-3.5 w-3.5 mr-2" />
                            Delete Milestone
                          </DropdownMenuItem>
                        </>
                      )}
                    </DropdownMenuContent>
                  </DropdownMenu>
                )}
                <div className="flex-1 h-px bg-border" />
              </div>
              {renderStatusColumns(visibleMTasks, '—')}
            </div>
          )
        })}
        {ungrouped.length > 0 && (() => {
          const visibleUngrouped = hideCompleted ? ungrouped.filter((t) => t.status !== 'completed') : ungrouped
          if (visibleUngrouped.length === 0) return null
          return (
            <div>
              <div className="flex items-center gap-3 mb-3">
                <h3 className="text-sm font-semibold text-muted-foreground">Ungrouped</h3>
                <span className="text-xs text-muted-foreground">{visibleUngrouped.length} tasks</span>
                <div className="flex-1 h-px bg-border" />
              </div>
              {renderStatusColumns(visibleUngrouped, '—')}
            </div>
          )
        })()}
      </div>
    )
  }

  // Flat view (no milestones or no project)
  const columns = visibleStatuses.map((status) => ({
    status,
    label: formatStatus(status),
    tasks: tasks.filter((t) => t.status === status),
  }))

  return (
    <div className={cn('grid gap-4', gridClass)}>
      {columns.map(({ status, label, tasks: colTasks }) => (
        <div
          key={status}
          className={cn(
            'rounded-lg border p-3 space-y-2 min-h-[240px]',
            statusColumnBg(status),
          )}
        >
          <div className="flex items-center justify-between pb-1">
            <span className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
              {label}
            </span>
            <span className="text-xs text-muted-foreground bg-muted/40 rounded px-1.5 py-0.5">
              {colTasks.length}
            </span>
          </div>
          {colTasks.length === 0 && (
            <p className="text-xs text-muted-foreground/60 italic text-center pt-6">No tasks</p>
          )}
          {colTasks.map((task) => (
            <TaskCard
              key={task.id}
              task={task}
              milestoneName={milestoneForTask(task, project)}
              onSelect={onSelectTask}
              onStatusChange={handleStatusChange}
              isUpdating={isUpdating}
              selected={selectedIds?.has(task.id)}
              onToggleSelect={onToggleSelect}
              onDelete={onDeleteTask}
            />
          ))}
        </div>
      ))}
    </div>
  )
}

// ─── All-Projects Board View ──────────────────────────────────────────────────

interface AllProjectsBoardViewProps {
  tasks: Task[]
  projects: Project[]
  onSelectTask: (task: Task) => void
  hideCompleted?: boolean
  selectedIds?: Set<string>
  onToggleSelect?: (taskId: string) => void
  onSelectAll?: (taskIds: string[]) => void
  onDeleteTask?: (taskId: string) => void
}

function AllProjectsBoardView({ tasks, projects, onSelectTask, hideCompleted = false, selectedIds, onToggleSelect, onSelectAll, onDeleteTask }: AllProjectsBoardViewProps) {
  const { mutate: updateStatus, isPending: isUpdating } = useUpdateTaskStatus()

  function handleStatusChange(taskId: string, status: string) {
    updateStatus(
      { task_id: taskId, status },
      {
        onSuccess: () => toast({ title: 'Status updated', description: `Task moved to ${formatStatus(status)}.` }),
        onError: (err) =>
          toast({ title: 'Update failed', description: (err as Error).message, variant: 'destructive' }),
      },
    )
  }

  // Group tasks by project_id
  const projectMap = new Map(projects.map((p) => [p.id, p]))
  const tasksByProject = new Map<string, Task[]>()
  const ungrouped: Task[] = []

  for (const task of tasks) {
    const pid = task.project_id
    if (pid && projectMap.has(pid)) {
      const existing = tasksByProject.get(pid) ?? []
      existing.push(task)
      tasksByProject.set(pid, existing)
    } else {
      ungrouped.push(task)
    }
  }

  const visibleStatuses = hideCompleted
    ? TASK_STATUSES.filter((s) => s !== 'completed')
    : TASK_STATUSES

  function renderProjectBoard(projectTasks: Task[], project: Project | null) {
    const colCount = visibleStatuses.length
    const gridClass =
      colCount === 4
        ? 'grid-cols-1 sm:grid-cols-2 xl:grid-cols-4'
        : colCount === 3
          ? 'grid-cols-1 sm:grid-cols-3'
          : 'grid-cols-1 sm:grid-cols-2'

    return (
      <div className={cn('grid gap-3', gridClass)}>
        {visibleStatuses.map((status) => {
          const colTasks = projectTasks.filter((t) => t.status === status)
          return (
            <div
              key={status}
              className={cn('rounded-lg border p-3 space-y-2 min-h-[160px]', statusColumnBg(status))}
            >
              <div className="flex items-center justify-between pb-1">
                <span className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
                  {formatStatus(status)}
                </span>
                <span className="text-xs text-muted-foreground bg-muted/40 rounded px-1.5 py-0.5">
                  {colTasks.length}
                </span>
              </div>
              {colTasks.length === 0 && (
                <p className="text-xs text-muted-foreground/60 italic text-center pt-4">No tasks</p>
              )}
              {colTasks.map((task) => (
                <TaskCard
                  key={task.id}
                  task={task}
                  milestoneName={milestoneForTask(task, project)}
                  onSelect={onSelectTask}
                  onStatusChange={handleStatusChange}
                  isUpdating={isUpdating}
                  selected={selectedIds?.has(task.id)}
                  onToggleSelect={onToggleSelect}
                  onDelete={onDeleteTask}
                />
              ))}
            </div>
          )
        })}
      </div>
    )
  }

  const sections: Array<{ key: string; name: string; tasks: Task[]; project: Project | null }> = []
  for (const [pid, ptasks] of tasksByProject) {
    sections.push({ key: pid, name: projectMap.get(pid)?.name ?? 'Unknown', tasks: ptasks, project: projectMap.get(pid) ?? null })
  }
  if (ungrouped.length > 0) {
    sections.push({ key: '__ungrouped', name: 'Ungrouped', tasks: ungrouped, project: null })
  }

  if (sections.length === 0) {
    return (
      <EmptyState
        icon={ClipboardList}
        title="No tasks"
        description="Tasks will appear here once projects have tasks assigned."
      />
    )
  }

  function handleBulkComplete(taskList: Task[]) {
    const pending = taskList.filter((t) => t.status !== 'completed')
    if (pending.length === 0) return
    if (!confirm(`Mark ${pending.length} ungrouped task${pending.length !== 1 ? 's' : ''} as completed?`)) return
    for (const t of pending) {
      updateStatus({ task_id: t.id, status: 'completed' })
    }
    toast({ title: 'Bulk update', description: `Marking ${pending.length} tasks as completed...` })
  }

  return (
    <div className="space-y-6">
      {sections.map(({ key, name, tasks: ptasks, project }) => (
        <div key={key}>
          <div className="flex items-center gap-3 mb-3">
            <h3 className={cn('text-sm font-semibold', key === '__ungrouped' ? 'text-muted-foreground' : 'text-foreground')}>{name}</h3>
            <span className="text-xs text-muted-foreground">
              {ptasks.length} task{ptasks.length !== 1 ? 's' : ''}
            </span>
            {onSelectAll && ptasks.length > 0 && (
              <Button
                size="sm"
                variant="outline"
                className="h-7 text-xs"
                onClick={() => onSelectAll(ptasks.map((t) => t.id))}
              >
                Select All ({ptasks.length})
              </Button>
            )}
            {key === '__ungrouped' && ptasks.some((t) => t.status !== 'completed') && (
              <Button
                size="sm"
                variant="outline"
                className="h-7 text-xs"
                onClick={() => handleBulkComplete(ptasks)}
              >
                <CheckCircle2 className="h-3 w-3 mr-1" />
                Mark All Completed
              </Button>
            )}
            <div className="flex-1 h-px bg-border" />
          </div>
          {renderProjectBoard(ptasks, project)}
        </div>
      ))}
    </div>
  )
}

// ─── List View ────────────────────────────────────────────────────────────────

type SortKey = 'title' | 'status' | 'priority' | 'assignee' | 'updated_at'
type SortDir = 'asc' | 'desc'

const PRIORITY_ORDER: Record<string, number> = { critical: 0, high: 1, medium: 2, low: 3 }
const STATUS_ORDER: Record<string, number> = { blocked: 0, in_progress: 1, pending: 2, completed: 3 }

interface ListViewProps {
  tasks: Task[]
  project: Project | null
  onSelectTask: (task: Task) => void
  hideCompleted?: boolean
  selectedIds?: Set<string>
  onToggleSelect?: (taskId: string) => void
  onSelectAll?: (taskIds: string[]) => void
  onDeleteTask?: (taskId: string) => void
  onCompleteMilestoneTasks?: (milestoneId: string) => void
  onDeleteMilestone?: (milestoneId: string) => void
}

function ListView({ tasks, project, onSelectTask, hideCompleted = false, selectedIds, onToggleSelect, onSelectAll, onDeleteTask, onCompleteMilestoneTasks, onDeleteMilestone }: ListViewProps) {
  const [sortKey, setSortKey] = useState<SortKey>('status')
  const [sortDir, setSortDir] = useState<SortDir>('asc')
  const { mutate: updateStatus } = useUpdateTaskStatus()

  function handleSort(key: SortKey) {
    if (key === sortKey) {
      setSortDir((d) => (d === 'asc' ? 'desc' : 'asc'))
    } else {
      setSortKey(key)
      setSortDir('asc')
    }
  }

  function handleStatusChange(taskId: string, status: string) {
    updateStatus(
      { task_id: taskId, status },
      {
        onSuccess: () => toast({ title: 'Status updated' }),
        onError: (err) =>
          toast({ title: 'Update failed', description: (err as Error).message, variant: 'destructive' }),
      },
    )
  }

  const visibleTasks = hideCompleted ? tasks.filter((t) => t.status !== 'completed') : tasks
  const sorted = [...visibleTasks].sort((a, b) => {
    let cmp = 0
    switch (sortKey) {
      case 'title':
        cmp = a.title.localeCompare(b.title)
        break
      case 'status':
        cmp = (STATUS_ORDER[a.status] ?? 99) - (STATUS_ORDER[b.status] ?? 99)
        break
      case 'priority':
        cmp = (PRIORITY_ORDER[a.priority] ?? 99) - (PRIORITY_ORDER[b.priority] ?? 99)
        break
      case 'assignee':
        cmp = (a.displayAssignee ?? a.assignee ?? '').localeCompare(b.displayAssignee ?? b.assignee ?? '')
        break
      case 'updated_at':
        cmp = (a.updated_at ?? a.created_at).localeCompare(b.updated_at ?? b.created_at)
        break
    }
    return sortDir === 'asc' ? cmp : -cmp
  })

  function SortHeader({ col, label }: { col: SortKey; label: string }) {
    return (
      <button
        type="button"
        className="flex items-center gap-1 text-xs font-medium text-muted-foreground hover:text-foreground transition-colors"
        onClick={() => handleSort(col)}
      >
        {label}
        <ArrowUpDown className={cn('h-3 w-3', sortKey === col ? 'text-foreground' : 'opacity-40')} />
      </button>
    )
  }

  const milestones = project && Array.isArray(project.milestones) ? project.milestones : []
  const hasMilestoneGroups = milestones.length > 0
  const hasCheckbox = !!onToggleSelect
  const hasDelete = !!onDeleteTask

  // Build grid-cols based on optional columns
  function buildGridCols(includeMilestone: boolean): string {
    const parts: string[] = []
    if (hasCheckbox) parts.push('auto')
    parts.push('1fr')
    parts.push('auto', 'auto', 'auto', 'auto')
    if (includeMilestone) parts.push('auto')
    if (hasDelete) parts.push('auto')
    return `grid-cols-[${parts.join('_')}]`
  }

  const gridCols = buildGridCols(!hasMilestoneGroups)

  function renderTaskRow(task: Task) {
    return (
      <div
        key={task.id}
        className={cn(
          'grid gap-x-4 items-center px-4 py-2.5 border-b last:border-0 hover:bg-muted/20 transition-colors',
          gridCols,
          selectedIds?.has(task.id) && 'bg-primary/5',
        )}
      >
        {hasCheckbox && (
          <input
            type="checkbox"
            checked={selectedIds?.has(task.id) ?? false}
            onChange={() => onToggleSelect!(task.id)}
            className="h-3.5 w-3.5 cursor-pointer accent-primary"
            aria-label={`Select task: ${task.title}`}
          />
        )}
        <button
          type="button"
          className="text-sm font-medium text-left truncate hover:text-primary transition-colors"
          onClick={() => onSelectTask(task)}
        >
          {task.title}
        </button>
        <div onClick={(e) => e.stopPropagation()} role="none">
          <Select value={task.status} onValueChange={(s) => handleStatusChange(task.id, s)}>
            <SelectTrigger className="h-7 w-28 text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {TASK_STATUSES.map((s) => (
                <SelectItem key={s} value={s} className="text-xs capitalize">
                  {formatStatus(s)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <Badge variant={priorityVariant(task.priority)} className="text-xs capitalize">
          {task.priority}
        </Badge>
        <span className="text-xs text-muted-foreground">{task.displayAssignee || task.assignee || '—'}</span>
        {!hasMilestoneGroups && (
          <span className="text-xs text-muted-foreground truncate max-w-[120px]">
            {milestoneForTask(task, project)}
          </span>
        )}
        <span className="text-xs text-muted-foreground">
          {formatDate(task.updated_at ?? task.created_at)}
        </span>
        {hasDelete && (
          <Button
            size="sm"
            variant="ghost"
            className="h-6 w-6 p-0 text-muted-foreground hover:text-destructive"
            onClick={() => onDeleteTask!(task.id)}
            title="Delete task"
          >
            <Trash2 className="h-3 w-3" />
          </Button>
        )}
      </div>
    )
  }

  // Milestone-grouped list view
  if (hasMilestoneGroups) {
    const tasksByMs = new Map<string, Task[]>()
    const ungrouped: Task[] = []
    for (const task of sorted) {
      if (task.milestone_id && milestones.some((m) => m.id === task.milestone_id)) {
        const arr = tasksByMs.get(task.milestone_id) ?? []
        arr.push(task)
        tasksByMs.set(task.milestone_id, arr)
      } else {
        ungrouped.push(task)
      }
    }

    const headerColsGrouped = buildGridCols(false)
    const allVisibleIds = sorted.map((t) => t.id)
    const allSelected = allVisibleIds.length > 0 && allVisibleIds.every((id) => selectedIds?.has(id))
    return (
      <div className="border rounded-lg overflow-hidden">
        <div className={cn('grid gap-x-4 items-center px-4 py-2.5 border-b bg-muted/30 text-xs', headerColsGrouped)}>
          {hasCheckbox && (
            <input
              type="checkbox"
              checked={allSelected}
              onChange={() => onSelectAll?.(allSelected ? [] : allVisibleIds)}
              className="h-3.5 w-3.5 cursor-pointer accent-primary"
              aria-label="Select all tasks"
              title={allSelected ? 'Deselect all' : `Select all (${allVisibleIds.length})`}
            />
          )}
          <SortHeader col="title" label="Title" />
          <SortHeader col="status" label="Status" />
          <SortHeader col="priority" label="Priority" />
          <SortHeader col="assignee" label="Assignee" />
          <SortHeader col="updated_at" label="Updated" />
          {hasDelete && <span />}
        </div>

        {milestones.map((milestone) => {
          const mTasks = tasksByMs.get(milestone.id) ?? []
          if (mTasks.length === 0 && hideCompleted) return null
          const allMTasks = milestone.tasks ?? []
          const completedCount = allMTasks.filter((t) => t.status === 'completed').length

          return (
            <div key={milestone.id}>
              <div className="flex items-center gap-3 px-4 py-2 bg-muted/40 border-b">
                <span className="text-xs font-semibold text-foreground">{milestone.name}</span>
                <Badge variant={priorityVariant(milestone.priority)} className="text-[10px] capitalize">
                  {milestone.priority}
                </Badge>
                <span className="text-xs text-muted-foreground tabular-nums">
                  {completedCount}/{allMTasks.length} completed
                </span>
                {(onSelectAll || onCompleteMilestoneTasks || onDeleteMilestone) && (
                  <DropdownMenu>
                    <DropdownMenuTrigger asChild>
                      <Button size="sm" variant="ghost" className="h-6 w-6 p-0 ml-auto">
                        <MoreHorizontal className="h-3.5 w-3.5" />
                      </Button>
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="start">
                      {onSelectAll && mTasks.length > 0 && (
                        <DropdownMenuItem onClick={() => onSelectAll(mTasks.map((t) => t.id))}>
                          <CheckCircle2 className="h-3.5 w-3.5 mr-2" />
                          Select All ({mTasks.length})
                        </DropdownMenuItem>
                      )}
                      {onCompleteMilestoneTasks && (
                        <DropdownMenuItem onClick={() => onCompleteMilestoneTasks(milestone.id)}>
                          <CheckCircle2 className="h-3.5 w-3.5 mr-2" />
                          Complete All Tasks
                        </DropdownMenuItem>
                      )}
                      {onDeleteMilestone && (
                        <>
                          <DropdownMenuSeparator />
                          <DropdownMenuItem
                            className="text-destructive focus:text-destructive"
                            onClick={() => onDeleteMilestone(milestone.id)}
                          >
                            <Trash2 className="h-3.5 w-3.5 mr-2" />
                            Delete Milestone
                          </DropdownMenuItem>
                        </>
                      )}
                    </DropdownMenuContent>
                  </DropdownMenu>
                )}
              </div>
              {mTasks.length === 0 ? (
                <div className="px-4 py-3 border-b text-xs text-muted-foreground italic">
                  {hideCompleted ? 'All tasks completed' : 'No tasks in this milestone'}
                </div>
              ) : (
                mTasks.map((task) => renderTaskRow(task))
              )}
            </div>
          )
        })}

        {ungrouped.length > 0 && (
          <div>
            <div className="flex items-center gap-3 px-4 py-2 bg-muted/40 border-b">
              <span className="text-xs font-semibold text-muted-foreground">Ungrouped</span>
              <span className="text-xs text-muted-foreground">{ungrouped.length} tasks</span>
            </div>
            {ungrouped.map((task) => renderTaskRow(task))}
          </div>
        )}
      </div>
    )
  }

  // Flat list view (no milestones)
  const flatHeaderCols = buildGridCols(true)
  const flatVisibleIds = sorted.map((t) => t.id)
  const flatAllSelected = flatVisibleIds.length > 0 && flatVisibleIds.every((id) => selectedIds?.has(id))
  return (
    <div className="border rounded-lg overflow-hidden">
      <div className={cn('grid gap-x-4 items-center px-4 py-2.5 border-b bg-muted/30 text-xs', flatHeaderCols)}>
        {hasCheckbox && (
          <input
            type="checkbox"
            checked={flatAllSelected}
            onChange={() => onSelectAll?.(flatAllSelected ? [] : flatVisibleIds)}
            className="h-3.5 w-3.5 cursor-pointer accent-primary"
            aria-label="Select all tasks"
            title={flatAllSelected ? 'Deselect all' : `Select all (${flatVisibleIds.length})`}
          />
        )}
        <SortHeader col="title" label="Title" />
        <SortHeader col="status" label="Status" />
        <SortHeader col="priority" label="Priority" />
        <SortHeader col="assignee" label="Assignee" />
        <span className="text-xs font-medium text-muted-foreground">Milestone</span>
        <SortHeader col="updated_at" label="Updated" />
        {hasDelete && <span />}
      </div>
      {sorted.map((task) => renderTaskRow(task))}
    </div>
  )
}

// ─── Project Detail ───────────────────────────────────────────────────────────

interface ProjectDetailProps {
  project: Project
  onAddMilestone: () => void
  onAddTask: (milestoneId?: string) => void
}

function ProjectDetail({ project, onAddMilestone, onAddTask }: ProjectDetailProps) {
  const [expanded, setExpanded] = useState<Record<string, boolean>>({})
  const { mutate: deleteProject } = useDeleteProject()
  const { mutate: archiveProject } = useArchiveProject()

  function toggleMilestone(id: string) {
    setExpanded((prev) => ({ ...prev, [id]: !prev[id] }))
  }

  function handleDelete() {
    if (!confirm(`Delete project "${project.name}"? This cannot be undone.`)) return
    deleteProject(project.id, {
      onSuccess: () => toast({ title: 'Project deleted', description: `"${project.name}" has been removed.` }),
      onError: (err) =>
        toast({ title: 'Delete failed', description: (err as Error).message, variant: 'destructive' }),
    })
  }

  function handleArchive() {
    archiveProject(project.id, {
      onSuccess: () => toast({ title: 'Project archived', description: `"${project.name}" has been archived.` }),
      onError: (err) =>
        toast({ title: 'Archive failed', description: (err as Error).message, variant: 'destructive' }),
    })
  }

  const milestones = Array.isArray(project.milestones) ? project.milestones : []

  return (
    <div className="space-y-3">
      {/* Project header bar */}
      <div className="flex items-center justify-between gap-2 border rounded-lg px-4 py-3 bg-card">
        <div className="flex items-center gap-3 min-w-0">
          <div className="min-w-0">
            <p className="text-sm font-semibold truncate">{project.name}</p>
            {project.description && (
              <p className="text-xs text-muted-foreground truncate">{project.description}</p>
            )}
          </div>
          <Badge variant={statusVariant(project.status)} className="capitalize text-xs shrink-0">
            {formatStatus(project.status)}
          </Badge>
          <Badge variant={priorityVariant(project.priority)} className="capitalize text-xs shrink-0">
            {project.priority}
          </Badge>
        </div>
        <div className="flex items-center gap-1 shrink-0">
          <Button size="sm" variant="outline" className="h-7 text-xs" onClick={onAddMilestone}>
            <PlusCircle className="h-3 w-3 mr-1" />
            Milestone
          </Button>
          <Button
            size="sm"
            variant="outline"
            className="h-7 text-xs"
            onClick={() => onAddTask()}
            disabled={milestones.length === 0}
            title={milestones.length === 0 ? 'Add a milestone first' : undefined}
          >
            <PlusCircle className="h-3 w-3 mr-1" />
            Task
          </Button>
          <Button
            size="sm"
            variant="ghost"
            className="h-7 w-7 p-0 text-muted-foreground hover:text-foreground"
            onClick={handleArchive}
            title="Archive project"
          >
            <Archive className="h-3.5 w-3.5" />
          </Button>
          <Button
            size="sm"
            variant="ghost"
            className="h-7 w-7 p-0 text-muted-foreground hover:text-destructive"
            onClick={handleDelete}
            title="Delete project"
          >
            <Trash2 className="h-3.5 w-3.5" />
          </Button>
        </div>
      </div>

      {/* Milestones */}
      {milestones.length === 0 ? (
        <div className="border rounded-lg px-4 py-6 text-center">
          <p className="text-xs text-muted-foreground italic">No milestones yet.</p>
          <Button size="sm" variant="outline" className="mt-3 text-xs h-7" onClick={onAddMilestone}>
            <PlusCircle className="h-3 w-3 mr-1" />
            Add first milestone
          </Button>
        </div>
      ) : (
        <div className="space-y-2">
          {milestones.map((milestone: Milestone) => {
            const isOpen = expanded[milestone.id] ?? true
            const mTasks = Array.isArray(milestone.tasks) ? milestone.tasks : []
            return (
              <div key={milestone.id} className="border rounded-lg overflow-hidden">
                <button
                  type="button"
                  className="w-full flex items-center gap-3 px-3 py-2.5 hover:bg-muted/40 transition-colors text-left"
                  onClick={() => toggleMilestone(milestone.id)}
                >
                  {isOpen ? (
                    <ChevronDown className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                  ) : (
                    <ChevronRight className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                  )}
                  <span className="flex-1 text-sm font-medium truncate">{milestone.name}</span>
                  <Badge variant={priorityVariant(milestone.priority)} className="text-xs capitalize shrink-0">
                    {milestone.priority}
                  </Badge>
                  {milestone.due_date && (
                    <span className="text-xs text-muted-foreground shrink-0">
                      Due {formatDate(milestone.due_date)}
                    </span>
                  )}
                  <span className="text-xs text-muted-foreground shrink-0">
                    {mTasks.length} task{mTasks.length !== 1 ? 's' : ''}
                  </span>
                  <Button
                    size="sm"
                    variant="ghost"
                    className="h-6 w-6 p-0 text-muted-foreground hover:text-foreground"
                    onClick={(e) => { e.stopPropagation(); onAddTask(milestone.id) }}
                    title="Add task to this milestone"
                  >
                    <PlusCircle className="h-3 w-3" />
                  </Button>
                </button>

                {isOpen && (
                  <div className="border-t bg-muted/5">
                    {mTasks.length === 0 ? (
                      <p className="text-xs text-muted-foreground italic px-4 py-3">No tasks in this milestone.</p>
                    ) : (
                      mTasks.map((task: Task) => (
                        <div
                          key={task.id}
                          className="flex items-center gap-3 px-4 py-2.5 border-b last:border-0 text-sm hover:bg-muted/20 transition-colors"
                        >
                          <span className="flex-1 truncate font-medium text-sm">{task.title}</span>
                          <Badge variant={priorityVariant(task.priority)} className="text-xs capitalize shrink-0">
                            {task.priority}
                          </Badge>
                          {(task.displayAssignee || task.assignee) && (
                            <span className="text-xs text-muted-foreground shrink-0">{task.displayAssignee || task.assignee}</span>
                          )}
                          <Badge variant={statusVariant(task.status)} className="capitalize text-xs shrink-0">
                            {formatStatus(task.status)}
                          </Badge>
                        </div>
                      ))
                    )}
                  </div>
                )}
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}

// ─── Tasks Page ───────────────────────────────────────────────────────────────

const ALL_PROJECTS_VALUE = '__all__'
const HIDE_COMPLETED_KEY = 'hyperax-tasks-hide-completed'
const SHOW_COMPLETED_PROJECTS_KEY = 'hyperax-tasks-show-completed-projects'

export function TasksPage() {
  // null means "All Projects", a string ID means a specific project
  const [selectedProjectId, setSelectedProjectId] = useState<string | null>(null)
  const [selectedTask, setSelectedTask] = useState<Task | null>(null)
  const [createProjectOpen, setCreateProjectOpen] = useState(false)
  const [addMilestoneOpen, setAddMilestoneOpen] = useState(false)
  const [addTaskOpen, setAddTaskOpen] = useState(false)
  const [addTaskMilestoneId, setAddTaskMilestoneId] = useState<string | undefined>(undefined)
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set())
  const [hideCompleted, setHideCompleted] = useState<boolean>(() => {
    try {
      return localStorage.getItem(HIDE_COMPLETED_KEY) === 'true'
    } catch {
      return false
    }
  })
  const [showCompletedProjects, setShowCompletedProjects] = useState<boolean>(() => {
    try {
      return localStorage.getItem(SHOW_COMPLETED_PROJECTS_KEY) === 'true'
    } catch {
      return false
    }
  })

  const { data: projects, isLoading: isLoadingProjects, error: projectsError, refetch: refetchProjects } = useProjects()
  const { data: projectDetails, isLoading: isLoadingDetails } = useProjectDetails(selectedProjectId)
  // Fetch all tasks directly from the workspace for the "All Projects" view
  const { data: workspaceTasks } = useTasks('hyperax')
  // Agent name lookup for resolving assignee UUIDs to display names
  const { data: agentList } = useAgents()
  const { mutate: archiveProject } = useArchiveProject()
  const { mutate: deleteProject } = useDeleteProject()
  const { mutate: deleteTask } = useDeleteTask()
  const { mutate: deleteMilestone } = useDeleteMilestone()
  const { mutate: purgeOrphans, isPending: isPurging } = usePurgeOrphans()
  const { mutate: bulkUpdateStatus, isPending: isBulkUpdating } = useBulkUpdateTaskStatus()
  const agentNameMap = new Map(
    (Array.isArray(agentList) ? agentList : []).map((a) => [a.id, a.name]),
  )

  const projectList = Array.isArray(projects) ? projects : []
  const selectedProject = projectDetails ?? projectList.find((p) => p.id === selectedProjectId) ?? null
  const milestones: Milestone[] = Array.isArray(selectedProject?.milestones) ? selectedProject!.milestones! : []
  // Enrich tasks with resolved assignee names for display.
  function enrichTasks(tasks: Task[]): Task[] {
    if (agentNameMap.size === 0) return tasks
    return tasks.map(t => ({
      ...t,
      displayAssignee: t.assignee ? agentNameMap.get(t.assignee) ?? t.assignee : undefined,
    }))
  }

  const allTasks = enrichTasks(collectAllTasks(selectedProject))

  // All-project tasks: use direct list_tasks result (list_projects doesn't embed tasks)
  const allProjectsTasks: Task[] = enrichTasks(Array.isArray(workspaceTasks) ? workspaceTasks : [])

  // Determine which projects have at least one non-completed task
  const projectsWithActiveTasks = new Set<string>()
  for (const task of allProjectsTasks) {
    if (task.project_id && task.status !== 'completed') {
      projectsWithActiveTasks.add(task.project_id)
    }
  }

  // Filtered project list: only show projects with active tasks unless toggle is on
  const filteredProjectList = showCompletedProjects
    ? projectList
    : projectList.filter((p) => projectsWithActiveTasks.has(p.id))

  function toggleShowCompletedProjects() {
    setShowCompletedProjects((prev) => {
      const next = !prev
      try {
        localStorage.setItem(SHOW_COMPLETED_PROJECTS_KEY, String(next))
      } catch {
        // ignore
      }
      // If hiding completed projects and the currently selected project is fully completed, reset to All Projects
      if (!next && selectedProjectId && !projectsWithActiveTasks.has(selectedProjectId)) {
        setSelectedProjectId(null)
        setSelectedTask(null)
      }
      return next
    })
  }

  function toggleHideCompleted() {
    setHideCompleted((prev) => {
      const next = !prev
      try {
        localStorage.setItem(HIDE_COMPLETED_KEY, String(next))
      } catch {
        // ignore
      }
      return next
    })
  }

  function handleOpenAddTask(milestoneId?: string) {
    setAddTaskMilestoneId(milestoneId)
    setAddTaskOpen(true)
  }

  function handleToggleSelect(taskId: string) {
    setSelectedIds((prev) => {
      const next = new Set(prev)
      if (next.has(taskId)) next.delete(taskId)
      else next.add(taskId)
      return next
    })
  }

  function handleDeleteTask(taskId: string) {
    if (!confirm('Delete this task? This cannot be undone.')) return
    deleteTask(taskId, {
      onSuccess: () => {
        toast({ title: 'Task deleted' })
        setSelectedIds((prev) => { const next = new Set(prev); next.delete(taskId); return next })
        if (selectedTask?.id === taskId) setSelectedTask(null)
      },
      onError: (err) =>
        toast({ title: 'Delete failed', description: (err as Error).message, variant: 'destructive' }),
    })
  }

  function handleBulkComplete() {
    const ids = Array.from(selectedIds)
    if (ids.length === 0) return
    if (!confirm(`Mark ${ids.length} task${ids.length !== 1 ? 's' : ''} as completed?`)) return
    bulkUpdateStatus(
      { taskIds: ids, status: 'completed' },
      {
        onSuccess: () => {
          toast({ title: 'Tasks completed', description: `${ids.length} task${ids.length !== 1 ? 's' : ''} marked as completed.` })
          setSelectedIds(new Set())
        },
        onError: (err) =>
          toast({ title: 'Bulk update failed', description: (err as Error).message, variant: 'destructive' }),
      },
    )
  }

  function handleBulkDelete() {
    const ids = Array.from(selectedIds)
    if (ids.length === 0) return
    if (!confirm(`Delete ${ids.length} task${ids.length !== 1 ? 's' : ''}? This cannot be undone.`)) return
    for (const id of ids) {
      deleteTask(id, {
        onError: (err) =>
          toast({ title: 'Delete failed', description: (err as Error).message, variant: 'destructive' }),
      })
    }
    toast({ title: 'Deleting tasks', description: `Removing ${ids.length} task${ids.length !== 1 ? 's' : ''}...` })
    setSelectedIds(new Set())
    if (selectedTask && ids.includes(selectedTask.id)) setSelectedTask(null)
  }

  function handleCompleteMilestoneTasks(milestoneId: string) {
    const milestone = milestones.find((m) => m.id === milestoneId)
    const mTasks = milestone && Array.isArray(milestone.tasks) ? milestone.tasks : []
    const pending = mTasks.filter((t) => t.status !== 'completed')
    if (pending.length === 0) {
      toast({ title: 'All done', description: 'All tasks in this milestone are already completed.' })
      return
    }
    if (!confirm(`Mark ${pending.length} task${pending.length !== 1 ? 's' : ''} in "${milestone?.name}" as completed?`)) return
    bulkUpdateStatus(
      { taskIds: pending.map((t) => t.id), status: 'completed' },
      {
        onSuccess: () => toast({ title: 'Tasks completed', description: `${pending.length} task${pending.length !== 1 ? 's' : ''} marked as completed.` }),
        onError: (err) => toast({ title: 'Update failed', description: (err as Error).message, variant: 'destructive' }),
      },
    )
  }

  function handleDeleteMilestone(milestoneId: string) {
    const milestone = milestones.find((m) => m.id === milestoneId)
    if (!confirm(`Delete milestone "${milestone?.name ?? milestoneId}" and all its tasks? This cannot be undone.`)) return
    deleteMilestone(milestoneId, {
      onSuccess: () => toast({ title: 'Milestone deleted', description: `"${milestone?.name}" has been removed.` }),
      onError: (err) => toast({ title: 'Delete failed', description: (err as Error).message, variant: 'destructive' }),
    })
  }

  function handlePurgeOrphans() {
    if (!confirm('Purge all orphaned tasks (tasks with no valid milestone or project)?')) return
    purgeOrphans(undefined, {
      onSuccess: () => toast({ title: 'Orphans purged', description: 'Orphaned tasks have been removed.' }),
      onError: (err) => toast({ title: 'Purge failed', description: (err as Error).message, variant: 'destructive' }),
    })
  }

  function handleSelectAll(taskIds: string[]) {
    if (taskIds.length === 0) {
      // Deselect all
      setSelectedIds(new Set())
    } else {
      setSelectedIds((prev) => {
        const next = new Set(prev)
        for (const id of taskIds) next.add(id)
        return next
      })
    }
  }

  function handleProjectSelectChange(value: string) {
    if (value === ALL_PROJECTS_VALUE) {
      setSelectedProjectId(null)
    } else {
      setSelectedProjectId(value)
    }
    setSelectedTask(null)
    setSelectedIds(new Set())
  }

  if (isLoadingProjects) {
    return (
      <div className="p-6 space-y-6">
        <PageHeader title="Tasks" description="Manage projects, milestones, and tasks." />
        <LoadingState message="Loading projects..." />
      </div>
    )
  }

  if (projectsError) {
    return (
      <div className="p-6 space-y-6">
        <PageHeader title="Tasks" description="Manage projects, milestones, and tasks." />
        <ErrorState error={projectsError as Error} onRetry={() => void refetchProjects()} />
      </div>
    )
  }

  const isAllProjects = selectedProjectId === null
  const selectValue = isAllProjects ? ALL_PROJECTS_VALUE : selectedProjectId

  return (
    <div className="p-6 space-y-6">
      {/* Page Header */}
      <PageHeader
        title="Tasks"
        description="Manage projects, milestones, and tasks across the hyperax workspace."
      >
        <Button size="sm" onClick={() => setCreateProjectOpen(true)}>
          <PlusCircle className="h-4 w-4 mr-2" />
          Create Project
        </Button>
      </PageHeader>

      {projectList.length === 0 ? (
        <EmptyState
          icon={ClipboardList}
          title="No projects yet"
          description="Create a project to start managing milestones and tasks."
          action={
            <Button size="sm" onClick={() => setCreateProjectOpen(true)}>
              Create your first project
            </Button>
          }
        />
      ) : (
        <>
          {/* Project Selector + Filters */}
          <div className="flex items-center gap-3 flex-wrap">
            <Label className="text-sm shrink-0">Project</Label>
            <Select value={selectValue} onValueChange={handleProjectSelectChange}>
              <SelectTrigger className="h-9 w-64">
                <SelectValue placeholder="All Projects" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={ALL_PROJECTS_VALUE}>— All Projects —</SelectItem>
                <SelectSeparator />
                {filteredProjectList.map((p) => (
                  <SelectItem key={p.id} value={p.id}>
                    {p.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>

            <button
              type="button"
              onClick={toggleHideCompleted}
              className={cn(
                'flex items-center gap-1.5 rounded-md border px-2.5 py-1.5 text-xs transition-colors',
                hideCompleted
                  ? 'border-primary bg-primary/10 text-primary'
                  : 'border-border bg-background text-muted-foreground hover:text-foreground hover:border-foreground/30',
              )}
              title={hideCompleted ? 'Show completed tasks' : 'Hide completed tasks'}
            >
              <EyeOff className="h-3.5 w-3.5" />
              Hide Completed
            </button>

            <button
              type="button"
              onClick={toggleShowCompletedProjects}
              className={cn(
                'flex items-center gap-1.5 rounded-md border px-2.5 py-1.5 text-xs transition-colors',
                showCompletedProjects
                  ? 'border-primary bg-primary/10 text-primary'
                  : 'border-border bg-background text-muted-foreground hover:text-foreground hover:border-foreground/30',
              )}
              title={showCompletedProjects ? 'Hide fully completed projects' : 'Show fully completed projects'}
            >
              <Archive className="h-3.5 w-3.5" />
              Show Completed Projects
            </button>

            <Button
              size="sm"
              variant="outline"
              className="h-8 text-xs"
              onClick={handlePurgeOrphans}
              disabled={isPurging}
              title="Remove tasks with no valid milestone or project"
            >
              <Recycle className="h-3.5 w-3.5 mr-1" />
              {isPurging ? 'Purging...' : 'Purge Orphans'}
            </Button>

            {!isAllProjects && selectedProject && (
              <div className="flex items-center gap-1 ml-auto">
                <Button
                  size="sm"
                  variant="outline"
                  className="h-8 text-xs"
                  onClick={() => {
                    archiveProject(selectedProjectId!, {
                      onSuccess: () => {
                        toast({ title: 'Project archived', description: `"${selectedProject.name}" has been archived.` })
                        setSelectedProjectId(null)
                        setSelectedTask(null)
                      },
                      onError: (err: unknown) =>
                        toast({ title: 'Archive failed', description: (err as Error).message, variant: 'destructive' }),
                    })
                  }}
                >
                  <Archive className="h-3.5 w-3.5 mr-1" />
                  Archive Project
                </Button>
                <Button
                  size="sm"
                  variant="outline"
                  className="h-8 text-xs text-destructive hover:text-destructive"
                  onClick={() => {
                    if (!confirm(`Delete project "${selectedProject.name}"? This cannot be undone.`)) return
                    deleteProject(selectedProjectId!, {
                      onSuccess: () => {
                        toast({ title: 'Project deleted', description: `"${selectedProject.name}" has been removed.` })
                        setSelectedProjectId(null)
                        setSelectedTask(null)
                      },
                      onError: (err: unknown) =>
                        toast({ title: 'Delete failed', description: (err as Error).message, variant: 'destructive' }),
                    })
                  }}
                >
                  <Trash2 className="h-3.5 w-3.5 mr-1" />
                  Delete Project
                </Button>
              </div>
            )}
          </div>

          {/* Floating Bulk Action Bar */}
          {selectedIds.size > 0 && (
            <div className="sticky top-0 z-10 flex items-center gap-3 rounded-lg border bg-card px-4 py-2.5 shadow-md">
              <span className="text-sm font-medium">
                {selectedIds.size} task{selectedIds.size !== 1 ? 's' : ''} selected
              </span>
              <div className="flex-1" />
              <Button
                size="sm"
                variant="outline"
                className="h-7 text-xs"
                onClick={handleBulkComplete}
                disabled={isBulkUpdating}
              >
                <CheckCircle2 className="h-3.5 w-3.5 mr-1" />
                {isBulkUpdating ? 'Updating...' : 'Complete Selected'}
              </Button>
              <Button
                size="sm"
                variant="outline"
                className="h-7 text-xs text-destructive hover:text-destructive"
                onClick={handleBulkDelete}
              >
                <Trash2 className="h-3.5 w-3.5 mr-1" />
                Delete Selected
              </Button>
              <Button
                size="sm"
                variant="ghost"
                className="h-7 text-xs"
                onClick={() => setSelectedIds(new Set())}
              >
                Clear
              </Button>
            </div>
          )}

          {/* Main Content — All Projects mode */}
          {isAllProjects && (
            <div className={cn('grid gap-6', selectedTask ? 'grid-cols-[1fr_360px]' : 'grid-cols-1')}>
              <div className="min-w-0">
                <Tabs defaultValue="board" className="space-y-4">
                  <TabsList>
                    <TabsTrigger value="board">Board</TabsTrigger>
                    <TabsTrigger value="list">List</TabsTrigger>
                  </TabsList>

                  <TabsContent value="board" className="mt-0">
                    <AllProjectsBoardView
                      tasks={allProjectsTasks}
                      projects={projectList}
                      onSelectTask={setSelectedTask}
                      hideCompleted={hideCompleted}
                      selectedIds={selectedIds}
                      onToggleSelect={handleToggleSelect}
                      onSelectAll={handleSelectAll}
                      onDeleteTask={handleDeleteTask}
                    />
                  </TabsContent>

                  <TabsContent value="list" className="mt-0">
                    {allProjectsTasks.length === 0 ? (
                      <EmptyState
                        icon={ClipboardList}
                        title="No tasks"
                        description="Add milestones and tasks to your projects."
                      />
                    ) : (
                      <ListView
                        tasks={allProjectsTasks}
                        project={null}
                        onSelectTask={setSelectedTask}
                        hideCompleted={hideCompleted}
                        selectedIds={selectedIds}
                        onToggleSelect={handleToggleSelect}
                        onSelectAll={handleSelectAll}
                        onDeleteTask={handleDeleteTask}
                      />
                    )}
                  </TabsContent>
                </Tabs>
              </div>
              {selectedTask && (
                <TaskDetailPanel
                  task={selectedTask}
                  onClose={() => setSelectedTask(null)}
                />
              )}
            </div>
          )}

          {/* Main Content — Single project mode */}
          {!isAllProjects && selectedProjectId && (
            <>
              {isLoadingDetails ? (
                <LoadingState message="Loading project details..." />
              ) : (
                <div className={cn('grid gap-6', selectedTask ? 'grid-cols-[1fr_360px]' : 'grid-cols-1')}>
                  {/* Left: Tabs */}
                  <div className="min-w-0">
                    <Tabs defaultValue="board" className="space-y-4">
                      <TabsList>
                        <TabsTrigger value="board">Board</TabsTrigger>
                        <TabsTrigger value="list">List</TabsTrigger>
                        <TabsTrigger value="project">Project</TabsTrigger>
                      </TabsList>

                      <TabsContent value="board" className="mt-0">
                        {allTasks.length === 0 ? (
                          <EmptyState
                            icon={ClipboardList}
                            title="No tasks"
                            description="Add milestones and tasks to this project."
                            action={
                              <Button
                                size="sm"
                                variant="outline"
                                onClick={() => handleOpenAddTask()}
                                disabled={milestones.length === 0}
                              >
                                Add Task
                              </Button>
                            }
                          />
                        ) : (
                          <BoardView
                            tasks={allTasks}
                            project={selectedProject}
                            onSelectTask={setSelectedTask}
                            hideCompleted={hideCompleted}
                            selectedIds={selectedIds}
                            onToggleSelect={handleToggleSelect}
                            onSelectAll={handleSelectAll}
                            onDeleteTask={handleDeleteTask}
                            onCompleteMilestoneTasks={handleCompleteMilestoneTasks}
                            onDeleteMilestone={handleDeleteMilestone}
                          />
                        )}
                      </TabsContent>

                      <TabsContent value="list" className="mt-0">
                        {allTasks.length === 0 ? (
                          <EmptyState
                            icon={ClipboardList}
                            title="No tasks"
                            description="Add milestones and tasks to this project."
                          />
                        ) : (
                          <ListView
                            tasks={allTasks}
                            project={selectedProject}
                            onSelectTask={setSelectedTask}
                            hideCompleted={hideCompleted}
                            selectedIds={selectedIds}
                            onToggleSelect={handleToggleSelect}
                            onSelectAll={handleSelectAll}
                            onDeleteTask={handleDeleteTask}
                            onCompleteMilestoneTasks={handleCompleteMilestoneTasks}
                            onDeleteMilestone={handleDeleteMilestone}
                          />
                        )}
                      </TabsContent>

                      <TabsContent value="project" className="mt-0">
                        {selectedProject ? (
                          <ProjectDetail
                            project={selectedProject}
                            onAddMilestone={() => setAddMilestoneOpen(true)}
                            onAddTask={handleOpenAddTask}
                          />
                        ) : (
                          <LoadingState message="Loading..." />
                        )}
                      </TabsContent>
                    </Tabs>
                  </div>

                  {/* Right: Task Detail Panel */}
                  {selectedTask && (
                    <TaskDetailPanel
                      task={selectedTask}
                      onClose={() => setSelectedTask(null)}
                    />
                  )}
                </div>
              )}
            </>
          )}
        </>
      )}

      {/* Dialogs */}
      <CreateProjectDialog
        open={createProjectOpen}
        onOpenChange={setCreateProjectOpen}
      />

      {selectedProjectId && (
        <AddMilestoneDialog
          open={addMilestoneOpen}
          onOpenChange={setAddMilestoneOpen}
          projectId={selectedProjectId}
        />
      )}

      {milestones.length > 0 && (
        <AddTaskDialog
          open={addTaskOpen}
          onOpenChange={setAddTaskOpen}
          milestones={milestones}
          defaultMilestoneId={addTaskMilestoneId}
        />
      )}
    </div>
  )
}
