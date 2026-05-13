package recovery

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// approvalRouteActivities returns a payload identical to the real Phase 6
// ApprovalGateRoute output. We swap the activity bodies here rather than
// touching the production Activities struct so each test scenario can
// inject its own severity.
type approvalRouteActivities struct {
	*recordingActivities
	severity string
}

func (r *approvalRouteActivities) ApprovalGateRoute(_ context.Context, in PipelineInput) (domain.ActivityResult, error) {
	return domain.ActivityResult{
		AgentRole: domain.AgentApprovalGate,
		Status:    domain.ActSucceeded,
		Message:   "approval pending",
		Payload: map[string]any{
			"decision_id": "decision-test",
			"severity":    r.severity,
			"scenario":    "null_deref",
			"risk_score":  50.0,
		},
	}, nil
}

func (r *approvalRouteActivities) ApprovalGateFinalize(_ context.Context, in ApprovalFinalizeInput) (domain.ActivityResult, error) {
	status := domain.ActSucceeded
	if in.Signal.Decision == domain.ApprovalRejected || in.Signal.Decision == domain.ApprovalTimeoutRejected {
		status = domain.ActFailed
	}
	return domain.ActivityResult{
		AgentRole: domain.AgentApprovalGate,
		Status:    status,
		Message:   "approval finalised",
		Payload: map[string]any{
			"decision":   string(in.Signal.Decision),
			"decided_by": in.Signal.DecidedBy,
		},
	}, nil
}

func buildApprovalEnv(t *testing.T, severity string) (*testsuite.TestWorkflowEnvironment, *approvalRouteActivities) {
	t.Helper()
	s := &testsuite.WorkflowTestSuite{}
	env := s.NewTestWorkflowEnvironment()
	a := &approvalRouteActivities{
		recordingActivities: &recordingActivities{Activities: NewActivities(nil, nil, 0)},
		severity:            severity,
	}
	env.RegisterActivity(a)
	env.RegisterWorkflow(RecoveryPipeline)
	return env, a
}

func TestRecoveryPipeline_LowSeverityAutoApproves(t *testing.T) {
	env, _ := buildApprovalEnv(t, "low")
	env.ExecuteWorkflow(RecoveryPipeline, PipelineInput{
		OrgID: "org-1", WorkspaceID: "ws-1", RunID: "run-low",
		IncidentID: "manual", TriggeredBy: "manual",
	})
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var out PipelineOutput
	require.NoError(t, env.GetWorkflowResult(&out))
	// Results = 9 agent steps + ApprovalGate.Finalize.
	require.Len(t, out.Results, 10)
	final := out.Results[len(out.Results)-1]
	require.Equal(t, domain.ApprovalAutoApproved, domain.ApprovalDecisionState(final.Payload["decision"].(string)))
	require.Equal(t, "decision-test", out.ApprovalDecisionID)
}

func TestRecoveryPipeline_MediumSeverityTimerFiresWhenNoSignal(t *testing.T) {
	env, _ := buildApprovalEnv(t, "medium")
	// Don't post any signal — the 2-minute timer wins on the test clock.
	env.ExecuteWorkflow(RecoveryPipeline, PipelineInput{
		OrgID: "org-1", WorkspaceID: "ws-1", RunID: "run-medium-timeout",
		IncidentID: "manual", TriggeredBy: "manual",
	})
	require.True(t, env.IsWorkflowCompleted())
	// Timeout rejection surfaces as a non-retryable application error.
	err := env.GetWorkflowError()
	require.Error(t, err)
	require.Contains(t, err.Error(), "timeout_rejected")
}

func TestRecoveryPipeline_MediumSeveritySignalBeatsTimer(t *testing.T) {
	env, _ := buildApprovalEnv(t, "medium")
	// Inject the signal partway through the workflow run so the selector
	// receive branch fires before the 2-minute timer.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(ApprovalSignalName, domain.ApprovalSignal{
			Decision: domain.ApprovalApproved, DecidedBy: "user-1", Notes: "lgtm",
		})
	}, 30*time.Second)

	env.ExecuteWorkflow(RecoveryPipeline, PipelineInput{
		OrgID: "org-1", WorkspaceID: "ws-1", RunID: "run-medium-approve",
		IncidentID: "manual", TriggeredBy: "manual",
	})
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var out PipelineOutput
	require.NoError(t, env.GetWorkflowResult(&out))
	final := out.Results[len(out.Results)-1]
	require.Equal(t, "approved", final.Payload["decision"])
	require.Equal(t, "user-1", final.Payload["decided_by"])
}

func TestRecoveryPipeline_HighSeverityBlocksUntilSignal(t *testing.T) {
	env, _ := buildApprovalEnv(t, "high")
	// Fire a rejection signal — high severity blocks forever otherwise.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(ApprovalSignalName, domain.ApprovalSignal{
			Decision: domain.ApprovalRejected, DecidedBy: "user-2", Notes: "no",
		})
	}, 5*time.Minute) // well past the medium timer window

	env.ExecuteWorkflow(RecoveryPipeline, PipelineInput{
		OrgID: "org-1", WorkspaceID: "ws-1", RunID: "run-high-rejected",
		IncidentID: "manual", TriggeredBy: "manual",
	})
	require.True(t, env.IsWorkflowCompleted())
	err := env.GetWorkflowError()
	require.Error(t, err)
	require.Contains(t, err.Error(), "rejected")
}
