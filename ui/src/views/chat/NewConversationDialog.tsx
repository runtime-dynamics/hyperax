import { useState, useMemo } from 'react'
import { Search, Star, User, MessageSquare } from 'lucide-react'
import { useAgents } from '@/services/agentService'
import { useProviders } from '@/services/providerService'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

interface NewConversationDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSelect: (agentId: string) => void
}

function isChiefOfStaff(clearanceLevel?: number, name?: string): boolean {
  return clearanceLevel === 3 || name?.toLowerCase() === 'chief of staff'
}

export function NewConversationDialog({ open, onOpenChange, onSelect }: NewConversationDialogProps) {
  const [search, setSearch] = useState('')
  const [selectedId, setSelectedId] = useState<string | undefined>()

  const { data: rawAgents } = useAgents()
  const { data: rawProviders } = useProviders()

  const allAgents = Array.isArray(rawAgents) ? rawAgents : []
  const providers = Array.isArray(rawProviders) ? rawProviders : []

  const eligibleAgents = useMemo(() => {
    return allAgents.filter(
      (a) => a.name.toLowerCase() !== 'postmaster' && !a.is_internal,
    )
  }, [allAgents])

  const sortedAgents = useMemo(() => {
    return [...eligibleAgents].sort((a, b) => {
      const aIsCoS = isChiefOfStaff(a.clearance_level, a.name)
      const bIsCoS = isChiefOfStaff(b.clearance_level, b.name)
      if (aIsCoS && !bIsCoS) return -1
      if (!aIsCoS && bIsCoS) return 1
      return a.name.localeCompare(b.name)
    })
  }, [eligibleAgents])

  // Pre-select Chief of Staff by default when dialog opens
  const defaultSelectedId = useMemo(() => {
    const cos = sortedAgents.find((a) => isChiefOfStaff(a.clearance_level, a.name))
    return cos?.id
  }, [sortedAgents])

  const effectiveSelectedId = selectedId ?? defaultSelectedId

  const filtered = useMemo(() => {
    if (!search.trim()) return sortedAgents
    const q = search.toLowerCase()
    return sortedAgents.filter(
      (a) =>
        a.name.toLowerCase().includes(q) ||
        a.default_model?.toLowerCase().includes(q),
    )
  }, [sortedAgents, search])

  function handleConfirm() {
    if (!effectiveSelectedId) return
    onSelect(effectiveSelectedId)
    onOpenChange(false)
    setSearch('')
    setSelectedId(undefined)
  }

  function handleOpenChange(val: boolean) {
    if (!val) {
      setSearch('')
      setSelectedId(undefined)
    }
    onOpenChange(val)
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <MessageSquare className="h-4 w-4" />
            New Conversation
          </DialogTitle>
          <DialogDescription>
            Choose an agent to start a conversation with.
          </DialogDescription>
        </DialogHeader>

        <div className="relative">
          <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
          <Input
            placeholder="Search agents..."
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="pl-8"
            autoFocus
          />
        </div>

        <div className="max-h-64 overflow-y-auto -mx-1 px-1 space-y-0.5">
          {filtered.length === 0 ? (
            <p className="text-sm text-muted-foreground text-center py-6">
              {eligibleAgents.length === 0
                ? 'No agents exist yet. Create one in the Organization view.'
                : 'No agents match your search.'}
            </p>
          ) : (
            filtered.map((agent) => {
              const provider = providers.find((p) => p.id === agent.provider_id)
              const isSelected = effectiveSelectedId === agent.id
              const isCoS = isChiefOfStaff(agent.clearance_level, agent.name)

              return (
                <button
                  key={agent.id}
                  onClick={() => setSelectedId(agent.id)}
                  className={cn(
                    'w-full flex items-center gap-2.5 px-3 py-2 rounded-md text-left text-sm transition-colors',
                    isSelected
                      ? 'bg-accent text-accent-foreground'
                      : 'hover:bg-accent/50 text-foreground',
                  )}
                >
                  <User className="h-4 w-4 text-muted-foreground shrink-0" />
                  <div className="flex-1 min-w-0">
                    <div className="font-medium truncate flex items-center gap-1.5">
                      {agent.name}
                      {isCoS && (
                        <span className="text-xs text-muted-foreground font-normal">(Chief of Staff)</span>
                      )}
                    </div>
                    {agent.default_model && (
                      <div className="text-xs text-muted-foreground truncate">
                        {agent.default_model}
                        {provider ? ` · ${provider.name}` : ''}
                      </div>
                    )}
                  </div>
                  {agent.is_favorite && (
                    <Star className="h-3.5 w-3.5 text-amber-400 shrink-0 fill-amber-400" />
                  )}
                </button>
              )
            })
          )}
        </div>

        <div className="flex justify-end gap-2 pt-2">
          <Button variant="ghost" onClick={() => handleOpenChange(false)}>
            Cancel
          </Button>
          <Button onClick={handleConfirm} disabled={!effectiveSelectedId}>
            Start Conversation
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  )
}
