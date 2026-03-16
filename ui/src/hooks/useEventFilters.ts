import { useState, useCallback } from 'react'

const STORAGE_KEY = 'hyperax-ws-event-filters'

export interface EventFilterCategory {
  id: string
  label: string
  pattern: string
  description: string
}

export const EVENT_FILTER_CATEGORIES: EventFilterCategory[] = [
  { id: 'comm', label: 'Communication', pattern: 'comm.*', description: 'Agent messaging and CommHub events' },
  { id: 'agent', label: 'Agents', pattern: 'agent.*', description: 'Agent create/update/delete' },
  { id: 'persona', label: 'Personas (Legacy)', pattern: 'persona.*', description: 'Persona create/update/delete (legacy)' },
  { id: 'pipeline', label: 'Pipelines', pattern: 'pipeline.*', description: 'Pipeline runs and job status' },
  { id: 'lifecycle', label: 'Lifecycle', pattern: 'lifecycle.*', description: 'Agent FSM state transitions' },
  { id: 'workspace', label: 'Workspaces', pattern: 'workspace.*', description: 'Workspace registration and changes' },
  { id: 'interject', label: 'Interjections', pattern: 'interject.*', description: 'Andon cord and halt events' },
  { id: 'budget', label: 'Budget', pattern: 'budget.*', description: 'Budget threshold and fiscal alerts' },
  { id: 'cron', label: 'Cron', pattern: 'cron.*', description: 'Scheduled job triggers' },
  { id: 'config', label: 'Config', pattern: 'config.*', description: 'Configuration changes' },
  { id: 'agentmail', label: 'AgentMail', pattern: 'agentmail.*', description: 'Outbound/inbound mail events' },
]

function loadFromStorage(): Set<string> {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return new Set(EVENT_FILTER_CATEGORIES.map((c) => c.id))
    const parsed = JSON.parse(raw) as unknown
    if (Array.isArray(parsed)) return new Set(parsed as string[])
  } catch {
    // ignore
  }
  return new Set(EVENT_FILTER_CATEGORIES.map((c) => c.id))
}

function saveToStorage(enabled: Set<string>) {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(Array.from(enabled)))
  } catch {
    // ignore
  }
}

export function useEventFilters() {
  const [enabledIds, setEnabledIds] = useState<Set<string>>(loadFromStorage)

  const toggleCategory = useCallback((id: string) => {
    setEnabledIds((prev) => {
      const next = new Set(prev)
      if (next.has(id)) {
        next.delete(id)
      } else {
        next.add(id)
      }
      saveToStorage(next)
      return next
    })
  }, [])

  const enableAll = useCallback(() => {
    const all = new Set(EVENT_FILTER_CATEGORIES.map((c) => c.id))
    saveToStorage(all)
    setEnabledIds(all)
  }, [])

  const disableAll = useCallback(() => {
    saveToStorage(new Set())
    setEnabledIds(new Set())
  }, [])

  // Derive the WS patterns from enabled categories.
  // If all categories are enabled, send [] (= receive everything, including uncategorised events).
  const allEnabled = enabledIds.size === EVENT_FILTER_CATEGORIES.length
  const patterns: string[] = allEnabled
    ? []
    : EVENT_FILTER_CATEGORIES.filter((c) => enabledIds.has(c.id)).map((c) => c.pattern)

  return { enabledIds, patterns, toggleCategory, enableAll, disableAll, allEnabled }
}
