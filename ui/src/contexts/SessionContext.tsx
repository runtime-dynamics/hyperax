import React, { createContext, useContext, useCallback, useState, useEffect } from 'react'

export interface ChatSession {
  agentId: string
  sessionId: string
  createdAt: string
  isActive: boolean
  type: 'persistent' | 'temporary'
}

interface SessionContextValue {
  sessions: Map<string, ChatSession>
  activeSessionId: string | undefined
  getOrCreateSession: (agentId: string, type?: 'persistent' | 'temporary') => ChatSession
  closeSession: (agentId: string) => void
  getSession: (agentId: string) => ChatSession | undefined
  switchSession: (agentId: string) => Promise<void>
  hasSession: (agentId: string) => boolean
  getAllSessions: () => ChatSession[]
  clearAllSessions: () => void
}

const SessionContext = createContext<SessionContextValue | undefined>(undefined)

const ACTIVE_SESSION_ID_KEY = 'active_chat_session_id'
const PERSISTENT_SESSIONS_KEY = 'persistent_chat_sessions'

/**
 * SessionProvider: Manages chat sessions with distinction between persistent and temporary sessions
 * - Persistent sessions: Stored in localStorage, restored on app reload, linked to favorites
 * - Temporary sessions: Stored in sessionStorage only, created from deep-links, can be closed without affecting favorites
 */
export function SessionProvider({ children }: { children: React.ReactNode }) {
  const [sessions, setSessions] = useState<Map<string, ChatSession>>(new Map())
  const [activeSessionId, setActiveSessionId] = useState<string | undefined>()

  // Load persistent sessions from localStorage on mount
  useEffect(() => {
    const storedPersistent = localStorage.getItem(PERSISTENT_SESSIONS_KEY)
    const storedActiveId = localStorage.getItem(ACTIVE_SESSION_ID_KEY)

    if (storedPersistent) {
      try {
        const sessionList = JSON.parse(storedPersistent) as ChatSession[]
        setSessions(new Map(sessionList.map((s) => [s.agentId, s])))
      } catch (e) {
        console.error('Failed to load persistent sessions from localStorage:', e)
      }
    }

    if (storedActiveId) {
      setActiveSessionId(storedActiveId)
    }
  }, [])

  // Persist only 'persistent' type sessions to localStorage
  useEffect(() => {
    const persistentSessions = Array.from(sessions.values()).filter((s) => s.type === 'persistent')
    localStorage.setItem(PERSISTENT_SESSIONS_KEY, JSON.stringify(persistentSessions))
  }, [sessions])

  // Persist active session ID to localStorage
  useEffect(() => {
    if (activeSessionId) {
      localStorage.setItem(ACTIVE_SESSION_ID_KEY, activeSessionId)
    }
  }, [activeSessionId])

  /**
   * Get or create a session for an agent
   * @param agentId - The agent ID
   * @param type - Session type: 'persistent' (saved to localStorage) or 'temporary' (sessionStorage only)
   */
  const getOrCreateSession = useCallback(
    (agentId: string, type: 'persistent' | 'temporary' = 'persistent'): ChatSession => {
      let session = sessions.get(agentId)
      if (!session) {
        session = {
          agentId,
          sessionId: `session_${agentId}_${Date.now()}`,
          createdAt: new Date().toISOString(),
          isActive: true,
          type,
        }
        setSessions((prev) => new Map(prev).set(agentId, session!))
      }
      return session
    },
    [sessions],
  )

  /**
   * Close a session (remove it from active sessions)
   * Persistent sessions can be reopened later; temporary sessions are discarded
   */
  const closeSession = useCallback((agentId: string) => {
    setSessions((prev) => {
      const next = new Map(prev)
      next.delete(agentId)
      return next
    })

    // Clear active session if it matches
    setActiveSessionId((prev) => (prev === agentId ? undefined : prev))
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
   * Switch to a different agent's session
   * Creates a new session if one doesn't exist
   */
  const switchSession = useCallback(
    async (agentId: string): Promise<void> => {
      const session = getOrCreateSession(agentId, 'persistent')
      setActiveSessionId(session.sessionId)
    },
    [getOrCreateSession],
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
   * Clear all sessions (both persistent and temporary)
   */
  const clearAllSessions = useCallback(() => {
    setSessions(new Map())
    setActiveSessionId(undefined)
    localStorage.removeItem(PERSISTENT_SESSIONS_KEY)
    localStorage.removeItem(ACTIVE_SESSION_ID_KEY)
  }, [])

  const value: SessionContextValue = {
    sessions,
    activeSessionId,
    getOrCreateSession,
    closeSession,
    getSession,
    switchSession,
    hasSession,
    getAllSessions,
    clearAllSessions,
  }

  return <SessionContext.Provider value={value}>{children}</SessionContext.Provider>
}

/**
 * Hook to use the session context
 */
export function useSessionContext(): SessionContextValue {
  const context = useContext(SessionContext)
  if (!context) {
    throw new Error('useSessionContext must be used within a SessionProvider')
  }
  return context
}

/**
 * Convenience hook for the current active session
 */
export function useActiveSession(agentId?: string) {
  const { getSession, activeSessionId } = useSessionContext()

  if (!agentId || !activeSessionId) {
    return undefined
  }

  return getSession(agentId)
}

/**
 * Convenience hook for switching sessions
 */
export function useSwitchSession() {
  const { switchSession } = useSessionContext()
  return switchSession
}

/**
 * Convenience hook for closing sessions
 */
export function useCloseSession() {
  const { closeSession } = useSessionContext()
  return closeSession
}
