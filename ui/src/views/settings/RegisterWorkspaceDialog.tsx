import { useState } from 'react'
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
import { toast } from '@/components/ui/use-toast'

const schema = z.object({
  name: z.string().min(1, 'Name is required'),
  root_path: z.string().min(1, 'Root path is required').startsWith('/', 'Path must start with /'),
})

interface RegisterWorkspaceDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function RegisterWorkspaceDialog({ open, onOpenChange }: RegisterWorkspaceDialogProps) {
  const [name, setName] = useState('')
  const [rootPath, setRootPath] = useState('')
  const [errors, setErrors] = useState<{ name?: string; root_path?: string }>({})

  const { mutate, isPending } = useRegisterWorkspace()

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    const result = schema.safeParse({ name, root_path: rootPath })
    if (!result.success) {
      const fieldErrors: { name?: string; root_path?: string } = {}
      for (const issue of result.error.issues) {
        const field = issue.path[0] as string
        if (field === 'name' || field === 'root_path') {
          fieldErrors[field] = issue.message
        }
      }
      setErrors(fieldErrors)
      return
    }
    setErrors({})
    mutate(
      { name, root_path: rootPath },
      {
        onSuccess: () => {
          toast({ title: 'Workspace registered', description: `"${name}" has been added.` })
          setName('')
          setRootPath('')
          onOpenChange(false)
        },
        onError: (err) => {
          toast({ title: 'Registration failed', description: (err as Error).message, variant: 'destructive' })
        },
      },
    )
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
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
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
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
