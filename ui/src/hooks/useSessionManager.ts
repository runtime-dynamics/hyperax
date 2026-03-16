import { useCallback, useEffect, useState } from 'react'

export interface ChatSession {
  agentId: string
  sessionId: string
  createdAt: string
  isActive: boolean
}

const ACTIVE_SESSIONS_KEY = 'active_chat_sessions'

/**
 * Hook to manage per-agent chat sessions
 * Sessions are stored in sessionStorage (browser tab-specific) with optional localStorage persistence
 */
export function useSessionManager() {
  const [sessions, setSessions] = useState<Map<string, ChatSession>>(new Map())

  // Load sessions from storage on mount
  useEffect(() => {
    const stored = sessionStorage.getItem(ACTIVE_SESSIONS_KEY)
    if (stored) {
      try {
        const sessionList = JSON.parse(stored) as ChatSession[]
        setSessions(new Map(sessionList.map((s) => [s.agentId, s])))
      } catch (e) {
        console.error('Failed to load sessions from storage:', e)
      }
    }
  }, [])

  // Persist sessions whenever they change
  useEffect(() => {
    const sessionArray = Array.from(sessions.values())
    sessionStorage.setItem(ACTIVE_SESSIONS_KEY, JSON.stringify(sessionArray))
  }, [sessions])

  /**
   * Get or create a session for an agent
   */
  const getOrCreateSession = useCallback((agentId: string): ChatSession => {
    let session = sessions.get(agentId)
    if (!session) {
      session = {
        agentId,
        sessionId: `session_${agentId}_${Date.now()}`,
        createdAt: new Date().toISOString(),
        isActive: true,
      }
      setSessions((prev) => new Map(prev).set(agentId, session!))
    }
    return session
  }, [sessions])

  /**
   * Close a session (remove it from active sessions)
   */
  const closeSession = useCallback((agentId: string) => {
    setSessions((prev) => {
      const next = new Map(prev)
      next.delete(agentId)
      return next
    })
  }, [])

  /**
   * Get the active session for an agent
   */
  const getSession = useCallback(
    (agentId: string): ChatSession | undefined => {
      return sessions.get(agentId)
    },
    [sessions],
  )

  /**
   * Check if an agent has an active session
   */
  const hasSession = useCallback((agentId: string): boolean => {
    return sessions.has(agentId)
  }, [sessions])

  /**
   * Get all active sessions
   */
  const getAllSessions = useCallback((): ChatSession[] => {
    return Array.from(sessions.values())
  }, [sessions])

  /**
   * Clear all sessions
   */
  const clearAllSessions = useCallback(() => {
    setSessions(new Map())
  }, [])

  return {
    sessions,
    getOrCreateSession,
    closeSession,
    getSession,
    hasSession,
    getAllSessions,
    clearAllSessions,
  }
}
