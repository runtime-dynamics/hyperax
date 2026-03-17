package delegation

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/internal/secrets"
	"github.com/hyperax/hyperax/pkg/types"
)

// Service manages delegation lifecycle including credential passthrough.
type Service struct {
	repo    repo.DelegationRepo
	secrets *secrets.Registry
	bus     *nervous.EventBus
	logger  *slog.Logger
	idFunc  func() string
}

// NewService creates a delegation Service.
func NewService(r repo.DelegationRepo, sec *secrets.Registry, bus *nervous.EventBus, logger *slog.Logger) *Service {
	return &Service{
		repo:    r,
		secrets: sec,
		bus:     bus,
		logger:  logger.With("component", "delegation"),
		idFunc:  generateID,
	}
}

// Grant creates a new delegation. For credential_passthrough grants, the
// credential value is stored in the active secret provider under
// key "delegation:{grant_id}" and the delegation.credential_key is set
// to that reference.
func (s *Service) Grant(ctx context.Context, req GrantRequest) (*types.Delegation, error) {
	if req.GranterID == "" {
		return nil, fmt.Errorf("delegation.Service.Grant: granter_id is required")
	}
	if req.GranteeID == "" {
		return nil, fmt.Errorf("delegation.Service.Grant: grantee_id is required")
	}
	if req.GranterID == req.GranteeID {
		return nil, fmt.Errorf("delegation.Service.Grant: cannot delegate to self")
	}

	d := &types.Delegation{
		ID:            s.idFunc(),
		GranterID:     req.GranterID,
		GranteeID:     req.GranteeID,
		GrantType:     req.GrantType,
		ElevatedLevel: req.ElevatedLevel,
		Scopes:        req.Scopes,
		ExpiresAt:     req.ExpiresAt,
		Reason:        req.Reason,
	}

	// For credential passthrough, store the credential in the secret provider.
	if req.GrantType == types.GrantCredentialPassthrough {
		if req.Credential == "" {
			return nil, fmt.Errorf("delegation.Service.Grant: credential is required for credential_passthrough grant")
		}

		credKey := fmt.Sprintf("delegation:%s", d.ID)
		if s.secrets != nil {
			provider, err := s.secrets.Active()
			if err != nil {
				return nil, fmt.Errorf("delegation.Service.Grant: %w", err)
			}
			if err := provider.Set(ctx, credKey, req.Credential, "global"); err != nil {
				return nil, fmt.Errorf("delegation.Service.Grant: %w", err)
			}
		}
		d.CredentialKey = credKey
	}

	if err := s.repo.Create(ctx, d); err != nil {
		return nil, fmt.Errorf("delegation.Service.Grant: %w", err)
	}

	s.logger.Info("delegation granted",
		"id", d.ID,
		"granter", d.GranterID,
		"grantee", d.GranteeID,
		"type", string(d.GrantType),
	)

	s.publishEvent(types.EventDelegationGranted, map[string]string{
		"delegation_id": d.ID,
		"granter_id":    d.GranterID,
		"grantee_id":    d.GranteeID,
		"grant_type":    string(d.GrantType),
	})

	return d, nil
}

// Revoke revokes a delegation and cleans up any stored credentials.
func (s *Service) Revoke(ctx context.Context, id string) error {
	d, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("delegation.Service.Revoke: %w", err)
	}

	// Clean up stored credential if this was a passthrough grant.
	if d.GrantType == types.GrantCredentialPassthrough && d.CredentialKey != "" && s.secrets != nil {
		provider, pErr := s.secrets.Active()
		if pErr == nil {
			if err := provider.Delete(ctx, d.CredentialKey, "global"); err != nil {
				s.logger.Error("failed to clean up credentials after delegation revoke", "key", d.CredentialKey, "error", err)
			}
		}
	}

	if err := s.repo.Revoke(ctx, id); err != nil {
		return fmt.Errorf("delegation.Service.Revoke: %w", err)
	}

	s.logger.Info("delegation revoked",
		"id", d.ID,
		"granter", d.GranterID,
		"grantee", d.GranteeID,
	)

	s.publishEvent(types.EventDelegationRevoked, map[string]string{
		"delegation_id": d.ID,
		"granter_id":    d.GranterID,
		"grantee_id":    d.GranteeID,
	})

	return nil
}

// GetCredential retrieves the stored credential for a credential_passthrough delegation.
// Only the grantee (or the granter) may retrieve the credential.
func (s *Service) GetCredential(ctx context.Context, delegationID, requesterID string) (string, error) {
	d, err := s.repo.GetByID(ctx, delegationID)
	if err != nil {
		return "", fmt.Errorf("delegation.Service.GetCredential: %w", err)
	}

	if !d.IsActive() {
		return "", fmt.Errorf("delegation.Service.GetCredential: delegation %s is not active", delegationID)
	}

	if d.GrantType != types.GrantCredentialPassthrough {
		return "", fmt.Errorf("delegation.Service.GetCredential: delegation %s is not a credential_passthrough grant", delegationID)
	}

	// Only the grantee or granter may retrieve.
	if requesterID != d.GranteeID && requesterID != d.GranterID {
		return "", fmt.Errorf("delegation.Service.GetCredential: requester %s is not authorized to access delegation %s", requesterID, delegationID)
	}

	if d.CredentialKey == "" {
		return "", fmt.Errorf("delegation.Service.GetCredential: no credential stored for delegation %s", delegationID)
	}

	if s.secrets == nil {
		return "", fmt.Errorf("delegation.Service.GetCredential: secret provider not available")
	}

	provider, err := s.secrets.Active()
	if err != nil {
		return "", fmt.Errorf("delegation.Service.GetCredential: %w", err)
	}

	val, err := provider.Get(ctx, d.CredentialKey, "global")
	if err != nil {
		return "", fmt.Errorf("delegation.Service.GetCredential: %w", err)
	}
	return val, nil
}

// ListByGrantee returns active delegations for a grantee.
func (s *Service) ListByGrantee(ctx context.Context, granteeID string) ([]*types.Delegation, error) {
	return s.repo.ListByGrantee(ctx, granteeID)
}

// ListByGranter returns all delegations by a granter.
func (s *Service) ListByGranter(ctx context.Context, granterID string) ([]*types.Delegation, error) {
	return s.repo.ListByGranter(ctx, granterID)
}

// ListAll returns all delegations across all personas.
func (s *Service) ListAll(ctx context.Context) ([]*types.Delegation, error) {
	return s.repo.ListAll(ctx)
}

// GetByID retrieves a delegation by ID.
func (s *Service) GetByID(ctx context.Context, id string) (*types.Delegation, error) {
	return s.repo.GetByID(ctx, id)
}

// CleanupExpired revokes expired delegations and cleans up their credentials.
func (s *Service) CleanupExpired(ctx context.Context) (int, error) {
	count, err := s.repo.CleanupExpired(ctx)
	if err != nil {
		return 0, fmt.Errorf("delegation.Service.CleanupExpired: %w", err)
	}
	if count > 0 {
		s.logger.Info("expired delegations cleaned up", "count", count)
		s.publishEvent(types.EventDelegationExpired, map[string]string{
			"count": fmt.Sprintf("%d", count),
		})
	}
	return count, nil
}

// publishEvent sends a NervousEvent to the EventBus if available.
func (s *Service) publishEvent(eventType string, payload map[string]string) {
	if s.bus == nil {
		return
	}
	data, err := json.Marshal(payload)
	if err != nil {
		s.logger.Error("delegation.Service.publishEvent: marshal payload failed", "error", err)
		return
	}
	s.bus.Publish(types.NervousEvent{
		Type:      types.EventType(eventType),
		Scope:     "delegation",
		Source:    "delegation.service",
		Payload:   data,
		Timestamp: time.Now(),
	})
}

// GrantRequest contains the parameters for creating a delegation.
type GrantRequest struct {
	GranterID     string
	GranteeID     string
	GrantType     types.DelegationGrantType
	Credential    string   // Only for credential_passthrough
	ElevatedLevel int      // Only for clearance_elevation
	Scopes        []string // Only for scope_access
	ExpiresAt     string
	Reason        string
}

// generateID creates a time-based unique identifier for delegations.
func generateID() string {
	return fmt.Sprintf("dlg_%d", time.Now().UnixNano())
}
