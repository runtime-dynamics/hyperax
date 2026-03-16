import { useState } from 'react'
import { GitBranch, ChevronDown, ChevronRight } from 'lucide-react'
import {
  usePipelines,
  usePipelineJobs,
  useStepResults,
  type Pipeline,
  type PipelineJob,
  type StepResult,
} from '@/services/pipelineService'
import { PageHeader } from '@/components/domain/page-header'
import { LoadingState } from '@/components/domain/loading-state'
import { ErrorState } from '@/components/domain/error-state'
import { EmptyState } from '@/components/domain/empty-state'
import { useWorkspaces } from '@/services/workspaceService'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import { WorkflowsPage } from '@/views/workflows/WorkflowsPage'

function statusVariant(status: string): 'default' | 'secondary' | 'destructive' | 'outline' {
  switch (status.toLowerCase()) {
    case 'completed':
    case 'success':
      return 'default'
    case 'running':
      return 'secondary'
    case 'failed':
    case 'error':
      return 'destructive'
    default:
      return 'outline'
  }
}

function formatDate(iso: string | null): string {
  if (!iso) return '—'
  return new Date(iso).toLocaleString()
}

function formatDuration(ms: number | null): string {
  if (ms === null || ms === undefined) return '—'
  if (ms < 1000) return `${ms}ms`
  return `${(ms / 1000).toFixed(1)}s`
}

interface StepRowProps {
  step: StepResult
}

function StepRow({ step }: StepRowProps) {
  const [expanded, setExpanded] = useState(true)

  return (
    <div className="border rounded-md overflow-hidden">
      <button
        type="button"
        className="w-full flex items-center gap-3 px-3 py-2 text-sm hover:bg-muted/40 transition-colors text-left"
        onClick={() => setExpanded((p) => !p)}
      >
        {expanded ? (
          <ChevronDown className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        ) : (
          <ChevronRight className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        )}
        <span className="flex-1 font-medium">{step.step_name}</span>
        <Badge variant={statusVariant(step.status)} className="capitalize text-xs">
          {step.status}
        </Badge>
        {step.exit_code !== null && (
          <span className="text-muted-foreground text-xs">exit {step.exit_code}</span>
        )}
        <span className="text-muted-foreground text-xs">{formatDuration(step.duration_ms)}</span>
      </button>

      {expanded && (
        <div className="border-t bg-muted/20 px-3 py-2">
          {step.output_log ? (
            <pre className="text-xs font-mono whitespace-pre-wrap break-all text-foreground bg-background border rounded p-3 overflow-auto max-h-64">
              {step.output_log}
            </pre>
          ) : (
            <p className="text-xs text-muted-foreground italic">No output.</p>
          )}
          {step.error && (
            <p className="mt-2 text-xs text-destructive font-mono">{step.error}</p>
          )}
        </div>
      )}
    </div>
  )
}

interface JobRowProps {
  pipelineId: string
  job: PipelineJob
}

function JobRow({ pipelineId, job }: JobRowProps) {
  const [expanded, setExpanded] = useState(false)
  const { data: steps, isLoading, error, refetch } = useStepResults(
    expanded ? pipelineId : '',
    expanded ? job.id : '',
  )

  return (
    <div className="border rounded-md overflow-hidden">
      <button
        type="button"
        className="w-full flex items-center gap-3 px-3 py-2.5 text-sm hover:bg-muted/40 transition-colors text-left"
        onClick={() => setExpanded((p) => !p)}
      >
        {expanded ? (
          <ChevronDown className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        ) : (
          <ChevronRight className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        )}
        <Badge variant={statusVariant(job.status)} className="capitalize text-xs">
          {job.status}
        </Badge>
        <span className="flex-1 text-muted-foreground text-xs font-mono">{job.id.slice(0, 8)}</span>
        {job.workspace_name && (
          <Badge variant="outline" className="text-xs">{job.workspace_name}</Badge>
        )}
        <span className="text-muted-foreground text-xs">{formatDate(job.started_at)}</span>
        {job.completed_at && (
          <span className="text-muted-foreground text-xs">→ {formatDate(job.completed_at)}</span>
        )}
      </button>

      {expanded && (
        <div className="border-t bg-muted/10 px-3 py-3 space-y-2">
          {isLoading && <LoadingState message="Loading steps..." className="py-4" />}
          {error && (
            <ErrorState error={error as Error} onRetry={() => void refetch()} className="py-4" />
          )}
          {!isLoading && !error && steps && steps.length === 0 && (
            <p className="text-xs text-muted-foreground italic">No step results recorded.</p>
          )}
          {steps && steps.length > 0 && (
            <div className="space-y-2">
              {steps.map((step) => (
                <StepRow key={step.id} step={step} />
              ))}
            </div>
          )}
          {job.error && (
            <p className="text-xs text-destructive font-mono mt-2">{job.error}</p>
          )}
        </div>
      )}
    </div>
  )
}

interface PipelineRowProps {
  pipeline: Pipeline
}

function PipelineRow({ pipeline }: PipelineRowProps) {
  const [expanded, setExpanded] = useState(false)
  const { data: jobs, isLoading, error, refetch } = usePipelineJobs(
    expanded ? pipeline.id : '',
  )

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
          <p className="text-sm font-medium truncate">{pipeline.name}</p>
          {pipeline.description && (
            <p className="text-xs text-muted-foreground truncate">{pipeline.description}</p>
          )}
        </div>
        {pipeline.workspace_name && (
          <Badge variant="outline" className="text-xs shrink-0">{pipeline.workspace_name}</Badge>
        )}
        <span className="text-xs text-muted-foreground shrink-0">
          {formatDate(pipeline.updated_at)}
        </span>
      </button>

      {expanded && (
        <div className="border-t bg-muted/10 px-4 py-3 space-y-2">
          {isLoading && <LoadingState message="Loading jobs..." className="py-4" />}
          {error && (
            <ErrorState error={error as Error} onRetry={() => void refetch()} className="py-4" />
          )}
          {!isLoading && !error && jobs && jobs.length === 0 && (
            <p className="text-xs text-muted-foreground italic">No jobs have run for this pipeline.</p>
          )}
          {jobs && jobs.length > 0 && (
            <div className="space-y-2">
              <p className="text-xs font-medium text-muted-foreground uppercase tracking-wide">
                Recent Jobs ({jobs.length})
              </p>
              {jobs.map((job) => (
                <JobRow key={job.id} pipelineId={pipeline.id} job={job} />
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  )
}

function PipelinesContent() {
  const [workspaceFilter, setWorkspaceFilter] = useState('')
  const [debouncedFilter, setDebouncedFilter] = useState('')

  const { data: workspaces } = useWorkspaces()
  const { data: pipelines, isLoading, error, refetch } = usePipelines(debouncedFilter)

  function handleFilterChange(value: string) {
    const filter = value === '__all__' ? '' : value
    setWorkspaceFilter(value)
    setDebouncedFilter(filter)
  }

  if (isLoading) return (
    <div className="p-6 space-y-6">
      <PageHeader
        title="Pipelines"
        description="Monitor pipeline execution and build history."
      />
      <LoadingState message="Loading pipelines..." />
    </div>
  )

  if (error) return (
    <div className="p-6 space-y-6">
      <PageHeader
        title="Pipelines"
        description="Monitor pipeline execution and build history."
      />
      <ErrorState error={error as Error} onRetry={() => void refetch()} />
    </div>
  )

  const filtered = pipelines ?? []

  return (
    <div className="p-6 space-y-6">
      <PageHeader
        title="Pipelines"
        description="Monitor pipeline execution and build history."
      >
        <div className="flex items-center gap-2">
          <Select value={workspaceFilter} onValueChange={handleFilterChange}>
            <SelectTrigger className="h-8 w-48 text-sm">
              <SelectValue placeholder="All workspaces" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="__all__">All workspaces</SelectItem>
              {(workspaces ?? []).map((ws) => (
                <SelectItem key={ws.id} value={ws.name}>{ws.name}</SelectItem>
              ))}
            </SelectContent>
          </Select>
          {workspaceFilter && workspaceFilter !== '__all__' && (
            <Button
              variant="ghost"
              size="sm"
              className="h-8 px-2 text-muted-foreground"
              onClick={() => handleFilterChange('__all__')}
            >
              Clear
            </Button>
          )}
        </div>
      </PageHeader>

      {filtered.length === 0 ? (
        <EmptyState
          icon={GitBranch}
          title="No pipelines found"
          description={
            debouncedFilter
              ? `No pipelines match workspace "${debouncedFilter}".`
              : 'No pipelines have been configured yet.'
          }
        />
      ) : (
        <div className="space-y-2">
          {filtered.map((pipeline) => (
            <PipelineRow key={pipeline.id} pipeline={pipeline} />
          ))}
        </div>
      )}
    </div>
  )
}

export function PipelinesPage() {
  return (
    <Tabs defaultValue="pipelines" className="flex flex-col h-full">
      <div className="px-6 pt-4 border-b">
        <TabsList>
          <TabsTrigger value="pipelines">Pipelines</TabsTrigger>
          <TabsTrigger value="workflows">Workflows</TabsTrigger>
        </TabsList>
      </div>
      <TabsContent value="pipelines" className="flex-1 mt-0">
        <PipelinesContent />
      </TabsContent>
      <TabsContent value="workflows" className="flex-1 mt-0">
        <WorkflowsPage />
      </TabsContent>
    </Tabs>
  )
}
