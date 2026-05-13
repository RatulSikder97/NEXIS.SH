package recovery

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// recordingActivities wraps Activities and intercepts RecordActivityEvent so
// the test can verify the call sequence. Every other method is delegated to
// the embedded *Activities, so the stub activity bodies still run.
type recordingActivities struct {
	*Activities
	mu     sync.Mutex
	events []RecordEventInput
}

// RecordActivityEvent overrides the embedded Activities.RecordActivityEvent.
// Returning nil keeps the workflow happy without touching Postgres.
func (r *recordingActivities) RecordActivityEvent(_ context.Context, in RecordEventInput) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, in)
	activity.GetLogger(context.Background()).Info("recordingActivities", "role", in.AgentRole, "status", in.Status)
	return nil
}

func (r *recordingActivities) snapshot() []RecordEventInput {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]RecordEventInput, len(r.events))
	copy(out, r.events)
	return out
}

func buildEnv(t *testing.T) (*testsuite.TestWorkflowEnvironment, *recordingActivities) {
	t.Helper()
	s := &testsuite.WorkflowTestSuite{}
	env := s.NewTestWorkflowEnvironment()

	a := &recordingActivities{Activities: NewActivities(nil, nil, 0)}
	// Register the wrapper as the activity object — Temporal resolves each
	// (*Activities).Foo method via the wrapper because Go's method resolution
	// goes through promoted fields, and RecordActivityEvent shadows the
	// embedded one.
	env.RegisterActivity(a)
	env.RegisterWorkflow(RecoveryPipeline)
	return env, a
}

func TestRecoveryPipeline_AllStubsSucceed(t *testing.T) {
	env, rec := buildEnv(t)
	env.ExecuteWorkflow(RecoveryPipeline, PipelineInput{
		OrgID: "org-1", WorkspaceID: "ws-1", RunID: "run-1",
		IncidentID: "manual", TriggeredBy: "manual",
	})
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var out PipelineOutput
	require.NoError(t, env.GetWorkflowResult(&out))
	assert.Len(t, out.Results, 9, "expect 9 activity results (one per agent)")

	// At minimum: each of the 9 agents has a started + succeeded pair, plus
	// the synthetic Pipeline.Complete record. 9*2 + 1 = 19 record calls.
	got := rec.snapshot()
	assert.GreaterOrEqual(t, len(got), 19, "expect at least 19 record calls, got %d", len(got))
}

func TestRecoveryPipeline_DAGOrder(t *testing.T) {
	env, rec := buildEnv(t)
	env.ExecuteWorkflow(RecoveryPipeline, PipelineInput{
		OrgID: "org-1", WorkspaceID: "ws-1", RunID: "run-2",
		IncidentID: "manual", TriggeredBy: "manual",
	})
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	got := rec.snapshot()

	// Pull out the first 'started' record for each agent role + the terminal
	// Pipeline.Complete event. Ignore succeeded/failed — they always trail
	// started.
	firstStartIdx := map[domain.AgentRole]int{}
	for i, e := range got {
		isAgentStart := e.Status == domain.ActStarted && e.AgentRole != domain.AgentPipeline
		isPipelineEnd := e.AgentRole == domain.AgentPipeline
		if !isAgentStart && !isPipelineEnd {
			continue
		}
		if _, seen := firstStartIdx[e.AgentRole]; !seen {
			firstStartIdx[e.AgentRole] = i
		}
	}
	for i, a := range []domain.AgentRole{
		domain.AgentSentinel, domain.AgentPathfinder, domain.AgentSynthesiser,
		domain.AgentArchitect, domain.AgentBackend, domain.AgentQA,
		domain.AgentDevOps, domain.AgentDataEngineer, domain.AgentApprovalGate,
		domain.AgentPipeline,
	} {
		_, ok := firstStartIdx[a]
		require.Truef(t, ok, "missing %s at index %d; recorded=%+v", a, i, got)
	}
	// Sequential L2 + L1 ordering.
	assert.Less(t, firstStartIdx[domain.AgentSentinel], firstStartIdx[domain.AgentPathfinder])
	assert.Less(t, firstStartIdx[domain.AgentPathfinder], firstStartIdx[domain.AgentSynthesiser])
	assert.Less(t, firstStartIdx[domain.AgentSynthesiser], firstStartIdx[domain.AgentArchitect])
	assert.Less(t, firstStartIdx[domain.AgentArchitect], firstStartIdx[domain.AgentBackend])
	assert.Less(t, firstStartIdx[domain.AgentBackend], firstStartIdx[domain.AgentQA])
	// Parallel: both devops + data_engineer started before approval_gate.
	assert.Less(t, firstStartIdx[domain.AgentDevOps], firstStartIdx[domain.AgentApprovalGate])
	assert.Less(t, firstStartIdx[domain.AgentDataEngineer], firstStartIdx[domain.AgentApprovalGate])
	// Terminal sentinel.
	assert.Less(t, firstStartIdx[domain.AgentApprovalGate], firstStartIdx[domain.AgentPipeline])
}
