import { useState } from 'react'
import { ChevronDown, ChevronRight, Radio } from 'lucide-react'
import { useEventStream } from '@/hooks/useEventStream'
import { cn } from '@/lib/utils'

/** Extract a human-readable summary from the event payload when available. */
function eventSummary(ev: { type: string; source: string; payload: unknown }): string {
  if (ev.payload && typeof ev.payload === 'object' && 'message' in ev.payload) {
    return String((ev.payload as Record<string, unknown>).message)
  }
  return ev.source
}

export function EventStream() {
  const { events, connected } = useEventStream()
  const [expanded, setExpanded] = useState(false)

  return (
    <div className="border-t bg-card">
      <button
        onClick={() => setExpanded((p) => !p)}
        className="w-full flex items-center gap-2 px-4 py-2 text-xs text-muted-foreground hover:text-foreground transition-colors"
      >
        {expanded ? <ChevronDown className="h-3.5 w-3.5" /> : <ChevronRight className="h-3.5 w-3.5" />}
        <Radio className="h-3.5 w-3.5" />
        <span>Event Stream</span>
        <span
          className={cn(
            'ml-1 h-1.5 w-1.5 rounded-full',
            connected ? 'bg-green-500' : 'bg-red-500',
          )}
        />
        {events.length > 0 && (
          <span className="ml-auto font-mono">{events.length} events</span>
        )}
      </button>

      {expanded && (
        <div className="max-h-48 overflow-y-auto px-4 pb-3 space-y-1">
          {events.length === 0 ? (
            <p className="text-xs text-muted-foreground py-2">No events received yet.</p>
          ) : (
            [...events].reverse().map((ev) => (
              <div
                key={ev.sequence_id}
                className="flex items-start gap-2 text-xs font-mono py-0.5"
              >
                <span className="text-muted-foreground shrink-0">
                  {new Date(ev.timestamp).toLocaleTimeString()}
                </span>
                <span className="text-blue-600 dark:text-blue-400 shrink-0">{ev.type}</span>
                <span className="text-muted-foreground truncate">{eventSummary(ev)}</span>
              </div>
            ))
          )}
        </div>
      )}
    </div>
  )
}
