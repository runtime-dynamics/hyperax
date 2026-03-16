import { useState, useEffect, useCallback, useRef } from 'react'

export interface NervousEvent {
  type: string
  scope: string
  source: string
  payload: unknown
  trace_id: string
  sequence_id: number
  timestamp: string
}

interface EventStreamOptions {
  url?: string
  /** @deprecated Use exponential backoff — this option is ignored */
  reconnectInterval?: number
  /** Glob-style patterns sent to the backend on connect. [] or ['*'] = all events. */
  patterns?: string[]
}

// Exponential backoff constants
const BACKOFF_BASE_MS = 1000
const BACKOFF_MAX_MS = 30000
const BACKOFF_JITTER_MS = 1000

export function useEventStream(options: EventStreamOptions = {}) {
  const {
    url = `ws://${window.location.host}/ws/events`,
    patterns,
  } = options

  const [connected, setConnected] = useState(false)
  const [events, setEvents] = useState<NervousEvent[]>([])
  const wsRef = useRef<WebSocket | null>(null)
  const reconnectTimerRef = useRef<ReturnType<typeof setTimeout>>()
  // Keep a stable ref to the current patterns so connect() closure sees latest value
  const patternsRef = useRef<string[] | undefined>(patterns)
  // Backoff attempt counter — reset on successful connection
  const attemptRef = useRef(0)

  useEffect(() => {
    patternsRef.current = patterns
  }, [patterns])

  const connect = useCallback(() => {
    try {
      const ws = new WebSocket(url)
      wsRef.current = ws

      ws.onopen = () => {
        // Reset backoff on successful connection
        attemptRef.current = 0
        setConnected(true)
        // Send subscription filter immediately after connecting if patterns are specified
        const p = patternsRef.current
        if (p && p.length > 0) {
          try {
            ws.send(JSON.stringify({ type: 'subscribe', patterns: p }))
          } catch {
            // ignore send errors on open — will retry on next reconnect
          }
        }
      }

      ws.onmessage = (event) => {
        try {
          const data = JSON.parse(event.data as string) as NervousEvent
          setEvents((prev) => [...prev.slice(-99), data])
        } catch {
          // ignore non-JSON messages
        }
      }

      ws.onclose = () => {
        setConnected(false)
        wsRef.current = null
        const delay = Math.min(BACKOFF_BASE_MS * Math.pow(2, attemptRef.current), BACKOFF_MAX_MS)
        const jitter = Math.random() * BACKOFF_JITTER_MS
        attemptRef.current += 1
        reconnectTimerRef.current = setTimeout(connect, delay + jitter)
      }

      ws.onerror = () => {
        ws.close()
      }
    } catch {
      const delay = Math.min(BACKOFF_BASE_MS * Math.pow(2, attemptRef.current), BACKOFF_MAX_MS)
      const jitter = Math.random() * BACKOFF_JITTER_MS
      attemptRef.current += 1
      reconnectTimerRef.current = setTimeout(connect, delay + jitter)
    }
  }, [url])

  // Allow callers to dynamically update patterns on an existing connection
  const sendPatterns = useCallback((newPatterns: string[]) => {
    patternsRef.current = newPatterns
    const ws = wsRef.current
    if (ws && ws.readyState === WebSocket.OPEN) {
      try {
        ws.send(JSON.stringify({ type: 'subscribe', patterns: newPatterns }))
      } catch {
        // ignore
      }
    }
  }, [])

  useEffect(() => {
    connect()
    return () => {
      clearTimeout(reconnectTimerRef.current)
      wsRef.current?.close()
    }
  }, [connect])

  return { connected, events, sendPatterns }
}
