package approval

import (
	"context"
	"errors"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// Service is the activity-side glue for the Approval Gate. It owns the
// "create pending row → classify severity → fire notifier" sequence executed
// by the ApprovalGate.Route activity body, and the terminal RecordDecision
// path used by both the signal handler and the auto-timeout race.
//
// The Temporal signal channel itself lives in the workflow function (see
// recovery/workflow.go's ApprovalGate step) because workflow.GetSignalChannel
// is replay-safe only inside a workflow.Context. WaitForDecision() returns
// the channel orchestration helper used from there.
type Service struct {
	repo     domain.ApprovalRepository
	notifier domain.Notifier // typically a multi-fanout
	audit    domain.AuditWriter
	feedback domain.FeedbackRepository // RLHF sink; nil = feedback disabled
}

// New constructs a Service. notifier may be nil — Notify becomes a no-op.
// audit may be nil — audit writes are best-effort and skipped silently.
func New(r domain.ApprovalRepository, n domain.Notifier, a domain.AuditWriter) *Service {
	return &Service{repo: r, notifier: n, audit: a}
}

// WithFeedback wires the RLHF feedback_examples sink. Chainable so main.go
// can keep the existing New(...) call shape; nil keeps feedback disabled
// (test path / dev without Postgres).
func (s *Service) WithFeedback(f domain.FeedbackRepository) *Service {
	s.feedback = f
	return s
}

// CreateInput is the activity-side payload assembled from the synthesiser
// scenario + backend patch + workflow run metadata.
type CreateInput struct {
	OrgID         string
	WorkspaceID   string
	WorkflowRunID string
	Severity      domain.Severity
	Scenario      string
	RiskScore     float64
}

// CreatePending inserts the pending decision row and records the
// "approval.requested" audit event. Returns the decision id so the caller
// can echo it back to the workflow output.
func (s *Service) CreatePending(ctx context.Context, in CreateInput) (string, error) {
	id, err := s.repo.Create(ctx, domain.ApprovalDecision{
		OrgID:         in.OrgID,
		WorkspaceID:   in.WorkspaceID,
		WorkflowRunID: in.WorkflowRunID,
		Severity:      in.Severity,
		Decision:      domain.ApprovalPending,
		Scenario:      in.Scenario,
		RiskScore:     in.RiskScore,
	})
	if err != nil {
		return "", err
	}
	if s.audit != nil {
		_ = s.audit.Write(ctx, domain.Principal{OrgID: in.OrgID}, "approval.requested", in.WorkflowRunID, map[string]any{
			"run_id":     in.WorkflowRunID,
			"severity":   string(in.Severity),
			"scenario":   in.Scenario,
			"risk_score": in.RiskScore,
		})
	}
	return id, nil
}

// AutoApprove flips a pending row to auto_approved without a decided_by
// user. Used for the LOW severity bucket. Writes "approval.decided" audit
// with auto=true so the UI can render the badge.
func (s *Service) AutoApprove(ctx context.Context, runID, orgID string) error {
	at := time.Now().UTC().Truncate(time.Microsecond)
	if err := s.repo.UpdateDecision(ctx, runID, domain.ApprovalAutoApproved, "", "auto-approved (low severity)", at); err != nil {
		return err
	}
	if s.audit != nil {
		_ = s.audit.Write(ctx, domain.Principal{OrgID: orgID}, "approval.decided", runID, map[string]any{
			"decision": string(domain.ApprovalAutoApproved),
			"auto":     true,
		})
	}
	return nil
}

// TimeoutReject flips a pending row to timeout_rejected. Used by the
// medium-severity 2-minute timer race when no signal arrives in time.
func (s *Service) TimeoutReject(ctx context.Context, runID, orgID string) error {
	at := time.Now().UTC().Truncate(time.Microsecond)
	if err := s.repo.UpdateDecision(ctx, runID, domain.ApprovalTimeoutRejected, "", "timer fired before signal", at); err != nil {
		return err
	}
	if s.audit != nil {
		_ = s.audit.Write(ctx, domain.Principal{OrgID: orgID}, "approval.decided", runID, map[string]any{
			"decision": string(domain.ApprovalTimeoutRejected),
			"auto":     true,
		})
	}
	return nil
}

// Notify dispatches the notification through the injected notifier. Errors
// are absorbed by the multi-fanout layer (per-channel failures are logged
// but never surfaced) so we deliberately ignore the return value here.
func (s *Service) Notify(ctx context.Context, n domain.Notification) {
	if s.notifier == nil {
		return
	}
	_ = s.notifier.Send(ctx, n)
}

// RecordDecision flips a pending row to a human decision (approved |
// rejected | modified) and writes the "approval.decided" audit. Returns
// ErrApprovalRejected when the human rejected — callers in the workflow
// signal handler propagate this to the workflow function which surfaces it
// as a terminal failure.
func (s *Service) RecordDecision(ctx context.Context, runID, orgID string, sig domain.ApprovalSignal) error {
	at := time.Now().UTC().Truncate(time.Microsecond)
	if err := s.repo.UpdateDecision(ctx, runID, sig.Decision, sig.DecidedBy, sig.Notes, at); err != nil {
		return err
	}
	if s.audit != nil {
		meta := map[string]any{
			"decision":   string(sig.Decision),
			"decided_by": sig.DecidedBy,
			"notes":      sig.Notes,
			"auto":       false,
		}
		if sig.Decision == domain.ApprovalModified {
			meta["modified"] = true
		}
		_ = s.audit.Write(ctx, domain.Principal{OrgID: orgID, UserID: sig.DecidedBy}, "approval.decided", runID, meta)
	}
	return nil
}

// FeedbackInput is the RLHF example assembled by ApprovalGateFinalize from
// the workflow's prior outputs + the terminal signal.
type FeedbackInput struct {
	OrgID         string
	WorkflowRunID string
	IncidentID    string
	Scenario      string
	PatchDiff     string
	Signal        domain.ApprovalSignal
}

// RecordFeedback writes one feedback_examples row for a terminal decision.
// The RLHF dataset keys on three labels — approved / rejected / modified —
// so the auto states collapse onto their human equivalents: auto_approved →
// approved (the patch shipped), timeout_rejected → rejected (the patch was
// discarded); both keep decided_by empty so a fine-tune can filter to
// human-only examples. No-op when the feedback sink is unwired or the
// example carries no scenario AND no patch (nothing to learn from).
func (s *Service) RecordFeedback(ctx context.Context, in FeedbackInput) error {
	if s.feedback == nil {
		return nil
	}
	if in.Scenario == "" && in.PatchDiff == "" {
		return nil
	}
	var decision string
	switch in.Signal.Decision {
	case domain.ApprovalApproved, domain.ApprovalAutoApproved:
		decision = "approved"
	case domain.ApprovalRejected, domain.ApprovalTimeoutRejected:
		decision = "rejected"
	case domain.ApprovalModified:
		decision = "modified"
	default:
		return nil // pending / unknown — not a terminal decision
	}
	modified := ""
	if in.Signal.Decision == domain.ApprovalModified {
		modified = in.Signal.ModifiedDiff
	}
	_, err := s.feedback.Insert(ctx, domain.FeedbackExample{
		OrgID:         in.OrgID,
		IncidentID:    in.IncidentID,
		WorkflowRunID: in.WorkflowRunID,
		Scenario:      in.Scenario,
		PatchDiff:     in.PatchDiff,
		Decision:      decision,
		ModifiedDiff:  modified,
		DecidedBy:     in.Signal.DecidedBy,
		DecidedAt:     time.Now().UTC(),
	})
	return err
}

// WaitForDecision is the auto-timeout race helper used by the workflow
// function. Behaviour by severity:
//
//   - LOW: returns ApprovalAutoApproved immediately.
//   - MEDIUM: select between the signal channel and a 2-minute time.After.
//     Whichever wins drives the terminal decision. Timer win = timeout_rejected.
//   - HIGH: block on the signal channel indefinitely. The workflow timeout
//     (10 minutes — see WorkflowExecutionTimeout in workflow/service.go)
//     is the only hard cap.
//
// signalCh is the channel the workflow's signal-receiver goroutine pushes
// the ApprovalSignal onto. Returning the resolved signal lets the workflow
// caller persist it via RecordDecision.
//
// NOTE: this helper does NOT itself import the Temporal SDK — the workflow
// function bridges workflow.GetSignalChannel onto signalCh so this code can
// be exercised in plain-Go unit tests.
func WaitForDecision(ctx context.Context, severity domain.Severity, signalCh <-chan domain.ApprovalSignal, timeout time.Duration) (domain.ApprovalSignal, error) {
	switch severity {
	case domain.SeverityLow:
		return domain.ApprovalSignal{
			Decision:  domain.ApprovalAutoApproved,
			DecidedBy: "",
			Notes:     "auto-approved (low severity)",
		}, nil

	case domain.SeverityMedium:
		if timeout <= 0 {
			timeout = 2 * time.Minute
		}
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		select {
		case sig, ok := <-signalCh:
			if !ok {
				return domain.ApprovalSignal{}, errors.New("approval channel closed")
			}
			return sig, nil
		case <-timer.C:
			return domain.ApprovalSignal{
				Decision:  domain.ApprovalTimeoutRejected,
				DecidedBy: "",
				Notes:     "timer fired before signal",
			}, nil
		case <-ctx.Done():
			return domain.ApprovalSignal{}, ctx.Err()
		}

	case domain.SeverityHigh:
		select {
		case sig, ok := <-signalCh:
			if !ok {
				return domain.ApprovalSignal{}, errors.New("approval channel closed")
			}
			return sig, nil
		case <-ctx.Done():
			return domain.ApprovalSignal{}, ctx.Err()
		}
	}

	// Unknown severity — fail closed so the gate doesn't silently approve.
	return domain.ApprovalSignal{}, errors.New("approval: unknown severity")
}
