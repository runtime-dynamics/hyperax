import { useState, useMemo } from 'react'
import { X, Plus } from 'lucide-react'
import { toast } from '@/components/ui/use-toast'
import { useAgents } from '@/services/agentService'
import { useSessionContext } from '@/contexts/SessionContext'
import { useArchiveSession, useActiveSession as useBackendActiveSession } from '@/services/commhubService'
import { NewConversationDialog } from './NewConversationDialog'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

const OPERATOR_ID = 'operator'

interface ChatTabBarProps {
  onArchiveSession?: (agentName: string) => void
}

function isChiefOfStaff(clearanceLevel?: number, name?: string): boolean {
  return clearanceLevel === 3 || name?.toLowerCase() === 'chief of staff'
}

interface TabProps {
  agentId: string
  agentName: string
  isActive: boolean
  isChiefOfStaff: boolean
  isClosing: boolean
  onSelect: () => void
  onClose: () => void
}

function Tab({ agentName, isActive, isChiefOfStaff: isCoS, isClosing, onSelect, onClose }: TabProps) {
  return (
    <div
      className={cn(
        'flex items-center gap-1 px-3 py-1.5 text-sm border-r shrink-0 cursor-pointer select-none group',
        isActive
          ? 'bg-accent text-accent-foreground'
          : 'hover:bg-accent/50 text-foreground',
      )}
      onClick={onSelect}
    >
      <span className="truncate max-w-[120px]">{agentName}</span>
      {!isCoS && (
        <button
          disabled={isClosing}
          className={cn(
            'ml-1 rounded p-0.5 opacity-0 group-hover:opacity-100 transition-opacity',
            isActive ? 'opacity-100' : '',
            isClosing
              ? 'opacity-50 cursor-not-allowed'
              : 'hover:bg-destructive/20 hover:text-destructive',
          )}
          onClick={(e) => {
            e.stopPropagation()
            onClose()
          }}
          title={isClosing ? 'Archiving...' : 'Close tab'}
        >
          <X className="h-3 w-3" />
        </button>
      )}
    </div>
  )
}

// Inner component so hooks can be called per-agent (Rules of Hooks)
function ClosableTab({
  agentId,
  agentName,
  isActive,
  isCoS,
  isClosing,
  onSelect,
  onCloseWithArchive,
}: {
  agentId: string
  agentName: string
  isActive: boolean
  isCoS: boolean
  isClosing: boolean
  onSelect: () => void
  onCloseWithArchive: (agentId: string, agentName: string, backendSessionId: string | undefined) => void
}) {
  const { data: backendSessionData } = useBackendActiveSession(agentName, OPERATOR_ID)
  const backendSessionId =
    backendSessionData &&
    typeof backendSessionData === 'object' &&
    'id' in backendSessionData &&
    backendSessionData.id
      ? (backendSessionData.id as string)
      : undefined

  return (
    <Tab
      agentId={agentId}
      agentName={agentName}
      isActive={isActive}
      isChiefOfStaff={isCoS}
      isClosing={isClosing}
      onSelect={onSelect}
      onClose={() => onCloseWithArchive(agentId, agentName, backendSessionId)}
    />
  )
}

export function ChatTabBar({ onArchiveSession }: ChatTabBarProps) {
  const [showNewConvo, setShowNewConvo] = useState(false)
  const [closingAgentId, setClosingAgentId] = useState<string | undefined>()
  const { getAllSessions, activeSessionId, switchSession, closeSession } = useSessionContext()
  const archiveSession = useArchiveSession()

  const { data: rawAgents } = useAgents()
  const allAgents = Array.isArray(rawAgents) ? rawAgents : []

  const sessions = getAllSessions()

  const agentMap = useMemo(() => {
    return new Map(allAgents.map((a) => [a.id, a]))
  }, [allAgents])

  // Derive active agent ID from activeSessionId
  const activeAgentId = useMemo(() => {
    if (!activeSessionId) return undefined
    const parts = activeSessionId.split('_')
    return parts.length >= 2 ? parts[1] : undefined
  }, [activeSessionId])

  function handleCloseWithArchive(agentId: string, agentName: string, backendSessionId: string | undefined) {
    if (backendSessionId) {
      setClosingAgentId(agentId)
      archiveSession.mutate(
        { session_id: backendSessionId },
        {
          onSuccess: () => {
            setClosingAgentId(undefined)
            closeSession(agentId)
            onArchiveSession?.(agentName)
          },
          onError: (error) => {
            setClosingAgentId(undefined)
            const msg = error instanceof Error ? error.message : 'Failed to archive session'
            toast({
              title: 'Could not archive session',
              description: msg,
              variant: 'destructive',
            })
          },
        },
      )
    } else {
      closeSession(agentId)
    }
  }

  async function handleNewConvoSelect(agentId: string) {
    await switchSession(agentId)
  }

  if (sessions.length === 0) return null

  return (
    <>
      <div className="flex border-b bg-card overflow-x-auto shrink-0">
        {sessions.map((session) => {
          const agent = agentMap.get(session.agentId)
          const agentName = agent?.name ?? session.agentId
          const isCoS = isChiefOfStaff(agent?.clearance_level, agent?.name)
          const isActive = activeAgentId === session.agentId

          return (
            <ClosableTab
              key={session.agentId}
              agentId={session.agentId}
              agentName={agentName}
              isActive={isActive}
              isCoS={isCoS}
              isClosing={closingAgentId === session.agentId}
              onSelect={() => void switchSession(session.agentId)}
              onCloseWithArchive={handleCloseWithArchive}
            />
          )
        })}
        <Button
          variant="ghost"
          size="sm"
          className="h-auto px-2 py-1.5 rounded-none shrink-0 text-muted-foreground hover:text-foreground"
          onClick={() => setShowNewConvo(true)}
          title="New conversation"
        >
          <Plus className="h-4 w-4" />
        </Button>
      </div>

      <NewConversationDialog
        open={showNewConvo}
        onOpenChange={setShowNewConvo}
        onSelect={(agentId) => void handleNewConvoSelect(agentId)}
      />
    </>
  )
}
