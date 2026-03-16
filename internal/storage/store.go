package storage

import (
	"io"

	"github.com/hyperax/hyperax/internal/repo"
)

// Store is the composition root that wires all repository implementations.
// Handlers access individual repositories: store.Workspaces, store.Config, etc.
type Store struct {
	Workspaces    repo.WorkspaceRepo
	Config        repo.ConfigRepo
	Symbols       repo.SymbolRepo
	Search        repo.SearchRepo
	Projects      repo.ProjectRepo
	Pipelines     repo.PipelineRepo
	Audits        repo.AuditRepo
	Interjections repo.InterjectionRepo
	Agents        repo.AgentRepo
	Git           repo.GitRepo
	Metrics       repo.MetricsRepo
	Memory        repo.MemoryRepo
	Lifecycle     repo.LifecycleRepo
	Secrets       repo.SecretRepo
	Budgets       repo.BudgetRepo
	Cron          repo.CronRepo
	Workflows     repo.WorkflowRepo
	Nervous       repo.NervousRepo
	CommHub       repo.CommHubRepo
	Telemetry     repo.TelemetryRepo
	Providers     repo.ProviderRepo
	Checkpoints   repo.CheckpointRepo
	Vectors       repo.VectorRepo
	MCPTokens     repo.MCPTokenRepo
	Delegations   repo.DelegationRepo
	AgentMail     repo.AgentMailRepo
	Plugins       repo.PluginRepo
	ExternalDocs  repo.ExternalDocRepo
	Sessions      repo.SessionRepo
	WorkQueue     repo.WorkQueueRepo
	Specs repo.SpecRepo

	// Closer is the underlying database connection closer.
	Closer io.Closer
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	if s.Closer != nil {
		return s.Closer.Close()
	}
	return nil
}
