package approval_test

// Coverage for the RLHF feedback pipeline surface on the approval service:
// RecordFeedback's decision-state mapping (auto states collapse onto their
// human equivalents), the modified-diff passthrough, the nil-sink /
// empty-example no-ops, and the signaler's modified-decision validation.

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/approval"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

type fakeFeedback struct {
	mu       sync.Mutex
	inserted []domain.FeedbackExample
}

func (f *fakeFeedback) Insert(_ context.Context, fe domain.FeedbackExample) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inserted = append(f.inserted, fe)
	return "fe-1", nil
}

func (f *fakeFeedback) ExportUnexported(_ context.Context, _ string, _ int) ([]domain.FeedbackExample, error) {
	return nil, nil
}

func feedbackSvc(fb *fakeFeedback) *approval.Service {
	return approval.New(&fakeRepo{}, nil, &fakeAudit{}).WithFeedback(fb)
}

func TestService_RecordFeedback_HumanApproval(t *testing.T) {
	fb := &fakeFeedback{}
	err := feedbackSvc(fb).RecordFeedback(context.Background(), approval.FeedbackInput{
		OrgID: "org-1", WorkflowRunID: "run-1", IncidentID: "inc-1",
		Scenario: "oom", PatchDiff: "diff --git a/x b/x\n+fix",
		Signal: domain.ApprovalSignal{Decision: domain.ApprovalApproved, DecidedBy: "user-1"},
	})
	require.NoError(t, err)
	require.Len(t, fb.inserted, 1)
	fe := fb.inserted[0]
	require.Equal(t, "approved", fe.Decision)
	require.Equal(t, "user-1", fe.DecidedBy)
	require.Equal(t, "oom", fe.Scenario)
	require.Empty(t, fe.ModifiedDiff)
}

func TestService_RecordFeedback_AutoStatesCollapse(t *testing.T) {
	cases := []struct {
		signal domain.ApprovalDecisionState
		want   string
	}{
		{domain.ApprovalAutoApproved, "approved"},
		{domain.ApprovalTimeoutRejected, "rejected"},
	}
	for _, tc := range cases {
		fb := &fakeFeedback{}
		err := feedbackSvc(fb).RecordFeedback(context.Background(), approval.FeedbackInput{
			OrgID: "org-1", WorkflowRunID: "run-1", Scenario: "null_deref", PatchDiff: "+x",
			Signal: domain.ApprovalSignal{Decision: tc.signal},
		})
		require.NoError(t, err)
		require.Len(t, fb.inserted, 1)
		require.Equal(t, tc.want, fb.inserted[0].Decision)
		require.Empty(t, fb.inserted[0].DecidedBy, "auto decisions carry no human label")
	}
}

func TestService_RecordFeedback_ModifiedCarriesBothDiffs(t *testing.T) {
	fb := &fakeFeedback{}
	err := feedbackSvc(fb).RecordFeedback(context.Background(), approval.FeedbackInput{
		OrgID: "org-1", WorkflowRunID: "run-1", Scenario: "oom",
		PatchDiff: "+original",
		Signal: domain.ApprovalSignal{
			Decision: domain.ApprovalModified, DecidedBy: "user-2",
			ModifiedDiff: "+edited",
		},
	})
	require.NoError(t, err)
	require.Len(t, fb.inserted, 1)
	fe := fb.inserted[0]
	require.Equal(t, "modified", fe.Decision)
	require.Equal(t, "+original", fe.PatchDiff)
	require.Equal(t, "+edited", fe.ModifiedDiff)
}

func TestService_RecordFeedback_NilSinkAndEmptyExampleNoOp(t *testing.T) {
	// No sink wired — must not panic.
	svc := approval.New(&fakeRepo{}, nil, &fakeAudit{})
	require.NoError(t, svc.RecordFeedback(context.Background(), approval.FeedbackInput{
		OrgID: "org-1", Scenario: "oom", PatchDiff: "+x",
		Signal: domain.ApprovalSignal{Decision: domain.ApprovalApproved},
	}))

	// Sink wired but nothing to learn from (no scenario, no patch).
	fb := &fakeFeedback{}
	require.NoError(t, feedbackSvc(fb).RecordFeedback(context.Background(), approval.FeedbackInput{
		OrgID: "org-1", WorkflowRunID: "run-1",
		Signal: domain.ApprovalSignal{Decision: domain.ApprovalApproved},
	}))
	require.Empty(t, fb.inserted)

	// Non-terminal decision — skipped.
	require.NoError(t, feedbackSvc(fb).RecordFeedback(context.Background(), approval.FeedbackInput{
		OrgID: "org-1", Scenario: "oom", PatchDiff: "+x",
		Signal: domain.ApprovalSignal{Decision: domain.ApprovalPending},
	}))
	require.Empty(t, fb.inserted)
}

func TestService_RecordDecision_ModifiedFlagsAudit(t *testing.T) {
	repo := &fakeRepo{}
	aud := &fakeAudit{}
	svc := approval.New(repo, nil, aud)

	require.NoError(t, svc.RecordDecision(context.Background(), "run-1", "org-1", domain.ApprovalSignal{
		Decision: domain.ApprovalModified, DecidedBy: "user-2", Notes: "tightened the fix",
		ModifiedDiff: "+edited",
	}))
	require.Len(t, repo.updates, 1)
	require.Equal(t, domain.ApprovalModified, repo.updates[0].state)
	require.Len(t, aud.written, 1)
	require.Equal(t, true, aud.written[0].metadata["modified"])
}

// ---- signaler: modified decision -------------------------------------------

func TestSignaler_Decide_ModifiedRequiresDiff(t *testing.T) {
	repo := &signalerFakeRepo{row: domain.ApprovalDecision{
		WorkspaceID: "ws-1", Decision: domain.ApprovalPending,
	}}
	tem := &recordingTemSig{}
	svc := approval.New(repo, nil, silentAudit{})
	sig := approval.NewSignalerService(repo, tem, svc)

	err := sig.Decide(context.Background(), approval.DecideInput{
		Principal:     domain.Principal{UserID: "user-1", OrgID: "org-1"},
		WorkspaceID:   "ws-1",
		WorkflowRunID: "run-1",
		Decision:      domain.ApprovalModified,
		// ModifiedDiff deliberately empty.
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "modified_diff")
	require.Empty(t, tem.calls)
}

func TestSignaler_Decide_ModifiedHappyPathCarriesDiff(t *testing.T) {
	repo := &signalerFakeRepo{row: domain.ApprovalDecision{
		WorkspaceID: "ws-1", Decision: domain.ApprovalPending,
	}}
	tem := &recordingTemSig{}
	svc := approval.New(repo, nil, silentAudit{})
	sig := approval.NewSignalerService(repo, tem, svc)

	err := sig.Decide(context.Background(), approval.DecideInput{
		Principal:     domain.Principal{UserID: "user-1", OrgID: "org-1"},
		WorkspaceID:   "ws-1",
		WorkflowRunID: "run-1",
		Decision:      domain.ApprovalModified,
		Notes:         "trimmed the retry loop",
		ModifiedDiff:  "+edited",
	})
	require.NoError(t, err)
	require.Len(t, tem.calls, 1)
	require.Equal(t, domain.ApprovalModified, tem.calls[0].signal.Decision)
	require.Equal(t, "+edited", tem.calls[0].signal.ModifiedDiff)
	require.Equal(t, 1, repo.updates)
}
