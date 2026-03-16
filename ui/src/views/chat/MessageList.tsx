import { useEffect, useRef, type ReactNode } from 'react'
import { MessageSquare } from 'lucide-react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { cn } from '@/lib/utils'
import type { CommLogEntry } from '@/services/commhubService'
import { DelegationTrace, parseDelegationChain } from './DelegationTrace'

const OPERATOR_ID = 'operator'

interface MessageListProps {
  messages: CommLogEntry[]
  currentAgentId: string
  children?: ReactNode
}

function formatTime(isoString: string): string {
  try {
    return new Date(isoString).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  } catch {
    return ''
  }
}

/**
 * Extract the displayable text from a message entry.
 * If the content is JSON (e.g. a structured delegation payload), try to surface
 * a human-readable `message`, `text`, or `response` field. Otherwise return as-is.
 */
function getDisplayContent(entry: CommLogEntry): string {
  if (!entry.content.startsWith('{')) return entry.content
  try {
    const parsed = JSON.parse(entry.content) as Record<string, unknown>
    // Prefer explicit text fields over the raw JSON blob
    for (const key of ['message', 'text', 'response', 'content', 'query']) {
      if (typeof parsed[key] === 'string' && (parsed[key] as string).length > 0) {
        return parsed[key] as string
      }
    }
  } catch {
    // ignore
  }
  return entry.content
}

function ChatBubble({ entry }: { entry: CommLogEntry }) {
  const isFromOperator = entry.from_agent === OPERATOR_ID
  const delegationChain = parseDelegationChain(entry)
  const displayContent = getDisplayContent(entry)

  return (
    <div
      className={cn(
        'flex flex-col gap-1 max-w-[75%]',
        isFromOperator ? 'self-end items-end' : 'self-start items-start',
      )}
    >
      <div
        className={cn(
          'chat-markdown rounded-2xl px-4 py-2 text-sm leading-relaxed break-words',
          isFromOperator
            ? 'bg-primary text-primary-foreground rounded-br-sm'
            : 'bg-muted text-foreground rounded-bl-sm',
          delegationChain && 'rounded-b-md',
        )}
      >
        <ReactMarkdown remarkPlugins={[remarkGfm]}>{displayContent}</ReactMarkdown>
      </div>

      {/* Delegation trace — shown below the bubble */}
      {delegationChain && (
        <DelegationTrace
          chain={delegationChain}
          className={cn(
            'w-full',
            isFromOperator ? 'self-end' : 'self-start',
          )}
        />
      )}

      <span className="text-xs text-muted-foreground px-1">
        {formatTime(entry.created_at)}
      </span>
    </div>
  )
}

export function MessageList({ messages, currentAgentId: _currentAgentId, children }: MessageListProps) {
  const bottomRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [messages.length])

  if (messages.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center h-full text-center gap-3 text-muted-foreground py-16">
        <MessageSquare className="h-8 w-8 opacity-30" />
        <p className="text-sm">No messages yet. Send one to start a conversation.</p>
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-3 py-4">
      {messages.map((entry) => (
        <ChatBubble key={entry.id} entry={entry} />
      ))}
      {children}
      <div ref={bottomRef} />
    </div>
  )
}
