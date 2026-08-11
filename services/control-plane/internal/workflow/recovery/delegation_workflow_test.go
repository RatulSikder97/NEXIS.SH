package recovery

// Phase 8 — Synthesiser delegation enforcement + Architect contract
// violation, exercised through the Temporal test environment. The wrappers
// swap individual activity bodies (same pattern as approval_workflow_test)
// so each scenario can inject the structured payloads the workflow reads
// out of the prior map.

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// synthPlanActivities overrides SynthesiserPlan with a structured plan so
// foldPrior lifts selected_agents into the prior map — the workflow must
// then skip every L1 agent outside the selected list.
type synthPlanActivities struct {
	*recordingActivities
	scenario string
	selected []any
	skipped  []any
}

func (s *synthPlanActivities) SynthesiserPlan(_ context.Context, _ PipelineInput) (domain.ActivityResult, error) {
	return domain.ActivityResult{
		AgentRole: domain.AgentSynthesiser,
		Status:    domain.ActSucceeded,
		Message:   "plan",
		Payload: map[string]any{
			"structured": map[string]any{
				"scenario":        s.scenario,
				"selected_agents": s.selected,
				"skipped_agents":  s.skipped,
			},
		},
	}, nil
}

func buildDelegationEnv(t *testing.T, scenario string, selected, skipped []any) (*testsuite.TestWorkflowEnvironment, *synthPlanActivities) {
	t.Helper()
	s := &testsuite.WorkflowTestSuite{}
	env := s.NewTestWorkflowEnvironment()
	a := &synthPlanActivities{
		recordingActivities: &recordingActivities{Activities: NewActivities(nil, nil, 0)},
		scenario:            scenario,
		selected:            selected,
		skipped:             skipped,
	}
	env.RegisterActivity(a)
	env.RegisterWorkflow(RecoveryPipeline)
	return env, a
}

// TestRecoveryPipeline_SynthesiserPlanSkipsUnselectedAgents drives the oom
// routing (architect, devops, backend) and asserts QA + DataEngineer are
// skipped WITH a timeline frame each — never silently omitted.
func TestRecoveryPipeline_SynthesiserPlanSkipsUnselectedAgents(t *testing.T) {
	env, rec := buildDelegationEnv(t, "oom",
		[]any{"architect", "devops", "backend"},
		[]any{"qa", "data_engineer"})
	env.ExecuteWorkflow(RecoveryPipeline, PipelineInput{
		OrgID: "org-1", WorkspaceID: "ws-1", RunID: "run-skip",
		IncidentID: "manual", TriggeredBy: "manual",
	})
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var out PipelineOutput
	require.NoError(t, env.GetWorkflowResult(&out))
	// 9 agent steps minus the 2 skipped ones (skips produce events, not
	// ActivityResults).
	require.Len(t, out.Results, 7)

	got := rec.snapshot()
	skippedRoles := map[domain.AgentRole]bool{}
	startedRoles := map[domain.AgentRole]bool{}
	for _, e := range got {
		switch e.Status {
		case domain.ActSkipped:
			skippedRoles[e.AgentRole] = true
			require.Contains(t, e.Message, "scenario=oom",
				"skip frame must carry the scenario for the UI")
			require.Equal(t, "synthesiser", e.Payload["skipped_by"])
		case domain.ActStarted:
			startedRoles[e.AgentRole] = true
		}
	}
	require.True(t, skippedRoles[domain.AgentQA], "qa must be skipped")
	require.True(t, skippedRoles[domain.AgentDataEngineer], "data_engineer must be skipped")
	require.False(t, startedRoles[domain.AgentQA], "qa must never start")
	require.False(t, startedRoles[domain.AgentDataEngineer], "data_engineer must never start")
	// Selected agents still run.
	require.True(t, startedRoles[domain.AgentArchitect])
	require.True(t, startedRoles[domain.AgentBackend])
	require.True(t, startedRoles[domain.AgentDevOps])
}

// TestRecoveryPipeline_NoPlanRunsEverything pins the back-compat contract:
// when the Synthesiser is on the stub path (no structured plan in the prior
// map), all 5 L1 agents run exactly as before — no skip frames.
func TestRecoveryPipeline_NoPlanRunsEverything(t *testing.T) {
	env, rec := buildEnv(t)
	env.ExecuteWorkflow(RecoveryPipeline, PipelineInput{
		OrgID: "org-1", WorkspaceID: "ws-1", RunID: "run-noplan",
		IncidentID: "manual", TriggeredBy: "manual",
	})
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var out PipelineOutput
	require.NoError(t, env.GetWorkflowResult(&out))
	require.Len(t, out.Results, 9)
	for _, e := range rec.snapshot() {
		require.NotEqual(t, domain.ActSkipped, e.Status,
			"stub path must not produce skip frames (%s)", e.ActivityName)
	}
}

// contractActivities injects an Architect plan + a Backend patch that
// strays outside it, and captures what ApprovalGateRoute saw in
// PriorOutputs so the test can assert the violation flag was threaded
// through to the severity classification path.
type contractActivities struct {
	*recordingActivities
	affectedFiles []any
	filesChanged  []any

	mu         sync.Mutex
	routePrior map[string]any
}

func (c *contractActivities) ArchitectSolution(_ context.Context, _ PipelineInput) (domain.ActivityResult, error) {
	return domain.ActivityResult{
		AgentRole: domain.AgentArchitect,
		Status:    domain.ActSucceeded,
		Message:   "plan",
		Payload: map[string]any{
			"structured": map[string]any{
				"plan_steps":     []any{},
				"affected_files": c.affectedFiles,
				"risk_level":     "low",
			},
		},
	}, nil
}

func (c *contractActivities) BackendCodegen(_ context.Context, _ PipelineInput) (domain.ActivityResult, error) {
	return domain.ActivityResult{
		AgentRole: domain.AgentBackend,
		Status:    domain.ActSucceeded,
		Message:   "patch",
		Payload: map[string]any{
			"structured": map[string]any{
				"patch_diff":    "diff --git a/apps/web/components/x.tsx b/apps/web/components/x.tsx\n+++ b/apps/web/components/x.tsx\n+1",
				"files_changed": c.filesChanged,
			},
		},
	}, nil
}

func (c *contractActivities) ApprovalGateRoute(_ context.Context, in PipelineInput) (domain.ActivityResult, error) {
	c.mu.Lock()
	c.routePrior = in.PriorOutputs
	c.mu.Unlock()
	return domain.ActivityResult{
		AgentRole: domain.AgentApprovalGate,
		Status:    domain.ActSucceeded,
		Message:   "approval pending",
		Payload: map[string]any{
			"decision_id": "decision-test",
			"severity":    "low",
			"scenario":    "null_deref",
			"risk_score":  15.0,
		},
	}, nil
}

func buildContractEnv(t *testing.T, affected, changed []any) (*testsuite.TestWorkflowEnvironment, *contractActivities) {
	t.Helper()
	s := &testsuite.WorkflowTestSuite{}
	env := s.NewTestWorkflowEnvironment()
	a := &contractActivities{
		recordingActivities: &recordingActivities{Activities: NewActivities(nil, nil, 0)},
		affectedFiles:       affected,
		filesChanged:        changed,
	}
	env.RegisterActivity(a)
	env.RegisterWorkflow(RecoveryPipeline)
	return env, a
}

// TestRecoveryPipeline_ContractViolationRecordsEventAndEscalates — backend
// touched b.go which the architect never declared: the workflow must record
// the Architect.Violation frame AND thread the flag into ApprovalGateRoute's
// PriorOutputs.
func TestRecoveryPipeline_ContractViolationRecordsEventAndEscalates(t *testing.T) {
	env, rec := buildContractEnv(t,
		[]any{"a.go"},
		[]any{"a.go", "b.go"})
	env.ExecuteWorkflow(RecoveryPipeline, PipelineInput{
		OrgID: "org-1", WorkspaceID: "ws-1", RunID: "run-violation",
		IncidentID: "manual", TriggeredBy: "manual",
	})
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var violation *RecordEventInput
	for _, e := range rec.snapshot() {
		if e.ActivityName == "Architect.Violation" {
			cp := e
			violation = &cp
		}
	}
	require.NotNil(t, violation, "Architect.Violation frame must be recorded")
	require.Equal(t, domain.AgentArchitect, violation.AgentRole)
	require.Contains(t, violation.Message, "b.go")
	require.NotContains(t, violation.Message, "a.go,", "authorised file must not be listed")

	rec.mu.Lock()
	defer rec.mu.Unlock()
	cv, ok := rec.routePrior["contract_violation"].(map[string]any)
	require.True(t, ok, "contract_violation must reach ApprovalGateRoute via PriorOutputs")
	require.Equal(t, true, cv["violation"])
	files := cv["unauthorized_files"]
	require.Contains(t, stringsFromAny(files), "b.go")
}

// TestRecoveryPipeline_PatchWithinContractIsQuiet — the in-contract patch
// must produce no violation frame and no contract_violation prior.
func TestRecoveryPipeline_PatchWithinContractIsQuiet(t *testing.T) {
	env, rec := buildContractEnv(t,
		[]any{"a.go", "b.go"},
		[]any{"a.go"})
	env.ExecuteWorkflow(RecoveryPipeline, PipelineInput{
		OrgID: "org-1", WorkspaceID: "ws-1", RunID: "run-clean",
		IncidentID: "manual", TriggeredBy: "manual",
	})
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	for _, e := range rec.snapshot() {
		require.NotEqual(t, "Architect.Violation", e.ActivityName)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	_, ok := rec.routePrior["contract_violation"]
	require.False(t, ok, "no violation flag for an in-contract patch")
}
