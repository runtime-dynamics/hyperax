import { useHierarchy, type HierarchyNode } from '@/services/commhubService'
import { GitBranch } from 'lucide-react'

function TreeNode({ node, depth = 0 }: { node: HierarchyNode; depth?: number }) {
  return (
    <li>
      <div
        className="flex items-center gap-1.5 py-1 text-sm"
        style={{ paddingLeft: `${depth * 16}px` }}
      >
        <GitBranch className="h-3.5 w-3.5 text-muted-foreground shrink-0" />
        <span className="font-mono text-xs">{node.agent_id}</span>
      </div>
      {node.children.length > 0 && (
        <ul>
          {node.children.map((child) => (
            <TreeNode key={child.agent_id} node={child} depth={depth + 1} />
          ))}
        </ul>
      )}
    </li>
  )
}

export function HierarchyTree() {
  const { data: hierarchy, isLoading, error } = useHierarchy()

  if (isLoading) return <p className="text-xs text-muted-foreground">Loading hierarchy...</p>
  if (error) return <p className="text-xs text-muted-foreground">Hierarchy unavailable</p>
  if (!hierarchy || hierarchy.length === 0) {
    return <p className="text-xs text-muted-foreground">No hierarchy defined.</p>
  }

  return (
    <ul className="space-y-0.5">
      {hierarchy.map((node) => (
        <TreeNode key={node.agent_id} node={node} />
      ))}
    </ul>
  )
}
