import { useEffect } from 'react'
import { User, Inbox, AlertTriangle, Settings, Star } from 'lucide-react'
import { Link } from 'react-router-dom'
import { useAgents, type Agent } from '@/services/agentService'
import { useProviders, parseModels, type Provider } from '@/services/providerService'
import { useAgentInboxes } from '@/services/commhubService'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

interface AgentSidebarProps {
  selectedAgentId?: string
  onSelectAgent?: (id: string) => void
}

function getConfigStatus(agent: Agent, providers: Provider[]): 'configured' | 'no-model' | 'provider-disabled' {
  if (!agent.provider_id && !agent.default_model) return 'no-model'
  if (agent.provider_id) {
    const provider = providers.find((p) => p.id === agent.provider_id)
    if (!provider || !provider.is_enabled) return 'provider-disabled'
    if (agent.default_model) {
      const models = parseModels(provider.models)
      if (models.length > 0 && !models.includes(agent.default_model)) return 'no-model'
    }
  }
  if (!agent.default_model) return 'no-model'
  return 'configured'
}

function getProviderName(providerId: string, providers: Provider[]): string | undefined {
  return providers.find((p) => p.id === providerId)?.name
}

function isChiefOfStaff(agent: Agent): boolean {
  return agent.clearance_level === 3 || agent.name.toLowerCase() === 'chief of staff'
}

export function AgentSidebar({ selectedAgentId, onSelectAgent }: AgentSidebarProps) {
  const { data: rawAgents, isLoading: loadingAgents } = useAgents()
  const { data: rawProviders } = useProviders()
  const { data: inboxes } = useAgentInboxes()

  const agents = Array.isArray(rawAgents) ? rawAgents : []
  const providers = Array.isArray(rawProviders) ? rawProviders : []

  // Filter: only include favorited agents or Chief of Staff
  const favoriteAgents = agents.filter((a) => {
    const isNotPostmaster = a.name.toLowerCase() !== 'postmaster' && !a.is_internal
    const isFavorite = a.is_favorite === true
    const isCoS = isChiefOfStaff(a)
    return isNotPostmaster && (isFavorite || isCoS)
  })

  const inboxMap = new Map(
    Array.isArray(inboxes) ? inboxes.map((i) => [i.agent_id, i.message_count ?? 0]) : [],
  )

  // Auto-select the first agent if none is selected and agents exist
  useEffect(() => {
    if (!selectedAgentId && favoriteAgents.length > 0 && onSelectAgent) {
      onSelectAgent(favoriteAgents[0].id)
    }
  }, [selectedAgentId, favoriteAgents.length, onSelectAgent])

  return (
    <aside className="w-64 border-r bg-card flex flex-col shrink-0">
      <div className="p-4 border-b">
        <h2 className="text-sm font-semibold">Favorite Agents</h2>
      </div>
      <div className="flex-1 overflow-y-auto p-2">
        {loadingAgents ? (
          <div className="space-y-2 p-2">
            {[1, 2, 3].map((i) => (
              <Skeleton key={i} className="h-10 w-full" />
            ))}
          </div>
        ) : favoriteAgents.length === 0 ? (
          <div className="p-3 space-y-3">
            <p className="text-xs text-muted-foreground">
              No favorite agents yet. Visit the org chart to add agents to your favorites.
            </p>
            <Button variant="outline" size="sm" className="w-full" asChild>
              <Link to="/org">
                <Settings className="h-3.5 w-3.5 mr-2" />
                Manage Agents
              </Link>
            </Button>
          </div>
        ) : (
          favoriteAgents.map((agent) => {
            const msgCount = inboxMap.get(agent.id) ?? 0
            const status = getConfigStatus(agent, providers)
            const providerName = agent.provider_id
              ? getProviderName(agent.provider_id, providers)
              : undefined
            return (
              <button
                key={agent.id}
                onClick={() => onSelectAgent?.(agent.id)}
                className={cn(
                  'w-full flex items-center gap-2.5 px-3 py-2 rounded-md text-left text-sm transition-colors',
                  selectedAgentId === agent.id
                    ? 'bg-accent text-accent-foreground'
                    : 'hover:bg-accent/50 text-foreground',
                )}
              >
                <User className="h-4 w-4 text-muted-foreground shrink-0" />
                <div className="flex-1 min-w-0">
                  <div className="font-medium truncate flex items-center gap-1.5">
                    {agent.name}
                    {agent.is_favorite && <Star className="h-3 w-3 text-yellow-500 fill-yellow-500 shrink-0" />}
                  </div>
                  {status === 'configured' ? (
                    <div className="text-xs text-muted-foreground truncate">
                      {agent.default_model}
                      {providerName ? ` · ${providerName}` : ''}
                    </div>
                  ) : status === 'provider-disabled' ? (
                    <div className="flex items-center gap-1 text-xs text-red-500">
                      <AlertTriangle className="h-3 w-3 shrink-0" />
                      Provider disabled
                    </div>
                  ) : (
                    <div className="flex items-center gap-1 text-xs text-amber-500">
                      <AlertTriangle className="h-3 w-3 shrink-0" />
                      No model assigned
                    </div>
                  )}
                </div>
                {msgCount > 0 && (
                  <Badge variant="default" className="text-xs h-5 px-1.5 shrink-0">
                    <Inbox className="h-3 w-3 mr-0.5" />
                    {msgCount}
                  </Badge>
                )}
              </button>
            )
          })
        )}
      </div>
    </aside>
  )
}
