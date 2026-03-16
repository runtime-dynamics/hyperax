import type React from 'react'
import { NavLink, Outlet, useLocation } from 'react-router-dom'
import {
  Server,
  FolderOpen,
  Activity,
  DollarSign,
  Puzzle,
  Settings,
  BookTemplate,
  Lock,
} from 'lucide-react'
import { cn } from '@/lib/utils'

interface SidebarItem {
  section: string
  label: string
  icon: React.ComponentType<{ className?: string }>
}

const sidebarItems: SidebarItem[] = [
  { section: 'providers', label: 'Providers', icon: Server },
  { section: 'role-templates', label: 'Role Templates', icon: BookTemplate },
  { section: 'workspaces', label: 'Workspaces', icon: FolderOpen },
  { section: 'security', label: 'Security', icon: Lock },
  { section: 'observability', label: 'Observability', icon: Activity },
  { section: 'budget', label: 'Budget', icon: DollarSign },
  { section: 'plugins', label: 'Plugins', icon: Puzzle },
  { section: 'system', label: 'System', icon: Settings },
]

export function SettingsLayout() {
  const location = useLocation()
  const section = location.pathname.split('/settings/')[1]?.split('/')[0] ?? 'providers'

  return (
    <div className="flex" style={{ minHeight: 'calc(100vh - 3.5rem)' }}>
      {/* Sidebar */}
      <aside className="w-[220px] shrink-0 border-r bg-card flex flex-col self-stretch">
        <div className="px-4 py-4 border-b">
          <h2 className="text-sm font-semibold text-foreground">Settings</h2>
        </div>
        <nav className="flex-1 overflow-y-auto py-2">
          {sidebarItems.map((item) => {
            const Icon = item.icon
            const isActive = section === item.section
            return (
              <NavLink
                key={item.section}
                to={`/settings/${item.section}`}
                className={cn(
                  'flex items-center gap-3 px-4 py-2 text-sm transition-colors',
                  isActive
                    ? 'bg-accent text-accent-foreground font-medium'
                    : 'text-muted-foreground hover:text-foreground hover:bg-accent/50',
                )}
              >
                <Icon className="h-4 w-4 shrink-0" />
                {item.label}
              </NavLink>
            )
          })}
        </nav>
      </aside>

      {/* Content area */}
      <div className="flex-1 min-w-0 overflow-y-auto">
        <Outlet />
      </div>
    </div>
  )
}
