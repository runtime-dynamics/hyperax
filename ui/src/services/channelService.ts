import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { mcpCall } from '@/lib/mcp-client'

// ─── Types ────────────────────────────────────────────────────────────────────

export type SessionStatus = 'connected' | 'disconnected' | 'idle'

export interface ChannelSession {
  id: string
  name: string
  status: SessionStatus
  workspace: string
  connected_at: string
  last_heartbeat: string | null
}

export interface ListSessionsResult {
  sessions: ChannelSession[]
}

export interface ChannelMessage {
  id: string
  session_id: string
  content: string
  direction: 'inbound' | 'outbound' | 'system'
  created_at: string
  metadata?: {
    task_id?: string
    task_status?: string
  }
}

export interface ChannelHistoryResult {
  messages: ChannelMessage[]
}

export interface PermissionRequest {
  id: string
  session_id: string
  tool_name: string
  description: string
  requested_at: string
}

export interface ListPermissionsResult {
  requests: PermissionRequest[]
}

export interface PostboxStatus {
  inbound_count: number
  outbound_count: number
  last_poll: string | null
}

export interface DeadLetter {
  id: string
  mail_id: string
  reason: string
  quarantined_at: string
}

export interface ListDeadLettersResult {
  dead_letters: DeadLetter[]
}

export interface RetryDeadLetterArgs {
  dead_letter_id: string
}

export interface RetryDeadLetterResult {
  message: string
}

export interface DiscardDeadLetterArgs {
  dead_letter_id: string
}

export interface DiscardDeadLetterResult {
  message: string
}

export interface SendMessageArgs {
  session_id: string
  content: string
}

export interface SendMessageResult {
  message: string
}

export interface ResolvePermissionArgs {
  request_id: string
  behavior: 'allow' | 'deny'
}

export interface ResolvePermissionResult {
  message: string
}

export interface DisconnectSessionArgs {
  session_id: string
}

export interface DisconnectSessionResult {
  message: string
}

export interface DispatchTaskArgs {
  session_id: string
  task_id: string
}

export interface DispatchTaskResult {
  message: string
}

// ─── Query Keys ───────────────────────────────────────────────────────────────

export const channelKeys = {
  sessions: () => ['channel', 'sessions'] as const,
  session: (id: string) => ['channel', 'session', id] as const,
  history: (id: string, limit: number) => ['channel', 'history', id, limit] as const,
  permissions: (id: string) => ['channel', 'permissions', id] as const,
  postbox: () => ['channels', 'postbox'] as const,
  deadLetters: () => ['channels', 'dead-letters'] as const,
}

// ─── Session Hooks ────────────────────────────────────────────────────────────

export function useChannelSessions() {
  return useQuery({
    queryKey: channelKeys.sessions(),
    queryFn: async () => {
      const result = await mcpCall<ListSessionsResult>('channel', { action: 'list_sessions' })
      return Array.isArray(result?.sessions) ? result.sessions : []
    },
    retry: false,
    refetchInterval: 5000,
  })
}

export function useChannelSession(id: string) {
  return useQuery({
    queryKey: channelKeys.session(id),
    queryFn: () => mcpCall<ChannelSession>('channel', { action: 'get_session', session_id: id }),
    enabled: !!id,
    retry: false,
  })
}

export function useChannelHistory(id: string, limit = 50) {
  return useQuery({
    queryKey: channelKeys.history(id, limit),
    queryFn: async () => {
      const result = await mcpCall<ChannelHistoryResult>('channel', {
        action: 'get_history',
        session_id: id,
        limit,
      })
      return Array.isArray(result?.messages) ? result.messages : []
    },
    enabled: !!id,
    retry: false,
    refetchInterval: 3000,
  })
}

export function useChannelPermissions(id: string) {
  return useQuery({
    queryKey: channelKeys.permissions(id),
    queryFn: async () => {
      const result = await mcpCall<ListPermissionsResult>('channel', {
        action: 'list_permissions',
        session_id: id,
      })
      return Array.isArray(result?.requests) ? result.requests : []
    },
    enabled: !!id,
    retry: false,
    refetchInterval: 3000,
  })
}

// ─── Mutation Hooks ───────────────────────────────────────────────────────────

export function useSendChannelMessage() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (args: SendMessageArgs) =>
      mcpCall<SendMessageResult>('channel', {
        action: 'send_message',
        ...(args as unknown as Record<string, unknown>),
      }),
    onSuccess: (_data, vars) => {
      void qc.invalidateQueries({ queryKey: channelKeys.history(vars.session_id, 50) })
    },
  })
}

export function useResolvePermission() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (args: ResolvePermissionArgs) =>
      mcpCall<ResolvePermissionResult>('channel', {
        action: 'resolve_permission',
        ...(args as unknown as Record<string, unknown>),
      }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['channel', 'permissions'] })
    },
  })
}

export function useDisconnectSession() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (args: DisconnectSessionArgs) =>
      mcpCall<DisconnectSessionResult>('channel', {
        action: 'disconnect_session',
        ...(args as unknown as Record<string, unknown>),
      }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: channelKeys.sessions() })
    },
  })
}

export function useDispatchTask() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (args: DispatchTaskArgs) =>
      mcpCall<DispatchTaskResult>('channel', {
        action: 'dispatch_task',
        ...(args as unknown as Record<string, unknown>),
      }),
    onSuccess: (_data, vars) => {
      void qc.invalidateQueries({ queryKey: channelKeys.history(vars.session_id, 50) })
    },
  })
}

// ─── CommHub / Postbox Hooks (preserved from original channelService) ─────────

export function usePostboxStatus() {
  return useQuery({
    queryKey: channelKeys.postbox(),
    queryFn: () => mcpCall<PostboxStatus>('comm', { action: 'postbox_status' }),
    retry: false,
    refetchInterval: 5000,
  })
}

export function useDeadLetters() {
  return useQuery({
    queryKey: channelKeys.deadLetters(),
    queryFn: () => mcpCall<ListDeadLettersResult>('comm', { action: 'list_dead_letters' }),
    retry: false,
  })
}

export function useRetryDeadLetter() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (args: RetryDeadLetterArgs) =>
      mcpCall<RetryDeadLetterResult>(
        'comm',
        { action: 'retry_dead_letter', ...(args as unknown as Record<string, unknown>) },
      ),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: channelKeys.deadLetters() })
    },
  })
}

export function useDiscardDeadLetter() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (args: DiscardDeadLetterArgs) =>
      mcpCall<DiscardDeadLetterResult>(
        'comm',
        { action: 'discard_dead_letter', ...(args as unknown as Record<string, unknown>) },
      ),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: channelKeys.deadLetters() })
    },
  })
}
