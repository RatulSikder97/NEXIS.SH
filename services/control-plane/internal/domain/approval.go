package domain

import (
	"context"
	"time"
)

// Severity is the routing key for the Approval Gate. low → auto-approved;
// medium → 2-minute timer race; high → block until signal.
type Severity string

const (
	SeverityLow    Severity = "low"
	SeverityMedium Severity = "medium"
	SeverityHigh   Severity = "high"
)

// ApprovalDecisionState mirrors the approval_decisions.decision CHECK column.
type ApprovalDecisionState string

const (
	ApprovalPending         ApprovalDecisionState = "pending"
	ApprovalApproved        ApprovalDecisionState = "approved"
	ApprovalRejected        ApprovalDecisionState = "rejected"
	ApprovalAutoApproved    ApprovalDecisionState = "auto_approved"
	ApprovalTimeoutRejected ApprovalDecisionState = "timeout_rejected"
)

// ApprovalDecision is the projection persisted in approval_decisions. One
// row per workflow_run_id; updates flip decision + decided_by + decided_at
// once a signal lands (or the timer fires).
type ApprovalDecision struct {
	ID            string
	OrgID         string
	WorkspaceID   string
	WorkflowRunID string
	Severity      Severity
	Decision      ApprovalDecisionState
	DecidedBy     string // user uuid or "" if pending / auto
	DecidedAt     time.Time
	Notes         string
	Scenario      string
	RiskScore     float64
	CreatedAt     time.Time
}

// ApprovalSignal is the payload the HTTP handler sends into Temporal via
// SignalWithStart-style signal. The workflow's selector reads it.
type ApprovalSignal struct {
	Decision  ApprovalDecisionState // approved | rejected | auto_approved | timeout_rejected
	DecidedBy string                // user uuid; "" on auto/timeout
	Notes     string
}

// ApprovalRepository is the port the approval service + HTTP handlers depend
// on. Implementations live in internal/adapter/repo/approval_repo.go.
type ApprovalRepository interface {
	Create(ctx context.Context, d ApprovalDecision) (string, error) // returns id
	GetByRun(ctx context.Context, runID string) (ApprovalDecision, error)
	UpdateDecision(ctx context.Context, runID string, state ApprovalDecisionState, decidedBy, notes string, at time.Time) error
	ListPending(ctx context.Context, orgID string, limit int) ([]ApprovalDecision, error)
}
