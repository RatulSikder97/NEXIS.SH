package recovery

import (
	"context"
	"encoding/json"
	"time"

	"go.temporal.io/sdk/activity"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/repo"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/sse"
)

// Activities holds the dependencies needed by every activity function. One
// instance is registered with the worker via RegisterActivity(a); Temporal
// dispatches method calls on it.
//
// Phase 4 wires Repo + Broker + PatchStore + Validator. The stub activity
// bodies exercise these dependencies on the happy path so Stage 8 verifies
// the full integration; Phase 5 will replace stubs with real LLM bodies
// without touching the dependency surface.
type Activities struct {
	Repo      *repo.WorkflowRepo
	Broker    *sse.Broker[domain.ActivityEvent]
	Patches   domain.PatchStore     // optional — when nil, BackendCodegen skips object-storage round-trip
	Validator ValidatorClient       // optional — when nil, BackendCodegen skips sandbox call
	StubSleep time.Duration         // 0 = no sleep; set via WORKFLOW_STUB_DURATION_MS
}

// ValidatorClient is the port BackendCodegen depends on to issue a sandbox
// validation. The concrete client lives in internal/adapter/validator; using
// an interface here keeps the recovery package free of the http client
// dependency.
type ValidatorClient interface {
	Validate(ctx context.Context, in ValidateRequest) (ValidateResponse, error)
}

// ValidateRequest mirrors the validator service's HTTP body. Kept here so
// callers don't need to import the validator adapter package.
type ValidateRequest struct {
	RepoSHA   string
	PatchDiff string
	Image     string
	TimeoutMs int
}

// ValidateResponse mirrors the validator service's HTTP response shape.
type ValidateResponse struct {
	TestsPassed bool
	TestCount   int
	FailCount   int
	Coverage    float64
	DurationMs  int64
	Logs        string
}

// NewActivities constructs an Activities. stubSleep is the duration each stub
// activity should hold before returning — set to 0 for fast unit tests; the
// compose env supplies a non-zero value for visible-in-UI runs. Pass nil for
// patches + validator to disable the storage / sandbox smoke-tests (tests do
// this; the real Compose setup always supplies both via NewActivitiesFull).
func NewActivities(r *repo.WorkflowRepo, b *sse.Broker[domain.ActivityEvent], stubSleep time.Duration) *Activities {
	return &Activities{Repo: r, Broker: b, StubSleep: stubSleep}
}

// NewActivitiesFull is the production constructor that wires every
// dependency. The test path uses NewActivities + nil patches/validator;
// main.go uses this to compose the full activity object before worker
// registration. The Temporal SDK only inspects methods of the form
// (ctx, …) (T, error) or (ctx, …) error so non-activity setters would
// panic at RegisterActivity — we avoid that by funneling all setup through
// constructors.
func NewActivitiesFull(r *repo.WorkflowRepo, b *sse.Broker[domain.ActivityEvent], ps domain.PatchStore, v ValidatorClient, stubSleep time.Duration) *Activities {
	return &Activities{Repo: r, Broker: b, Patches: ps, Validator: v, StubSleep: stubSleep}
}

// RecordActivityEvent persists a row to activity_events + publishes to the
// SSE broker. Called explicitly from the workflow function (NOT triggered by
// Temporal heartbeats) so we control the seq numbering and the wire shape.
func (a *Activities) RecordActivityEvent(ctx context.Context, in RecordEventInput) error {
	if a.Repo == nil {
		// Test path — pipeline_test.go injects a mock RecordActivityEvent via
		// OnActivity so this branch is only reached when a test bypasses the
		// mock. Returning nil keeps the workflow happy.
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

// stubSleep is the body shared by every Phase 4 activity. Phases 5+6 replace
// each per-agent function with real work while keeping this signature.
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

// One method per agent. Phase 5 swaps bodies; the workflow orchestration
// stays put.
func (a *Activities) SentinelDetect(ctx context.Context, _ PipelineInput) (domain.ActivityResult, error) {
	return a.stub(ctx, domain.AgentSentinel, "Sentinel.Detect")
}
func (a *Activities) PathfinderDiagnose(ctx context.Context, _ PipelineInput) (domain.ActivityResult, error) {
	return a.stub(ctx, domain.AgentPathfinder, "Pathfinder.Diagnose")
}
func (a *Activities) SynthesiserPlan(ctx context.Context, _ PipelineInput) (domain.ActivityResult, error) {
	return a.stub(ctx, domain.AgentSynthesiser, "Synthesiser.Plan")
}
func (a *Activities) ArchitectSolution(ctx context.Context, _ PipelineInput) (domain.ActivityResult, error) {
	return a.stub(ctx, domain.AgentArchitect, "Architect.Solution")
}
// BackendCodegen exercises the full patch-store + validator round-trip on
// the happy path so Stage 8 e2e verifies the wiring. When Patches or
// Validator is nil (test path) the activity falls back to the bare stub.
func (a *Activities) BackendCodegen(ctx context.Context, in PipelineInput) (domain.ActivityResult, error) {
	activity.GetLogger(ctx).Info("Backend.Codegen begin", "run", in.RunID)
	if a.StubSleep > 0 {
		select {
		case <-time.After(a.StubSleep):
		case <-ctx.Done():
			return domain.ActivityResult{}, ctx.Err()
		}
	}

	// Best-effort smoke: store an empty patch + a fake report so Stage 8
	// can grep the MinIO bucket. Errors are logged but don't fail the
	// activity in Phase 4 — Phase 5 will surface real failures.
	payload := map[string]interface{}{}
	if a.Patches != nil {
		bucket := domain.BucketForOrg(in.OrgID)
		patchKey := "patches/" + in.RunID + "/Backend.Codegen.patch.enc"
		if err := a.Patches.Put(ctx, domain.PutOptions{
			Bucket: bucket, Key: patchKey, Body: []byte(""),
			ContentType: "application/octet-stream",
		}); err != nil {
			activity.GetLogger(ctx).Warn("Backend.Codegen patch put failed", "err", err)
		} else {
			payload["patch_key"] = patchKey
		}

		if a.Validator != nil {
			rep, err := a.Validator.Validate(ctx, ValidateRequest{
				RepoSHA:   "fixture",
				PatchDiff: "",
			})
			if err != nil {
				activity.GetLogger(ctx).Warn("Backend.Codegen validator failed", "err", err)
			} else {
				payload["tests_passed"] = rep.TestsPassed
				payload["test_count"] = rep.TestCount
				payload["coverage"] = rep.Coverage
				reportJSON, _ := json.Marshal(rep)
				reportKey := "reports/" + in.RunID + "/Backend.Codegen.report.json.enc"
				_ = a.Patches.Put(ctx, domain.PutOptions{
					Bucket: bucket, Key: reportKey, Body: reportJSON,
					ContentType: "application/json",
				})
				payload["report_key"] = reportKey
			}
		}
	}

	activity.GetLogger(ctx).Info("Backend.Codegen end", "run", in.RunID, "payload", payload)
	return domain.ActivityResult{
		AgentRole: domain.AgentBackend,
		Status:    domain.ActSucceeded,
		Message:   "stub completed",
		Payload:   payload,
	}, nil
}
func (a *Activities) QATestGen(ctx context.Context, _ PipelineInput) (domain.ActivityResult, error) {
	return a.stub(ctx, domain.AgentQA, "QA.TestGen")
}
func (a *Activities) DevOpsPipeline(ctx context.Context, _ PipelineInput) (domain.ActivityResult, error) {
	return a.stub(ctx, domain.AgentDevOps, "DevOps.Pipeline")
}
func (a *Activities) DataEngineerMigrate(ctx context.Context, _ PipelineInput) (domain.ActivityResult, error) {
	return a.stub(ctx, domain.AgentDataEngineer, "DataEngineer.Migrations")
}
func (a *Activities) ApprovalGateRoute(ctx context.Context, _ PipelineInput) (domain.ActivityResult, error) {
	return a.stub(ctx, domain.AgentApprovalGate, "ApprovalGate.Route")
}
