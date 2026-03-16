package interject

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/pkg/types"
)

// ChildResolverFunc returns the child agent IDs for a given parent agent.
// This is injected by the application wiring layer to allow hierarchy-aware
// cascade halts without creating a circular dependency on commhub.
type ChildResolverFunc func(ctx context.Context, parentAgent string) ([]string, error)

// RemediationDispatchFunc sends a remediation message to a persona.
// The function receives the target persona ID, the interjection ID, and a
// human-readable description of the issue. It is injected by the application
// wiring layer to dispatch via CommHub without a direct dependency.
type RemediationDispatchFunc func(ctx context.Context, personaID, interjectionID, description string) error

// ConfigLookupFunc retrieves a config value by key. Used to look up the
// default remediation persona from the "safety.remediation_persona" config key.
type ConfigLookupFunc func(ctx context.Context, key string) (string, error)

// Manager orchestrates the Andon Cord interjection lifecycle: halt, safe mode
// coordination, clearance validation, and resolution. It is the single entry
// point for all interjection operations.
type Manager struct {
	repo                repo.InterjectionRepo
	bus                 *nervous.EventBus
	logger              *slog.Logger
	safeMode            *SafeModeController
	childResolver       ChildResolverFunc
	remediationDispatch RemediationDispatchFunc
	configLookup        ConfigLookupFunc
}

// NewManager creates an InterjectionManager wired to the repo and EventBus.
func NewManager(ijRepo repo.InterjectionRepo, bus *nervous.EventBus, logger *slog.Logger) *Manager {
	m := &Manager{
		repo:   ijRepo,
		bus:    bus,
		logger: logger.With("component", "interject"),
	}
	m.safeMode = NewSafeModeController(m, bus, logger)
	return m
}

// SafeMode returns the SafeModeController for querying halt state.
func (m *Manager) SafeMode() *SafeModeController {
	return m.safeMode
}

// SetChildResolver configures the function used to resolve child agents
// for cascading halts through the agent hierarchy. Must be called after
// the CommHub hierarchy is initialised.
func (m *Manager) SetChildResolver(fn ChildResolverFunc) {
	m.childResolver = fn
}

// SetRemediationDispatch configures the function used to send remediation
// messages to a designated persona when a halt fires.
func (m *Manager) SetRemediationDispatch(fn RemediationDispatchFunc) {
	m.remediationDispatch = fn
}

// SetConfigLookup configures the function used to look up config values
// (e.g., "safety.remediation_persona" for the default remediation persona).
func (m *Manager) SetConfigLookup(fn ConfigLookupFunc) {
	m.configLookup = fn
}

// Halt creates an interjection and activates Safe Mode for the affected scope.
// It validates the caller's clearance, stores the interjection, publishes a
// halt event on the Nervous System, and engages Safe Mode.
func (m *Manager) Halt(ctx context.Context, ij *types.Interjection) (string, error) {
	// Validate caller clearance if specified.
	if ij.CreatedBy != "" {
		clearance, err := m.repo.GetClearanceLevel(ctx, ij.CreatedBy)
		if err != nil {
			m.logger.Warn("clearance lookup failed, proceeding with level 0",
				"persona_id", ij.CreatedBy, "error", err)
		} else {
			ij.SourceClearance = clearance
		}
	}

	// Store the interjection.
	id, err := m.repo.Create(ctx, ij)
	if err != nil {
		return "", fmt.Errorf("interject.Manager.Halt: %w", err)
	}

	m.logger.Warn("andon cord pulled",
		"id", id,
		"scope", ij.Scope,
		"severity", ij.Severity,
		"source", ij.Source,
		"reason", ij.Reason,
	)

	// Publish halt event on Nervous System (Flood Push).
	if m.bus != nil {
		m.bus.Publish(nervous.NewEvent(
			types.EventInterjectHalt,
			"interject.manager",
			ij.Scope,
			map[string]any{
				"interjection_id": id,
				"scope":           ij.Scope,
				"severity":        ij.Severity,
				"source":          ij.Source,
				"reason":          ij.Reason,
			},
		))
	}

	// Engage Safe Mode for this scope (critical and fatal only).
	if ij.Severity == string(types.SeverityCritical) || ij.Severity == string(types.SeverityFatal) {
		m.safeMode.Engage(ij.Scope, id)
	}

	// Cascade halt down the agent hierarchy. Uses the interjection's Scope
	// as the root agent to walk children from. Cascading is only performed
	// when a ChildResolver is configured and for critical/fatal severity.
	if (ij.Severity == string(types.SeverityCritical) || ij.Severity == string(types.SeverityFatal)) && ij.Scope != string(types.ScopeGlobal) {
		m.cascadeHalt(ctx, ij, id)
	}

	// Dispatch remediation persona if configured.
	m.dispatchRemediation(ctx, ij, id)

	return id, nil
}

// Resolve clears an interjection after validating the resolver's clearance.
// The resolver must have clearance >= the interjection's source clearance.
func (m *Manager) Resolve(ctx context.Context, action *types.ResolutionAction) error {
	// Fetch the interjection to validate clearance.
	ij, err := m.repo.GetByID(ctx, action.InterjectionID)
	if err != nil {
		return fmt.Errorf("interject.Manager.Resolve: %w", err)
	}

	// Validate clearance if resolver is specified.
	if action.ResolvedBy != "" {
		clearance, err := m.repo.GetClearanceLevel(ctx, action.ResolvedBy)
		if err != nil {
			m.logger.Warn("resolver clearance lookup failed",
				"persona_id", action.ResolvedBy, "error", err)
		} else {
			action.ResolverClearance = clearance
			if clearance < ij.SourceClearance {
				return fmt.Errorf("insufficient clearance: resolver has %d, interjection requires %d",
					clearance, ij.SourceClearance)
			}
		}
	}

	// Validate action.
	switch action.Action {
	case "resume", "abort", "retry":
		// valid
	case "":
		action.Action = "resume"
	default:
		return fmt.Errorf("invalid action %q: must be resume, abort, or retry", action.Action)
	}

	// Resolve in the database.
	if err := m.repo.Resolve(ctx, action.InterjectionID, action); err != nil {
		return fmt.Errorf("interject.Manager.Resolve: %w", err)
	}

	m.logger.Info("interjection resolved",
		"id", action.InterjectionID,
		"action", action.Action,
		"resolved_by", action.ResolvedBy,
	)

	// Publish resolve event.
	if m.bus != nil {
		m.bus.Publish(nervous.NewEvent(
			types.EventInterjectResolve,
			"interject.manager",
			ij.Scope,
			map[string]any{
				"interjection_id": action.InterjectionID,
				"action":          action.Action,
				"resolved_by":     action.ResolvedBy,
				"scope":           ij.Scope,
			},
		))
	}

	// Disengage Safe Mode if no more active interjections for this scope.
	m.disengageSafeModeIfClear(ctx, ij.Scope)

	return nil
}

// GetActive returns active interjections for a scope.
func (m *Manager) GetActive(ctx context.Context, scope string) ([]*types.Interjection, error) {
	ijs, err := m.repo.GetActive(ctx, scope)
	if err != nil {
		return nil, fmt.Errorf("interject.Manager.GetActive: %w", err)
	}
	return ijs, nil
}

// GetAllActive returns all active interjections across all scopes.
func (m *Manager) GetAllActive(ctx context.Context) ([]*types.Interjection, error) {
	ijs, err := m.repo.GetAllActive(ctx)
	if err != nil {
		return nil, fmt.Errorf("interject.Manager.GetAllActive: %w", err)
	}
	return ijs, nil
}

// GetByID returns a single interjection.
func (m *Manager) GetByID(ctx context.Context, id string) (*types.Interjection, error) {
	ij, err := m.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("interject.Manager.GetByID: %w", err)
	}
	return ij, nil
}

// GetHistory returns resolved interjections for audit trail.
func (m *Manager) GetHistory(ctx context.Context, scope string, limit int) ([]*types.Interjection, error) {
	ijs, err := m.repo.GetHistory(ctx, scope, limit)
	if err != nil {
		return nil, fmt.Errorf("interject.Manager.GetHistory: %w", err)
	}
	return ijs, nil
}

// IsHalted returns true if the given scope has any active critical/fatal interjections.
func (m *Manager) IsHalted(ctx context.Context, scope string) bool {
	active, err := m.repo.GetActive(ctx, scope)
	if err != nil {
		return false
	}
	for _, ij := range active {
		if ij.Severity == string(types.SeverityCritical) || ij.Severity == string(types.SeverityFatal) {
			return true
		}
	}
	// Also check global scope — a global halt affects all scopes.
	if scope != string(types.ScopeGlobal) {
		globals, err := m.repo.GetActive(ctx, string(types.ScopeGlobal))
		if err != nil {
			return false
		}
		for _, ij := range globals {
			if ij.Severity == string(types.SeverityCritical) || ij.Severity == string(types.SeverityFatal) {
				return true
			}
		}
	}
	return false
}

// RecoverOnStartup checks for active interjections from a previous run
// and re-engages Safe Mode for any affected scopes.
func (m *Manager) RecoverOnStartup(ctx context.Context) error {
	active, err := m.repo.GetAllActive(ctx)
	if err != nil {
		return fmt.Errorf("interject.Manager.RecoverOnStartup: %w", err)
	}

	for _, ij := range active {
		if ij.Severity == string(types.SeverityCritical) || ij.Severity == string(types.SeverityFatal) {
			m.safeMode.Engage(ij.Scope, ij.ID)
			m.logger.Warn("recovered active halt from previous run",
				"id", ij.ID,
				"scope", ij.Scope,
				"severity", ij.Severity,
			)
		}
	}

	if len(active) > 0 {
		m.logger.Info("startup halt recovery complete", "active_count", len(active))
	}

	return nil
}

// ExpireTTL checks for interjections past their TTL and expires them.
func (m *Manager) ExpireTTL(ctx context.Context) (int, error) {
	active, err := m.repo.GetAllActive(ctx)
	if err != nil {
		return 0, fmt.Errorf("interject.Manager.ExpireTTL: %w", err)
	}

	expired := 0
	for _, ij := range active {
		if ij.ExpiresAt != nil && !ij.ExpiresAt.IsZero() {
			// Use a simple time.Now() comparison.
			if ij.ExpiresAt.Before(timeNow()) {
				if err := m.repo.Expire(ctx, ij.ID); err != nil {
					m.logger.Error("expire interjection failed", "id", ij.ID, "error", err)
					continue
				}
				expired++
				m.disengageSafeModeIfClear(ctx, ij.Scope)
			}
		}
	}

	return expired, nil
}

// cascadeHalt walks the agent hierarchy tree downward from the interjection's
// scope agent (CreatedBy), creating child interjections for each descendant.
// Each cascaded halt publishes an EventInterjectCascade event. The walk is
// recursive up to maxCascadeDepth to prevent infinite loops in cyclic hierarchies.
func (m *Manager) cascadeHalt(ctx context.Context, parent *types.Interjection, parentID string) {
	if m.childResolver == nil {
		return
	}

	m.cascadeWalk(ctx, parent, parentID, parent.Scope, 0)
}

// maxCascadeDepth limits the hierarchy walk to prevent runaway recursion.
const maxCascadeDepth = 20

// cascadeWalk recursively halts child agents down the hierarchy tree.
func (m *Manager) cascadeWalk(ctx context.Context, parent *types.Interjection, parentID, agentID string, depth int) {
	if depth >= maxCascadeDepth {
		m.logger.Warn("cascade halt depth limit reached",
			"agent", agentID,
			"parent_interjection", parentID,
			"depth", depth,
		)
		return
	}

	children, err := m.childResolver(ctx, agentID)
	if err != nil {
		m.logger.Debug("cascade halt: no children found",
			"agent", agentID,
			"error", err,
		)
		return
	}

	for _, childAgent := range children {
		childIJ := &types.Interjection{
			Scope:     childAgent,
			Severity:  parent.Severity,
			Source:    "cascade:" + parent.Source,
			Reason:    fmt.Sprintf("Cascaded from parent interjection %s: %s", parentID, parent.Reason),
			CreatedBy: parent.CreatedBy,
			TraceID:   parent.TraceID,
		}
		if parent.ExpiresAt != nil {
			t := *parent.ExpiresAt
			childIJ.ExpiresAt = &t
		}

		childID, err := m.repo.Create(ctx, childIJ)
		if err != nil {
			m.logger.Error("cascade halt: failed to create child interjection",
				"child_agent", childAgent,
				"parent_interjection", parentID,
				"error", err,
			)
			continue
		}

		// Engage Safe Mode for child scope if critical/fatal.
		if childIJ.Severity == string(types.SeverityCritical) || childIJ.Severity == string(types.SeverityFatal) {
			m.safeMode.Engage(childAgent, childID)
		}

		// Publish cascade event.
		if m.bus != nil {
			m.bus.Publish(nervous.NewEvent(
				types.EventInterjectCascade,
				"interject.manager",
				childAgent,
				map[string]any{
					"parent_interjection_id": parentID,
					"child_interjection_id":  childID,
					"child_agent":            childAgent,
					"severity":               childIJ.Severity,
				},
			))
		}

		m.logger.Warn("cascade halt propagated to child agent",
			"child_agent", childAgent,
			"child_interjection_id", childID,
			"parent_interjection_id", parentID,
		)

		// Recurse into child's children.
		m.cascadeWalk(ctx, parent, parentID, childAgent, depth+1)
	}
}

// dispatchRemediation sends a message to the remediation persona to investigate
// and resolve the interjection. The persona is determined by:
//  1. The interjection's RemediationPersona field (explicit per-halt override)
//  2. The "safety.remediation_persona" config key (system default)
//
// If neither is set or no dispatch function is configured, this is a no-op.
func (m *Manager) dispatchRemediation(ctx context.Context, ij *types.Interjection, interjectionID string) {
	if m.remediationDispatch == nil {
		return
	}

	persona := ij.RemediationPersona

	// Fall back to the configured default remediation persona.
	if persona == "" && m.configLookup != nil {
		val, err := m.configLookup(ctx, "safety.remediation_persona")
		if err != nil || val == "" {
			return
		}
		persona = val
	}

	if persona == "" {
		return
	}

	description := fmt.Sprintf(
		"INTERJECTION ALERT [%s/%s]: %s (scope=%s, source=%s, interjection_id=%s). Investigate and resolve.",
		ij.Severity, ij.Scope, ij.Reason, ij.Scope, ij.Source, interjectionID,
	)

	if err := m.remediationDispatch(ctx, persona, interjectionID, description); err != nil {
		m.logger.Error("remediation dispatch failed",
			"persona", persona,
			"interjection_id", interjectionID,
			"error", err,
		)
		return
	}

	m.logger.Info("remediation persona dispatched",
		"persona", persona,
		"interjection_id", interjectionID,
	)
}

// disengageSafeModeIfClear disengages Safe Mode for a scope if no more
// active critical/fatal interjections remain.
func (m *Manager) disengageSafeModeIfClear(ctx context.Context, scope string) {
	active, err := m.repo.GetActive(ctx, scope)
	if err != nil {
		return
	}

	hasHalt := false
	for _, ij := range active {
		if ij.Severity == string(types.SeverityCritical) || ij.Severity == string(types.SeverityFatal) {
			hasHalt = true
			break
		}
	}

	if !hasHalt {
		m.safeMode.Disengage(scope)

		if m.bus != nil {
			m.bus.Publish(nervous.NewEvent(
				types.EventInterjectSafeMode,
				"interject.manager",
				scope,
				map[string]any{
					"scope":  scope,
					"action": "disengaged",
				},
			))
		}
	}
}

// EnqueueDLQ adds an entry to the dead letter queue for the given interjection.
func (m *Manager) EnqueueDLQ(ctx context.Context, entry *types.DLQEntry) (string, error) {
	id, err := m.repo.EnqueueDLQ(ctx, entry)
	if err != nil {
		return "", fmt.Errorf("interject.Manager.EnqueueDLQ: %w", err)
	}
	return id, nil
}

// ListDLQ returns queued DLQ entries for an interjection.
func (m *Manager) ListDLQ(ctx context.Context, interjectionID string, limit int) ([]*types.DLQEntry, error) {
	entries, err := m.repo.ListDLQ(ctx, interjectionID, limit)
	if err != nil {
		return nil, fmt.Errorf("interject.Manager.ListDLQ: %w", err)
	}
	return entries, nil
}

// ReplayDLQ marks a DLQ entry as replayed.
func (m *Manager) ReplayDLQ(ctx context.Context, id string) error {
	if err := m.repo.ReplayDLQ(ctx, id); err != nil {
		return fmt.Errorf("interject.Manager.ReplayDLQ: %w", err)
	}
	return nil
}

// DismissDLQ marks a DLQ entry as dismissed.
func (m *Manager) DismissDLQ(ctx context.Context, id string) error {
	if err := m.repo.DismissDLQ(ctx, id); err != nil {
		return fmt.Errorf("interject.Manager.DismissDLQ: %w", err)
	}
	return nil
}

// --- Sieve Bypass ---

// GetClearanceLevel returns the clearance level for a persona, delegating to the repo.
func (m *Manager) GetClearanceLevel(ctx context.Context, personaID string) (int, error) {
	level, err := m.repo.GetClearanceLevel(ctx, personaID)
	if err != nil {
		return 0, fmt.Errorf("interject.Manager.GetClearanceLevel: %w", err)
	}
	return level, nil
}

// GrantBypass creates a temporary sieve bypass for a scope.
func (m *Manager) GrantBypass(ctx context.Context, bypass *types.SieveBypass) (string, error) {
	id, err := m.repo.CreateBypass(ctx, bypass)
	if err != nil {
		return "", fmt.Errorf("interject.Manager.GrantBypass: %w", err)
	}

	if m.bus != nil {
		m.bus.Publish(nervous.NewEvent(
			types.EventInterjectSieveBypassGranted,
			"interject.manager",
			bypass.Scope,
			map[string]any{
				"bypass_id": id,
				"scope":     bypass.Scope,
				"pattern":   bypass.Pattern,
				"expires":   bypass.ExpiresAt,
			},
		))
	}

	return id, nil
}

// RevokeBypass revokes a sieve bypass.
func (m *Manager) RevokeBypass(ctx context.Context, id string) error {
	if err := m.repo.RevokeBypass(ctx, id); err != nil {
		return fmt.Errorf("interject.Manager.RevokeBypass: %w", err)
	}
	return nil
}

// GetActiveBypasses returns active bypasses for a scope.
func (m *Manager) GetActiveBypasses(ctx context.Context, scope string) ([]*types.SieveBypass, error) {
	bypasses, err := m.repo.GetActiveBypass(ctx, scope)
	if err != nil {
		return nil, fmt.Errorf("interject.Manager.GetActiveBypasses: %w", err)
	}
	return bypasses, nil
}

// ExpireBypasses marks all expired bypasses as revoked and publishes events.
func (m *Manager) ExpireBypasses(ctx context.Context) (int, error) {
	n, err := m.repo.ExpireBypasses(ctx)
	if err != nil {
		return 0, fmt.Errorf("interject.Manager.ExpireBypasses: %w", err)
	}

	if n > 0 && m.bus != nil {
		m.bus.Publish(nervous.NewEvent(
			types.EventInterjectSieveBypassExpired,
			"interject.manager",
			"system",
			map[string]any{"expired_count": n},
		))
	}

	return n, nil
}
