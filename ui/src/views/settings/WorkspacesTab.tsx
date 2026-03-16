import { useState } from 'react'
import { Trash2, FolderOpen, PlusCircle } from 'lucide-react'
import { useWorkspaces, useDeleteWorkspace } from '@/services/workspaceService'
import { LoadingState } from '@/components/domain/loading-state'
import { ErrorState } from '@/components/domain/error-state'
import { EmptyState } from '@/components/domain/empty-state'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { RegisterWorkspaceDialog } from './RegisterWorkspaceDialog'
import { toast } from '@/components/ui/use-toast'

export function WorkspacesTab() {
  const [dialogOpen, setDialogOpen] = useState(false)
  const { data: workspaces, isLoading, error, refetch } = useWorkspaces()
  const { mutate: deleteWorkspace, isPending: isDeleting } = useDeleteWorkspace()

  function handleDelete(name: string) {
    if (!confirm(`Delete workspace "${name}"? This cannot be undone.`)) return
    deleteWorkspace(name, {
      onSuccess: () => toast({ title: 'Workspace removed', description: `"${name}" has been unregistered.` }),
      onError: (err) => toast({ title: 'Delete failed', description: (err as Error).message, variant: 'destructive' }),
    })
  }

  if (isLoading) return <LoadingState message="Loading workspaces..." />
  if (error) return <ErrorState error={error as Error} onRetry={() => void refetch()} />

  return (
    <div className="space-y-4">
      <div className="flex justify-end">
        <Button onClick={() => setDialogOpen(true)} size="sm">
          <PlusCircle className="h-4 w-4 mr-2" />
          Register Workspace
        </Button>
      </div>

      {!workspaces || workspaces.length === 0 ? (
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
                  <td className="px-4 py-3 font-mono text-xs text-muted-foreground">{ws.root_path}</td>
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
