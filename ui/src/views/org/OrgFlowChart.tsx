import { useMemo, useCallback, useRef, useEffect } from 'react'
import {
  ReactFlow,
  Background,
  Controls,
  MiniMap,
  type Node,
  type Edge,
  BackgroundVariant,
  MarkerType,
  type ColorMode,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'

import { AgentNode, type AgentNodeData } from './AgentNode'
import { getLayoutedElements } from './layout'
import type { HierarchyNode, RuntimeStateSummary } from '@/services/orgService'
import type { Task } from '@/services/taskService'

// ─── Node types registry ─────────────────────────────────────────────────────

const nodeTypes = {
  agent: AgentNode,
}

// ─── Hierarchy → Nodes/Edges conversion ──────────────────────────────────────

function flattenHierarchy(
  roots: HierarchyNode[],
  stateMap: Map<string, RuntimeStateSummary>,
  roleNameMap: Map<string, string>,
  tasksByAgent: Map<string, Task[]>,
  inboxByAgent: Map<string, number>,
  selectedId: string | null,
  onSelect: (agentId: string) => void,
  onAddChild: (parentId: string) => void,
  onToggleFavorite?: (agentId: string, isFavorite: boolean) => void,
  favoriteIds?: Set<string>,
  onStartChat?: (agentId: string) => void,
  onResetState?: (agentId: string) => void,
  onDeleteAgent?: (agentId: string) => void,
  disabledProviderAgentIds?: Set<string>,
  providerKindByAgentId?: Map<string, string>,
  onOpenActivity?: (agentId: string) => void,
  activeAgentId?: string | null,
  activeToolName?: string | null,
): { nodes: Node[]; edges: Edge[] } {
  const nodes: Node[] = []
  const edges: Edge[] = []

  function walk(node: HierarchyNode, parentId?: string) {
    const agent = stateMap.get(node.agent_id) ?? {
      agent_id: node.agent_id,
      status: 'unknown',
    }
    const isThisAgentActive = activeAgentId === node.agent_id
    const nodeData: AgentNodeData = {
      agent: agent as RuntimeStateSummary,
      roleName: (agent as RuntimeStateSummary).role_template_id
        ? roleNameMap.get((agent as RuntimeStateSummary).role_template_id!)
        : undefined,
      tasks: tasksByAgent.get(node.agent_id) ?? [],
      inboxSize: inboxByAgent.get(node.agent_id) ?? 0,
      selected: selectedId === node.agent_id,
      onSelect,
      onAddChild,
      onToggleFavorite,
      isFavorite: favoriteIds?.has(node.agent_id) ?? false,
      onStartChat,
      onResetState,
      onDeleteAgent,
      onOpenActivity,
      isThinking: isThisAgentActive,
      currentTool: isThisAgentActive ? activeToolName : null,
      isProviderDisabled: disabledProviderAgentIds?.has(node.agent_id) ?? false,
      providerKind: providerKindByAgentId?.get(node.agent_id),
    }

    nodes.push({
      id: node.agent_id,
      type: 'agent',
      data: nodeData as unknown as Record<string, unknown>,
      position: { x: 0, y: 0 },
    })

    if (parentId) {
      edges.push({
        id: `${parentId}->${node.agent_id}`,
        source: parentId,
        target: node.agent_id,
        type: 'smoothstep',
        animated: false,
        style: { strokeWidth: 1.5 },
        markerEnd: {
          type: MarkerType.ArrowClosed,
          width: 14,
          height: 14,
        },
      })
    }

    for (const child of node.children) {
      walk(child, node.agent_id)
    }
  }

  for (const root of roots) {
    walk(root)
  }

  return getLayoutedElements(nodes, edges)
}

interface OrgFlowChartProps {
  roots: HierarchyNode[]
  stateMap: Map<string, RuntimeStateSummary>
  selectedId: string | null
  onSelect: (agentId: string) => void
  onAddChild: (parentId: string) => void
  onDrop: (draggedId: string, targetId: string) => void
  roleNameMap: Map<string, string>
  tasksByAgent: Map<string, Task[]>
  inboxByAgent: Map<string, number>
  onToggleFavorite?: (agentId: string, isFavorite: boolean) => void
  favoriteIds?: Set<string>
  onStartChat?: (agentId: string) => void
  onResetState?: (agentId: string) => void
  onDeleteAgent?: (agentId: string) => void
  disabledProviderAgentIds?: Set<string>
  providerKindByAgentId?: Map<string, string>
  onOpenActivity?: (agentId: string) => void
  activeAgentId?: string | null
  activeToolName?: string | null
}

export function OrgFlowChart({
  roots,
  stateMap,
  selectedId,
  onSelect,
  onAddChild,
  onDrop,
  roleNameMap,
  tasksByAgent,
  inboxByAgent,
  onToggleFavorite,
  favoriteIds,
  onStartChat,
  onResetState,
  onDeleteAgent,
  disabledProviderAgentIds,
  providerKindByAgentId,
  onOpenActivity,
  activeAgentId,
  activeToolName,
}: OrgFlowChartProps) {
  // Track stateMap version: increment whenever the map content changes so that
  // useMemo sees a new dep and rebuilds nodes even if the Map reference is stable.
  const stateMapVersionRef = useRef(0)
  const prevStatesRef = useRef('')
  useEffect(() => {
    const serialized = JSON.stringify(
      Array.from(stateMap.entries()).map(([k, v]) => [k, v.status, v.last_active_at, v.tool_calls]),
    )
    if (serialized !== prevStatesRef.current) {
      prevStatesRef.current = serialized
      stateMapVersionRef.current += 1
    }
  })
  const stateMapVersion = stateMapVersionRef.current

  const { nodes, edges } = useMemo(
    () =>
      flattenHierarchy(
        roots,
        stateMap,
        roleNameMap,
        tasksByAgent,
        inboxByAgent,
        selectedId,
        onSelect,
        onAddChild,
        onToggleFavorite,
        favoriteIds,
        onStartChat,
        onResetState,
        onDeleteAgent,
        disabledProviderAgentIds,
        providerKindByAgentId,
        onOpenActivity,
        activeAgentId,
        activeToolName,
      ),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [roots, stateMap, roleNameMap, tasksByAgent, inboxByAgent, selectedId, onSelect, onAddChild, onToggleFavorite, favoriteIds, onStartChat, onResetState, onDeleteAgent, disabledProviderAgentIds, providerKindByAgentId, onOpenActivity, activeAgentId, activeToolName, stateMapVersion],
  )

  // Handle node drag-and-drop for reparenting
  const onNodeDragStop = useCallback(
    (_event: React.MouseEvent, node: Node) => {
      const targetNode = nodes.find(
        (n) =>
          n.id !== node.id &&
          node.position.x > n.position.x - 20 &&
          node.position.x < n.position.x + 200 &&
          node.position.y > n.position.y - 20 &&
          node.position.y < n.position.y + 110,
      )
      if (targetNode) {
        onDrop(node.id, targetNode.id)
      }
    },
    [nodes, onDrop],
  )

  // Detect dark mode
  const colorMode: ColorMode =
    typeof document !== 'undefined' && document.documentElement.classList.contains('dark')
      ? 'dark'
      : 'light'

  return (
    <div className="w-full h-full">
      <ReactFlow
        nodes={nodes}
        edges={edges}
        onNodeDragStop={onNodeDragStop}
        nodeTypes={nodeTypes}
        fitView
        fitViewOptions={{ padding: 0.3 }}
        minZoom={0.1}
        maxZoom={2}
        colorMode={colorMode}
        proOptions={{ hideAttribution: true }}
        nodesDraggable={true}
        nodesConnectable={false}
        elementsSelectable={false}
        panOnScroll
        zoomOnScroll
      >
        <Background variant={BackgroundVariant.Dots} gap={20} size={1} />
        <Controls showInteractive={false} />
        <MiniMap
          nodeStrokeWidth={3}
          zoomable
          pannable
          className="!bg-card !border !border-border !rounded-md"
        />
      </ReactFlow>
    </div>
  )
}
