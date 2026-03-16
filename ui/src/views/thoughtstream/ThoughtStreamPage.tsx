/**
 * Thought Stream — Live force-directed graph of agent communication.
 *
 * Nodes = agents (colored by trust level of their most recent message).
 * Edges = messages flowing between agents (animated, width/speed by priority).
 * Red-out overlay activates on interject.halt events from the nervous system.
 *
 * Layout: lightweight spring simulation via requestAnimationFrame — no d3 dependency.
 */

import { useState, useEffect, useRef, useCallback, useMemo } from 'react'
import { useEventStream } from '@/hooks/useEventStream'
import { useEventFilters, EVENT_FILTER_CATEGORIES } from '@/hooks/useEventFilters'
import { PageHeader } from '@/components/domain/page-header'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { RefreshCw, Radio, Wifi, WifiOff, AlertTriangle, Filter, ChevronDown, ChevronRight } from 'lucide-react'
import { cn } from '@/lib/utils'

// ─── Types ────────────────────────────────────────────────────────────────────

type TrustLevel = 'internal' | 'authorized' | 'external' | 'unknown'
type Priority = 'urgent' | 'normal' | 'background'

interface GraphNode {
  id: string
  x: number
  y: number
  vx: number
  vy: number
  lastTrust: TrustLevel
  messageCount: number
  isActive: boolean // pulsing when received a message recently
  activeUntil: number // epoch ms
}

interface GraphEdge {
  id: string
  from: string
  to: string
  trust: TrustLevel
  priority: Priority
  createdAt: number // epoch ms — used to fade out old edges
  animOffset: number // 0-1, animated forward each frame
}

interface NervousEventPayload {
  from?: string
  to?: string
  from_agent?: string
  to_agent?: string
  trust?: string
  priority?: string
  type?: string
}

// ─── Constants ────────────────────────────────────────────────────────────────

const EDGE_TTL_MS = 8_000
const ACTIVE_TTL_MS = 3_000
const MAX_EDGES = 60
const NODE_RADIUS = 22
const REPEL_DIST = 120
const SPRING_LEN = 160
const SPRING_K = 0.01
const REPEL_K = 800
const DAMPING = 0.85
const CENTER_K = 0.002

const trustColor: Record<TrustLevel, { stroke: string; fill: string; label: string }> = {
  internal: { stroke: '#22c55e', fill: '#16a34a', label: 'Internal' },
  authorized: { stroke: '#f59e0b', fill: '#d97706', label: 'Authorized' },
  external: { stroke: '#ef4444', fill: '#dc2626', label: 'External' },
  unknown: { stroke: '#6b7280', fill: '#4b5563', label: 'Unknown' },
}

const priorityAnimSpeed: Record<Priority, number> = {
  urgent: 0.04,
  normal: 0.02,
  background: 0.008,
}

const priorityStrokeWidth: Record<Priority, number> = {
  urgent: 3,
  normal: 2,
  background: 1,
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

function parseTrust(raw?: string): TrustLevel {
  const s = (raw ?? '').toLowerCase()
  if (s === 'internal' || s === '0') return 'internal'
  if (s === 'authorized' || s === '1') return 'authorized'
  if (s === 'external' || s === '2') return 'external'
  return 'unknown'
}

function parsePriority(raw?: string): Priority {
  const s = (raw ?? '').toLowerCase()
  if (s === 'urgent' || s === 'high') return 'urgent'
  if (s === 'background' || s === 'low') return 'background'
  return 'normal'
}

function randomPos(width: number, height: number): { x: number; y: number } {
  const margin = 60
  return {
    x: margin + Math.random() * (width - margin * 2),
    y: margin + Math.random() * (height - margin * 2),
  }
}

// ─── Force simulation (mutates nodes in-place) ─────────────────────────────────

function tickSimulation(
  nodes: GraphNode[],
  edges: GraphEdge[],
  width: number,
  height: number,
) {
  const cx = width / 2
  const cy = height / 2

  // Repulsion between every pair
  for (let i = 0; i < nodes.length; i++) {
    for (let j = i + 1; j < nodes.length; j++) {
      const a = nodes[i]
      const b = nodes[j]
      const dx = b.x - a.x
      const dy = b.y - a.y
      const dist = Math.sqrt(dx * dx + dy * dy) || 1
      if (dist < REPEL_DIST) {
        const force = (REPEL_K / (dist * dist))
        const fx = (dx / dist) * force
        const fy = (dy / dist) * force
        a.vx -= fx
        a.vy -= fy
        b.vx += fx
        b.vy += fy
      }
    }
  }

  // Spring attraction along edges
  const nodeMap = new Map(nodes.map((n) => [n.id, n]))
  for (const edge of edges) {
    const a = nodeMap.get(edge.from)
    const b = nodeMap.get(edge.to)
    if (!a || !b) continue
    const dx = b.x - a.x
    const dy = b.y - a.y
    const dist = Math.sqrt(dx * dx + dy * dy) || 1
    const force = (dist - SPRING_LEN) * SPRING_K
    const fx = (dx / dist) * force
    const fy = (dy / dist) * force
    a.vx += fx
    a.vy += fy
    b.vx -= fx
    b.vy -= fy
  }

  // Centre gravity + integrate
  for (const node of nodes) {
    node.vx += (cx - node.x) * CENTER_K
    node.vy += (cy - node.y) * CENTER_K
    node.vx *= DAMPING
    node.vy *= DAMPING
    node.x += node.vx
    node.y += node.vy
    // Boundary
    node.x = Math.max(NODE_RADIUS + 4, Math.min(width - NODE_RADIUS - 4, node.x))
    node.y = Math.max(NODE_RADIUS + 4, Math.min(height - NODE_RADIUS - 4, node.y))
  }
}

// ─── SVG rendering helpers ────────────────────────────────────────────────────

interface EdgeArrowProps {
  edge: GraphEdge
  fromNode: GraphNode
  toNode: GraphNode
  now: number
}

function EdgeArrow({ edge, fromNode, toNode, now }: EdgeArrowProps) {
  const dx = toNode.x - fromNode.x
  const dy = toNode.y - fromNode.y
  const dist = Math.sqrt(dx * dx + dy * dy) || 1
  // Trim endpoints to node radius
  const ux = dx / dist
  const uy = dy / dist
  const x1 = fromNode.x + ux * (NODE_RADIUS + 2)
  const y1 = fromNode.y + uy * (NODE_RADIUS + 2)
  const x2 = toNode.x - ux * (NODE_RADIUS + 6)
  const y2 = toNode.y - uy * (NODE_RADIUS + 6)

  const age = now - edge.createdAt
  const opacity = Math.max(0, 1 - age / EDGE_TTL_MS)

  // Animated dot along the edge
  const dotT = edge.animOffset
  const dotX = x1 + (x2 - x1) * dotT
  const dotY = y1 + (y2 - y1) * dotT

  const color = trustColor[edge.trust].stroke
  const sw = priorityStrokeWidth[edge.priority]

  return (
    <g opacity={opacity}>
      {/* Edge line */}
      <line
        x1={x1}
        y1={y1}
        x2={x2}
        y2={y2}
        stroke={color}
        strokeWidth={sw}
        strokeDasharray={edge.priority === 'background' ? '4 4' : undefined}
        markerEnd={`url(#arrow-${edge.trust})`}
      />
      {/* Animated travelling dot */}
      <circle cx={dotX} cy={dotY} r={sw + 1} fill={color} opacity={0.9} />
    </g>
  )
}

interface NodeCircleProps {
  node: GraphNode
  now: number
  isAndon: boolean
}

function NodeCircle({ node, now, isAndon }: NodeCircleProps) {
  const colors = trustColor[node.lastTrust]
  const isActive = node.activeUntil > now
  const scale = isActive ? 1.15 : 1

  return (
    <g transform={`translate(${node.x},${node.y})`}>
      {/* Active pulse ring */}
      {isActive && !isAndon && (
        <circle
          r={NODE_RADIUS + 6}
          fill="none"
          stroke={colors.stroke}
          strokeWidth={2}
          opacity={0.4}
          className="animate-ping"
          style={{ transformOrigin: '0 0' }}
        />
      )}
      {/* Main circle */}
      <circle
        r={NODE_RADIUS}
        fill={isAndon ? '#7f1d1d' : colors.fill}
        stroke={isAndon ? '#ef4444' : colors.stroke}
        strokeWidth={2}
        transform={`scale(${scale})`}
        style={{ transition: 'transform 0.2s ease', transformOrigin: '0 0' }}
      />
      {/* Label */}
      <text
        textAnchor="middle"
        dominantBaseline="middle"
        fill="white"
        fontSize={9}
        fontWeight={600}
        style={{ pointerEvents: 'none', userSelect: 'none' }}
      >
        {node.id.length > 10 ? node.id.slice(0, 9) + '…' : node.id}
      </text>
      {/* Message count badge */}
      {node.messageCount > 0 && (
        <text
          x={NODE_RADIUS - 4}
          y={-(NODE_RADIUS - 4)}
          textAnchor="middle"
          dominantBaseline="middle"
          fill="white"
          fontSize={8}
          fontWeight={700}
          style={{ pointerEvents: 'none', userSelect: 'none' }}
        >
          {node.messageCount > 99 ? '99+' : node.messageCount}
        </text>
      )}
    </g>
  )
}

// ─── ThoughtStreamPage ────────────────────────────────────────────────────────

export default function ThoughtStreamPage() {
  const svgRef = useRef<SVGSVGElement>(null)
  const nodesRef = useRef<Map<string, GraphNode>>(new Map())
  const edgesRef = useRef<GraphEdge[]>([])
  const animFrameRef = useRef<number>(0)
  const [renderTick, setRenderTick] = useState(0)
  const [isAndon, setIsAndon] = useState(false)
  const [andonSource, setAndonSource] = useState<string | null>(null)
  const [filterPanelOpen, setFilterPanelOpen] = useState(false)
  const { enabledIds, patterns, toggleCategory, enableAll, disableAll, allEnabled } = useEventFilters()
  const { connected, events, sendPatterns } = useEventStream({ patterns })
  const processedRef = useRef<Set<number>>(new Set())
  const [dimensions, setDimensions] = useState({ width: 800, height: 500 })

  // Sync filter pattern changes to the live WS connection
  useEffect(() => {
    sendPatterns(patterns)
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [patterns.join(','), sendPatterns])

  // Resize observer
  useEffect(() => {
    const el = svgRef.current?.parentElement
    if (!el) return
    const ro = new ResizeObserver((entries) => {
      const entry = entries[0]
      if (entry) {
        setDimensions({
          width: Math.max(400, entry.contentRect.width),
          height: Math.max(300, entry.contentRect.height),
        })
      }
    })
    ro.observe(el)
    return () => ro.disconnect()
  }, [])

  // Process events from event stream
  useEffect(() => {
    if (events.length === 0) return
    const latest = events[events.length - 1]
    if (processedRef.current.has(latest.sequence_id)) return
    processedRef.current.add(latest.sequence_id)

    const { type, payload } = latest
    const now = Date.now()

    // Andon cord detection
    if (type === 'interject.halt' || type === 'interject.safe_mode') {
      setIsAndon(true)
      const p = payload as NervousEventPayload
      setAndonSource(p?.from_agent ?? p?.from ?? latest.source ?? null)
      return
    }
    if (type === 'interject.resolved' || type === 'interject.resume') {
      setIsAndon(false)
      setAndonSource(null)
      return
    }

    // Comm messages
    if (!type.startsWith('comm.')) return
    const p = payload as NervousEventPayload
    const from = p?.from_agent ?? p?.from
    const to = p?.to_agent ?? p?.to

    if (!from || !to) return

    const trust = parseTrust(p?.trust)
    const priority = parsePriority(p?.priority)

    const { width, height } = dimensions

    // Ensure nodes exist
    for (const id of [from, to]) {
      if (!nodesRef.current.has(id)) {
        const pos = randomPos(width, height)
        nodesRef.current.set(id, {
          id,
          ...pos,
          vx: 0,
          vy: 0,
          lastTrust: trust,
          messageCount: 0,
          isActive: true,
          activeUntil: now + ACTIVE_TTL_MS,
        })
      }
    }

    // Update source node
    const fromNode = nodesRef.current.get(from)!
    fromNode.lastTrust = trust
    fromNode.messageCount += 1
    fromNode.isActive = true
    fromNode.activeUntil = now + ACTIVE_TTL_MS

    const toNode = nodesRef.current.get(to)!
    toNode.isActive = true
    toNode.activeUntil = now + ACTIVE_TTL_MS

    // Add edge
    const edge: GraphEdge = {
      id: `${from}->${to}-${now}`,
      from,
      to,
      trust,
      priority,
      createdAt: now,
      animOffset: 0,
    }
    edgesRef.current.push(edge)

    // Trim old edges
    if (edgesRef.current.length > MAX_EDGES) {
      edgesRef.current = edgesRef.current.slice(-MAX_EDGES)
    }
  }, [events, dimensions])

  // Animation loop
  const animate = useCallback(() => {
    const now = Date.now()
    const { width, height } = dimensions
    const nodes = Array.from(nodesRef.current.values())

    // Tick simulation
    if (nodes.length > 0) {
      tickSimulation(nodes, edgesRef.current, width, height)
      for (const node of nodes) {
        nodesRef.current.set(node.id, node)
      }
    }

    // Advance edge animation offsets, prune expired edges
    edgesRef.current = edgesRef.current
      .filter((e) => now - e.createdAt < EDGE_TTL_MS)
      .map((e) => ({
        ...e,
        animOffset: (e.animOffset + priorityAnimSpeed[e.priority]) % 1,
      }))

    setRenderTick((t) => t + 1)
    animFrameRef.current = requestAnimationFrame(animate)
  }, [dimensions])

  useEffect(() => {
    animFrameRef.current = requestAnimationFrame(animate)
    return () => cancelAnimationFrame(animFrameRef.current)
  }, [animate])

  const nodes = useMemo(
    () => Array.from(nodesRef.current.values()),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [renderTick],
  )

  const edges = useMemo(
    () => edgesRef.current,
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [renderTick],
  )

  const nodeMap = useMemo(
    () => new Map(nodes.map((n) => [n.id, n])),
    [nodes],
  )

  const now = Date.now()

  function handleReset() {
    nodesRef.current.clear()
    edgesRef.current = []
    setIsAndon(false)
    setAndonSource(null)
  }

  return (
    <div className="p-6 space-y-4 flex flex-col" style={{ minHeight: 'calc(100vh - 3.5rem)' }}>
      <PageHeader
        title="Thought Stream"
        description="Live force-directed graph of agent communication. Edges animate by priority; colors indicate trust level."
      >
        <div className="flex items-center gap-3">
          {/* Connection status */}
          <span
            className={cn(
              'inline-flex items-center gap-1.5 text-xs font-medium',
              connected ? 'text-green-600 dark:text-green-400' : 'text-muted-foreground',
            )}
          >
            {connected ? <Wifi className="h-3.5 w-3.5" /> : <WifiOff className="h-3.5 w-3.5" />}
            {connected ? 'Live' : 'Disconnected'}
          </span>
          <Button size="sm" variant="outline" onClick={handleReset}>
            <RefreshCw className="h-4 w-4 mr-1.5" />
            Reset
          </Button>
        </div>
      </PageHeader>

      {/* Trust level legend */}
      <div className="flex items-center gap-3 flex-wrap text-xs">
        {(Object.entries(trustColor) as [TrustLevel, typeof trustColor[TrustLevel]][]).map(
          ([trust, colors]) => (
            <span key={trust} className="flex items-center gap-1.5">
              <span
                className="inline-block h-2.5 w-2.5 rounded-full"
                style={{ backgroundColor: colors.fill, border: `2px solid ${colors.stroke}` }}
              />
              {colors.label}
            </span>
          ),
        )}
        <span className="flex items-center gap-1.5 ml-2 text-muted-foreground border-l pl-3">
          <span className="inline-block h-0.5 w-6 bg-current opacity-80" />
          Urgent
        </span>
        <span className="flex items-center gap-1.5 text-muted-foreground">
          <span className="inline-block h-0.5 w-6 bg-current opacity-50" style={{ backgroundImage: 'repeating-linear-gradient(to right, currentColor 0, currentColor 4px, transparent 4px, transparent 8px)' }} />
          Background
        </span>
      </div>

      {/* Event filter panel */}
      <div className="rounded-lg border bg-card">
        <button
          type="button"
          className="w-full flex items-center gap-2 px-4 py-2.5 text-sm hover:bg-muted/40 transition-colors text-left"
          onClick={() => setFilterPanelOpen((p) => !p)}
          aria-expanded={filterPanelOpen}
        >
          <Filter className="h-3.5 w-3.5 text-muted-foreground shrink-0" />
          <span className="font-medium">Event Filters</span>
          {!allEnabled && (
            <Badge variant="secondary" className="text-xs ml-1">
              {enabledIds.size}/{EVENT_FILTER_CATEGORIES.length} active
            </Badge>
          )}
          {filterPanelOpen ? (
            <ChevronDown className="h-3.5 w-3.5 text-muted-foreground ml-auto" />
          ) : (
            <ChevronRight className="h-3.5 w-3.5 text-muted-foreground ml-auto" />
          )}
        </button>

        {filterPanelOpen && (
          <div className="border-t px-4 py-3 space-y-3">
            <div className="flex items-center gap-2">
              <p className="text-xs text-muted-foreground flex-1">
                Select which event categories the server streams to this client.
              </p>
              <Button size="sm" variant="ghost" className="h-6 text-xs" onClick={enableAll}>
                All
              </Button>
              <Button size="sm" variant="ghost" className="h-6 text-xs" onClick={disableAll}>
                None
              </Button>
            </div>
            <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-5 gap-2">
              {EVENT_FILTER_CATEGORIES.map((cat) => {
                const enabled = enabledIds.has(cat.id)
                return (
                  <button
                    key={cat.id}
                    type="button"
                    title={cat.description}
                    onClick={() => toggleCategory(cat.id)}
                    className={cn(
                      'flex items-center gap-1.5 rounded-md border px-2.5 py-1.5 text-xs font-medium transition-colors text-left',
                      enabled
                        ? 'border-primary/50 bg-primary/10 text-primary'
                        : 'border-border text-muted-foreground hover:bg-muted/40',
                    )}
                    aria-pressed={enabled}
                  >
                    <span
                      className={cn(
                        'h-1.5 w-1.5 rounded-full shrink-0',
                        enabled ? 'bg-primary' : 'bg-muted-foreground/40',
                      )}
                    />
                    {cat.label}
                  </button>
                )
              })}
            </div>
          </div>
        )}
      </div>

      {/* Andon cord overlay */}
      {isAndon && (
        <div className="flex items-center gap-3 rounded-md border border-red-500 bg-red-50 dark:bg-red-950/30 px-4 py-3 text-sm text-red-700 dark:text-red-300">
          <AlertTriangle className="h-4 w-4 shrink-0 animate-pulse" />
          <div>
            <span className="font-semibold">Andon Cord Pulled</span>
            {andonSource && <span className="ml-1 text-xs opacity-80">by {andonSource}</span>}
            <span className="ml-2 text-xs">— System halted. All agent communication suspended.</span>
          </div>
          <Button
            size="sm"
            variant="outline"
            className="ml-auto border-red-500 text-red-700 hover:bg-red-100 dark:text-red-300 dark:hover:bg-red-900/30"
            onClick={() => { setIsAndon(false); setAndonSource(null) }}
          >
            Dismiss
          </Button>
        </div>
      )}

      {/* Main graph canvas */}
      <div
        className={cn(
          'relative flex-1 rounded-lg border bg-card overflow-hidden',
          isAndon && 'border-red-500 ring-2 ring-red-500/30',
        )}
        style={{ minHeight: 400 }}
      >
        {nodes.length === 0 && (
          <div className="absolute inset-0 flex flex-col items-center justify-center text-center gap-3 text-muted-foreground">
            <Radio className="h-8 w-8 opacity-30" />
            <p className="text-sm">Waiting for agent communication events…</p>
            <p className="text-xs opacity-60">Messages will appear here as agents interact.</p>
          </div>
        )}

        {/* Red-out overlay for Andon */}
        {isAndon && (
          <div className="absolute inset-0 bg-red-500/10 pointer-events-none z-10" />
        )}

        <svg
          ref={svgRef}
          width="100%"
          height="100%"
          className="block"
          style={{ minHeight: 400 }}
        >
          <defs>
            {(Object.entries(trustColor) as [TrustLevel, typeof trustColor[TrustLevel]][]).map(
              ([trust, colors]) => (
                <marker
                  key={trust}
                  id={`arrow-${trust}`}
                  viewBox="0 0 10 10"
                  refX="9"
                  refY="5"
                  markerWidth={6}
                  markerHeight={6}
                  orient="auto-start-reverse"
                >
                  <path d="M 0 0 L 10 5 L 0 10 z" fill={colors.stroke} />
                </marker>
              ),
            )}
          </defs>

          {/* Edges */}
          {edges.map((edge) => {
            const fromNode = nodeMap.get(edge.from)
            const toNode = nodeMap.get(edge.to)
            if (!fromNode || !toNode) return null
            return (
              <EdgeArrow
                key={edge.id}
                edge={edge}
                fromNode={fromNode}
                toNode={toNode}
                now={now}
              />
            )
          })}

          {/* Nodes */}
          {nodes.map((node) => (
            <NodeCircle key={node.id} node={node} now={now} isAndon={isAndon} />
          ))}
        </svg>
      </div>

      {/* Stats bar */}
      {nodes.length > 0 && (
        <div className="flex items-center gap-4 text-xs text-muted-foreground">
          <span>{nodes.length} agent{nodes.length !== 1 ? 's' : ''}</span>
          <span>{edges.length} active edge{edges.length !== 1 ? 's' : ''}</span>
          <span>{nodes.reduce((s, n) => s + n.messageCount, 0)} total messages</span>
        </div>
      )}
    </div>
  )
}
