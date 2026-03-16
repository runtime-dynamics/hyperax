package types

// Workflow status and step status constants.
const (
	// WorkflowStatus values.
	WorkflowStatusPending   = "pending"
	WorkflowStatusRunning   = "running"
	WorkflowStatusCompleted = "completed"
	WorkflowStatusFailed    = "failed"
	WorkflowStatusCancelled = "cancelled"

	// StepStatus values.
	StepStatusPending         = "pending"
	StepStatusRunning         = "running"
	StepStatusCompleted       = "completed"
	StepStatusFailed          = "failed"
	StepStatusSkipped         = "skipped"
	StepStatusWaitingApproval = "waiting_approval"

	// StepType values.
	StepTypeTool      = "tool"
	StepTypeCondition = "condition"
	StepTypeApproval  = "approval"

	// TriggerType values.
	TriggerTypeManual  = "manual"
	TriggerTypeEvent   = "event"
	TriggerTypeCron    = "cron"
	TriggerTypeWebhook = "webhook"
)

// Workflow event types for the Nervous System.
const (
	EventWorkflowStarted   EventType = "workflow.started"
	EventWorkflowCompleted EventType = "workflow.completed"
	EventWorkflowFailed    EventType = "workflow.failed"
	EventWorkflowStepStart EventType = "workflow.step.start"
	EventWorkflowStepDone  EventType = "workflow.step.done"
	EventWorkflowApproval  EventType = "workflow.approval.pending"
)
