package approval

import (
	"context"
	"errors"

	"go.temporal.io/sdk/client"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// SignalName is the Temporal signal channel name the workflow reads from
// (see recovery/workflow.go's signal-receiver goroutine). Keep this constant
// in sync with the workflow side — they MUST match exactly.
const SignalName = "approval.decision"

// TemporalSignaler is the narrow port the HTTP approve/reject endpoint
// depends on. Production wires it to the real Temporal client below;
// tests substitute a fake that records the signal payload.
type TemporalSignaler interface {
	SignalApproval(ctx context.Context, workflowID string, sig domain.ApprovalSignal) error
}

// ClientSignaler implements TemporalSignaler against the Temporal Go SDK
// client. The workflowID we receive is the workflow_run_id stored on the
// approval_decisions row — same value the WorkflowService used to start the
// workflow (workflowID = runID).
type ClientSignaler struct {
	c client.Client
}

// NewClientSignaler constructs a ClientSignaler. The Temporal client must be
// the same one used to start workflows (workflow_run_id is identical to the
// Temporal WorkflowID per workflow/service.go).
func NewClientSignaler(c client.Client) *ClientSignaler {
	return &ClientSignaler{c: c}
}

// SignalApproval delivers the ApprovalSignal to the named workflow. The
// SDK's SignalWorkflow accepts an empty runID — Temporal routes to the
// latest open execution which is what we want (the workflow is parked on
// the signal channel; there is no replay/retry race here).
func (s *ClientSignaler) SignalApproval(ctx context.Context, workflowID string, sig domain.ApprovalSignal) error {
	if s.c == nil {
		return errors.New("temporal client not configured")
	}
	return s.c.SignalWorkflow(ctx, workflowID, "", SignalName, sig)
}

// SignalerService is the usecase-style facade the HTTP handler depends on.
// It enforces the state invariants (must be pending, must belong to the
// caller's workspace) before firing the signal AND persisting the row.
//
// Ordering: we signal Temporal FIRST, then UPDATE the row. If the signal
// fails the row stays pending; if the UPDATE fails post-signal the workflow
// has already advanced and the next GetRun read will reconcile (the workflow
// returns the decision in its output payload).
type SignalerService struct {
	repo     domain.ApprovalRepository
	tem      TemporalSignaler
	service  *Service
}

// NewSignalerService wires the components together.
func NewSignalerService(r domain.ApprovalRepository, t TemporalSignaler, svc *Service) *SignalerService {
	return &SignalerService{repo: r, tem: t, service: svc}
}

// DecideInput is the HTTP handler's resolved payload.
type DecideInput struct {
	Principal     domain.Principal
	WorkspaceID   string
	WorkflowRunID string
	Decision      domain.ApprovalDecisionState // approved | rejected
	Notes         string
}

// Decide is the entry point for POST /v1/approvals/{run_id}/decide. It
// validates state, fires the Temporal signal, and records the terminal
// decision row.
//
// Returns domain.ErrNotFound when the row doesn't exist (or RLS hid it),
// domain.ErrConflict when the row is already terminal, and
// domain.ErrForbidden when the caller's workspace doesn't match.
func (s *SignalerService) Decide(ctx context.Context, in DecideInput) error {
	if in.Decision != domain.ApprovalApproved && in.Decision != domain.ApprovalRejected {
		return errors.New("approval: decision must be approved|rejected")
	}
	d, err := s.repo.GetByRun(ctx, in.WorkflowRunID)
	if err != nil {
		return err
	}
	if d.Decision != domain.ApprovalPending {
		return domain.ErrConflict
	}
	if d.WorkspaceID != in.WorkspaceID {
		return domain.ErrForbidden
	}
	sig := domain.ApprovalSignal{
		Decision:  in.Decision,
		DecidedBy: in.Principal.UserID,
		Notes:     in.Notes,
	}
	if s.tem != nil {
		if err := s.tem.SignalApproval(ctx, in.WorkflowRunID, sig); err != nil {
			return err
		}
	}
	return s.service.RecordDecision(ctx, in.WorkflowRunID, in.Principal.OrgID, sig)
}
