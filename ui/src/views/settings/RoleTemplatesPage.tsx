/**
 * Role Templates — Settings page for viewing and overriding agent role templates.
 *
 * Templates are fetched from the backend via list_role_templates MCP tool.
 * Built-in templates can have their system prompts overridden by the user.
 */

import { useState } from 'react'
import { Copy, Check, BookOpen, Shield, Pencil } from 'lucide-react'
import {
  useRoleTemplates,
  useOverrideRoleTemplate,
  useRemoveRoleTemplateOverride,
  type RoleTemplate,
} from '@/services/roleTemplateService'
import { PageHeader } from '@/components/domain/page-header'
import { LoadingState } from '@/components/domain/loading-state'
import { ErrorState } from '@/components/domain/error-state'
import { EmptyState } from '@/components/domain/empty-state'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '@/components/ui/dialog'
import { Textarea } from '@/components/ui/textarea'
import { toast } from '@/components/ui/use-toast'
import { cn } from '@/lib/utils'

// ─── Helpers ────────────────────────────────────────────────────────────────

const CLEARANCE_LABELS: Record<number, string> = {
  0: 'Observer',
  1: 'Operator',
  2: 'Admin',
  3: 'Chief of Staff',
}

function clearanceBadgeVariant(level: number): 'default' | 'secondary' | 'destructive' | 'outline' {
  if (level >= 3) return 'destructive'
  if (level >= 2) return 'default'
  return 'secondary'
}

// ─── TemplateCard ─────────────────────────────────────────────────────────────

interface TemplateCardProps {
  template: RoleTemplate
  onEdit: (template: RoleTemplate) => void
}

function TemplateCard({ template, onEdit }: TemplateCardProps) {
  const [copied, setCopied] = useState(false)

  function handleCopyId() {
    void navigator.clipboard.writeText(template.id).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    })
  }

  return (
    <Card className={cn('flex flex-col', template.built_in && 'border-dashed')}>
      <CardHeader className="pb-2">
        <div className="flex items-start justify-between gap-2">
          <div className="min-w-0">
            <CardTitle className="text-sm flex items-center gap-2">
              {template.name}
              {template.built_in && (
                <span className="text-[10px] font-normal text-muted-foreground bg-muted rounded-full px-1.5 py-0.5">
                  Built-in
                </span>
              )}
              {template.has_override && (
                <span className="text-[10px] font-normal text-amber-600 bg-amber-100 dark:text-amber-400 dark:bg-amber-900/30 rounded-full px-1.5 py-0.5">
                  Overridden
                </span>
              )}
            </CardTitle>
            <CardDescription className="text-xs mt-0.5 font-mono">
              {template.id}
            </CardDescription>
          </div>

          <div className="flex items-center gap-1 shrink-0">
            {template.built_in && (
              <Button
                size="sm"
                variant="ghost"
                className="h-7 w-7 p-0"
                onClick={() => onEdit(template)}
                title="Edit system prompt"
                aria-label="Edit system prompt"
              >
                <Pencil className="h-3.5 w-3.5" />
              </Button>
            )}
            <Button
              size="sm"
              variant="ghost"
              className="h-7 w-7 p-0"
              onClick={handleCopyId}
              title="Copy template ID"
              aria-label="Copy template ID"
            >
              {copied ? <Check className="h-3.5 w-3.5 text-green-500" /> : <Copy className="h-3.5 w-3.5" />}
            </Button>
          </div>
        </div>
      </CardHeader>

      <CardContent className="pt-0 pb-3 flex-1 flex flex-col gap-2">
        {template.description && (
          <p className="text-xs text-muted-foreground">{template.description}</p>
        )}
        <div className="flex items-center gap-1.5 flex-wrap">
          <Badge variant={clearanceBadgeVariant(template.clearance_level)} className="text-xs">
            <Shield className="h-3 w-3 mr-1" />
            {CLEARANCE_LABELS[template.clearance_level] ?? `Level ${template.clearance_level}`}
          </Badge>
          {template.suggested_model && (
            <Badge variant="outline" className="text-xs font-mono">
              {template.suggested_model}
            </Badge>
          )}
        </div>
      </CardContent>
    </Card>
  )
}

// ─── EditOverrideDialog ─────────────────────────────────────────────────────

interface EditOverrideDialogProps {
  template: RoleTemplate | null
  open: boolean
  onOpenChange: (open: boolean) => void
}

function EditOverrideDialog({ template, open, onOpenChange }: EditOverrideDialogProps) {
  const [prompt, setPrompt] = useState('')
  const overrideMutation = useOverrideRoleTemplate()
  const removeMutation = useRemoveRoleTemplateOverride()

  // Sync prompt text when template changes
  const [lastId, setLastId] = useState<string | null>(null)
  if (template && template.id !== lastId) {
    setLastId(template.id)
    setPrompt(template.system_prompt ?? '')
  }

  function handleSave() {
    if (!template) return
    overrideMutation.mutate(
      { template_id: template.id, system_prompt: prompt },
      {
        onSuccess: () => {
          toast({ title: 'Override saved', description: `System prompt for "${template.name}" has been overridden.` })
          onOpenChange(false)
        },
        onError: (err) => {
          toast({ title: 'Error', description: err instanceof Error ? err.message : 'Failed to save override.', variant: 'destructive' })
        },
      },
    )
  }

  function handleRemove() {
    if (!template) return
    removeMutation.mutate(
      { template_id: template.id },
      {
        onSuccess: () => {
          toast({ title: 'Override removed', description: `"${template.name}" reverted to built-in prompt.` })
          onOpenChange(false)
        },
        onError: (err) => {
          toast({ title: 'Error', description: err instanceof Error ? err.message : 'Failed to remove override.', variant: 'destructive' })
        },
      },
    )
  }

  const isSaving = overrideMutation.isPending || removeMutation.isPending

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl max-h-[85vh] flex flex-col">
        <DialogHeader>
          <DialogTitle className="text-base">Edit System Prompt</DialogTitle>
          <DialogDescription className="text-xs">
            {template?.name} — Override the built-in system prompt for this role template.
          </DialogDescription>
        </DialogHeader>
        <div className="flex-1 min-h-0 py-2">
          <Textarea
            value={prompt}
            onChange={(e) => setPrompt(e.target.value)}
            className="h-[40vh] font-mono text-xs resize-none"
            placeholder="Enter system prompt..."
          />
        </div>
        <DialogFooter className="flex-row justify-between sm:justify-between gap-2">
          <div>
            {template?.has_override && (
              <Button
                variant="outline"
                size="sm"
                onClick={handleRemove}
                disabled={isSaving}
              >
                Remove Override
              </Button>
            )}
          </div>
          <div className="flex gap-2">
            <Button variant="ghost" size="sm" onClick={() => onOpenChange(false)} disabled={isSaving}>
              Cancel
            </Button>
            <Button size="sm" onClick={handleSave} disabled={isSaving || !prompt.trim()}>
              {overrideMutation.isPending ? 'Saving...' : 'Save Override'}
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// ─── RoleTemplatesPage ────────────────────────────────────────────────────────

export function RoleTemplatesPage() {
  const { data: templates, isLoading, error, refetch } = useRoleTemplates()
  const [editingTemplate, setEditingTemplate] = useState<RoleTemplate | null>(null)

  if (isLoading) return <LoadingState message="Loading role templates..." />
  if (error) return <ErrorState error={error as Error} onRetry={() => void refetch()} />

  const templateList = Array.isArray(templates) ? templates : []

  return (
    <div className="p-6 space-y-6">
      <PageHeader
        title="Role Templates"
        description="Pre-configured agent role definitions. Templates provide system prompts, clearance levels, and model suggestions."
      />

      {templateList.length === 0 ? (
        <EmptyState
          icon={BookOpen}
          title="No role templates"
          description="Role templates are defined in the backend and loaded automatically."
        />
      ) : (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
          {templateList.map((template) => (
            <TemplateCard key={template.id} template={template} onEdit={setEditingTemplate} />
          ))}
        </div>
      )}

      <EditOverrideDialog
        template={editingTemplate}
        open={editingTemplate !== null}
        onOpenChange={(open) => { if (!open) setEditingTemplate(null) }}
      />
    </div>
  )
}
