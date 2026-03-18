package types

import (
	"encoding/json"
	"time"
)

// EventType defines the taxonomy of events in the Nervous System.
// This file is the public contract — changing an event type here
// forces a compile-time break everywhere it's referenced.
type EventType string

const (
	// CommHub
	EventCommMessage          EventType = "comm.message"
	EventCommMessageDelivered EventType = "comm.message.delivered"
	EventCommOverflow         EventType = "comm.overflow"
	EventCommSieveFlag        EventType = "comm.sieve_flag"

	// Pulse Engine
	EventPulseFire         EventType = "pulse.fire"
	EventPulseSkipped      EventType = "pulse.skipped"
	EventPulseError        EventType = "pulse.error"
	EventPulseDefer        EventType = "pulse.defer"
	EventPulseBackpressure EventType = "pulse.backpressure"

	// Interjection
	EventInterjectHalt     EventType = "interject.halt"
	EventInterjectResolve  EventType = "interject.resolve"
	EventInterjectSafeMode EventType = "interject.safemode"
	EventInterjectCascade  EventType = "interject.cascade"

	// Pipeline
	EventPipelineStart    EventType = "pipeline.start"
	EventPipelineLog      EventType = "pipeline.log"
	EventPipelineComplete EventType = "pipeline.complete"

	// Memory
	EventMemoryRecall      EventType = "memory.recall"
	EventMemoryConsolidate EventType = "memory.consolidate"
	EventMemoryEvict       EventType = "memory.evict"
	EventMemoryConflict    EventType = "memory.conflict"

	// MCP Server
	EventMCPRequest  EventType = "mcp.request"
	EventMCPResponse EventType = "mcp.response"

	// AgentMail
	EventAgentMailSent              EventType = "agentmail.sent"
	EventAgentMailReceived          EventType = "agentmail.received"
	EventAgentMailAck               EventType = "agentmail.ack"
	EventAgentMailPartitionDetected EventType = "agentmail.partition.detected"
	EventAgentMailPartitionResolved EventType = "agentmail.partition.resolved"
	EventAgentMailBackpressure      EventType = "agentmail.backpressure"
	EventAgentMailDLOQuarantined    EventType = "agentmail.dlo.quarantined"
	EventAgentMailDLOAudit          EventType = "agentmail.dlo.audit"

	// Lifecycle
	EventLifecycleTransition     EventType = "lifecycle.transition"
	EventLifecycleStalled        EventType = "lifecycle.stalled"
	EventLifecycleZombieResolved EventType = "lifecycle.zombie_resolved"
	EventLifecycleCheckpoint     EventType = "lifecycle.checkpoint"

	// Onboarding (lifecycle sub-events routed through CommHub)
	EventOnboardingStarted          EventType = "onboarding.started"
	EventOnboardingIdentityDone     EventType = "onboarding.identity_done"
	EventOnboardingRelationshipDone EventType = "onboarding.relationships_done"
	EventOnboardingContextDone      EventType = "onboarding.context_done"
	EventOnboardingTasksDone        EventType = "onboarding.tasks_done"
	EventOnboardingCompleted        EventType = "onboarding.completed"

	// Nervous System
	EventNervousDriftDetected       EventType = "nervous.drift_detected"
	EventNervousSubscriptionAdded   EventType = "nervous.subscription.added"
	EventNervousSubscriptionRemoved EventType = "nervous.subscription.removed"

	// Interjection (Sieve bypass)
	EventInterjectSieveBypassGranted EventType = "interject.sieve_bypass.granted"
	EventInterjectSieveBypassExpired EventType = "interject.sieve_bypass.expired"

	// Index
	EventIndexStarted       EventType = "index.started"
	EventIndexCompleted     EventType = "index.completed"
	EventIndexError         EventType = "index.error"
	EventIndexFileReindexed EventType = "index.file.reindexed"
	EventIndexFileRemoved   EventType = "index.file.removed"

	// Cron
	EventCronFire     EventType = "cron.fire"
	EventCronComplete EventType = "cron.complete"
	EventCronFailed   EventType = "cron.failed"
	EventCronDLQ      EventType = "cron.dlq"

	// Plugin System
	EventPluginLoaded   EventType = "plugin.loaded"
	EventPluginEnabled  EventType = "plugin.enabled"
	EventPluginDisabled EventType = "plugin.disabled"
	EventPluginUnloaded EventType = "plugin.unloaded"
	EventPluginError    EventType = "plugin.error"
	EventPluginHealth   EventType = "plugin.health"

	// Filesystem
	EventFSConflictDetected EventType = "fs.conflict.detected"

	// Chat Completion
	EventChatCompletionStart EventType = "chat.completion.start"
	EventChatCompletionDone  EventType = "chat.completion.done"
	EventChatCompletionError EventType = "chat.completion.error"

	// Budget
	EventBudgetWarning   EventType = "budget.warning"   // 80% threshold
	EventBudgetCritical  EventType = "budget.critical"   // 95% threshold
	EventBudgetExhausted EventType = "budget.exhausted"  // 100% threshold

	// Watchdog
	EventWatchdogTriggered EventType = "watchdog.triggered" // Pulse Engine heartbeat stale
	EventWatchdogRecovered EventType = "watchdog.recovered" // Pulse Engine heartbeat resumed

	// Token Audit Trail
	EventTokenValidated EventType = "token.validated"
	EventTokenCreated   EventType = "token.created"
	EventTokenRotated   EventType = "token.rotated"
	EventTokenRevoked   EventType = "token.revoked"

	// Tool Use Loop
	EventToolUseLoopStart     EventType = "tooluse.loop.start"
	EventToolUseLoopIteration EventType = "tooluse.loop.iteration"
	EventToolUseLoopComplete  EventType = "tooluse.loop.complete"
	EventToolUseLoopError     EventType = "tooluse.loop.error"
	EventToolUseCycleDetected EventType = "tooluse.cycle.detected"
	EventToolUseToolDispatch  EventType = "tooluse.tool.dispatch"
	EventToolUseAutoExtend         EventType = "tooluse.loop.auto_extend"
	EventToolUseMaxIterReached     EventType = "tooluse.loop.max_iter_reached"
	EventToolUseGuardrailTriggered EventType = "tooluse.guardrail.triggered"

	// Guard System
	EventGuardPending  EventType = "guard.pending"
	EventGuardApproved EventType = "guard.approved"
	EventGuardRejected EventType = "guard.rejected"
	EventGuardTimeout  EventType = "guard.timeout"

	// Audit Stream
	EventAuditWritten  EventType = "audit.written"
	EventAuditDropped  EventType = "audit.dropped"
	EventAuditError    EventType = "audit.error"

	// Agent Work Queue & Scheduler
	EventWorkQueueEnqueued   EventType = "workqueue.enqueued"
	EventWorkQueueConsumed   EventType = "workqueue.consumed"
	EventWorkQueueDrained    EventType = "workqueue.drained"
	EventSchedulerTick       EventType = "scheduler.tick"
	EventSchedulerDispatch   EventType = "scheduler.dispatch"
	EventSchedulerTaskAssign EventType = "scheduler.task.assign"
	EventSchedulerAgentCron  EventType = "scheduler.agent.cron"

	// Provider Delegation
	EventProviderUnavailable   EventType = "chat.completion.provider_unavailable"
	EventDelegationAttempted   EventType = "chat.delegation.attempted"
	EventDelegationExhausted   EventType = "chat.delegation.exhausted"
	EventProviderReEnabled     EventType = "provider.re_enabled"

	// Channel Bridge
	EventChannelBridgeReceived    EventType = "channel.bridge.received"
	EventChannelBridgeDelivered   EventType = "channel.bridge.delivered"
	EventChannelBridgeRejected    EventType = "channel.bridge.rejected"
	EventChannelBridgeReviewStart EventType = "channel.bridge.review.start"
	EventChannelBridgeReviewDone  EventType = "channel.bridge.review.done"
	EventChannelBridgeResponse    EventType = "channel.bridge.response"
	EventChannelBridgeError       EventType = "channel.bridge.error"
)

// NervousEvent is the universal event envelope for the Nervous System.
// SequenceID is a Lamport logical clock: on send local++, on receive local = max(local, remote) + 1.
type NervousEvent struct {
	Type       EventType       `json:"type"`
	Scope      string          `json:"scope"`
	Source     string          `json:"source"`
	Payload    json.RawMessage `json:"payload"`
	TraceID    string          `json:"trace_id"`
	SequenceID uint64          `json:"sequence_id"`
	Timestamp  time.Time       `json:"timestamp"`
}
