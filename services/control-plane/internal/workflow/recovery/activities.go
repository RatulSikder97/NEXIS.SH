package recovery

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/agents"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/repo"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/sse"
)

// Activities holds the dependencies needed by every activity function. One
// instance is registered with the worker via RegisterActivity(a); Temporal
// dispatches method calls on it.
//
// Phase 5 wires the L1 agents (Architect/Backend/QA/DevOps/DataEngineer)
// behind a Registry; the 4 L2 stubs (Sentinel/Pathfinder/Synthesiser/
// ApprovalGate) remain stubs until Phase 6. When Agents is nil the L1
// activities fall back to the Phase 4 stub path so the test suite keeps
// working without LLM mocks.
type Activities struct {
	Repo      *repo.WorkflowRepo
	Broker    *sse.Broker[domain.ActivityEvent]
	Patches   domain.PatchStore
	Validator ValidatorClient
	Agents    *agents.Registry
	Ledger    domain.TokenLedger
	StubSleep time.Duration
}

// ValidatorClient is the port BackendCodegen depends on to issue a sandbox
// validation. The concrete client lives in internal/adapter/validator; using
// an interface here keeps the recovery package free of the http client
// dependency.
type ValidatorClient interface {
	Validate(ctx context.Context, in ValidateRequest) (ValidateResponse, error)
}

type ValidateRequest struct {
	RepoSHA   string
	PatchDiff string
	Image     string
	TimeoutMs int
}

type ValidateResponse struct {
	TestsPassed bool
	TestCount   int
	FailCount   int
	Coverage    float64
	DurationMs  int64
	Logs        string
}

// NewActivities is the test-friendly constructor (Phase 4 compatibility).
func NewActivities(r *repo.WorkflowRepo, b *sse.Broker[domain.ActivityEvent], stubSleep time.Duration) *Activities {
	return &Activities{Repo: r, Broker: b, StubSleep: stubSleep}
}

// NewActivitiesFull is the production constructor that wires every
// dependency. Phase 5 adds Agents + Ledger.
func NewActivitiesFull(
	r *repo.WorkflowRepo,
	b *sse.Broker[domain.ActivityEvent],
	ps domain.PatchStore,
	v ValidatorClient,
	ag *agents.Registry,
	ledger domain.TokenLedger,
	stubSleep time.Duration,
) *Activities {
	return &Activities{
		Repo: r, Broker: b, Patches: ps, Validator: v,
		Agents: ag, Ledger: ledger, StubSleep: stubSleep,
	}
}

// RecordActivityEvent persists a row to activity_events + publishes to the
// SSE broker. Called explicitly from the workflow function (NOT triggered by
// Temporal heartbeats) so we control the seq numbering and the wire shape.
func (a *Activities) RecordActivityEvent(ctx context.Context, in RecordEventInput) error {
	if a.Repo == nil {
		return nil
	}
	seq, err := a.Repo.NextSeq(ctx, in.WorkflowRunID)
	if err != nil {
		return err
	}
	var payload []byte
	if in.Payload != nil {
		payload, _ = json.Marshal(in.Payload)
	}
	evt := &domain.ActivityEvent{
		OrgID:         in.OrgID,
		WorkflowRunID: in.WorkflowRunID,
		Seq:           seq,
		AgentRole:     in.AgentRole,
		ActivityName:  in.ActivityName,
		Status:        in.Status,
		Attempt:       in.Attempt,
		Message:       in.Message,
		Payload:       payload,
		TS:            time.Now().UTC().Truncate(time.Microsecond),
	}
	if err := a.Repo.InsertEvent(ctx, evt); err != nil {
		return err
	}
	if a.Broker != nil {
		a.Broker.Publish(in.WorkflowRunID, *evt)
	}
	return nil
}

// stub is the body shared by the L2 stub activities (Phase 4 carryover).
// Phase 6 replaces those four bodies.
func (a *Activities) stub(ctx context.Context, role domain.AgentRole, name string) (domain.ActivityResult, error) {
	activity.GetLogger(ctx).Info("stub start", "agent", role, "activity", name)
	if a.StubSleep > 0 {
		select {
		case <-time.After(a.StubSleep):
		case <-ctx.Done():
			return domain.ActivityResult{}, ctx.Err()
		}
	}
	activity.GetLogger(ctx).Info("stub end", "agent", role, "activity", name)
	return domain.ActivityResult{
		AgentRole: role,
		Status:    domain.ActSucceeded,
		Message:   "stub completed",
	}, nil
}

// runAgent dispatches one L1 agent via the Registry. When Agents is nil
// (test path / Phase 4 compatibility) it falls back to the stub.
func (a *Activities) runAgent(ctx context.Context, name domain.AgentName, in PipelineInput) (domain.ActivityResult, error) {
	if a.Agents == nil {
		return a.stub(ctx, agentRoleOf(name), string(name)+".Run")
	}
	out, err := a.Agents.Run(ctx, name, domain.AgentInput{
		WorkflowRunID: in.RunID,
		OrgID:         in.OrgID,
		WorkspaceID:   in.WorkspaceID,
		PriorOutputs:  in.PriorOutputs,
		Incident:      in.Incident,
		RepoSHA:       in.RepoSHA,
	})
	if err != nil {
		// Budget exceeded is non-retryable so the workflow surfaces it as
		// status='cancelled' rather than retry-storming.
		if isBudgetExceeded(err) {
			return domain.ActivityResult{}, temporal.NewNonRetryableApplicationError(
				err.Error(), "BudgetError", err,
			)
		}
		return domain.ActivityResult{}, err
	}
	payload := map[string]any{
		"tokens_in":     out.TokensIn,
		"tokens_out":    out.TokensOut,
		"cached_tokens": out.CachedTokens,
		"cost_cents":    out.CostCents,
		"model":         out.Model,
		"provider":      out.Provider,
		"structured":    out.Structured,
		"schema_retries": out.SchemaRetries,
	}
	return domain.ActivityResult{
		AgentRole: agentRoleOf(name),
		Status:    domain.ActSucceeded,
		Message: fmt.Sprintf("agent=%s tokens=%d/%d cost_cents=%.4f",
			name, out.TokensIn, out.TokensOut, out.CostCents),
		Payload: payload,
	}, nil
}

func isBudgetExceeded(err error) bool {
	for e := err; e != nil; {
		if e == domain.ErrBudgetExceeded {
			return true
		}
		if u, ok := e.(interface{ Unwrap() error }); ok {
			e = u.Unwrap()
		} else {
			return false
		}
	}
	return false
}

func agentRoleOf(n domain.AgentName) domain.AgentRole {
	switch n {
	case domain.AgentNameArchitect:
		return domain.AgentArchitect
	case domain.AgentNameBackend:
		return domain.AgentBackend
	case domain.AgentNameQA:
		return domain.AgentQA
	case domain.AgentNameDevOps:
		return domain.AgentDevOps
	case domain.AgentNameDataEngineer:
		return domain.AgentDataEngineer
	}
	return ""
}

// One method per agent. Phase 5 swaps the L1 bodies (Architect / Backend /
// QA / DevOps / DataEngineer); the L2 set stays on the stub path.

func (a *Activities) SentinelDetect(ctx context.Context, _ PipelineInput) (domain.ActivityResult, error) {
	return a.stub(ctx, domain.AgentSentinel, "Sentinel.Detect")
}
func (a *Activities) PathfinderDiagnose(ctx context.Context, _ PipelineInput) (domain.ActivityResult, error) {
	return a.stub(ctx, domain.AgentPathfinder, "Pathfinder.Diagnose")
}
func (a *Activities) SynthesiserPlan(ctx context.Context, _ PipelineInput) (domain.ActivityResult, error) {
	return a.stub(ctx, domain.AgentSynthesiser, "Synthesiser.Plan")
}
func (a *Activities) ArchitectSolution(ctx context.Context, in PipelineInput) (domain.ActivityResult, error) {
	return a.runAgent(ctx, domain.AgentNameArchitect, in)
}

// BackendCodegen runs the L1 Backend agent and, when patches + validator
// are wired, persists the resulting diff to MinIO + validates against the
// fixture sandbox. The activity treats patch / validator wiring as best-
// effort: any failure is logged and surfaced in payload but doesn't fail
// the activity.
func (a *Activities) BackendCodegen(ctx context.Context, in PipelineInput) (domain.ActivityResult, error) {
	res, err := a.runAgent(ctx, domain.AgentNameBackend, in)
	if err != nil {
		return res, err
	}
	if a.Patches == nil {
		return res, nil
	}
	// Extract patch_diff from structured payload.
	var patchDiff string
	if structured, ok := res.Payload["structured"].(map[string]any); ok {
		if d, ok := structured["patch_diff"].(string); ok {
			patchDiff = d
		}
	}

	bucket := domain.BucketForOrg(in.OrgID)
	patchKey := "patches/" + in.RunID + "/Backend.Codegen.patch.enc"
	if err := a.Patches.Put(ctx, domain.PutOptions{
		Bucket: bucket, Key: patchKey, Body: []byte(patchDiff),
		ContentType: "text/x-diff",
	}); err != nil {
		activity.GetLogger(ctx).Warn("Backend.Codegen patch put failed", "err", err)
	} else {
		res.Payload["patch_key"] = patchKey
	}

	if a.Validator != nil && patchDiff != "" {
		rep, vErr := a.Validator.Validate(ctx, ValidateRequest{
			RepoSHA: in.RepoSHA, PatchDiff: patchDiff,
		})
		if vErr != nil {
			activity.GetLogger(ctx).Warn("Backend.Codegen validator failed", "err", vErr)
		} else {
			res.Payload["tests_passed"] = rep.TestsPassed
			res.Payload["test_count"] = rep.TestCount
			res.Payload["coverage"] = rep.Coverage
			reportJSON, _ := json.Marshal(rep)
			reportKey := "reports/" + in.RunID + "/Backend.Codegen.report.json.enc"
			_ = a.Patches.Put(ctx, domain.PutOptions{
				Bucket: bucket, Key: reportKey, Body: reportJSON,
				ContentType: "application/json",
			})
			res.Payload["report_key"] = reportKey
		}
	}
	return res, nil
}

func (a *Activities) QATestGen(ctx context.Context, in PipelineInput) (domain.ActivityResult, error) {
	return a.runAgent(ctx, domain.AgentNameQA, in)
}
func (a *Activities) DevOpsPipeline(ctx context.Context, in PipelineInput) (domain.ActivityResult, error) {
	return a.runAgent(ctx, domain.AgentNameDevOps, in)
}
func (a *Activities) DataEngineerMigrate(ctx context.Context, in PipelineInput) (domain.ActivityResult, error) {
	return a.runAgent(ctx, domain.AgentNameDataEngineer, in)
}
func (a *Activities) ApprovalGateRoute(ctx context.Context, _ PipelineInput) (domain.ActivityResult, error) {
	return a.stub(ctx, domain.AgentApprovalGate, "ApprovalGate.Route")
}
