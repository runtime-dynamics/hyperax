import { Link, useLocation } from 'react-router-dom'
import { Bell } from 'lucide-react'
import { useEventStream } from '@/hooks/useEventStream'
import { useEventStreamInvalidation } from '@/hooks/useEventStreamInvalidation'
import { useServerStatus } from '@/services/statusService'
import { usePendingActions } from '@/services/actionService'
import { useActiveInterjections } from '@/services/interjectionService'
import { GlobalSearch } from '@/components/domain/global-search'
import { Toaster } from '@/components/ui/toaster'
import { cn } from '@/lib/utils'

interface NavItem {
  path: string
  label: string
  exact: boolean
  badge?: number
  alert?: boolean
}

function NavLink({ item, isActive }: { item: NavItem; isActive: boolean }) {
  return (
    <Link
      to={item.path}
      className={cn(
        'relative flex items-center gap-1.5 px-3 py-1.5 rounded-md text-sm font-medium transition-colors',
        isActive
          ? 'bg-primary text-primary-foreground'
          : item.alert
            ? 'text-amber-600 dark:text-amber-400 hover:bg-amber-500/10'
            : 'text-muted-foreground hover:text-foreground hover:bg-accent',
      )}
    >
      {item.alert && (
        <Bell className={cn('h-3.5 w-3.5 shrink-0', item.badge && item.badge > 0 && 'animate-bounce')} />
      )}
      {item.label}
      {item.badge != null && item.badge > 0 && (
        <span className="absolute -top-1 -right-1 inline-flex items-center justify-center h-4 min-w-4 px-1 rounded-full bg-red-500 text-white text-[10px] font-bold leading-none">
          {item.badge > 99 ? '99+' : item.badge}
        </span>
      )}
    </Link>
  )
}

export function Layout({ children }: { children: React.ReactNode }) {
  const location = useLocation()
  const { connected } = useEventStream()
  const { data: status } = useServerStatus()
  const { data: pendingData } = usePendingActions()
  const { data: activeInterjections } = useActiveInterjections()

  useEventStreamInvalidation()

  const pendingCount = pendingData?.count ?? 0
  const interjectionCount = activeInterjections?.count ?? 0
  const totalActionable = pendingCount + interjectionCount

  const navItems: NavItem[] = [
    { path: '/', label: 'Chat', exact: true },
    { path: '/pipelines', label: 'Pipelines', exact: false },
    { path: '/tasks', label: 'Tasks', exact: false },
    { path: '/org', label: 'Organization', exact: false },
    { path: '/docs', label: 'Documentation', exact: false },
    { path: '/audit', label: 'Audit', exact: false },
    { path: '/actions', label: 'Actions', exact: false, badge: totalActionable, alert: totalActionable > 0 },
    { path: '/settings', label: 'Settings', exact: false },
  ]

  return (
    <div className="min-h-screen flex flex-col">
      <header className="border-b bg-card">
        <div className="flex items-center justify-between px-6 h-14">
          <div className="flex items-center gap-6">
            <h1 className="text-lg font-semibold tracking-tight">Hyperax</h1>
            <nav className="flex items-center gap-1">
              {navItems.map((item) => {
                const isActive = item.exact
                  ? location.pathname === item.path
                  : location.pathname === item.path || location.pathname.startsWith(item.path + '/')
                return <NavLink key={item.path} item={item} isActive={isActive} />
              })}
            </nav>
          </div>
          <div className="flex items-center gap-3 text-xs text-muted-foreground">
            <GlobalSearch />
            {status && (
              <span className="hidden sm:inline">
                {status.tool_count} tools
              </span>
            )}
            <span className="flex items-center gap-1.5">
              <span
                className={cn(
                  'h-2 w-2 rounded-full',
                  connected ? 'bg-green-500' : 'bg-red-500',
                )}
              />
              {connected ? 'Connected' : 'Disconnected'}
            </span>
          </div>
        </div>
      </header>
      <main className="flex-1">{children}</main>
      <Toaster />
    </div>
  )
}
