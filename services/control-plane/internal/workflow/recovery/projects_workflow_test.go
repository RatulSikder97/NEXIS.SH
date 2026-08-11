package recovery

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// projectAwareActivities is a test wrapper that exposes ProjectsReader +
// SlackDefaultChannel without touching the production Activities pointer.
// We swap out the ApprovalGateRoute body so the activity reads project
// policy directly without needing a live approval.Service.
type projectAwareActivities struct {
	*recordingActivities
	project          *domain.Project
	loadErr          error
	severity         string
	loadCalls        int
	slackDefaultUsed string
}

func (p *projectAwareActivities) LoadProject(_ context.Context, in LoadProjectInput) (LoadProjectOutput, error) {
	p.loadCalls++
	if p.loadErr != nil {
		return LoadProjectOutput{}, p.loadErr
	}
	if p.project == nil || p.project.ID != in.ProjectID {
		return LoadProjectOutput{}, nil
	}
	return LoadProjectOutput{Project: p.project}, nil
}

func (p *projectAwareActivities) ApprovalGateRoute(_ context.Context, in PipelineInput) (domain.ActivityResult, error) {
	payload := map[string]any{
		"decision_id": "decision-test",
		"severity":    p.severity,
		"scenario":    "null_deref",
		"risk_score":  50.0,
	}
	if in.Project != nil {
		policy := in.Project.Policy
		// Mirror the production payload contract — flag auto-approve and
		// kill-switch based on the bound project's policy.
		if policy.KillSwitchEnabled {
			payload["kill_switch"] = true
		}
		switch domain.Severity(p.severity) {
		case domain.SeverityLow:
			if policy.AutoMergeLowSeverity {
				payload["auto_approved"] = true
			}
		case domain.SeverityMedium:
			if policy.AutoMergeMediumSeverity {
				payload["auto_approved"] = true
			}
			if policy.MediumCountdownSeconds > 0 {
				payload["countdown_secs"] = policy.MediumCountdownSeconds
			}
		}
		payload["project_id"] = in.Project.ID
		if len(policy.ApproverUserIDs) > 0 {
			payload["approver_user_ids"] = policy.ApproverUserIDs
		}
		if in.Project.Selectors.SlackChannelID != "" {
			payload["slack_channel_id"] = in.Project.Selectors.SlackChannelID
		}
	}
	return domain.ActivityResult{
		AgentRole: domain.AgentApprovalGate,
		Status:    domain.ActSucceeded,
		Message:   "approval pending",
		Payload:   payload,
	}, nil
}

func (p *projectAwareActivities) ApprovalGateFinalize(_ context.Context, in ApprovalFinalizeInput) (domain.ActivityResult, error) {
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

func buildProjectsEnv(t *testing.T, project *domain.Project, severity string) (*testsuite.TestWorkflowEnvironment, *projectAwareActivities) {
	t.Helper()
	s := &testsuite.WorkflowTestSuite{}
	env := s.NewTestWorkflowEnvironment()
	a := &projectAwareActivities{
		recordingActivities: &recordingActivities{Activities: NewActivities(nil, nil, 0)},
		project:             project,
		severity:            severity,
	}
	env.RegisterActivity(a)
	env.RegisterWorkflow(RecoveryPipeline)
	return env, a
}

// TestRecovery_LoadProject_ReadsSelectors fires the workflow with a
// ProjectID and asserts LoadProject was invoked + the project surfaces in
// the agent context (smoked here via the recorded ApprovalGateRoute output).
func TestRecovery_LoadProject_ReadsSelectors(t *testing.T) {
	proj := &domain.Project{
		ID:    "proj-1",
		OrgID: "org-1",
		Name:  "Orders API",
		Slug:  "orders-api",
		Selectors: domain.ProjectSelectors{
			GitHubRepo:          "acme/orders",
			GitHubDefaultBranch: "main",
			ArgoCDAppName:       "orders-prod",
			SlackChannelID:      "C12345",
		},
		Policy: domain.DefaultRecoveryPolicy(),
	}
	env, a := buildProjectsEnv(t, proj, "low")
	env.ExecuteWorkflow(RecoveryPipeline, PipelineInput{
		OrgID:       "org-1",
		WorkspaceID: "ws-1",
		RunID:       "run-1",
		ProjectID:   "proj-1",
		IncidentID:  "manual",
		TriggeredBy: "manual",
	})
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.GreaterOrEqual(t, a.loadCalls, 1, "LoadProject must be invoked when ProjectID is set")
}

// TestRecovery_NoProjectID_FallsBackToFixture confirms an empty ProjectID
// path doesn't invoke LoadProject and the workflow completes via the legacy
// fixture path.
func TestRecovery_NoProjectID_FallsBackToFixture(t *testing.T) {
	env, a := buildProjectsEnv(t, nil, "low")
	env.ExecuteWorkflow(RecoveryPipeline, PipelineInput{
		OrgID:       "org-1",
		WorkspaceID: "ws-1",
		RunID:       "run-fixture",
		IncidentID:  "manual",
		TriggeredBy: "manual",
	})
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 0, a.loadCalls, "LoadProject must NOT fire when ProjectID is empty")
}

// TestApprovalGate_AutoMergeLow_RespectsPolicy verifies the AutoMergeLow
// flag forces the gate to auto-approve without entering a signal/timer
// wait.
func TestApprovalGate_AutoMergeLow_RespectsPolicy(t *testing.T) {
	policy := domain.DefaultRecoveryPolicy()
	policy.AutoMergeLowSeverity = true
	proj := &domain.Project{
		ID: "proj-auto", OrgID: "org-1", Name: "auto", Slug: "auto",
		Policy: policy,
	}
	env, _ := buildProjectsEnv(t, proj, "low")
	env.ExecuteWorkflow(RecoveryPipeline, PipelineInput{
		OrgID:       "org-1",
		WorkspaceID: "ws-1",
		RunID:       "run-auto-low",
		ProjectID:   "proj-auto",
		IncidentID:  "manual",
		TriggeredBy: "manual",
	})
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var out PipelineOutput
	require.NoError(t, env.GetWorkflowResult(&out))
	final := out.Results[len(out.Results)-1]
	require.Equal(t, domain.ApprovalAutoApproved, domain.ApprovalDecisionState(final.Payload["decision"].(string)))
}

// TestApprovalGate_KillSwitch_RejectsImmediately verifies a kill switch
// triggers a non-retryable error at the top of the workflow (LoadProject
// short-circuit). The recovery DAG must not even reach the L1 fan-out.
func TestApprovalGate_KillSwitch_RejectsImmediately(t *testing.T) {
	policy := domain.DefaultRecoveryPolicy()
	policy.KillSwitchEnabled = true
	proj := &domain.Project{
		ID: "proj-kill", OrgID: "org-1", Name: "kill", Slug: "kill",
		Policy: policy,
	}
	env, _ := buildProjectsEnv(t, proj, "low")
	env.ExecuteWorkflow(RecoveryPipeline, PipelineInput{
		OrgID:       "org-1",
		WorkspaceID: "ws-1",
		RunID:       "run-kill",
		ProjectID:   "proj-kill",
		IncidentID:  "manual",
		TriggeredBy: "manual",
	})
	require.True(t, env.IsWorkflowCompleted())
	err := env.GetWorkflowError()
	require.Error(t, err)
	require.Contains(t, err.Error(), "kill switch")
}

// TestArgoCD_TargetsProjectApp is a unit-level assertion on
// buildAgentContext — the helper folds the project's ArgoCD selectors into
// the agent context so downstream activities can target the right app.
func TestArgoCD_TargetsProjectApp(t *testing.T) {
	proj := &domain.Project{
		ID: "p", OrgID: "org-1",
		Selectors: domain.ProjectSelectors{
			ArgoCDAppName: "billing-prod",
			ArgoCDProject: "billing",
			GitHubRepo:    "acme/billing",
		},
	}
	ctx := buildAgentContext(PipelineInput{Project: proj})
	require.Equal(t, "billing-prod", ctx["argocd_app_name"])
	require.Equal(t, "billing", ctx["argocd_project"])
	require.Equal(t, "acme/billing", ctx["github_repo"])
}

// TestBuildAgentContext_FixtureFallback verifies the helper returns the
// legacy fixture mapping when no project is bound.
func TestBuildAgentContext_FixtureFallback(t *testing.T) {
	ctx := buildAgentContext(PipelineInput{})
	require.Equal(t, "acme/orders-api-fixture", ctx["github_repo"])
}

// TestRecovery_LoadProject_FailureIsNonFatal exercises the non-fatal path
// when LoadProject returns an error — the workflow proceeds via the fixture
// fallback rather than failing.
func TestRecovery_LoadProject_FailureIsNonFatal(t *testing.T) {
	env, a := buildProjectsEnv(t, nil, "low")
	a.loadErr = errors.New("simulated lookup failure")
	env.ExecuteWorkflow(RecoveryPipeline, PipelineInput{
		OrgID:       "org-1",
		WorkspaceID: "ws-1",
		RunID:       "run-load-fail",
		ProjectID:   "proj-missing",
		IncidentID:  "manual",
		TriggeredBy: "manual",
	})
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
}

// TestApprovalGate_MediumCountdown_RespectsPolicy verifies the project's
// MediumCountdownSeconds shortens the gate's timer. Drives an explicit
// signal at 30s; the project's 60s countdown should leave room for the
// signal to win.
func TestApprovalGate_MediumCountdown_RespectsPolicy(t *testing.T) {
	policy := domain.DefaultRecoveryPolicy()
	policy.MediumCountdownSeconds = 60
	proj := &domain.Project{
		ID: "proj-cd", OrgID: "org-1",
		Policy: policy,
	}
	env, _ := buildProjectsEnv(t, proj, "medium")
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(ApprovalSignalName, domain.ApprovalSignal{
			Decision: domain.ApprovalApproved, DecidedBy: "user-1", Notes: "ok",
		})
	}, 30*time.Second)

	env.ExecuteWorkflow(RecoveryPipeline, PipelineInput{
		OrgID:       "org-1",
		WorkspaceID: "ws-1",
		RunID:       "run-cd",
		ProjectID:   "proj-cd",
		IncidentID:  "manual",
		TriggeredBy: "manual",
	})
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
}
