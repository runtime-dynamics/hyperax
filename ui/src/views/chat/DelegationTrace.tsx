/**
 * DelegationTrace — Collapsible inline panel showing agent delegation chain.
 *
 * Renders when a message carries delegation metadata (JSON content with a
 * `delegation_chain` or `delegated_to` field), or when a message was forwarded
 * between multiple agents (direction === 'delegation').
 */

import { useState } from 'react'
import { ChevronDown, ChevronRight, ArrowRight, GitBranch, Clock } from 'lucide-react'
import { cn } from '@/lib/utils'
import type { CommLogEntry } from '@/services/commhubService'

// ─── Types ────────────────────────────────────────────────────────────────────

export interface DelegationStep {
  from: string
  to: string
  query: string
  response?: string
  latency_ms?: number
  trust?: string
}

export interface DelegationChain {
  steps: DelegationStep[]
  total_latency_ms?: number
}

// ─── Parsing ─────────────────────────────────────────────────────────────────

/**
 * Attempt to extract a delegation chain from a message's content.
 * The backend may encode this as a JSON payload with a `delegation_chain` key,
 * or as a structured content_type of 'delegation'.
 */
export function parseDelegationChain(entry: CommLogEntry): DelegationChain | null {
  // Explicit delegation content_type
  if (entry.content_type === 'delegation' || entry.direction === 'delegation') {
    try {
      const parsed = JSON.parse(entry.content) as {
        delegation_chain?: DelegationStep[]
        steps?: DelegationStep[]
        delegated_to?: string
        query?: string
        response?: string
        total_latency_ms?: number
      }
      const steps = parsed.delegation_chain ?? parsed.steps
      if (Array.isArray(steps) && steps.length > 0) {
        return { steps, total_latency_ms: parsed.total_latency_ms }
      }
      // Single-hop delegation: delegated_to is set
      if (parsed.delegated_to) {
        return {
          steps: [
            {
              from: entry.from_agent,
              to: parsed.delegated_to,
              query: parsed.query ?? entry.content,
              response: parsed.response,
              trust: entry.trust,
            },
          ],
          total_latency_ms: parsed.total_latency_ms,
        }
      }
    } catch {
      // Not JSON — treat entire content as a single delegation step
      return {
        steps: [
          {
            from: entry.from_agent,
            to: entry.to_agent,
            query: entry.content,
            trust: entry.trust,
          },
        ],
      }
    }
  }

  // Try parsing any JSON content for embedded delegation metadata
  if (entry.content.startsWith('{')) {
    try {
      const parsed = JSON.parse(entry.content) as {
        delegation_chain?: DelegationStep[]
        delegated_to?: string
        query?: string
        response?: string
        total_latency_ms?: number
      }
      if (parsed.delegation_chain && Array.isArray(parsed.delegation_chain) && parsed.delegation_chain.length > 0) {
        return { steps: parsed.delegation_chain, total_latency_ms: parsed.total_latency_ms }
      }
    } catch {
      // Not a delegation message
    }
  }

  return null
}

// ─── TrustBadge ───────────────────────────────────────────────────────────────

function TrustBadge({ trust }: { trust?: string }) {
  if (!trust) return null
  const t = trust.toLowerCase()
  const cls =
    t === 'internal'
      ? 'bg-green-100 text-green-700 dark:bg-green-900/40 dark:text-green-300'
      : t === 'authorized'
        ? 'bg-amber-100 text-amber-700 dark:bg-amber-900/40 dark:text-amber-300'
        : 'bg-red-100 text-red-700 dark:bg-red-900/40 dark:text-red-300'

  return (
    <span className={cn('inline-flex items-center rounded-full px-1.5 py-0.5 text-[10px] font-medium capitalize', cls)}>
      {trust}
    </span>
  )
}

// ─── DelegationStep row ───────────────────────────────────────────────────────

interface DelegationStepRowProps {
  step: DelegationStep
  index: number
}

function DelegationStepRow({ step, index }: DelegationStepRowProps) {
  const [showResponse, setShowResponse] = useState(false)

  return (
    <div className="pl-3 border-l-2 border-border space-y-1.5 py-1.5">
      {/* Step header */}
      <div className="flex items-center gap-1.5 flex-wrap text-[11px]">
        <span className="font-mono font-medium text-muted-foreground">#{index + 1}</span>
        <span className="font-medium text-foreground">{step.from}</span>
        <ArrowRight className="h-3 w-3 text-muted-foreground shrink-0" />
        <span className="font-medium text-foreground">{step.to}</span>
        {step.trust && <TrustBadge trust={step.trust} />}
        {step.latency_ms !== undefined && (
          <span className="flex items-center gap-0.5 text-muted-foreground ml-auto">
            <Clock className="h-3 w-3" />
            {step.latency_ms}ms
          </span>
        )}
      </div>

      {/* Query */}
      <p className="text-[11px] text-muted-foreground bg-muted/40 rounded px-2 py-1 font-mono whitespace-pre-wrap break-words max-h-24 overflow-y-auto">
        {step.query}
      </p>

      {/* Response (collapsible) */}
      {step.response && (
        <div>
          <button
            type="button"
            className="flex items-center gap-1 text-[10px] text-muted-foreground hover:text-foreground transition-colors"
            onClick={() => setShowResponse((v) => !v)}
            aria-expanded={showResponse}
          >
            {showResponse ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
            {showResponse ? 'Hide response' : 'Show response'}
          </button>
          {showResponse && (
            <p className="mt-1 text-[11px] text-foreground bg-muted/20 rounded px-2 py-1 font-mono whitespace-pre-wrap break-words max-h-32 overflow-y-auto">
              {step.response}
            </p>
          )}
        </div>
      )}
    </div>
  )
}

// ─── DelegationTrace ──────────────────────────────────────────────────────────

interface DelegationTraceProps {
  chain: DelegationChain
  className?: string
}

export function DelegationTrace({ chain, className }: DelegationTraceProps) {
  const [expanded, setExpanded] = useState(false)

  const stepCount = chain.steps.length
  const agents = [...new Set(chain.steps.flatMap((s) => [s.from, s.to]))]

  return (
    <div className={cn('mt-1.5 rounded-md border border-border/60 bg-muted/20 text-xs overflow-hidden', className)}>
      {/* Header toggle */}
      <button
        type="button"
        className="w-full flex items-center gap-2 px-3 py-2 text-[11px] font-medium text-muted-foreground hover:text-foreground hover:bg-muted/40 transition-colors"
        onClick={() => setExpanded((v) => !v)}
        aria-expanded={expanded}
      >
        <GitBranch className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        <span>
          Delegation chain — {stepCount} hop{stepCount !== 1 ? 's' : ''} across {agents.length} agent{agents.length !== 1 ? 's' : ''}
        </span>
        {chain.total_latency_ms !== undefined && (
          <span className="ml-auto flex items-center gap-0.5 text-muted-foreground/70">
            <Clock className="h-3 w-3" />
            {chain.total_latency_ms}ms total
          </span>
        )}
        {expanded ? (
          <ChevronDown className="h-3.5 w-3.5 shrink-0 ml-1" />
        ) : (
          <ChevronRight className="h-3.5 w-3.5 shrink-0 ml-1" />
        )}
      </button>

      {/* Expanded steps */}
      {expanded && (
        <div className="px-3 pb-3 space-y-0.5">
          {/* Agent path summary */}
          <div className="flex items-center gap-1 flex-wrap py-1.5 text-[10px] text-muted-foreground border-b mb-2">
            {agents.map((agent, i) => (
              <span key={agent} className="flex items-center gap-1">
                <span className="font-medium text-foreground font-mono">{agent}</span>
                {i < agents.length - 1 && <ArrowRight className="h-2.5 w-2.5" />}
              </span>
            ))}
          </div>

          {chain.steps.map((step, i) => (
            <DelegationStepRow key={i} step={step} index={i} />
          ))}
        </div>
      )}
    </div>
  )
}
