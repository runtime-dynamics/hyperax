import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { mcpCall } from '@/lib/mcp-client'

export interface EventHandler {
  id: string
  event_filter: string
  action: string
  target?: string
  created_at: string
}

export interface DomainEvent {
  id?: string
  type: string
  source: string
  target?: string
  data?: unknown
  sequence_id: number
  timestamp: string
}

export interface QueryDomainEventsArgs {
  event_type?: string
  source?: string
  limit?: number
}

export interface CreateEventHandlerArgs {
  event_filter: string
  action: string
  target?: string
}

export interface FireEventArgs {
  event_type: string
  source: string
  target?: string
  data?: Record<string, unknown>
}

export function useEventStats() {
  return useQuery({
    queryKey: ['event-stats'],
    queryFn: () => mcpCall<Record<string, number>>('event', { action: 'get_event_stats' }),
    refetchInterval: 5000,
    retry: false,
  })
}

export function useEventHandlers() {
  return useQuery({
    queryKey: ['event-handlers'],
    queryFn: () => mcpCall<EventHandler[]>('event', { action: 'list_event_handlers' }),
    retry: false,
  })
}

export function useDomainEvents(args: QueryDomainEventsArgs = {}) {
  return useQuery({
    queryKey: ['domain-events', args],
    queryFn: () =>
      mcpCall<DomainEvent[]>('event', { action: 'query_domain_events', ...(args as unknown as Record<string, unknown>) }),
    refetchInterval: 5000,
    retry: false,
  })
}

export function useCreateEventHandler() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (args: CreateEventHandlerArgs) =>
      mcpCall<{ id: string; message: string }>('event', { action: 'create_event_handler', ...(args as unknown as Record<string, unknown>) }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['event-handlers'] }),
  })
}

export function useFireEvent() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (args: FireEventArgs) =>
      mcpCall<Record<string, unknown>>('event', { action: 'fire_event', ...(args as unknown as Record<string, unknown>) }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['domain-events'] })
      void qc.invalidateQueries({ queryKey: ['event-stats'] })
    },
  })
}

export function useDeleteEventHandler() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) =>
      mcpCall<{ message: string }>('event', { action: 'delete_handler', id }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['event-handlers'] }),
  })
}
