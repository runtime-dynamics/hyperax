/**
 * Progressive onboarding cards shown after the 4-step initial setup completes.
 * Each card is individually dismissible; dismissed state persists in localStorage.
 */

import { useState } from 'react'
import { Link } from 'react-router-dom'
import { X, ChevronDown, ChevronRight, Bot, CalendarClock, BookOpen, ExternalLink } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

// ─── Types ────────────────────────────────────────────────────────────────────

interface TutorialCard {
  id: string
  icon: React.ComponentType<{ className?: string }>
  title: string
  description: string
  body: React.ReactNode
  cta?: { label: string; href: string; external?: boolean }
}

// ─── Storage ─────────────────────────────────────────────────────────────────

const STORAGE_KEY = 'hyperax-onboarding-dismissed'

function loadDismissed(): Set<string> {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw) {
      const parsed = JSON.parse(raw) as unknown
      if (Array.isArray(parsed)) return new Set(parsed as string[])
    }
  } catch {
    // ignore
  }
  return new Set()
}

function saveDismissed(ids: Set<string>) {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(Array.from(ids)))
  } catch {
    // ignore
  }
}

// ─── Card definitions ─────────────────────────────────────────────────────────

const TUTORIAL_CARDS: TutorialCard[] = [
  {
    id: 'personas',
    icon: Bot,
    title: 'Build your team with Agents',
    description: 'Agents are named AI workers with specific roles, models, and system prompts.',
    body: (
      <ul className="text-xs text-muted-foreground space-y-1.5 list-disc list-inside">
        <li>Each agent can have a different LLM provider and model</li>
        <li>System prompts define how each agent behaves and communicates</li>
        <li>Role templates give you a head-start for common team structures</li>
        <li>Drag agents on the Org page to arrange your hierarchy</li>
      </ul>
    ),
    cta: { label: 'Manage Agents', href: '/org' },
  },
  {
    id: 'events-crons',
    icon: CalendarClock,
    title: 'Automate with Events & Cron',
    description: 'Trigger actions automatically based on system events or a schedule.',
    body: (
      <ul className="text-xs text-muted-foreground space-y-1.5 list-disc list-inside">
        <li>Event handlers react to nervous system events (e.g. pipeline.completed)</li>
        <li>Cron jobs run on a schedule — standard 5-field or shortcuts like @daily</li>
        <li>Actions include: promote event, route message, call webhook, or write log</li>
        <li>Combine with Workflows for multi-step automated pipelines</li>
      </ul>
    ),
    cta: { label: 'Open Workspaces', href: '/settings/workspaces' },
  },
  {
    id: 'learn-more',
    icon: BookOpen,
    title: 'Learn more about Hyperax',
    description: 'Explore guides, tutorials, and community resources to get the most out of the platform.',
    body: (
      <div className="text-xs text-muted-foreground space-y-2">
        <p>
          Hyperax is an agent operating system. Once your agents are configured, they can collaborate
          autonomously, share tasks, and build complex multi-agent workflows.
        </p>
        <p>
          Check out the documentation and community resources to explore advanced features like
          delegation chains, memory consolidation, and hybrid search.
        </p>
      </div>
    ),
    cta: { label: 'View Documentation', href: 'https://hyperax.dev/docs', external: true },
  },
]

// ─── SingleCard ───────────────────────────────────────────────────────────────

interface SingleCardProps {
  card: TutorialCard
  onDismiss: (id: string) => void
}

function SingleCard({ card, onDismiss }: SingleCardProps) {
  const [expanded, setExpanded] = useState(true)
  const Icon = card.icon

  return (
    <div className="rounded-lg border bg-card overflow-hidden">
      <div className="flex items-start gap-3 px-4 py-3">
        <div className="h-7 w-7 rounded-md bg-primary/10 text-primary flex items-center justify-center shrink-0 mt-0.5">
          <Icon className="h-4 w-4" />
        </div>
        <div className="flex-1 min-w-0">
          <button
            type="button"
            className="w-full flex items-start gap-2 text-left"
            onClick={() => setExpanded((p) => !p)}
          >
            <div className="flex-1 min-w-0">
              <p className="text-sm font-semibold leading-tight">{card.title}</p>
              <p className="text-xs text-muted-foreground mt-0.5">{card.description}</p>
            </div>
            {expanded ? (
              <ChevronDown className="h-3.5 w-3.5 text-muted-foreground shrink-0 mt-0.5" />
            ) : (
              <ChevronRight className="h-3.5 w-3.5 text-muted-foreground shrink-0 mt-0.5" />
            )}
          </button>
        </div>
        <button
          type="button"
          onClick={() => onDismiss(card.id)}
          className="h-6 w-6 rounded-sm flex items-center justify-center text-muted-foreground hover:text-foreground hover:bg-muted/50 transition-colors shrink-0"
          aria-label={`Dismiss "${card.title}" guide`}
        >
          <X className="h-3.5 w-3.5" />
        </button>
      </div>

      {expanded && (
        <div className="border-t bg-muted/20 px-4 py-3 space-y-3">
          {card.body}
          {card.cta && (
            card.cta.external ? (
              <a
                href={card.cta.href}
                target="_blank"
                rel="noopener noreferrer"
                className="inline-flex items-center gap-1.5 text-xs font-medium text-primary hover:underline"
              >
                {card.cta.label}
                <ExternalLink className="h-3 w-3" />
              </a>
            ) : (
              <Button size="sm" variant="outline" className="h-7 text-xs" asChild>
                <Link to={card.cta.href}>{card.cta.label}</Link>
              </Button>
            )
          )}
        </div>
      )}
    </div>
  )
}

// ─── ProgressiveOnboarding ────────────────────────────────────────────────────

interface ProgressiveOnboardingProps {
  /** Rendered only when all 4 setup steps are complete (personas exist). */
  className?: string
}

export function ProgressiveOnboarding({ className }: ProgressiveOnboardingProps) {
  const [dismissed, setDismissed] = useState<Set<string>>(loadDismissed)

  function handleDismiss(id: string) {
    setDismissed((prev) => {
      const next = new Set(prev)
      next.add(id)
      saveDismissed(next)
      return next
    })
  }

  const visibleCards = TUTORIAL_CARDS.filter((c) => !dismissed.has(c.id))

  if (visibleCards.length === 0) return null

  return (
    <div className={cn('space-y-2', className)}>
      <p className="text-xs font-medium text-muted-foreground uppercase tracking-wide px-1">
        Getting started guide
      </p>
      {visibleCards.map((card) => (
        <SingleCard key={card.id} card={card} onDismiss={handleDismiss} />
      ))}
    </div>
  )
}
