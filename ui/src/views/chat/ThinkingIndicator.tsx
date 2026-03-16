import { useEffect, useState, useRef, useCallback } from 'react'
import { Loader2, Wrench, Brain, ChevronDown, ChevronRight, Play } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import type { NervousEvent } from '@/hooks/useEventStream'

type ActivityState = 'idle' | 'thinking' | 'tool' | 'max_iter_reached'

interface ToolCallEntry {
  name: string
  timestamp: number
}

interface ThinkingIndicatorProps {
  events: NervousEvent[]
  agentName: string | undefined
  onContinue?: () => void
  className?: string
}

/**
 * Shows a real-time activity log during LLM processing:
 * - "Thinking..." when a chat completion starts
 * - Collapsible list of tool calls as they happen
 * - "Continue" button when max iterations reached (auto_continue=false)
 */
export function ThinkingIndicator({ events, agentName, onContinue, className }: ThinkingIndicatorProps) {
  const [activity, setActivity] = useState<ActivityState>('idle')
  const [currentTool, setCurrentTool] = useState<string>('')
  const [toolLog, setToolLog] = useState<ToolCallEntry[]>([])
  const [iteration, setIteration] = useState(0)
  const [expanded, setExpanded] = useState(false)
  const lastSeqRef = useRef(0)

  const resetState = useCallback(() => {
    setActivity('idle')
    setCurrentTool('')
    setToolLog([])
    setIteration(0)
    setExpanded(false)
  }, [])

  useEffect(() => {
    if (events.length === 0) return

    // Process only new events since last render
    const newEvents = events.filter((e) => e.sequence_id > lastSeqRef.current)
    if (newEvents.length === 0) return
    lastSeqRef.current = events[events.length - 1].sequence_id

    for (const ev of newEvents) {
      const payload = ev.payload as Record<string, unknown> | null

      // Filter events to the current agent by scope.
      if (agentName && ev.scope && ev.scope !== agentName) continue

      switch (ev.type) {
        case 'chat.completion.start':
          setActivity('thinking')
          setCurrentTool('')
          setToolLog([])
          setIteration(0)
          break

        case 'tooluse.loop.start':
          setActivity('thinking')
          setCurrentTool('')
          setToolLog([])
          setIteration(0)
          break

        case 'tooluse.tool.dispatch':
          if (payload?.tool) {
            const toolName = payload.tool as string
            setActivity('tool')
            setCurrentTool(toolName)
            setToolLog((prev) => [...prev, { name: toolName, timestamp: Date.now() }])
          }
          break

        case 'tooluse.loop.iteration': {
          const iter = payload?.iteration as number | undefined
          if (iter) setIteration(iter)
          setActivity('thinking')
          setCurrentTool('')
          break
        }

        case 'tooluse.loop.auto_extend':
          // Auto-continue kicked in — keep showing activity
          setActivity('thinking')
          break

        case 'tooluse.loop.max_iter_reached':
          // Max iterations hit without auto-continue — show continue button
          setActivity('max_iter_reached')
          setCurrentTool('')
          break

        case 'chat.completion.done':
        case 'chat.completion.error':
        case 'tooluse.loop.complete':
        case 'tooluse.loop.error':
          resetState()
          break
      }
    }
  }, [events, agentName, resetState])

  if (activity === 'idle') return null

  const uniqueTools = [...new Set(toolLog.map((t) => t.name))]

  return (
    <div
      className={cn(
        'px-4 py-2 text-sm text-muted-foreground animate-in fade-in duration-300 space-y-1',
        className,
      )}
    >
      {/* Current status line */}
      <div className="flex items-center gap-2">
        {activity === 'max_iter_reached' ? (
          <>
            <Brain className="h-4 w-4 text-amber-500" />
            <span className="text-amber-600 dark:text-amber-400">
              Reached iteration limit ({iteration || '?'})
            </span>
            {onContinue && (
              <Button
                variant="outline"
                size="sm"
                className="h-6 text-xs ml-2"
                onClick={onContinue}
              >
                <Play className="h-3 w-3 mr-1" />
                Continue
              </Button>
            )}
          </>
        ) : activity === 'thinking' ? (
          <>
            <Brain className="h-4 w-4 animate-pulse text-blue-500" />
            <span>Thinking{iteration > 0 ? ` (iteration ${iteration})` : ''}...</span>
          </>
        ) : (
          <>
            <Wrench className="h-4 w-4 text-amber-500" />
            <span>
              Using <code className="text-xs bg-muted px-1 py-0.5 rounded font-mono">{currentTool}</code>
            </span>
            <Loader2 className="h-3 w-3 animate-spin text-muted-foreground" />
            {iteration > 0 && (
              <span className="text-xs opacity-60">· iteration {iteration}</span>
            )}
          </>
        )}
      </div>

      {/* Tool call log (collapsible) */}
      {toolLog.length > 0 && (
        <div className="ml-6">
          <button
            className="flex items-center gap-1 text-xs text-muted-foreground/70 hover:text-muted-foreground transition-colors"
            onClick={() => setExpanded(!expanded)}
          >
            {expanded ? (
              <ChevronDown className="h-3 w-3" />
            ) : (
              <ChevronRight className="h-3 w-3" />
            )}
            {toolLog.length} tool call{toolLog.length !== 1 ? 's' : ''}
            {!expanded && uniqueTools.length > 0 && (
              <span className="ml-1 opacity-60">
                ({uniqueTools.slice(0, 3).join(', ')}{uniqueTools.length > 3 ? '…' : ''})
              </span>
            )}
          </button>
          {expanded && (
            <div className="mt-1 space-y-0.5 max-h-40 overflow-y-auto">
              {toolLog.map((entry, i) => (
                <div key={i} className="flex items-center gap-1.5 text-xs text-muted-foreground/60">
                  <Wrench className="h-2.5 w-2.5 shrink-0" />
                  <code className="bg-muted px-1 py-0.5 rounded font-mono">{entry.name}</code>
                </div>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  )
}
