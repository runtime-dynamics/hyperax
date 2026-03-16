import { useState } from 'react'
import { FolderKanban, ChevronDown, ChevronRight } from 'lucide-react'
import {
  useRestProjects,
  useRestMilestones,
  useRestTasks,
  useRestUpdateTaskStatus,
  type ProjectPlan,
  type Milestone,
  type Task,
} from '@/services/restProjectService'
import { PageHeader } from '@/components/domain/page-header'
import { LoadingState } from '@/components/domain/loading-state'
import { ErrorState } from '@/components/domain/error-state'
import { EmptyState } from '@/components/domain/empty-state'
import { Badge } from '@/components/ui/badge'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { toast } from '@/components/ui/use-toast'

const STATUS_OPTIONS = ['pending', 'in_progress', 'completed', 'cancelled'] as const

function statusVariant(
  status: string,
): 'default' | 'secondary' | 'destructive' | 'outline' {
  switch (status.toLowerCase()) {
    case 'completed':
      return 'default'
    case 'in_progress':
      return 'secondary'
    case 'cancelled':
    case 'failed':
      return 'destructive'
    default:
      return 'outline'
  }
}

function priorityVariant(
  priority: string,
): 'default' | 'secondary' | 'destructive' | 'outline' {
  switch (priority.toLowerCase()) {
    case 'critical':
    case 'high':
      return 'destructive'
    case 'medium':
      return 'secondary'
    default:
      return 'outline'
  }
}

function formatStatus(status: string): string {
  return status.replace(/_/g, ' ')
}

function formatDate(iso: string | null | undefined): string {
  if (!iso) return '—'
  return new Date(iso).toLocaleDateString()
}

interface TaskRowProps {
  task: Task
}

function TaskRow({ task }: TaskRowProps) {
  const { mutate: updateStatus, isPending } = useRestUpdateTaskStatus()

  function handleStatusChange(newStatus: string) {
    updateStatus(
      { taskId: task.ID, status: newStatus },
      {
        onSuccess: () =>
          toast({
            title: 'Task updated',
            description: `"${task.Name}" status set to ${formatStatus(newStatus)}.`,
          }),
        onError: (err) =>
          toast({
            title: 'Update failed',
            description: (err as Error).message,
            variant: 'destructive',
          }),
      },
    )
  }

  return (
    <div className="flex items-center gap-3 px-3 py-2 text-sm border-b last:border-0">
      <div className="flex-1 min-w-0">
        <p className="truncate font-medium text-sm">{task.Name}</p>
        {task.AssigneePersonaID && (
          <p className="text-xs text-muted-foreground truncate">
            Assignee: {task.AssigneePersonaID}
          </p>
        )}
      </div>

      <Badge
        variant={priorityVariant(task.Priority)}
        className="capitalize text-xs shrink-0"
      >
        {task.Priority}
      </Badge>

      <Select
        value={task.Status}
        onValueChange={handleStatusChange}
        disabled={isPending}
      >
        <SelectTrigger className="h-7 w-32 text-xs border-input shrink-0">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {STATUS_OPTIONS.map((s) => (
            <SelectItem key={s} value={s} className="text-xs capitalize">
              {formatStatus(s)}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  )
}

interface MilestoneRowProps {
  milestone: Milestone
  projectId: string
}

function MilestoneRow({ milestone, projectId }: MilestoneRowProps) {
  const [expanded, setExpanded] = useState(false)
  const {
    data: tasks,
    isLoading,
    error,
    refetch,
  } = useRestTasks(expanded ? projectId : '', expanded ? milestone.ID : '')

  return (
    <div className="border rounded-md overflow-hidden">
      <button
        type="button"
        className="w-full flex items-center gap-3 px-3 py-2.5 hover:bg-muted/40 transition-colors text-left"
        onClick={() => setExpanded((p) => !p)}
      >
        {expanded ? (
          <ChevronDown className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        ) : (
          <ChevronRight className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        )}
        <span className="flex-1 text-sm font-medium truncate">{milestone.Name}</span>
        <Badge
          variant={statusVariant(milestone.Status)}
          className="capitalize text-xs shrink-0"
        >
          {formatStatus(milestone.Status)}
        </Badge>
        <Badge
          variant={priorityVariant(milestone.Priority)}
          className="capitalize text-xs shrink-0"
        >
          {milestone.Priority}
        </Badge>
        {milestone.DueDate && (
          <span className="text-xs text-muted-foreground shrink-0">
            Due {formatDate(milestone.DueDate)}
          </span>
        )}
      </button>

      {expanded && (
        <div className="border-t bg-muted/10">
          {isLoading && <LoadingState message="Loading tasks..." className="py-4" />}
          {error && (
            <ErrorState
              error={error as Error}
              onRetry={() => void refetch()}
              className="py-4"
            />
          )}
          {!isLoading && !error && tasks && tasks.length === 0 && (
            <p className="text-xs text-muted-foreground italic px-4 py-3">
              No tasks in this milestone.
            </p>
          )}
          {tasks && tasks.length > 0 && (
            <div>
              {tasks.map((task) => (
                <TaskRow key={task.ID} task={task} />
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  )
}

interface ProjectRowProps {
  project: ProjectPlan
}

function ProjectRow({ project }: ProjectRowProps) {
  const [expanded, setExpanded] = useState(false)
  const {
    data: milestones,
    isLoading,
    error,
    refetch,
  } = useRestMilestones(expanded ? project.ID : '')

  return (
    <div className="border rounded-lg overflow-hidden">
      <button
        type="button"
        className="w-full flex items-center gap-3 px-4 py-3 hover:bg-muted/40 transition-colors text-left"
        onClick={() => setExpanded((p) => !p)}
      >
        {expanded ? (
          <ChevronDown className="h-4 w-4 shrink-0 text-muted-foreground" />
        ) : (
          <ChevronRight className="h-4 w-4 shrink-0 text-muted-foreground" />
        )}
        <div className="flex-1 min-w-0">
          <p className="text-sm font-medium truncate">{project.Name}</p>
          {project.Description && (
            <p className="text-xs text-muted-foreground truncate">{project.Description}</p>
          )}
        </div>
        <Badge
          variant={statusVariant(project.Status)}
          className="capitalize text-xs shrink-0"
        >
          {formatStatus(project.Status)}
        </Badge>
        <Badge
          variant={priorityVariant(project.Priority)}
          className="capitalize text-xs shrink-0"
        >
          {project.Priority}
        </Badge>
        {project.WorkspaceName && (
          <Badge variant="outline" className="text-xs shrink-0">
            {project.WorkspaceName}
          </Badge>
        )}
        <span className="text-xs text-muted-foreground shrink-0">
          {formatDate(project.UpdatedAt)}
        </span>
      </button>

      {expanded && (
        <div className="border-t bg-muted/10 px-4 py-3 space-y-2">
          {isLoading && <LoadingState message="Loading milestones..." className="py-4" />}
          {error && (
            <ErrorState
              error={error as Error}
              onRetry={() => void refetch()}
              className="py-4"
            />
          )}
          {!isLoading && !error && milestones && milestones.length === 0 && (
            <p className="text-xs text-muted-foreground italic">No milestones defined.</p>
          )}
          {milestones && milestones.length > 0 && (
            <div className="space-y-2">
              <p className="text-xs font-medium text-muted-foreground uppercase tracking-wide">
                Milestones ({milestones.length})
              </p>
              {milestones.map((milestone) => (
                <MilestoneRow
                  key={milestone.ID}
                  milestone={milestone}
                  projectId={project.ID}
                />
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  )
}

export function ProjectsPage() {
  const { data: projects, isLoading, error, refetch } = useRestProjects()

  if (isLoading)
    return (
      <div className="p-6 space-y-6">
        <PageHeader
          title="Projects"
          description="Manage project plans, milestones, and tasks."
        />
        <LoadingState message="Loading projects..." />
      </div>
    )

  if (error)
    return (
      <div className="p-6 space-y-6">
        <PageHeader
          title="Projects"
          description="Manage project plans, milestones, and tasks."
        />
        <ErrorState error={error as Error} onRetry={() => void refetch()} />
      </div>
    )

  const items = projects ?? []

  return (
    <div className="p-6 space-y-6">
      <PageHeader
        title="Projects"
        description="Manage project plans, milestones, and tasks."
      />

      {items.length === 0 ? (
        <EmptyState
          icon={FolderKanban}
          title="No projects found"
          description="No project plans have been created yet."
        />
      ) : (
        <div className="space-y-2">
          {items.map((project) => (
            <ProjectRow key={project.ID} project={project} />
          ))}
        </div>
      )}
    </div>
  )
}
