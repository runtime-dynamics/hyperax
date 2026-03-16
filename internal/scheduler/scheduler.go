package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/internal/storage"
	"github.com/hyperax/hyperax/pkg/types"
)

// CompletionFunc performs synchronous LLM completion for an agent.
// It blocks until the completion (including any tool-use iterations) finishes.
// Returns an error if the completion failed (e.g. provider error, tool-use failure).
// The scheduler calls this synchronously so it can implement the drain loop.
type CompletionFunc func(agentName, senderID, content, sessionID string) error

// AgentScheduler polls the durable work queue and task list on a fixed interval,
// dispatching work to idle agents. It implements two task-routing strategies:
//
//  1. Agents with assigned pending tasks receive a prompt to work on them.
//  2. Unassigned pending tasks are routed to the Chief of Staff for triage.
//
// The scheduler follows the same Run(ctx) pattern as cron.Scheduler.
type AgentScheduler struct {
	store        *storage.Store
	bus          *nervous.EventBus
	logger       *slog.Logger
	tick         time.Duration
	completionFn CompletionFunc

	// Debounce state for task prompts. Protected by mu.
	mu                  sync.Mutex
	lastUnassignedSweep time.Time
	agentTaskPrompted   map[string]time.Time
}

// New creates an AgentScheduler with a 60-second default tick.
func New(store *storage.Store, bus *nervous.EventBus, logger *slog.Logger) *AgentScheduler {
	return &AgentScheduler{
		store:             store,
		bus:               bus,
		logger:            logger,
		tick:              60 * time.Second,
		agentTaskPrompted: make(map[string]time.Time),
	}
}

// SetTick overrides the default tick interval. Intended for testing.
func (s *AgentScheduler) SetTick(d time.Duration) {
	s.tick = d
}

// SetCompletionFunc wires the synchronous LLM completion function.
// This must be set before calling Run.
func (s *AgentScheduler) SetCompletionFunc(fn CompletionFunc) {
	s.completionFn = fn
}

// taskPromptInterval is the minimum time between re-prompting an agent about
// their assigned tasks. Set to match the tick interval so idle agents with
// outstanding tasks are prompted once per scheduling cycle.
const taskPromptInterval = 60 * time.Second

// unassignedSweepInterval is the minimum time between routing unassigned tasks
// to the Chief of Staff.
const unassignedSweepInterval = 5 * time.Minute

// minStateDuration is the minimum time an agent must hold a status before
// transitioning. This ensures the UI (which polls every 10s) can observe
// each state.
const minStateDuration = 10 * time.Second

// Run starts the scheduler loop. It blocks until ctx is cancelled.
func (s *AgentScheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(s.tick)
	defer ticker.Stop()

	s.logger.Info("agent scheduler started", "tick", s.tick)

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("agent scheduler stopped")
			return
		case <-ticker.C:
			s.checkAgents(ctx)
		}
	}
}

// checkAgents iterates over all non-internal agents. For each idle or error'd
// agent, it checks the work queue first, then assigned tasks. Error'd agents
// are automatically retried — transient failures (provider timeouts, API errors)
// resolve on their own and agents should not stay stuck until the next restart.
// After per-agent checks it runs a global sweep to route unassigned tasks to
// the Chief of Staff.
func (s *AgentScheduler) checkAgents(ctx context.Context) {
	if s.store.Agents == nil || s.store.WorkQueue == nil {
		return
	}

	agents, err := s.store.Agents.List(ctx)
	if err != nil {
		s.logger.Warn("agent scheduler: failed to list agents", "error", err)
		return
	}

	dispatched := 0
	for _, agent := range agents {
		// Skip internal/system agents.
		if agent.IsInternal {
			continue
		}

		// Only process idle or error agents. Active/suspended/halted agents
		// are handled by other mechanisms (drain loop, manual intervention).
		if agent.Status != "idle" && agent.Status != repo.AgentStatusError {
			continue
		}

		// Skip agents without a configured provider/model — they can't run completions.
		if agent.ProviderID == "" || agent.DefaultModel == "" {
			continue
		}

		// Publish per-agent cron trigger event so the event stream shows
		// individual agent checks (visible as "agent cron trigger for <name>").
		if s.bus != nil {
			s.bus.Publish(nervous.NewEvent(
				types.EventSchedulerAgentCron,
				"scheduler",
				agent.Name,
				map[string]string{
					"agent_id":   agent.ID,
					"agent_name": agent.Name,
					"message":    fmt.Sprintf("agent cron trigger for %s", agent.Name),
				},
			))
		}

		if s.tryDispatchMessages(ctx, agent) {
			dispatched++
			continue
		}

		// If the agent is in error with an empty queue, reset to idle so
		// it can be prompted about assigned tasks and self-heal. Without
		// this, error'd agents with no queued messages stay stuck forever.
		if agent.Status == repo.AgentStatusError {
			agent.Status = "idle"
			agent.StatusReason = ""
			if err := s.store.Agents.Update(ctx, agent.ID, agent); err != nil {
				s.logger.Warn("agent scheduler: failed to reset error'd agent to idle",
					"agent", agent.Name, "error", err)
				continue
			}
			s.logger.Info("agent scheduler: auto-reset error'd agent with empty queue",
				"agent", agent.Name, "agent_id", agent.ID)
		}

		if s.tryPromptAssignedTasks(ctx, agent) {
			dispatched++
		}
	}

	// Global sweep: route unassigned tasks to Chief of Staff.
	s.routeUnassignedTasks(ctx, agents)

	if dispatched > 0 {
		s.logger.Info("agent scheduler tick", "dispatched", dispatched)
	}

	if s.bus != nil {
		s.bus.Publish(nervous.NewEvent(
			types.EventSchedulerTick,
			"scheduler",
			"agent",
			map[string]int{"agents_checked": len(agents), "dispatched": dispatched},
		))
	}
}

// tryDispatchMessages checks if the agent has pending work queue items.
// If so, it sets the agent active and spawns the drain loop goroutine.
func (s *AgentScheduler) tryDispatchMessages(ctx context.Context, agent *repo.Agent) bool {
	count, err := s.store.WorkQueue.PeekCount(ctx, agent.Name)
	if err != nil {
		s.logger.Warn("agent scheduler: peek count failed",
			"agent", agent.Name, "error", err)
		return false
	}
	if count == 0 {
		return false
	}

	// Optimistic lock: only activate if still idle.
	if !s.setAgentActive(ctx, agent.ID, agent.Name) {
		return false
	}

	s.logger.Info("agent scheduler: dispatching queued messages",
		"agent", agent.Name, "queue_depth", count)

	if s.bus != nil {
		s.bus.Publish(nervous.NewEvent(
			types.EventSchedulerDispatch,
			"scheduler",
			agent.Name,
			map[string]any{
				"agent":       agent.Name,
				"queue_depth": count,
			},
		))
	}

	// Run the drain loop in a goroutine: process all queued items, then go idle.
	go s.drainLoop(ctx, agent.Name, agent.ID)

	return true
}

// tryPromptAssignedTasks checks if the agent has pending tasks assigned to them.
// If so, it enqueues a prompt message to the agent's work queue so they process
// it on the next tick. Debounced to avoid spamming the same agent repeatedly.
func (s *AgentScheduler) tryPromptAssignedTasks(ctx context.Context, agent *repo.Agent) bool {
	if s.store.Projects == nil {
		return false
	}

	// Debounce: don't re-prompt an agent more often than taskPromptInterval.
	s.mu.Lock()
	lastPrompt, ok := s.agentTaskPrompted[agent.ID]
	if ok && time.Since(lastPrompt) < taskPromptInterval {
		s.mu.Unlock()
		return false
	}
	s.mu.Unlock()

	// Count pending or stalled in-progress tasks assigned to this agent.
	pendingTasks, err := s.store.Projects.ListTasksByAgent(ctx, agent.ID, "pending", "")
	if err != nil {
		s.logger.Error("failed to list agent tasks",
			"agent", agent.Name, "agent_id", agent.ID, "status", "pending", "error", err)
	}
	inProgressTasks, ipErr := s.store.Projects.ListTasksByAgent(ctx, agent.ID, "in-progress", "")
	if ipErr != nil {
		s.logger.Error("failed to list agent tasks",
			"agent", agent.Name, "agent_id", agent.ID, "status", "in-progress", "error", ipErr)
	}
	taskCount := len(pendingTasks) + len(inProgressTasks)

	if taskCount == 0 {
		return false
	}

	content := fmt.Sprintf(
		"You have %d task(s) awaiting your attention. Use get_task with your agent ID to retrieve your next task.",
		taskCount,
	)

	// Enqueue to work queue.
	item := &types.WorkQueueItem{
		AgentName:   agent.Name,
		FromAgent:   "system:scheduler",
		Content:     content,
		ContentType: "text",
		Trust:       "internal",
		Priority:    1, // Higher priority than regular messages.
	}
	if err := s.store.WorkQueue.Enqueue(ctx, item); err != nil {
		s.logger.Warn("agent scheduler: failed to enqueue task prompt",
			"agent", agent.Name, "error", err)
		return false
	}

	// Immediately activate the agent and start the drain loop so the prompt
	// is processed on this tick (not deferred to the next tick).
	if !s.setAgentActive(ctx, agent.ID, agent.Name) {
		// Agent was activated by something else — the queued item will be
		// picked up by that drain loop.
		return false
	}

	// Record the prompt time for debouncing.
	s.mu.Lock()
	s.agentTaskPrompted[agent.ID] = time.Now()
	s.mu.Unlock()

	s.logger.Info("agent scheduler: dispatching task prompt to agent",
		"agent", agent.Name, "task_count", taskCount)

	if s.bus != nil {
		s.bus.Publish(nervous.NewEvent(
			types.EventSchedulerTaskAssign,
			"scheduler",
			agent.Name,
			map[string]any{
				"agent":      agent.Name,
				"task_count": taskCount,
				"action":     "prompt_assigned",
			},
		))
	}

	go s.drainLoop(ctx, agent.Name, agent.ID)

	return true
}

// routeUnassignedTasks finds pending tasks with no assignee and sends them
// to the Chief of Staff agent for triage and assignment. Debounced to avoid
// flooding the CoS on every tick.
func (s *AgentScheduler) routeUnassignedTasks(ctx context.Context, agents []*repo.Agent) {
	if s.store.Projects == nil {
		return
	}

	// Debounce: only sweep every unassignedSweepInterval.
	s.mu.Lock()
	if time.Since(s.lastUnassignedSweep) < unassignedSweepInterval {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()

	// Query unassigned pending tasks (empty agentID = unassigned).
	tasks, err := s.store.Projects.ListTasksByAgent(ctx, "", "pending", "")
	if err != nil {
		s.logger.Error("failed to list unassigned tasks", "error", err)
		return
	}

	// Filter for truly unassigned tasks.
	var unassigned []*repo.Task
	for _, t := range tasks {
		if t.AssigneeAgentID == "" {
			unassigned = append(unassigned, t)
		}
	}
	if len(unassigned) == 0 {
		return
	}

	// Find the Chief of Staff agent — first by role template, then by name.
	var cosAgent *repo.Agent
	for _, a := range agents {
		if a.RoleTemplateID == "chief_of_staff" && a.ProviderID != "" && a.DefaultModel != "" {
			cosAgent = a
			break
		}
	}
	if cosAgent == nil {
		// Fallback: try by name.
		if resolved, err := s.store.Agents.GetByName(ctx, "Chief of Staff"); err == nil {
			cosAgent = resolved
		}
	}
	if cosAgent == nil {
		s.logger.Warn("agent scheduler: no Chief of Staff agent found for task routing",
			"unassigned_count", len(unassigned))
		return
	}

	content := fmt.Sprintf(
		"There are %d unassigned pending task(s) requiring triage. Use get_task (without agent_id) to retrieve the next task for assignment.",
		len(unassigned),
	)

	item := &types.WorkQueueItem{
		AgentName:   cosAgent.Name,
		FromAgent:   "system:scheduler",
		Content:     content,
		ContentType: "text",
		Trust:       "internal",
		Priority:    2, // Urgent — task routing is critical.
	}
	if err := s.store.WorkQueue.Enqueue(ctx, item); err != nil {
		s.logger.Warn("agent scheduler: failed to enqueue unassigned task sweep to Chief of Staff",
			"error", err)
		return
	}

	// Update sweep timestamp.
	s.mu.Lock()
	s.lastUnassignedSweep = time.Now()
	s.mu.Unlock()

	s.logger.Info("agent scheduler: routed unassigned tasks to Chief of Staff",
		"count", len(unassigned), "cos_agent", cosAgent.Name)

	if s.bus != nil {
		s.bus.Publish(nervous.NewEvent(
			types.EventSchedulerTaskAssign,
			"scheduler",
			cosAgent.Name,
			map[string]any{
				"agent":            cosAgent.Name,
				"unassigned_count": len(unassigned),
				"action":           "route_to_cos",
			},
		))
	}
}

// drainLoop processes all queued items for an agent sequentially, then
// sets the agent back to idle. Runs in its own goroutine.
func (s *AgentScheduler) drainLoop(ctx context.Context, agentName, agentID string) {
	activeAt := time.Now()

	// Track whether the agent was set to error status so the deferred
	// idle reset doesn't clobber it.
	errored := false
	defer func() {
		if !errored {
			// Ensure the agent holds "active" status long enough for the UI
			// (which polls every 10s) to observe the state transition.
			if elapsed := time.Since(activeAt); elapsed < minStateDuration {
				time.Sleep(minStateDuration - elapsed)
			}
			// Use background context so idle reset succeeds even during app
			// shutdown when the parent ctx is already cancelled.
			s.setAgentIdle(context.Background(), agentID, agentName)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		item, err := s.dequeueWithRetry(ctx, agentName)
		if err != nil {
			s.logger.Warn("agent scheduler: dequeue failed after retries",
				"agent", agentName, "error", err)
			return
		}
		if item == nil {
			// Queue empty — done processing.
			break
		}

		s.logger.Info("agent scheduler: processing queued message",
			"agent", agentName, "from", item.FromAgent)

		if s.bus != nil {
			s.bus.Publish(nervous.NewEvent(
				types.EventWorkQueueConsumed,
				"scheduler",
				agentName,
				map[string]string{
					"agent":      agentName,
					"from":       item.FromAgent,
					"item_id":    item.ID,
					"session_id": item.SessionID,
				},
			))
		}

		// Run completion synchronously — block until the agent finishes responding.
		if s.completionFn != nil {
			if err := s.completionFn(agentName, item.FromAgent, item.Content, item.SessionID); err != nil {
				s.logger.Error("agent completion failed — setting agent to error status",
					"agent", agentName, "error", err)
				if s.store.Agents != nil {
					if setErr := s.store.Agents.SetAgentError(ctx, agentID, err.Error()); setErr != nil {
						s.logger.Error("failed to set agent error status",
							"agent_id", agentID, "error", setErr)
					}
				}
				errored = true
				return
			}
		}
	}

	// Queue drained.
	s.logger.Debug("agent scheduler: queue drained", "agent", agentName)
	if s.bus != nil {
		s.bus.Publish(nervous.NewEvent(
			types.EventWorkQueueDrained,
			"scheduler",
			agentName,
			map[string]string{"agent": agentName},
		))
	}
}

// dequeueWithRetry wraps Dequeue with retry logic for transient SQLITE_BUSY errors.
// Returns (nil, nil) when the queue is empty, matching Dequeue semantics.
func (s *AgentScheduler) dequeueWithRetry(ctx context.Context, agentName string) (*types.WorkQueueItem, error) {
	const maxRetries = 3
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		item, err := s.store.WorkQueue.Dequeue(ctx, agentName)
		if err == nil {
			return item, nil
		}
		// Only retry on SQLITE_BUSY / database-locked errors.
		if !strings.Contains(err.Error(), "SQLITE_BUSY") && !strings.Contains(err.Error(), "database is locked") {
			return nil, err
		}
		lastErr = err
		if attempt < maxRetries {
			backoff := time.Duration(1<<uint(attempt)) * 500 * time.Millisecond
			s.logger.Debug("agent scheduler: retrying dequeue after SQLITE_BUSY",
				"agent", agentName, "attempt", attempt+1, "backoff", backoff)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}
	}
	return nil, lastErr
}

// setAgentActive sets the agent's status to "active" if currently "idle" or "error".
// Agents in "error" status are allowed to transition to "active" so they can
// attempt to process new messages — a successful completion will clear the error.
// Returns false if the agent was already active (lost the race) or in another
// non-recoverable state.
func (s *AgentScheduler) setAgentActive(ctx context.Context, agentID, agentName string) bool {
	agent, err := s.store.Agents.Get(ctx, agentID)
	if err != nil {
		return false
	}
	if agent.Status != "idle" && agent.Status != repo.AgentStatusError {
		return false
	}

	if agent.Status == repo.AgentStatusError {
		s.logger.Info("agent scheduler: attempting to recover error'd agent",
			"agent", agentName, "previous_reason", agent.StatusReason)
	}

	agent.Status = "active"
	agent.StatusReason = ""
	if err := s.store.Agents.Update(ctx, agentID, agent); err != nil {
		s.logger.Warn("agent scheduler: failed to set agent active",
			"agent", agentName, "error", err)
		return false
	}
	return true
}

// setAgentIdle resets the agent's status to "idle" and clears any status reason.
// This is called after a successful completion or when the queue drains, so it
// also clears "error" status — a successful completion proves the agent is healthy.
func (s *AgentScheduler) setAgentIdle(ctx context.Context, agentID, agentName string) {
	agent, err := s.store.Agents.Get(ctx, agentID)
	if err != nil {
		s.logger.Warn("agent scheduler: failed to get agent for idle reset",
			"agent", agentName, "error", err)
		return
	}
	if agent.Status == repo.AgentStatusIdle {
		return
	}

	if agent.Status == repo.AgentStatusError {
		s.logger.Info("agent scheduler: clearing error status after successful completion",
			"agent", agentName, "previous_reason", agent.StatusReason)
	}

	agent.Status = "idle"
	agent.StatusReason = ""
	if err := s.store.Agents.Update(ctx, agentID, agent); err != nil {
		s.logger.Error("failed to set agent idle — agent may be stuck active",
			"agent", agentName, "agent_id", agentID, "error", err)
	}
}

// RecoverOnStartup resets any agents stuck in "active" status from a
// previous run. On startup no drain loops are running, so all active
// agents are stale and must be returned to "idle" for the scheduler
// to pick them up again.
func (s *AgentScheduler) RecoverOnStartup(ctx context.Context) {
	if s.store.Agents == nil {
		return
	}

	agents, err := s.store.Agents.List(ctx)
	if err != nil {
		s.logger.Warn("agent scheduler: recovery failed to list agents", "error", err)
		return
	}

	recovered := 0
	for _, agent := range agents {
		// Reset active agents (stale from previous run), error agents
		// (transient errors like provider failures may have resolved),
		// and suspended agents (provider may have been re-enabled).
		if agent.Status != repo.AgentStatusActive && agent.Status != repo.AgentStatusError && agent.Status != "suspended" {
			continue
		}

		prevStatus := agent.Status
		agent.Status = "idle"
		agent.StatusReason = ""
		if err := s.store.Agents.Update(ctx, agent.ID, agent); err != nil {
			s.logger.Warn("agent scheduler: failed to reset stale agent",
				"agent", agent.Name, "error", err)
			continue
		}

		s.logger.Info("agent scheduler: recovered stale agent",
			"agent", agent.Name, "agent_id", agent.ID, "previous_status", prevStatus)
		recovered++
	}

	if recovered > 0 {
		s.logger.Info("agent scheduler: startup recovery complete", "recovered", recovered)
	}
}
