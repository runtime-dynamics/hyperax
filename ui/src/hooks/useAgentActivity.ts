import { useState, useEffect, useRef } from 'react'
import { useEventStream, type NervousEvent } from './useEventStream'

// ─── Types ────────────────────────────────────────────────────────────────────

export type ActivityKind =
  | 'tool_dispatch'
  | 'tool_iteration'
  | 'loop_complete'
  | 'chat_started'
  | 'chat_completed'

export interface ActivityEntry {
  id: number
  timestamp: string
  kind: ActivityKind
  label: string
  toolName?: string
  iterationNum?: number
  durationMs?: number
}

// ─── Hook ─────────────────────────────────────────────────────────────────────

const ACTIVITY_BUFFER_SIZE = 50
let entrySeq = 0

function parseActivityEntry(event: NervousEvent, agentId: string): ActivityEntry | null {
  const payload = event.payload as Record<string, unknown> | null

  // Match by agent_name (preferred, now included in tooluse.* payloads),
  // falling back to agent_id in payload, then agent_id in event scope.
  const payloadAgentName = (payload?.agent_name as string) ?? ''
  const payloadAgentId = (payload?.agent_id as string) ?? ''
  const scopeAgentId = (event.scope ?? '').split('.').pop() ?? ''

  if (payloadAgentName !== agentId && payloadAgentId !== agentId && scopeAgentId !== agentId) return null

  const ts = event.timestamp ?? new Date().toISOString()

  switch (event.type) {
    case 'tooluse.tool.dispatch':
      return {
        id: ++entrySeq,
        timestamp: ts,
        kind: 'tool_dispatch',
        label: `Dispatched ${String(payload?.tool_name ?? 'tool')}`,
        toolName: payload?.tool_name as string | undefined,
      }

    case 'tooluse.loop.iteration':
      return {
        id: ++entrySeq,
        timestamp: ts,
        kind: 'tool_iteration',
        label: `Iteration ${String(payload?.iteration ?? '')}`,
        iterationNum:
          typeof payload?.iteration === 'number' ? payload.iteration : undefined,
        toolName: payload?.current_tool as string | undefined,
      }

    case 'tooluse.loop.complete':
      return {
        id: ++entrySeq,
        timestamp: ts,
        kind: 'loop_complete',
        label: 'Tool loop complete',
        durationMs:
          typeof payload?.duration_ms === 'number' ? payload.duration_ms : undefined,
      }

    case 'chat.completion.started':
      return {
        id: ++entrySeq,
        timestamp: ts,
        kind: 'chat_started',
        label: 'Completion started',
      }

    case 'chat.completion.completed':
      return {
        id: ++entrySeq,
        timestamp: ts,
        kind: 'chat_completed',
        label: 'Completion finished',
        durationMs:
          typeof payload?.duration_ms === 'number' ? payload.duration_ms : undefined,
      }

    default:
      return null
  }
}

export interface AgentActivityState {
  entries: ActivityEntry[]
  currentTool: string | null
  isActive: boolean
  iterationCount: number
}

export function useAgentActivity(agentId: string | null): AgentActivityState {
  const { events } = useEventStream({
    patterns: ['tooluse.*', 'chat.completion.*'],
  })

  const [entries, setEntries] = useState<ActivityEntry[]>([])
  const [currentTool, setCurrentTool] = useState<string | null>(null)
  const [isActive, setIsActive] = useState(false)
  const [iterationCount, setIterationCount] = useState(0)
  const processedRef = useRef<Set<number>>(new Set())

  useEffect(() => {
    if (!agentId || events.length === 0) return

    const latest = events[events.length - 1]
    if (processedRef.current.has(latest.sequence_id)) return
    processedRef.current.add(latest.sequence_id)

    const entry = parseActivityEntry(latest, agentId)
    if (!entry) return

    setEntries((prev) => [...prev.slice(-(ACTIVITY_BUFFER_SIZE - 1)), entry])

    switch (entry.kind) {
      case 'chat_started':
        setIsActive(true)
        setCurrentTool(null)
        setIterationCount(0)
        break
      case 'tool_dispatch':
        setCurrentTool(entry.toolName ?? null)
        break
      case 'tool_iteration':
        if (entry.iterationNum !== undefined) setIterationCount(entry.iterationNum)
        setCurrentTool(entry.toolName ?? currentTool)
        break
      case 'loop_complete':
      case 'chat_completed':
        setIsActive(false)
        setCurrentTool(null)
        break
    }
  }, [events, agentId, currentTool])

  // Reset when agent changes
  useEffect(() => {
    setEntries([])
    setCurrentTool(null)
    setIsActive(false)
    setIterationCount(0)
    processedRef.current.clear()
  }, [agentId])

  return { entries, currentTool, isActive, iterationCount }
}
