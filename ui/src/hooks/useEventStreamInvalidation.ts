import { useEffect, useRef } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { useEventStream } from './useEventStream'
import { toast } from '@/components/ui/use-toast'

export function useEventStreamInvalidation() {
  const { events } = useEventStream()
  const qc = useQueryClient()
  const processedRef = useRef<Set<number>>(new Set())

  useEffect(() => {
    if (events.length === 0) return

    const latest = events[events.length - 1]
    if (processedRef.current.has(latest.sequence_id)) return
    processedRef.current.add(latest.sequence_id)

    const { type, payload } = latest

    if (type === 'config.changed') {
      void qc.invalidateQueries({ queryKey: ['config-keys'] })
    } else if (type.startsWith('workspace.')) {
      void qc.invalidateQueries({ queryKey: ['workspaces'] })
    } else if (type.startsWith('agent.')) {
      void qc.invalidateQueries({ queryKey: ['agents'] })
      void qc.invalidateQueries({ queryKey: ['agent-hierarchy'] })
      void qc.invalidateQueries({ queryKey: ['agent-states-org'] })
      void qc.invalidateQueries({ queryKey: ['agent-detail'] })
    } else if (type.startsWith('lifecycle.')) {
      void qc.invalidateQueries({ queryKey: ['agent-states-org'] })
      void qc.invalidateQueries({ queryKey: ['agent-detail'] })
      void qc.invalidateQueries({ queryKey: ['agents'] })
    } else if (type.startsWith('chat.completion.') || type.startsWith('tooluse.')) {
      void qc.invalidateQueries({ queryKey: ['agent-states-org'] })
      void qc.invalidateQueries({ queryKey: ['agent-detail'] })
    } else if (type.startsWith('persona.')) {
      // Legacy: still support persona events for backward compatibility during migration
      void qc.invalidateQueries({ queryKey: ['agents'] })
      void qc.invalidateQueries({ queryKey: ['agent-hierarchy'] })
    } else if (type.startsWith('comm.')) {
      void qc.invalidateQueries({ queryKey: ['agent-inboxes'] })
    } else if (type.startsWith('agentmail.')) {
      void qc.invalidateQueries({ queryKey: ['channels'] })
    } else if (type.startsWith('guard.')) {
      void qc.invalidateQueries({ queryKey: ['guard'] })
      if (type === 'guard.pending') {
        const toolName = (payload as Record<string, unknown>)?.tool_name ?? 'unknown tool'
        toast({
          title: 'Action requires approval',
          description: `Guard intercepted "${toolName}" — review it in Actions.`,
        })
      }
    } else if (type.startsWith('interjection.')) {
      void qc.invalidateQueries({ queryKey: ['interjections'] })
      if (type === 'interjection.created') {
        const severity = (payload as Record<string, unknown>)?.severity ?? 'warning'
        const reason = (payload as Record<string, unknown>)?.reason ?? 'Safety interjection triggered'
        toast({
          title: `Safety alert (${severity})`,
          description: String(reason).slice(0, 120),
          variant: 'destructive',
        })
      }
    }
  }, [events, qc])
}
