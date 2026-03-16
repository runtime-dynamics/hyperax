import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { mcpCall } from '@/lib/mcp-client'
import { apiGet, apiPost } from '@/lib/api-client'

export interface InboxInfo {
  agent_id: string
  message_count: number
  overflow_count: number
}

export interface HierarchyNode {
  agent_id: string
  parent_id?: string
  children: HierarchyNode[]
}

export interface CommLogEntry {
  id: string
  from_agent: string
  to_agent: string
  content_type: string
  content: string
  trust: string
  direction: string
  created_at: string
}

export interface SendMessageArgs {
  from: string
  to: string
  content: string
  content_type?: string
  trust?: string
}

export interface ChatSession {
  id: string
  agent_name: string
  peer_id: string
  started_at: string
  ended_at?: string
  summary: string
}

export function useAgentInboxes() {
  return useQuery({
    queryKey: ['agent-inboxes'],
    queryFn: () => mcpCall<InboxInfo[]>('comm', { action: 'list_inboxes' }),
    retry: false,
  })
}

export function useHierarchy() {
  return useQuery({
    queryKey: ['agent-hierarchy'],
    queryFn: () => mcpCall<HierarchyNode[]>('comm', { action: 'get_hierarchy' }),
    retry: false,
  })
}

export function useCommLog(agentId: string | undefined, peerId: string | undefined, sessionId?: string, sessionReady = true, limit = 50) {
  return useQuery({
    queryKey: ['comm-log', agentId, peerId, sessionId, sessionReady],
    queryFn: () => {
      const params = new URLSearchParams({ agent_id: agentId!, limit: String(limit) })
      if (peerId) params.set('peer_id', peerId)
      if (sessionId) params.set('session_id', sessionId)
      return apiGet<CommLogEntry[]>(`/chat/history?${params}`)
    },
    // Don't fire until we know the session state — prevents loading the
    // full unscoped history before the active session query resolves.
    enabled: !!agentId && !!peerId && sessionReady,
    refetchInterval: 30000, // Fallback only — primary updates arrive via WebSocket events
    retry: false,
  })
}

export function useSendMessage() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (args: SendMessageArgs) =>
      apiPost<{ status: string }>('/chat/send', args),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['comm-log'] })
      void qc.invalidateQueries({ queryKey: ['agent-inboxes'] })
    },
  })
}

export function useStopGeneration() {
  return useMutation({
    mutationFn: (agentName: string) =>
      apiPost<{ status: string }>('/chat/stop', { agent: agentName }),
  })
}

export function useActiveSession(agentName: string | undefined, peerId: string) {
  return useQuery({
    queryKey: ['active-session', agentName, peerId],
    queryFn: () => mcpCall<ChatSession | { session_id: ''; status: 'none' }>('comm', {
      action: 'get_session',
      agent_name: agentName!,
      peer_id: peerId,
    }),
    enabled: !!agentName,
    retry: false,
  })
}

export function useNewSession() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (args: { agent_name: string; peer_id: string }) =>
      mcpCall<{ session_id: string; status: string }>('comm', { action: 'new_session', ...args }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['comm-log'] })
      void qc.invalidateQueries({ queryKey: ['active-session'] })
    },
  })
}

export function useArchiveSession() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (args: { session_id: string }) =>
      mcpCall<{ status: string }>('comm', { action: 'archive_session', ...args }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['active-session'] })
      void qc.invalidateQueries({ queryKey: ['comm-log'] })
    },
  })
}

export function useListChatSessions(agentName: string | undefined, peerId: string) {
  return useQuery({
    queryKey: ['chat-sessions', agentName, peerId],
    queryFn: () =>
      mcpCall<ChatSession[]>('comm', {
        action: 'list_sessions',
        agent_name: agentName!,
        peer_id: peerId,
      }),
    enabled: !!agentName,
    retry: false,
  })
}
