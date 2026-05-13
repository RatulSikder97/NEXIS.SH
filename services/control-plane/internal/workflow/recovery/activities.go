package recovery

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/agents"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/approval"
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

	// Phase 6 — Approval Gate activity dependency. When nil the
	// ApprovalGateRoute activity falls back to the stub path so the test
	// suite keeps running without a Postgres dependency.
	Approval *approval.Service

	// Phase 6 — Sentinel.Detect ack dependency. The Sentinel goroutine does
	// the real detection; the activity body just looks up the triggered row
	// so downstream activities (Pathfinder) have title/service/environment
	// in their payload trail. Nil tolerated — the activity falls back to the
	// stub path so the test suite keeps working without a Postgres dep.
	IncidentsAdmin domain.IncidentsReader
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

// SentinelDetect is the thin acknowledgement step that runs inside the
// workflow. The heavy lifting moved to the Sentinel detector goroutine
// (internal/sentinel/) — by the time this activity executes, the row already
// exists in incidents_raw and the workflow run was started with the row's id
// in PipelineInput.IncidentID.
//
// The activity looks up the row via the admin pool (no RLS principal in ctx)
// and returns a small payload {incident_id, title, service, environment,
// level, received_at} for downstream activities (Pathfinder) to consume from
// PriorOutputs without re-reading the database.
//
// When IncidentsAdmin is nil (test path, or boot without DATABASE_URL) the
// activity falls back to the Phase 4 stub — keeps the test suite running
// without DB ceremony.
func (a *Activities) SentinelDetect(ctx context.Context, in PipelineInput) (domain.ActivityResult, error) {
	if a.IncidentsAdmin == nil || in.IncidentID == "" || in.IncidentID == "manual" || in.IncidentID == "demo" {
		return a.stub(ctx, domain.AgentSentinel, "Sentinel.Detect")
	}
	// We don't have a "GetByID" port — PollFatalSince since unix epoch returns
	// up to 50 rows ordered by received_at ASC. For Phase 6 fixtures + the
	// fixture-pump path that's perfectly adequate (one or two rows in the
	// table at trigger time). The Phase 7 metric-pipeline rewrite of Sentinel
	// will widen the port with a dedicated GetByID.
	rows, err := a.IncidentsAdmin.PollFatalSince(ctx, in.OrgID, time.Time{})
	var row domain.IncidentRow
	if err == nil {
		for _, r := range rows {
			if r.ID == in.IncidentID {
				row = r
				break
			}
		}
	}
	activity.GetLogger(ctx).Info("sentinel.detect.ack",
		"incident_id", in.IncidentID, "level", row.Level, "service", row.Service)
	return domain.ActivityResult{
		AgentRole: domain.AgentSentinel,
		Status:    domain.ActSucceeded,
		Message:   "sentinel ack",
		Payload: map[string]interface{}{
			"incident_id": in.IncidentID,
			"title":       row.Title,
			"service":     row.Service,
			"environment": row.Environment,
			"level":       row.Level,
			"received_at": row.ReceivedAt,
		},
	}, nil
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
// ApprovalGateRoute is the activity body for the ApprovalGate.Route step.
// Phase 6 wires the real approval gate: classify severity from the
// synthesiser scenario + backend patch, INSERT the pending decision row,
// fire the notifier (best-effort), and return the decision id + severity
// in the activity payload so the workflow can branch.
//
// The actual signal/timer race happens INSIDE the workflow function (see
// recovery/workflow.go) — this activity only sets up the row + notifies.
// We split it that way because workflow.GetSignalChannel + workflow.NewTimer
// are replay-safe only inside a workflow.Context; doing the wait here would
// silently break determinism on history replay.
func (a *Activities) ApprovalGateRoute(ctx context.Context, in PipelineInput) (domain.ActivityResult, error) {
	if a.Approval == nil {
		return a.stub(ctx, domain.AgentApprovalGate, "ApprovalGate.Route")
	}

	// Extract scenario (Synthesiser output) + patch (Backend output) from
	// the prior map. Synthesiser plan keyed under "synthesiser"; backend
	// patch_diff inside "backend".structured.patch_diff.
	scenario := ""
	if syn, ok := in.PriorOutputs["synthesiser"].(map[string]any); ok {
		if v, ok := syn["scenario"].(string); ok {
			scenario = v
		}
	}
	patchDiff := ""
	if be, ok := in.PriorOutputs["backend"].(map[string]any); ok {
		// Backend's payload may already be flattened to its structured shape
		// by foldPrior — try the direct lookup first, then fall back.
		if d, ok := be["patch_diff"].(string); ok {
			patchDiff = d
		}
		if patchDiff == "" {
			if s, ok := be["structured"].(map[string]any); ok {
				if d, ok := s["patch_diff"].(string); ok {
					patchDiff = d
				}
			}
		}
	}

	sev, risk := approval.Classify(scenario, patchDiff)
	id, err := a.Approval.CreatePending(ctx, approval.CreateInput{
		OrgID:         in.OrgID,
		WorkspaceID:   in.WorkspaceID,
		WorkflowRunID: in.RunID,
		Severity:      sev,
		Scenario:      scenario,
		RiskScore:     risk,
	})
	if err != nil {
		return domain.ActivityResult{}, err
	}

	// Notification dispatch is best-effort — channel failures shouldn't
	// fail the activity. The multi-fanout swallows per-channel errors.
	a.Approval.Notify(ctx, domain.Notification{
		OrgID:         in.OrgID,
		WorkspaceID:   in.WorkspaceID,
		WorkflowRunID: in.RunID,
		Kind:          domain.NotifApprovalRequested,
		Severity:      sev,
		Scenario:      scenario,
		Title:         fmt.Sprintf("Approval required — %s", scenario),
		Body:          "Pipeline is parked at the Approval Gate.",
	})

	return domain.ActivityResult{
		AgentRole: domain.AgentApprovalGate,
		Status:    domain.ActSucceeded,
		Message:   fmt.Sprintf("approval pending — severity=%s scenario=%s", sev, scenario),
		Payload: map[string]any{
			"decision_id": id,
			"severity":    string(sev),
			"scenario":    scenario,
			"risk_score":  risk,
		},
	}, nil
}

// ApprovalGateFinalize is the activity body that runs AFTER the workflow's
// signal/timer race resolves. It records the terminal decision (auto |
// timeout | human) and writes the audit row. The result payload echoes the
// decision so the workflow output's last frame surfaces it to the UI.
func (a *Activities) ApprovalGateFinalize(ctx context.Context, in ApprovalFinalizeInput) (domain.ActivityResult, error) {
	if a.Approval == nil {
		// Phase 4 test path — no approval wired, no-op.
		return domain.ActivityResult{
			AgentRole: domain.AgentApprovalGate,
			Status:    domain.ActSucceeded,
			Message:   "approval gate finalised (stub)",
		}, nil
	}
	var err error
	switch in.Signal.Decision {
	case domain.ApprovalAutoApproved:
		err = a.Approval.AutoApprove(ctx, in.WorkflowRunID, in.OrgID)
	case domain.ApprovalTimeoutRejected:
		err = a.Approval.TimeoutReject(ctx, in.WorkflowRunID, in.OrgID)
	case domain.ApprovalApproved, domain.ApprovalRejected:
		err = a.Approval.RecordDecision(ctx, in.WorkflowRunID, in.OrgID, in.Signal)
	default:
		err = fmt.Errorf("approval: unknown terminal decision %q", in.Signal.Decision)
	}
	if err != nil {
		return domain.ActivityResult{}, err
	}
	status := domain.ActSucceeded
	if in.Signal.Decision == domain.ApprovalRejected || in.Signal.Decision == domain.ApprovalTimeoutRejected {
		status = domain.ActFailed
	}
	return domain.ActivityResult{
		AgentRole: domain.AgentApprovalGate,
		Status:    status,
		Message:   fmt.Sprintf("approval decided — %s", in.Signal.Decision),
		Payload: map[string]any{
			"decision":   string(in.Signal.Decision),
			"decided_by": in.Signal.DecidedBy,
			"notes":      in.Signal.Notes,
		},
	}, nil
}
