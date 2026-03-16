import { useQuery } from '@tanstack/react-query'

export interface StatusInfo {
  version: string
  storage: string
  tool_count: number
  uptime_seconds: number
  workspace_count: number
}

async function fetchStatus(): Promise<StatusInfo> {
  const res = await fetch('/api/status')
  if (!res.ok) {
    throw new Error(`HTTP ${res.status}: ${res.statusText}`)
  }
  return res.json() as Promise<StatusInfo>
}

export function useServerStatus() {
  return useQuery({
    queryKey: ['server-status'],
    queryFn: fetchStatus,
    refetchInterval: 30_000,
  })
}
