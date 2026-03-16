import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { mcpCall } from '@/lib/mcp-client'

// ─── Types ────────────────────────────────────────────────────────────────────

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

// ─── Query Keys ───────────────────────────────────────────────────────────────

export const channelKeys = {
  postbox: () => ['channels', 'postbox'] as const,
  deadLetters: () => ['channels', 'dead-letters'] as const,
}

// ─── Hooks ────────────────────────────────────────────────────────────────────

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
