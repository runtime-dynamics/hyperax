import { useState } from 'react'
import { FolderOpen, PlusCircle, Trash2 } from 'lucide-react'
import { PageHeader } from '@/components/domain/page-header'
import { LoadingState } from '@/components/domain/loading-state'
import { ErrorState } from '@/components/domain/error-state'
import { EmptyState } from '@/components/domain/empty-state'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  useRestWorkspaces,
  useRestCreateWorkspace,
  useRestDeleteWorkspace,
} from '@/services/restWorkspaceService'
import { toast } from '@/components/ui/use-toast'

function RegisterWorkspaceDialog({
  open,
  onOpenChange,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const [name, setName] = useState('')
  const [rootPath, setRootPath] = useState('')
  const [errors, setErrors] = useState<{ name?: string; root_path?: string }>({})

  const { mutate, isPending } = useRestCreateWorkspace()

  function validate(): boolean {
    const next: { name?: string; root_path?: string } = {}
    if (!name.trim()) next.name = 'Name is required'
    if (!rootPath.trim()) next.root_path = 'Root path is required'
    else if (!rootPath.startsWith('/')) next.root_path = 'Path must start with /'
    setErrors(next)
    return Object.keys(next).length === 0
  }

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (!validate()) return
    mutate(
      { name: name.trim(), root_path: rootPath.trim() },
      {
        onSuccess: () => {
          toast({ title: 'Workspace registered', description: `"${name.trim()}" has been added.` })
          setName('')
          setRootPath('')
          setErrors({})
          onOpenChange(false)
        },
        onError: (err) => {
          toast({
            title: 'Registration failed',
            description: (err as Error).message,
            variant: 'destructive',
          })
        },
      },
    )
  }

  function handleOpenChange(open: boolean) {
    if (!open) {
      setName('')
      setRootPath('')
      setErrors({})
    }
    onOpenChange(open)
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Register Workspace</DialogTitle>
          <DialogDescription>Add a new workspace directory to Hyperax.</DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="ws-name">Name</Label>
            <Input
              id="ws-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="my-project"
              autoFocus
            />
            {errors.name && <p className="text-xs text-destructive">{errors.name}</p>}
          </div>
          <div className="space-y-2">
            <Label htmlFor="ws-path">Root Path</Label>
            <Input
              id="ws-path"
              value={rootPath}
              onChange={(e) => setRootPath(e.target.value)}
              placeholder="/Users/you/projects/my-project"
            />
            {errors.root_path && <p className="text-xs text-destructive">{errors.root_path}</p>}
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => handleOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={isPending}>
              {isPending ? 'Registering...' : 'Register'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

export default function WorkspacesPage() {
  const [dialogOpen, setDialogOpen] = useState(false)
  const { data: workspaces, isLoading, error, refetch } = useRestWorkspaces()
  const { mutate: deleteWorkspace, isPending: isDeleting } = useRestDeleteWorkspace()

  function handleDelete(name: string) {
    if (!confirm(`Delete workspace "${name}"? This cannot be undone.`)) return
    deleteWorkspace(name, {
      onSuccess: () =>
        toast({ title: 'Workspace removed', description: `"${name}" has been unregistered.` }),
      onError: (err) =>
        toast({
          title: 'Delete failed',
          description: (err as Error).message,
          variant: 'destructive',
        }),
    })
  }

  return (
    <div className="p-6 space-y-6">
      <PageHeader
        title="Workspaces"
        description="Manage registered workspace directories."
      >
        <Button onClick={() => setDialogOpen(true)} size="sm">
          <PlusCircle className="h-4 w-4 mr-2" />
          Register Workspace
        </Button>
      </PageHeader>

      {isLoading ? (
        <LoadingState message="Loading workspaces..." />
      ) : error ? (
        <ErrorState error={error as Error} onRetry={() => void refetch()} />
      ) : !workspaces || workspaces.length === 0 ? (
        <EmptyState
          icon={FolderOpen}
          title="No workspaces registered"
          description="Register a workspace directory to start working with Hyperax."
          action={
            <Button onClick={() => setDialogOpen(true)} size="sm">
              Register your first workspace
            </Button>
          }
        />
      ) : (
        <div className="border rounded-lg overflow-hidden">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b bg-muted/50">
                <th className="text-left px-4 py-3 font-medium text-muted-foreground">Name</th>
                <th className="text-left px-4 py-3 font-medium text-muted-foreground">Root Path</th>
                <th className="text-left px-4 py-3 font-medium text-muted-foreground">Created</th>
                <th className="w-12 px-4 py-3" />
              </tr>
            </thead>
            <tbody>
              {workspaces.map((ws, i) => (
                <tr key={ws.id} className={i < workspaces.length - 1 ? 'border-b' : ''}>
                  <td className="px-4 py-3">
                    <Badge variant="secondary" className="font-mono text-xs">
                      {ws.name}
                    </Badge>
                  </td>
                  <td className="px-4 py-3 font-mono text-xs text-muted-foreground">
                    {ws.root_path}
                  </td>
                  <td className="px-4 py-3 text-muted-foreground">
                    {new Date(ws.created_at).toLocaleDateString()}
                  </td>
                  <td className="px-4 py-3">
                    <Button
                      variant="ghost"
                      size="icon"
                      onClick={() => handleDelete(ws.name)}
                      disabled={isDeleting}
                      className="h-8 w-8 text-muted-foreground hover:text-destructive"
                    >
                      <Trash2 className="h-4 w-4" />
                      <span className="sr-only">Delete {ws.name}</span>
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <RegisterWorkspaceDialog open={dialogOpen} onOpenChange={setDialogOpen} />
    </div>
  )
}
