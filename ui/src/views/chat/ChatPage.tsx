import { useState, useMemo, useEffect, useRef } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { useQueryClient } from '@tanstack/react-query'
import { AlertTriangle, CheckCircle2, Circle, Plug, Bot, MessageSquare, Settings2, RotateCcw, Square } from 'lucide-react'
import { ProgressiveOnboarding } from './ProgressiveOnboarding'
import { toast } from '@/components/ui/use-toast'
import { AgentSidebar } from './AgentSidebar'
import { ChatTabBar } from './ChatTabBar'
import { MessageList } from './MessageList'
import { ThinkingIndicator } from './ThinkingIndicator'
import { MessageInput } from './MessageInput'
import { EventStream } from './EventStream'
import { useEventStream } from '@/hooks/useEventStream'
import { useCommLog, useSendMessage, useStopGeneration, useNewSession, useActiveSession as useBackendActiveSession } from '@/services/commhubService'
import { useAgents, useUpdateAgent } from '@/services/agentService'
import type { Agent } from '@/services/agentService'
import { useProviders, parseModels } from '@/services/providerService'
import type { Provider } from '@/services/providerService'
import { useSessionContext } from '@/contexts/SessionContext'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

const OPERATOR_ID = 'operator'

interface OnboardingStepperProps {
  hasProviders: boolean
  hasAgents: boolean
  postmaster: Agent | undefined
  isPostmasterConfigured: boolean
  providers: Provider[]
}

function OnboardingStepper({
  hasProviders,
  hasAgents,
  postmaster,
  isPostmasterConfigured,
  providers,
}: OnboardingStepperProps) {
  const [selectedProviderId, setSelectedProviderId] = useState<string>('')
  const [selectedModel, setSelectedModel] = useState<string>('')
  const [isSaving, setIsSaving] = useState(false)
  const updateAgent = useUpdateAgent()
  const queryClient = useQueryClient()

  const enabledProviders = providers.filter((p) => p.is_enabled)

  const modelsForSelectedProvider = useMemo(() => {
    if (!selectedProviderId) return []
    const provider = enabledProviders.find((p) => p.id === selectedProviderId)
    if (!provider) return []
    return parseModels(provider.models)
  }, [selectedProviderId, enabledProviders])

  // Reset model when provider changes
  function handleProviderChange(providerId: string) {
    setSelectedProviderId(providerId)
    setSelectedModel('')
  }

  async function handleSavePostmaster() {
    if (!postmaster) {
      toast({ title: 'Error', description: 'Postmaster agent not found. Please refresh and try again.', variant: 'destructive' })
      return
    }
    if (!postmaster.id) {
      toast({ title: 'Error', description: 'Postmaster agent has no ID. Please refresh and try again.', variant: 'destructive' })
      return
    }
    if (!selectedProviderId) {
      toast({ title: 'Error', description: 'Please select a provider.', variant: 'destructive' })
      return
    }
    if (!selectedModel) {
      toast({ title: 'Error', description: 'Please select a model.', variant: 'destructive' })
      return
    }

    setIsSaving(true)
    try {
      await updateAgent.mutateAsync({
        agent_id: postmaster.id,
        provider_id: selectedProviderId,
        default_model: selectedModel,
      })
      toast({ title: 'Postmaster configured', description: 'Routing is ready.' })
      void queryClient.invalidateQueries({ queryKey: ['agents'] })
    } catch (error) {
      const errorMsg = error instanceof Error ? error.message : 'Failed to configure Postmaster. Please try again.'
      toast({ title: 'Error', description: errorMsg, variant: 'destructive' })
    } finally {
      setIsSaving(false)
    }
  }

  const steps = [
    {
      number: 1,
      icon: Plug,
      title: 'Add a Provider',
      description: 'Connect an LLM provider to power your assistants.',
      done: hasProviders,
      active: !hasProviders,
      cta: 'Add Provider',
      href: '/settings/providers',
      inline: null,
    },
    {
      number: 2,
      icon: Settings2,
      title: 'Configure Routing',
      description: 'Assign a model to the Postmaster so it can route messages to your assistants.',
      done: isPostmasterConfigured,
      active: hasProviders && !isPostmasterConfigured,
      cta: null,
      href: null,
      inline: 'postmaster-selector',
    },
    {
      number: 3,
      icon: Bot,
      title: 'Create an Assistant',
      description: 'Define an AI persona with a model and system prompt.',
      done: hasAgents,
      active: hasProviders && isPostmasterConfigured && !hasAgents,
      cta: 'Create Agent',
      href: '/org',
      inline: null,
    },
    {
      number: 4,
      icon: MessageSquare,
      title: 'Start Chatting',
      description: 'Select your assistant from the sidebar and send a message.',
      done: hasAgents,
      active: false,
      cta: null,
      href: null,
      inline: null,
    },
  ]

  return (
    <div className="space-y-3">
      {steps.map((step) => {
        const Icon = step.icon
        return (
          <Card
            key={step.number}
            className={step.active ? 'border-primary/50 bg-primary/5' : 'border-border/50'}
          >
            <CardContent className="p-4 flex gap-4 items-start">
              <div className="shrink-0 mt-0.5">
                {step.done ? (
                  <CheckCircle2 className="h-6 w-6 text-primary" />
                ) : step.active ? (
                  <div className="h-6 w-6 rounded-full border-2 border-primary flex items-center justify-center">
                    <span className="text-xs font-bold text-primary">{step.number}</span>
                  </div>
                ) : (
                  <Circle className="h-6 w-6 text-muted-foreground/40" />
                )}
              </div>
              <div className="flex-1 min-w-0 space-y-1">
                <div className="flex items-center gap-2">
                  <Icon
                    className={`h-4 w-4 shrink-0 ${step.done ? 'text-primary' : step.active ? 'text-foreground' : 'text-muted-foreground/50'}`}
                  />
                  <span
                    className={`text-sm font-medium ${step.done || step.active ? 'text-foreground' : 'text-muted-foreground/60'}`}
                  >
                    {step.title}
                  </span>
                </div>
                <p
                  className={`text-xs ${step.done || step.active ? 'text-muted-foreground' : 'text-muted-foreground/50'}`}
                >
                  {step.description}
                </p>
                {step.active && step.cta && step.href && (
                  <Button variant="default" size="sm" className="mt-2" asChild>
                    <Link to={step.href}>
                      <Icon className="h-3.5 w-3.5 mr-1.5" />
                      {step.cta}
                    </Link>
                  </Button>
                )}
                {step.active && step.inline === 'postmaster-selector' && !postmaster && (
                  <div className="mt-3">
                    <p className="text-xs text-destructive">
                      Postmaster agent not found. Restart the server or check migrations.
                    </p>
                  </div>
                )}
                {step.active && step.inline === 'postmaster-selector' && postmaster && (
                  <div className="mt-3 space-y-2">
                    <p className="text-xs text-muted-foreground italic">
                      Pick a fast, inexpensive model — routing decisions are lightweight.
                    </p>
                    <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
                      <Select value={selectedProviderId} onValueChange={handleProviderChange}>
                        <SelectTrigger className="h-8 text-xs w-full sm:w-40">
                          <SelectValue placeholder="Provider" />
                        </SelectTrigger>
                        <SelectContent>
                          {enabledProviders.map((p) => (
                            <SelectItem key={p.id} value={p.id} className="text-xs">
                              {p.name}
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                      <Select
                        value={selectedModel}
                        onValueChange={setSelectedModel}
                        disabled={!selectedProviderId || modelsForSelectedProvider.length === 0}
                      >
                        <SelectTrigger className="h-8 text-xs w-full sm:w-48">
                          <SelectValue placeholder="Model" />
                        </SelectTrigger>
                        <SelectContent>
                          {modelsForSelectedProvider.map((m) => (
                            <SelectItem key={m} value={m} className="text-xs font-mono">
                              {m}
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                      <Button
                        size="sm"
                        className="h-8 text-xs shrink-0"
                        disabled={!selectedProviderId || !selectedModel || isSaving}
                        onClick={() => void handleSavePostmaster()}
                      >
                        {isSaving ? 'Saving…' : 'Save'}
                      </Button>
                    </div>
                  </div>
                )}
              </div>
            </CardContent>
          </Card>
        )
      })}
    </div>
  )
}

export default function ChatPage() {
  // Use SessionContext for session management instead of local state
  const { activeSessionId, getOrCreateSession, switchSession } = useSessionContext()

  // Deep-link support: Read ?agent={id} from URL and auto-select that agent
  const [searchParams, setSearchParams] = useSearchParams()
  const agentParam = searchParams.get('agent')

  useEffect(() => {
    if (agentParam) {
      // Create or switch to session for the agent specified in URL
      getOrCreateSession(agentParam)
      // Clear the URL parameter after handling it
      setSearchParams({}, { replace: true })
    }
  }, [agentParam, getOrCreateSession, setSearchParams])


  // Derive selectedAgentId from activeSessionId
  // sessionId format: "session_${agentId}_${timestamp}"
  const selectedAgentId = useMemo(() => {
    if (!activeSessionId) return undefined
    const parts = activeSessionId.split('_')
    return parts.length >= 2 ? parts[1] : undefined
  }, [activeSessionId])
  const { data: rawAgents } = useAgents()
  const { data: rawProviders } = useProviders()

  const allAgents = Array.isArray(rawAgents) ? rawAgents : []
  const agents = allAgents.filter((a) => a.name.toLowerCase() !== 'postmaster' && !a.is_internal)
  const providers = Array.isArray(rawProviders) ? rawProviders : []
  const enabledProviders = providers.filter((p) => p.is_enabled)

  const postmaster = allAgents.find((a) => a.name.toLowerCase() === 'postmaster')
  const isPostmasterConfigured = !!(postmaster?.default_model && postmaster?.provider_id)

  const selectedAgent = useMemo(
    () => agents.find((a) => a.id === selectedAgentId),
    [selectedAgentId, agents],
  )

  const selectedAgentName = selectedAgent?.name ?? selectedAgentId

  // Check if selected agent has a valid model+provider
  const isModelConfigured = useMemo(() => {
    if (!selectedAgent) return false
    if (!selectedAgent.default_model) return false
    if (selectedAgent.provider_id) {
      const provider = providers.find((p) => p.id === selectedAgent.provider_id)
      if (!provider || !provider.is_enabled) return false
    }
    return true
  }, [selectedAgent, providers])

  // Fetch the active session for this agent+peer pair.
  // The comm-log query is gated on this — it will NOT fire until we know
  // which session to scope to, preventing stale full-history loads.
  // Fetch the active BACKEND session for this agent+peer pair.
  // The comm-log query is gated on this — it will NOT fire until we know
  // which session to scope to, preventing stale full-history loads.
  const { data: backendSessionData, isFetched: backendSessionFetched } = useBackendActiveSession(selectedAgentName, OPERATOR_ID)
  const backendSessionId = backendSessionData && typeof backendSessionData === 'object' && 'id' in backendSessionData && backendSessionData.id
    ? backendSessionData.id
    : undefined

  // CommHub uses persona names (not UUIDs) as agent identifiers.
  // Only fetch history once session state is known; pass session_id so the
  // backend returns only messages from the current session.
  const { data: rawMessages = [], isLoading } = useCommLog(
    OPERATOR_ID,
    selectedAgentName,
    backendSessionId,
    backendSessionFetched,
  )

  // Subscribe to WebSocket events for real-time message delivery.
  // When a comm.message event arrives for the current conversation, invalidate
  // the query cache so messages appear instantly without polling.
  const wsQc = useQueryClient()
  const { events: wsEvents } = useEventStream({ patterns: ['comm.message*', 'chat.completion.*', 'tooluse.*'] })

  useEffect(() => {
    if (wsEvents.length === 0 || !selectedAgentName) return
    const latest = wsEvents[wsEvents.length - 1]
    const payload = latest.payload as Record<string, unknown> | null
    if (!payload) return
    const from = payload.from as string | undefined
    const to = payload.to as string | undefined
    const agent = payload.agent as string | undefined
    // Invalidate if the event involves the current conversation partner
    if (from === selectedAgentName || to === selectedAgentName || agent === selectedAgentName) {
      void wsQc.invalidateQueries({ queryKey: ['comm-log'] })
    }
  }, [wsEvents, selectedAgentName, wsQc])

  // Handle deep-link from org page (?agent=uuid)
  // Waits for agents to load, creates a backend session, then switches to it.
  // deepLinkHandled ref prevents double-firing as allAgents loads.
  useEffect(() => {
    const params = new URLSearchParams(window.location.search)
    const agentParam = params.get('agent')

    if (!agentParam || deepLinkHandled.current) return
    if (allAgents.length === 0) return // wait for agents to load

    const agent = allAgents.find((a) => a.id === agentParam)
    if (!agent) return // agent not found — don't mark handled so we can retry

    deepLinkHandled.current = true

    const doSwitch = async () => {
      try {
        // Create the backend session first so CommHub has a session record
        await newSession.mutateAsync({ agent_name: agent.name, peer_id: OPERATOR_ID })
        // Then create the client-side temporary session and activate it
        getOrCreateSession(agentParam, 'temporary')
        await switchSession(agentParam)
      } catch (error) {
        console.error('Failed to switch to agent:', agentParam, error)
        toast({
          title: 'Error',
          description: 'Failed to load agent from link',
          variant: 'destructive',
        })
      }
    }
    void doSwitch()
  }, [allAgents]) // re-run once agents load; ref prevents double execution


  // Sort chronologically (oldest first) so newest messages appear at the bottom.
  const messages = useMemo(
    () => [...rawMessages].sort((a, b) =>
      new Date(a.created_at).getTime() - new Date(b.created_at).getTime()
    ),
    [rawMessages],
  )
  const sendMessage = useSendMessage()
  const stopGeneration = useStopGeneration()
  const newSession = useNewSession()
  const deepLinkHandled = useRef(false)

  function handleSend(content: string) {
    if (!selectedAgentName) return
    sendMessage.mutate({
      from: OPERATOR_ID,
      to: selectedAgentName,
      content,
      content_type: 'text',
      trust: 'authorized',
    })
  }

  function handleContinue() {
    handleSend('Please continue from where you left off.')
  }

  function handleNewSession() {
    if (!selectedAgentName) return
    newSession.mutate(
      { agent_name: selectedAgentName, peer_id: OPERATOR_ID },
      {
        onSuccess: (data) => {
          toast({ title: 'New session started' })
          // Immediately set the active session to the new one so the comm-log
          // query key changes right now — no stale messages flicker.
          wsQc.setQueryData(
            ['active-session', selectedAgentName, OPERATOR_ID],
            { id: data.session_id, agent_name: selectedAgentName, peer_id: OPERATOR_ID, started_at: new Date().toISOString(), summary: '' },
          )
          // The new session has zero messages — seed the cache with an empty array.
          wsQc.setQueryData(
            ['comm-log', OPERATOR_ID, selectedAgentName, data.session_id, true],
            [],
          )
          void wsQc.invalidateQueries({ queryKey: ['active-session'] })
          void wsQc.invalidateQueries({ queryKey: ['comm-log'] })
        },
        onError: (error) => {
          const msg = error instanceof Error ? error.message : 'Failed to start new session.'
          toast({ title: 'Error', description: msg, variant: 'destructive' })
        },
      },
    )
  }

  // Handle agent selection from sidebar
  // Creates or switches to a persistent session
  async function handleSelectAgent(agentId: string) {
    try {
      console.log('🔄 Switching to agent:', agentId)
      await switchSession(agentId) // Creates persistent session
      console.log('✅ Session switched')
    } catch (error) {
      console.error('❌ Switch failed:', error)
      toast({
        title: 'Error',
        description: 'Failed to switch agent session',
        variant: 'destructive',
      })
    }
  }

  const isSending = sendMessage.isPending

  function handleArchiveSession(agentName: string) {
    toast({ title: 'Session archived', description: `Closed session with ${agentName}` })
  }

  return (
    <div className="flex h-[calc(100vh-3.5rem)]">
      <AgentSidebar selectedAgentId={selectedAgentId} onSelectAgent={handleSelectAgent} />

      <div className="flex-1 flex flex-col min-w-0">
        <ChatTabBar onArchiveSession={handleArchiveSession} />

        {selectedAgentId && (
          <div className="px-4 py-2 border-b bg-card text-sm text-muted-foreground shrink-0 flex items-center justify-between">
            <span>
              Chatting with <span className="text-foreground font-medium">{selectedAgentName}</span>
              {selectedAgent?.default_model && (
                <span className="ml-2 text-xs font-mono opacity-60">{selectedAgent.default_model}</span>
              )}
            </span>
            <div className="flex items-center gap-2">
              {selectedAgentId && !isModelConfigured && (
                <>
                  <span className="text-xs text-amber-500 flex items-center gap-1">
                    <AlertTriangle className="h-3 w-3" />
                    No model assigned
                  </span>
                  <Button variant="outline" size="sm" className="h-6 text-xs" asChild>
                    <Link to="/org">Configure</Link>
                  </Button>
                </>
              )}
              <Button
                variant="ghost"
                size="sm"
                className="h-7 px-2 text-xs text-muted-foreground hover:text-foreground"
                onClick={handleNewSession}
                disabled={newSession.isPending}
                title="Start a new session"
              >
                <RotateCcw className="h-3.5 w-3.5 mr-1" />
                New Session
              </Button>
            </div>
          </div>
        )}

        <div className="flex-1 overflow-y-auto px-4">
          <div className="max-w-3xl mx-auto h-full">
            {!selectedAgentId ? (
              <div className="flex flex-col items-center justify-center h-full py-16">
                {agents.length === 0 ? (
                  <div className="w-full max-w-md space-y-2">
                    <p className="text-sm font-semibold text-foreground text-center mb-6">
                      Get started with Hyperax
                    </p>
                    <OnboardingStepper
                      hasProviders={enabledProviders.length > 0}
                      hasAgents={false}
                      postmaster={postmaster}
                      isPostmasterConfigured={isPostmasterConfigured}
                      providers={providers}
                    />
                  </div>
                ) : (
                  <div className="w-full max-w-md space-y-4">
                    <p className="text-sm text-muted-foreground text-center">
                      Select an agent from the sidebar to start chatting.
                    </p>
                    <ProgressiveOnboarding />
                  </div>
                )}
              </div>
            ) : isLoading ? (
              <div className="flex items-center justify-center h-full text-sm text-muted-foreground">
                Loading conversation...
              </div>
            ) : (
              <MessageList messages={messages} currentAgentId={OPERATOR_ID}>
                <ThinkingIndicator events={wsEvents} agentName={selectedAgentName} onContinue={handleContinue} />
              </MessageList>
            )}
          </div>
        </div>

        <div className="border-t p-4 bg-card shrink-0">
          <div className="max-w-3xl mx-auto">
            {isSending && selectedAgentName && (
              <div className="flex justify-center pb-2">
                <Button
                  variant="destructive"
                  size="sm"
                  onClick={() => stopGeneration.mutate(selectedAgentName)}
                  disabled={stopGeneration.isPending}
                >
                  <Square className="h-3.5 w-3.5 mr-1.5 fill-current" />
                  Stop
                </Button>
              </div>
            )}
            <MessageInput
              onSend={handleSend}
              disabled={!selectedAgentId || isSending || !isModelConfigured}
            />
            {selectedAgentId && !isModelConfigured && (
              <p className="mt-2 text-xs text-amber-500 text-center">
                This agent needs a provider and model assigned before it can respond.{' '}
                <Link to="/org" className="underline">
                  Configure in Organization
                </Link>
              </p>
            )}
          </div>
        </div>

        <EventStream />
      </div>
    </div>
  )
}
