// Package approval — Slack interactivity decider.
//
// The Slack interactivity handler depends on a narrow SlackApprovalsService
// port (defined in handler/slack_interactivity.go). The existing
// *approval.SignalerService.Decide signature is broader — it takes a
// DecideInput that bundles Principal + WorkspaceID + WorkflowRunID + the
// decision shape — so we cannot satisfy the port directly.
//
// SlackDecider is the adapter that bridges the two. It:
//
//  1. Looks up the approval row via the ApprovalRepository to recover the
//     workspace_id + org_id from the run id (the Slack payload only carries
//     the run id encoded in the button's `value` field — it has no notion of
//     workspaces or tenants).
//  2. Synthesises a Principal stamped with that org/user. The user id stays
//     empty because the actor is identified by email only; we surface the
//     email via DecideInput.Notes so the decision row attribution is "slack:
//     <email>" without a faked user uuid.
//  3. Delegates to SignalerService.Decide so all the existing invariants
//     (workspace match, must-be-pending, Temporal signal ordering) are
//     enforced uniformly with the HTTP endpoint.
package approval

import (
	"context"
	"errors"
	"fmt"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// SlackDecider adapts the SignalerService to the narrow port the Slack
// interactivity handler depends on.
//
// repo is the same domain.ApprovalRepository the SignalerService uses; we
// reuse it instead of holding a separate lookup so a single repo backs both
// HTTP and Slack-driven decisions.
//
// signaler is the existing *SignalerService. The decider does not duplicate
// any of its logic — it just resolves the run id to a workspace and forwards.
type SlackDecider struct {
	repo     domain.ApprovalRepository
	signaler *SignalerService
}

// NewSlackDecider wires the components together.
func NewSlackDecider(repo domain.ApprovalRepository, signaler *SignalerService) *SlackDecider {
	return &SlackDecider{repo: repo, signaler: signaler}
}

// Decide implements handler.SlackApprovalsService. The handler hands us the
// workflow run id (decoded from the button's value field), the decision
// (already validated to be approved|rejected), and the actor email pulled from
// the Slack payload. We:
//
//   - Read the approval row to recover org/workspace.
//   - Build a synthetic Principal owning the org. UserID stays empty so the
//     audit trail can distinguish Slack-driven decisions from authed HTTP
//     ones by the "decided_by" column being blank + the notes field carrying
//     "slack:<email>".
//   - Delegate to SignalerService.Decide.
//
// Returns the underlying error from SignalerService unchanged so the handler
// can surface 500 on transient failures without a noisy wrapping.
func (d *SlackDecider) Decide(ctx context.Context, workflowRunID string, decision domain.ApprovalDecisionState, actorEmail string) error {
	if d == nil || d.signaler == nil || d.repo == nil {
		return errors.New("slack_decider: not configured")
	}
	if workflowRunID == "" {
		return errors.New("slack_decider: workflow_run_id required")
	}
	row, err := d.repo.GetByRun(ctx, workflowRunID)
	if err != nil {
		return fmt.Errorf("slack_decider: lookup run: %w", err)
	}
	princ := domain.Principal{
		OrgID: row.OrgID,
		Role:  domain.RoleAdmin,
	}
	in := DecideInput{
		Principal:     princ,
		WorkspaceID:   row.WorkspaceID,
		WorkflowRunID: workflowRunID,
		Decision:      decision,
		Notes:         "slack:" + actorEmail,
	}
	return d.signaler.Decide(ctx, in)
}
