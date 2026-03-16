import { useServerStatus } from '@/services/statusService'
import { LoadingState } from '@/components/domain/loading-state'
import { ErrorState } from '@/components/domain/error-state'
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Server } from 'lucide-react'

function formatUptime(seconds: number): string {
  const h = Math.floor(seconds / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  const s = Math.floor(seconds % 60)
  if (h > 0) return `${h}h ${m}m ${s}s`
  if (m > 0) return `${m}m ${s}s`
  return `${s}s`
}

export function ServerTab() {
  const { data, isLoading, error, refetch } = useServerStatus()

  if (isLoading) return <LoadingState message="Fetching server status..." />
  if (error) return <ErrorState error={error as Error} onRetry={() => void refetch()} />

  return (
    <div className="space-y-4">
      <Card>
        <CardHeader>
          <div className="flex items-center gap-2">
            <Server className="h-4 w-4 text-muted-foreground" />
            <CardTitle className="text-base">Server Status</CardTitle>
          </div>
          <CardDescription>Live Hyperax server information</CardDescription>
        </CardHeader>
        <CardContent>
          {data ? (
            <dl className="grid grid-cols-1 sm:grid-cols-2 gap-x-8 gap-y-4 text-sm">
              <div className="space-y-1">
                <dt className="text-xs font-medium text-muted-foreground uppercase tracking-wider">Version</dt>
                <dd className="font-mono">{data.version}</dd>
              </div>
              <div className="space-y-1">
                <dt className="text-xs font-medium text-muted-foreground uppercase tracking-wider">Storage</dt>
                <dd>{data.storage}</dd>
              </div>
              <div className="space-y-1">
                <dt className="text-xs font-medium text-muted-foreground uppercase tracking-wider">Tool Count</dt>
                <dd>
                  <Badge variant="secondary">{data.tool_count} tools</Badge>
                </dd>
              </div>
              <div className="space-y-1">
                <dt className="text-xs font-medium text-muted-foreground uppercase tracking-wider">Workspaces</dt>
                <dd>
                  <Badge variant="secondary">{data.workspace_count} registered</Badge>
                </dd>
              </div>
              <div className="space-y-1">
                <dt className="text-xs font-medium text-muted-foreground uppercase tracking-wider">Uptime</dt>
                <dd className="font-mono">{formatUptime(data.uptime_seconds)}</dd>
              </div>
              <div className="space-y-1">
                <dt className="text-xs font-medium text-muted-foreground uppercase tracking-wider">Status</dt>
                <dd>
                  <span className="inline-flex items-center gap-1.5">
                    <span className="h-2 w-2 rounded-full bg-green-500" />
                    Running
                  </span>
                </dd>
              </div>
            </dl>
          ) : (
            <p className="text-sm text-muted-foreground">No status data available.</p>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
