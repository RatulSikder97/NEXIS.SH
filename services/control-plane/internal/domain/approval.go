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
	// ApprovalModified is the RLHF state: an engineer edited the proposed
	// patch before approving it. Treated as an approval by the workflow (the
	// GitOps deploy ships the MODIFIED diff), and recorded in
	// feedback_examples with both the original and the edited diff so the
	// fine-tuning export carries the correction signal.
	ApprovalModified ApprovalDecisionState = "modified"
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
	Decision  ApprovalDecisionState // approved | rejected | modified | auto_approved | timeout_rejected
	DecidedBy string                // user uuid; "" on auto/timeout
	Notes     string
	// ModifiedDiff is the engineer-edited unified diff, set only when
	// Decision == ApprovalModified. The workflow substitutes it for the
	// Backend agent's patch before the GitOps deploy.
	ModifiedDiff string `json:"modified_diff,omitempty"`
}

// ApprovalRepository is the port the approval service + HTTP handlers depend
// on. Implementations live in internal/adapter/repo/approval_repo.go.
type ApprovalRepository interface {
	Create(ctx context.Context, d ApprovalDecision) (string, error) // returns id
	GetByRun(ctx context.Context, runID string) (ApprovalDecision, error)
	UpdateDecision(ctx context.Context, runID string, state ApprovalDecisionState, decidedBy, notes string, at time.Time) error
	ListPending(ctx context.Context, orgID string, limit int) ([]ApprovalDecision, error)
}

// FeedbackExample is one RLHF training example persisted in
// feedback_examples: the scenario + the agent's proposed patch + the human
// decision (and edited diff, when the decision was "modified"). ExportedAt
// implements the JSONL export cursor — nil means the row has not yet been
// pulled into a fine-tuning dataset.
type FeedbackExample struct {
	ID            string
	OrgID         string
	IncidentID    string // incidents_raw uuid OR 'manual' / 'demo'
	WorkflowRunID string
	Scenario      string
	PatchDiff     string
	Decision      string // approved | rejected | modified
	ModifiedDiff  string // "" unless Decision == modified
	DecidedBy     string // user uuid; "" on auto/timeout decisions
	DecidedAt     time.Time
	ExportedAt    *time.Time
	CreatedAt     time.Time
}

// FeedbackRepository is the port the approval service (write side) and the
// RLHF export handler (read side) depend on. Implementation lives in
// internal/adapter/repo/feedback_repo.go.
type FeedbackRepository interface {
	// Insert lands one example. Activity-side — runs outside any request tx.
	Insert(ctx context.Context, fe FeedbackExample) (string, error)
	// ExportUnexported atomically stamps exported_at=now() on up to limit
	// unexported rows for the org and returns them oldest-first.
	ExportUnexported(ctx context.Context, orgID string, limit int) ([]FeedbackExample, error)
}
