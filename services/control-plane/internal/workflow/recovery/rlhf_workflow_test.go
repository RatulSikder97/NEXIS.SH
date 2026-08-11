package recovery

// Coverage for the RLHF surfaces of the recovery workflow:
//
//   - a 'modified' approval signal completes the pipeline (it is an
//     approval, not a rejection) and the finalize frame flags it;
//   - the REAL ApprovalGateFinalize activity records the terminal decision
//     AND lands a feedback_examples row through the approval service's
//     feedback sink, including the scenario + patch the workflow threads
//     through ApprovalFinalizeInput.
//
// The route activity is overridden (same trick as approval_workflow_test.go)
// so the test can pin severity=high; the finalize activity is the production
// body wired to in-memory fakes.

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/approval"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// rlhfFakeApprovalRepo satisfies domain.ApprovalRepository in-memory.
type rlhfFakeApprovalRepo struct {
	mu      sync.Mutex
	updates []domain.ApprovalDecisionState
}

func (f *rlhfFakeApprovalRepo) Create(_ context.Context, _ domain.ApprovalDecision) (string, error) {
	return "decision-rlhf", nil
}
func (f *rlhfFakeApprovalRepo) GetByRun(_ context.Context, _ string) (domain.ApprovalDecision, error) {
	return domain.ApprovalDecision{}, nil
}
func (f *rlhfFakeApprovalRepo) UpdateDecision(_ context.Context, _ string, state domain.ApprovalDecisionState, _, _ string, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates = append(f.updates, state)
	return nil
}
func (f *rlhfFakeApprovalRepo) ListPending(_ context.Context, _ string, _ int) ([]domain.ApprovalDecision, error) {
	return nil, nil
}

// rlhfFakeFeedback satisfies domain.FeedbackRepository in-memory.
type rlhfFakeFeedback struct {
	mu       sync.Mutex
	inserted []domain.FeedbackExample
}

func (f *rlhfFakeFeedback) Insert(_ context.Context, fe domain.FeedbackExample) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inserted = append(f.inserted, fe)
	return "fe-1", nil
}
func (f *rlhfFakeFeedback) ExportUnexported(_ context.Context, _ string, _ int) ([]domain.FeedbackExample, error) {
	return nil, nil
}

// rlhfActivities overrides ApprovalGateRoute to force severity=high with a
// known scenario, and Backend.Codegen to plant a deterministic patch in the
// prior map. Everything else — including the production
// ApprovalGateFinalize — is promoted from the embedded *Activities.
type rlhfActivities struct {
	*recordingActivities
}

func (r *rlhfActivities) ApprovalGateRoute(_ context.Context, _ PipelineInput) (domain.ActivityResult, error) {
	return domain.ActivityResult{
		AgentRole: domain.AgentApprovalGate,
		Status:    domain.ActSucceeded,
		Message:   "approval pending",
		Payload: map[string]any{
			"decision_id": "decision-rlhf",
			"severity":    "high",
			"scenario":    "oom",
			"risk_score":  80.0,
		},
	}, nil
}

func (r *rlhfActivities) BackendCodegen(_ context.Context, _ PipelineInput) (domain.ActivityResult, error) {
	return domain.ActivityResult{
		AgentRole: domain.AgentBackend,
		Status:    domain.ActSucceeded,
		Message:   "patch ready",
		Payload: map[string]any{
			"structured": map[string]any{"patch_diff": "+original fix"},
		},
	}, nil
}

func (r *rlhfActivities) SynthesiserPlan(_ context.Context, _ PipelineInput) (domain.ActivityResult, error) {
	return domain.ActivityResult{
		AgentRole: domain.AgentSynthesiser,
		Status:    domain.ActSucceeded,
		Message:   "planned",
		Payload: map[string]any{
			"structured": map[string]any{"scenario": "oom"},
		},
	}, nil
}

func buildRLHFEnv(t *testing.T) (*testsuite.TestWorkflowEnvironment, *rlhfFakeApprovalRepo, *rlhfFakeFeedback) {
	t.Helper()
	s := &testsuite.WorkflowTestSuite{}
	env := s.NewTestWorkflowEnvironment()

	repo := &rlhfFakeApprovalRepo{}
	fb := &rlhfFakeFeedback{}
	base := NewActivities(nil, nil, 0)
	base.Approval = approval.New(repo, nil, nil).WithFeedback(fb)

	a := &rlhfActivities{recordingActivities: &recordingActivities{Activities: base}}
	env.RegisterActivity(a)
	env.RegisterWorkflow(RecoveryPipeline)
	return env, repo, fb
}

func TestRecoveryPipeline_ModifiedSignalCompletesAndRecordsFeedback(t *testing.T) {
	env, repo, fb := buildRLHFEnv(t)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(ApprovalSignalName, domain.ApprovalSignal{
			Decision:     domain.ApprovalModified,
			DecidedBy:    "user-9",
			Notes:        "tightened the retry cap",
			ModifiedDiff: "+edited fix",
		})
	}, 30*time.Second)

	env.ExecuteWorkflow(RecoveryPipeline, PipelineInput{
		OrgID: "org-1", WorkspaceID: "ws-1", RunID: "run-modified",
		IncidentID: "inc-42", TriggeredBy: "manual",
	})
	require.True(t, env.IsWorkflowCompleted())
	// Modified is an approval — the workflow must NOT fail.
	require.NoError(t, env.GetWorkflowError())

	var out PipelineOutput
	require.NoError(t, env.GetWorkflowResult(&out))
	// The finalize frame is not the last result here — the planted backend
	// patch means the (nil-client, skipped) GitOps deploy step appends one
	// more result after it — so locate it by its decision payload.
	final := findResultWithDecision(t, out.Results)
	require.Equal(t, "modified", final.Payload["decision"])
	require.Equal(t, true, final.Payload["modified"])
	require.Equal(t, "user-9", final.Payload["decided_by"])

	// The decision row flipped to modified.
	require.Equal(t, []domain.ApprovalDecisionState{domain.ApprovalModified}, repo.updates)

	// The feedback example carries scenario + original patch + edited diff.
	require.Len(t, fb.inserted, 1)
	fe := fb.inserted[0]
	require.Equal(t, "modified", fe.Decision)
	require.Equal(t, "oom", fe.Scenario)
	require.Equal(t, "+original fix", fe.PatchDiff)
	require.Equal(t, "+edited fix", fe.ModifiedDiff)
	require.Equal(t, "inc-42", fe.IncidentID)
	require.Equal(t, "user-9", fe.DecidedBy)
}

func TestRecoveryPipeline_ApprovedSignalRecordsFeedback(t *testing.T) {
	env, repo, fb := buildRLHFEnv(t)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(ApprovalSignalName, domain.ApprovalSignal{
			Decision: domain.ApprovalApproved, DecidedBy: "user-1", Notes: "lgtm",
		})
	}, 30*time.Second)

	env.ExecuteWorkflow(RecoveryPipeline, PipelineInput{
		OrgID: "org-1", WorkspaceID: "ws-1", RunID: "run-approved",
		IncidentID: "manual", TriggeredBy: "manual",
	})
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	require.Equal(t, []domain.ApprovalDecisionState{domain.ApprovalApproved}, repo.updates)
	require.Len(t, fb.inserted, 1)
	require.Equal(t, "approved", fb.inserted[0].Decision)
	require.Equal(t, "+original fix", fb.inserted[0].PatchDiff)
	require.Empty(t, fb.inserted[0].ModifiedDiff)
}

// findResultWithDecision returns the single result frame carrying a
// "decision" payload key — the ApprovalGate.Finalize output.
func findResultWithDecision(t *testing.T, results []domain.ActivityResult) domain.ActivityResult {
	t.Helper()
	for _, r := range results {
		if r.Payload == nil {
			continue
		}
		if _, ok := r.Payload["decision"]; ok {
			return r
		}
	}
	t.Fatalf("no result frame carries a decision payload (results=%d)", len(results))
	return domain.ActivityResult{}
}
