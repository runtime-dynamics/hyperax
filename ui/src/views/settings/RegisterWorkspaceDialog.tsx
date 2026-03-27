import { useState, useEffect } from 'react'
import { z } from 'zod'
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
import { useRegisterWorkspace } from '@/services/workspaceService'
import { browseDirectories, type BrowseEntry } from '@/services/restWorkspaceService'
import { toast } from '@/components/ui/use-toast'
import { Folder, FolderGit, ChevronUp, ArrowRight } from 'lucide-react'
import { cn } from '@/lib/utils'

const schema = z.object({
  root_path: z.string().min(1, 'Root path is required').startsWith('/', 'Path must start with /'),
})

interface RegisterWorkspaceDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function RegisterWorkspaceDialog({ open, onOpenChange }: RegisterWorkspaceDialogProps) {
  const [name, setName] = useState('')
  const [rootPath, setRootPath] = useState('')
  const [errors, setErrors] = useState<{ root_path?: string }>({})
  const [browsing, setBrowsing] = useState(false)
  const [browseDir, setBrowseDir] = useState('')
  const [browseEntries, setBrowseEntries] = useState<BrowseEntry[]>([])
  const [browseParent, setBrowseParent] = useState('')
  const [browseLoading, setBrowseLoading] = useState(false)

  const { mutate, isPending } = useRegisterWorkspace()

  async function loadDir(path?: string) {
    setBrowseLoading(true)
    try {
      const result = await browseDirectories(path)
      setBrowseDir(result.current_path)
      setBrowseParent(result.parent)
      setBrowseEntries(result.entries.filter((e) => e.is_dir))
    } catch {
      toast({ title: 'Browse failed', description: 'Could not read directory', variant: 'destructive' })
    } finally {
      setBrowseLoading(false)
    }
  }

  useEffect(() => {
    if (browsing && !browseDir) {
      loadDir()
    }
  }, [browsing])

  function selectPath(path: string) {
    setRootPath(path)
    const base = path.split('/').pop() || ''
    if (!name) setName(base)
    setBrowsing(false)
  }

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    const result = schema.safeParse({ root_path: rootPath })
    if (!result.success) {
      const fieldErrors: { root_path?: string } = {}
      for (const issue of result.error.issues) {
        if (issue.path[0] === 'root_path') fieldErrors.root_path = issue.message
      }
      setErrors(fieldErrors)
      return
    }
    setErrors({})
    const args: { root_path: string; name?: string } = { root_path: rootPath }
    if (name) args.name = name
    mutate(args, {
      onSuccess: () => {
        const label = name || rootPath.split('/').pop() || rootPath
        toast({ title: 'Workspace registered', description: `"${label}" has been added.` })
        setName('')
        setRootPath('')
        setBrowsing(false)
        setBrowseDir('')
        onOpenChange(false)
      },
      onError: (err) => {
        toast({ title: 'Registration failed', description: (err as Error).message, variant: 'destructive' })
      },
    })
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>Register Workspace</DialogTitle>
          <DialogDescription>Add a new workspace directory to Hyperax.</DialogDescription>
        </DialogHeader>
        {browsing ? (
          <div className="space-y-2">
            <div className="flex items-center gap-2 text-sm text-muted-foreground font-mono truncate">
              {browseDir}
            </div>
            <div className="border rounded-md max-h-64 overflow-y-auto">
              {browseDir !== browseParent && (
                <button
                  type="button"
                  className="flex items-center gap-2 w-full px-3 py-1.5 text-sm hover:bg-accent text-left"
                  onClick={() => loadDir(browseParent)}
                  disabled={browseLoading}
                >
                  <ChevronUp className="h-4 w-4 text-muted-foreground" />
                  <span>..</span>
                </button>
              )}
              {browseEntries.map((entry) => (
                <div
                  key={entry.path}
                  className="flex items-center gap-2 w-full px-3 py-1.5 text-sm hover:bg-accent"
                >
                  <button
                    type="button"
                    className="flex items-center gap-2 flex-1 text-left truncate"
                    onClick={() => loadDir(entry.path)}
                    disabled={browseLoading}
                  >
                    {entry.is_git ? (
                      <FolderGit className="h-4 w-4 text-orange-500 shrink-0" />
                    ) : (
                      <Folder className="h-4 w-4 text-muted-foreground shrink-0" />
                    )}
                    <span className={cn(entry.is_git && 'font-medium')}>{entry.name}</span>
                  </button>
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    className="h-6 px-2 shrink-0"
                    onClick={() => selectPath(entry.path)}
                  >
                    <ArrowRight className="h-3 w-3" />
                  </Button>
                </div>
              ))}
              {!browseLoading && browseEntries.length === 0 && (
                <p className="text-sm text-muted-foreground px-3 py-2">No subdirectories</p>
              )}
            </div>
            <div className="flex gap-2">
              <Button type="button" variant="outline" size="sm" onClick={() => setBrowsing(false)}>
                Cancel
              </Button>
              <Button type="button" size="sm" onClick={() => selectPath(browseDir)}>
                Select this directory
              </Button>
            </div>
          </div>
        ) : (
          <form onSubmit={handleSubmit} className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="ws-path">Root Path</Label>
              <div className="flex gap-2">
                <Input
                  id="ws-path"
                  value={rootPath}
                  onChange={(e) => setRootPath(e.target.value)}
                  placeholder="/home/you/projects/my-project"
                  autoFocus
                />
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => {
                    setBrowseDir(rootPath || '')
                    setBrowsing(true)
                    if (rootPath) loadDir(rootPath)
                  }}
                >
                  Browse
                </Button>
              </div>
              {errors.root_path && <p className="text-xs text-destructive">{errors.root_path}</p>}
            </div>
            <div className="space-y-2">
              <Label htmlFor="ws-name">
                Name <span className="text-muted-foreground font-normal">(optional, defaults to directory name)</span>
              </Label>
              <Input
                id="ws-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder={rootPath ? rootPath.split('/').pop() : 'my-project'}
              />
            </div>
            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
                Cancel
              </Button>
              <Button type="submit" disabled={isPending}>
                {isPending ? 'Registering...' : 'Register'}
              </Button>
            </DialogFooter>
          </form>
        )}
      </DialogContent>
    </Dialog>
  )
}
