package recovery

import (
	"context"
	"encoding/json"
	"errors"
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

	// Phase 7 — Projects (self-healing targets). When wired, LoadProject
	// reads the project snapshot for ProjectID from the projects repo. nil
	// tolerated — the workflow falls back to the legacy fixture path.
	Projects ProjectsReader

	// SlackDefaultChannel is the workspace-wide default channel id used
	// when a project's SlackChannelID is empty. Sourced from
	// cfg.SlackDefaultChannel via main.go. Empty string disables Slack
	// notifications when the project has no channel either.
	SlackDefaultChannel string
}

// ProjectsReader is the narrow port the LoadProject activity uses to fetch
// the project row. Implemented naturally by *repo.ProjectsRepo via Get.
// Defined here so tests can substitute a fake without dragging in pgx.
type ProjectsReader interface {
	Get(ctx context.Context, projectID string) (domain.Project, error)
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

// LoadProject reads the project snapshot for the run's ProjectID. Runs at
// the top of the workflow when ProjectID is non-empty. Non-fatal: when the
// projects reader is unwired, the project is archived, or the lookup
// returns ErrNotFound, we return an empty output and the workflow falls
// back to the legacy fixture path. Other errors (transient DB failure)
// surface so the workflow's retry policy can re-run the activity.
func (a *Activities) LoadProject(ctx context.Context, in LoadProjectInput) (LoadProjectOutput, error) {
	if a.Projects == nil || in.ProjectID == "" {
		return LoadProjectOutput{}, nil
	}
	p, err := a.Projects.Get(ctx, in.ProjectID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			activity.GetLogger(ctx).Info("recovery.load_project.not_found",
				"project_id", in.ProjectID, "org_id", in.OrgID)
			return LoadProjectOutput{}, nil
		}
		return LoadProjectOutput{}, err
	}
	// Defence-in-depth: only return the project when its org_id matches the
	// caller. A misconfigured projects pool would otherwise leak a sibling
	// tenant's project mappings into the wrong workflow.
	if in.OrgID != "" && p.OrgID != in.OrgID {
		activity.GetLogger(ctx).Warn("recovery.load_project.org_mismatch",
			"project_id", in.ProjectID, "expected_org", in.OrgID, "got_org", p.OrgID)
		return LoadProjectOutput{}, nil
	}
	return LoadProjectOutput{Project: &p}, nil
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
// Phase 6 replaces those four bodies for the agents that have real
// implementations; Pathfinder + Synthesiser still call into this path until
// their Phase 7 rewrites land.
//
// The stub now emits a richer payload (tokens, cost, tool_calls,
// output_summary) so the per-agent drill-down UI has something to render
// even when the LLM fleet is offline. The agent_role / model / degrade
// fields are owned by the caller (runAgent or the L2 activity body).
func (a *Activities) stub(ctx context.Context, role domain.AgentRole, name string) (domain.ActivityResult, error) {
	activity.GetLogger(ctx).Info("stub start", "agent", role, "activity", name)
	start := time.Now()
	if a.StubSleep > 0 {
		select {
		case <-time.After(a.StubSleep):
		case <-ctx.Done():
			return domain.ActivityResult{}, ctx.Err()
		}
	}
	activity.GetLogger(ctx).Info("stub end", "agent", role, "activity", name)

	// Synthesise the agent-specific stub payload up-front so every stubbed
	// run carries the same key set as a real run — tool_calls, input/output
	// summaries, token estimates, model="stub-fallback".
	payload := buildStubPayload(role, "", 0)
	payload["agent_role"] = string(role)
	payload["duration_ms"] = time.Since(start).Milliseconds()

	return domain.ActivityResult{
		AgentRole: role,
		Status:    domain.ActSucceeded,
		Message:   fmt.Sprintf("agent=%s stub completed", role),
		Payload:   payload,
	}, nil
}

// runAgent dispatches one L1 agent via the Registry. When Agents is nil
// (test path / Phase 4 compatibility) it falls back to the stub.
func (a *Activities) runAgent(ctx context.Context, name domain.AgentName, in PipelineInput) (domain.ActivityResult, error) {
	start := time.Now()
	role := agentRoleOf(name)
	inputSummary := summariseAgentInput(in, name)

	if a.Agents == nil {
		// No registry wired — Phase 4 stub path. Promote the stub to a
		// degraded synthetic run so admins can still inspect tool calls
		// and tokens in the drill-down UI.
		res, err := a.stub(ctx, role, string(name)+".Run")
		if err != nil {
			return res, err
		}
		enrichStubFallback(res.Payload, role, inputSummary,
			"agents registry not wired", start, name)
		res.Message = fmt.Sprintf("agent=%s degraded (no registry)", name)
		return res, nil
	}
	out, err := a.Agents.Run(ctx, name, domain.AgentInput{
		WorkflowRunID: in.RunID,
		OrgID:         in.OrgID,
		WorkspaceID:   in.WorkspaceID,
		PriorOutputs:  in.PriorOutputs,
		Incident:      in.Incident,
		RepoSHA:       in.RepoSHA,
		Context:       buildAgentContext(in),
	})
	if err != nil {
		// Budget exceeded is non-retryable so the workflow surfaces it as
		// status='cancelled' rather than retry-storming.
		if isBudgetExceeded(err) {
			return domain.ActivityResult{}, temporal.NewNonRetryableApplicationError(
				err.Error(), "BudgetError", err,
			)
		}
		// Phase 5 stub fallback: any other agent error (LLM 401, network, schema
		// retries exhausted, etc.) demotes to the Phase-4 stub so the demo loop
		// still lights up end-to-end without a live LLM key. The stub payload
		// includes the original error so the timeline UI can show "degraded".
		res, stubErr := a.stub(ctx, role, string(name)+".Run")
		if stubErr != nil {
			return domain.ActivityResult{}, err
		}
		if res.Payload == nil {
			res.Payload = map[string]any{}
		}
		enrichStubFallback(res.Payload, role, inputSummary, err.Error(), start, name)
		res.Message = fmt.Sprintf("agent=%s degraded (stub fallback)", name)
		return res, nil
	}
	// Success path. Every finish frame MUST carry duration_ms / model /
	// output_summary alongside the existing token/cost trio so the admin
	// drill-down can render the same set of fields for real + stubbed
	// runs.
	duration := out.DurationMs
	if duration == 0 {
		duration = time.Since(start).Milliseconds()
	}
	payload := map[string]any{
		"agent_role":      string(role),
		"tokens_in":       out.TokensIn,
		"tokens_out":      out.TokensOut,
		"cached_tokens":   out.CachedTokens,
		"cost_cents":      out.CostCents,
		"duration_ms":     duration,
		"model":           out.Model,
		"provider":        out.Provider,
		"structured":      out.Structured,
		"schema_retries":  out.SchemaRetries,
		"input_summary":   inputSummary,
		"output_summary":  summariseAgentOutput(role, out),
		"degraded":        false,
	}
	return domain.ActivityResult{
		AgentRole: role,
		Status:    domain.ActSucceeded,
		Message: fmt.Sprintf("agent=%s tokens=%d/%d cost_cents=%.4f",
			name, out.TokensIn, out.TokensOut, out.CostCents),
		Payload: payload,
	}, nil
}

// enrichStubFallback decorates a stub payload with the full key set required
// by the admin drill-down: degraded + reason, agent_role, token estimates,
// duration_ms, model="stub-fallback", tool_calls, output_summary, and the
// input_summary the caller pre-computed.
//
// `start` is the time runAgent began, so duration_ms reflects the real
// elapsed wall-clock — including the (failed) LLM call that triggered the
// fallback, not just the stub's sleep.
func enrichStubFallback(payload map[string]any, role domain.AgentRole, inputSummary, reason string, start time.Time, name domain.AgentName) {
	if payload == nil {
		return
	}
	stubOut := stubTokenEstimate(role)
	tokensIn := estimateTokensFromText(inputSummary)
	payload["agent_role"] = string(role)
	payload["degraded"] = true
	payload["degrade_reason"] = reason
	payload["tokens_in"] = tokensIn
	payload["tokens_out"] = stubOut
	payload["cost_cents"] = 0
	payload["duration_ms"] = time.Since(start).Milliseconds()
	payload["model"] = "stub-fallback"
	payload["input_summary"] = inputSummary
	payload["output_summary"] = stubOutputSummary(role)
	payload["tool_calls"] = stubToolCalls(role, inputSummary)
	_ = name // currently unused, here so future per-name branching can land cleanly
}

// summariseAgentInput returns a <=200 char digest of the incoming
// AgentInput message — incident title + service + first stacktrace line
// when present, falling back to the synthesiser scenario or the agent name.
//
// This is what the drill-down UI shows as "what did this agent see when it
// started" so the field must always be filled in even when the incident is
// nil.
func summariseAgentInput(in PipelineInput, name domain.AgentName) string {
	const maxLen = 200
	var s string
	switch {
	case in.Incident != nil && in.Incident.Title != "":
		s = in.Incident.Title
		if in.Incident.Service != "" {
			s += " (" + in.Incident.Service + ")"
		}
		if in.Incident.Stacktrace != "" {
			// First line of the stack adds the most signal.
			first := in.Incident.Stacktrace
			for i, c := range first {
				if c == '\n' {
					first = first[:i]
					break
				}
			}
			s += " — " + first
		}
	case in.IncidentID != "":
		s = "incident=" + in.IncidentID + " triggered_by=" + in.TriggeredBy
	default:
		s = "agent=" + string(name) + " run=" + in.RunID
	}
	if len(s) > maxLen {
		s = s[:maxLen-3] + "..."
	}
	return s
}

// summariseAgentOutput returns a one-line description of what the agent
// produced — used by the drill-down UI as the "output_summary" tile.
// Inspects out.Structured by agent role; falls back to a token / model
// snapshot when the structured payload is empty.
func summariseAgentOutput(role domain.AgentRole, out domain.AgentOutput) string {
	if out.Structured != nil {
		switch role {
		case domain.AgentBackend:
			if d, ok := out.Structured["patch_diff"].(string); ok && d != "" {
				lines := 1
				for _, c := range d {
					if c == '\n' {
						lines++
					}
				}
				return fmt.Sprintf("Unified diff: %d lines (%d tokens, %s)", lines, out.TokensOut, out.Model)
			}
		case domain.AgentQA:
			if cases, ok := out.Structured["test_cases"].([]any); ok {
				return fmt.Sprintf("Generated %d test cases (%d tokens, %s)", len(cases), out.TokensOut, out.Model)
			}
		case domain.AgentArchitect:
			if plan, ok := out.Structured["solution_plan"].(string); ok && plan != "" {
				return fmt.Sprintf("Solution plan: %d chars (%d tokens, %s)", len(plan), out.TokensOut, out.Model)
			}
		case domain.AgentDevOps:
			if yaml, ok := out.Structured["pipeline_yaml"].(string); ok && yaml != "" {
				return fmt.Sprintf("Pipeline YAML: %d chars (%d tokens, %s)", len(yaml), out.TokensOut, out.Model)
			}
		case domain.AgentDataEngineer:
			if sql, ok := out.Structured["migration_sql"].(string); ok && sql != "" {
				return fmt.Sprintf("Migration SQL: %d chars (%d tokens, %s)", len(sql), out.TokensOut, out.Model)
			}
		}
	}
	return fmt.Sprintf("Completed: %d tokens out (%s)", out.TokensOut, out.Model)
}

// buildStubPayload returns the per-agent synthetic payload skeleton — tool
// calls + output summary keyed by role. The runAgent / activity caller
// decorates it with degraded + tokens + duration on top. Kept as a separate
// helper so tests can assert the exact tool_calls shape without dragging in
// the full activity stack.
func buildStubPayload(role domain.AgentRole, inputSummary string, tokensIn int) map[string]any {
	return map[string]any{
		"tool_calls":     stubToolCalls(role, inputSummary),
		"output_summary": stubOutputSummary(role),
		"tokens_in":      tokensIn,
		"tokens_out":     stubTokenEstimate(role),
		"cost_cents":     0,
		"model":          "stub-fallback",
	}
}

// stubTokenEstimate returns the constant tokens_out estimate per the spec
// — 256 for L1, 128 for L2 detectors, 64 for routers.
func stubTokenEstimate(role domain.AgentRole) int {
	switch role {
	case domain.AgentArchitect, domain.AgentBackend, domain.AgentQA,
		domain.AgentDevOps, domain.AgentDataEngineer:
		return 256
	case domain.AgentSentinel, domain.AgentPathfinder,
		domain.AgentSynthesiser:
		return 128
	case domain.AgentApprovalGate, domain.AgentPipeline:
		return 64
	}
	return 64
}

// estimateTokensFromText is the cheap len/4 approximation used by the stub
// fallback to fill tokens_in. Real agents pass through tiktoken; this is a
// good-enough proxy when there's no LLM in the loop.
func estimateTokensFromText(s string) int {
	if s == "" {
		return 0
	}
	return len(s) / 4
}

// stubToolCalls returns the synthetic but plausible tool-call array per
// agent. The shape mirrors what a real LLM-driven tool-use trace looks like
// so the admin drill-down UI can reuse the same renderer.
func stubToolCalls(role domain.AgentRole, inputSummary string) []map[string]any {
	switch role {
	case domain.AgentBackend:
		return []map[string]any{
			{"tool": "graph.fetch_caller_chain", "args": map[string]any{"symbol": firstWord(inputSummary)}, "result": "ok"},
			{"tool": "patch.synthesize", "args": map[string]any{"scope": "single-file"}, "result": "draft.patch (32 lines)"},
		}
	case domain.AgentQA:
		return []map[string]any{
			{"tool": "test.generate", "args": map[string]any{"target": firstWord(inputSummary)}, "result": "3 cases"},
			{"tool": "test.coverage_estimate", "args": map[string]any{"target": firstWord(inputSummary)}, "result": "94% line coverage"},
		}
	case domain.AgentArchitect:
		return []map[string]any{
			{"tool": "plan.synthesize", "args": map[string]any{"layers": []string{"backend", "qa"}}, "result": "2-step recovery plan"},
			{"tool": "graph.list_callers", "args": map[string]any{"symbol": firstWord(inputSummary)}, "result": "4 callers"},
		}
	case domain.AgentDevOps:
		return []map[string]any{
			{"tool": "ci.synthesize_workflow", "args": map[string]any{"runtime": "github-actions"}, "result": "workflow.yaml (12 jobs)"},
			{"tool": "ci.diff", "args": map[string]any{"branch": "main"}, "result": "3 lines changed"},
		}
	case domain.AgentDataEngineer:
		return []map[string]any{
			{"tool": "schema.diff", "args": map[string]any{"target": "public"}, "result": "1 column added"},
			{"tool": "migration.synthesize", "args": map[string]any{"engine": "postgres"}, "result": "0042_add_column.up.sql"},
		}
	case domain.AgentPathfinder:
		return []map[string]any{
			{"tool": "graph.causal_estimand", "args": map[string]any{"symbol": firstWord(inputSummary)}, "result": "1 cause"},
			{"tool": "log.cluster", "args": map[string]any{"service": firstWord(inputSummary)}, "result": "2 clusters"},
		}
	case domain.AgentSynthesiser:
		return []map[string]any{
			{"tool": "plan.classify_scenario", "args": map[string]any{"input": firstWord(inputSummary)}, "result": "null-deref (0.91)"},
			{"tool": "plan.pick_agents", "args": map[string]any{"scenario": "null-deref"}, "result": "[backend, qa, devops]"},
		}
	case domain.AgentRole("validator_l2"):
		return []map[string]any{
			{"tool": "sandbox.run_tests", "args": map[string]any{"timeout_ms": 30000}, "result": "12 passed / 0 failed"},
			{"tool": "hypothesis.property_check", "args": map[string]any{"strategy": "fuzz"}, "result": "0 counter-examples"},
		}
	case domain.AgentSentinel:
		return []map[string]any{
			{"tool": "sentry.poll_fatal", "args": map[string]any{"since": "5m"}, "result": "1 incident"},
		}
	case domain.AgentApprovalGate:
		return []map[string]any{
			{"tool": "approval.classify_severity", "args": map[string]any{"scenario": "null-deref"}, "result": "medium (risk 0.42)"},
		}
	}
	return []map[string]any{
		{"tool": "stub.noop", "args": map[string]any{}, "result": "ok"},
	}
}

// stubOutputSummary returns the per-agent one-liner shown in the
// "output_summary" tile. Used by both the stub-fallback path (degraded
// runs) and the regular L2 stub bodies still on the Phase 4 carryover.
func stubOutputSummary(role domain.AgentRole) string {
	switch role {
	case domain.AgentArchitect:
		return "Stubbed plan: dispatch [backend, qa] in sequence"
	case domain.AgentBackend:
		return "Stubbed unified diff: 1 file, +12 −3 lines"
	case domain.AgentQA:
		return "Stubbed 3 unit tests covering happy + 2 edge paths"
	case domain.AgentDevOps:
		return "Stubbed CI workflow: 12 jobs, 1 patched gate"
	case domain.AgentDataEngineer:
		return "Stubbed migration: 1 column add, backfill plan included"
	case domain.AgentPathfinder:
		return "Stubbed root-cause: null-pointer at handler/foo.go:42"
	case domain.AgentSynthesiser:
		return "Stubbed plan: scenario=null-deref, fleet=[backend, qa, devops]"
	case domain.AgentRole("validator_l2"):
		return "Stubbed validation: 12 tests passed, 0 property counter-examples"
	case domain.AgentSentinel:
		return "Stubbed detection: 1 fatal incident ingested"
	case domain.AgentApprovalGate:
		return "Stubbed decision: severity=medium, 2-min countdown queued"
	case domain.AgentPipeline:
		return "Stubbed pipeline summary: 9 stages, all completed"
	}
	return "Stub completed"
}

// firstWord plucks the first whitespace-delimited token out of the input
// summary so tool-call args can carry a "symbol" / "service" hint without
// dragging the whole sentence into the wire payload.
func firstWord(s string) string {
	if s == "" {
		return "unknown"
	}
	for i, c := range s {
		if c == ' ' || c == '\t' || c == '\n' {
			if i == 0 {
				continue
			}
			return s[:i]
		}
	}
	return s
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

// buildAgentContext folds the project selectors (when bound) into the
// AgentInput.Context map so L1 prompts can target the right repo / branch /
// installation. Falls back to the fixture mapping when no project is bound —
// "acme/orders-api-fixture" matches the existing Phase 5/6 stub paths.
func buildAgentContext(in PipelineInput) map[string]any {
	ctx := map[string]any{}
	if in.Project != nil {
		ctx["project_id"] = in.Project.ID
		ctx["project_slug"] = in.Project.Slug
		ctx["github_repo"] = in.Project.Selectors.GitHubRepo
		ctx["github_default_branch"] = in.Project.Selectors.GitHubDefaultBranch
		ctx["github_installation_id"] = in.Project.Selectors.GitHubInstallationID
		ctx["argocd_app_name"] = in.Project.Selectors.ArgoCDAppName
		ctx["argocd_project"] = in.Project.Selectors.ArgoCDProject
		ctx["environment"] = string(in.Project.Environment)
		return ctx
	}
	// Fixture fallback — keeps the Phase 5/6 demo loop alive when no
	// project is bound to the run.
	ctx["github_repo"] = "acme/orders-api-fixture"
	ctx["github_default_branch"] = "main"
	ctx["environment"] = "prod"
	return ctx
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

	// Phase 7 — Projects (self-healing). When a project is bound, the
	// policy can:
	//  - kill-switch → reject immediately (and the workflow short-circuits
	//    on the kill-switch check at the top, so reaching here means the
	//    switch flipped mid-run).
	//  - auto-merge LOW → demote to auto-approved (workflow returns
	//    immediately, no signal/timer race).
	//  - auto-merge MEDIUM → keep severity=medium but tell the workflow to
	//    use the project's countdown rather than the default 2-min timer.
	autoApprove := false
	killSwitch := false
	countdownSecs := 0
	if in.Project != nil {
		policy := in.Project.Policy
		if policy.KillSwitchEnabled {
			killSwitch = true
		}
		switch sev {
		case domain.SeverityLow:
			if policy.AutoMergeLowSeverity {
				autoApprove = true
			}
		case domain.SeverityMedium:
			if policy.AutoMergeMediumSeverity {
				autoApprove = true
			}
			if policy.MediumCountdownSeconds > 0 {
				countdownSecs = policy.MediumCountdownSeconds
			}
		}
	}

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
	//
	// When a project is bound, the project's SlackChannelID is the routing
	// target; we fold it into the Notification under a metadata-style key
	// the slack notifier picks up. (Falling back to cfg.SlackDefaultChannel
	// is the notifier's responsibility — it sees an empty channel and
	// substitutes the workspace default.)
	notif := domain.Notification{
		OrgID:         in.OrgID,
		WorkspaceID:   in.WorkspaceID,
		WorkflowRunID: in.RunID,
		Kind:          domain.NotifApprovalRequested,
		Severity:      sev,
		Scenario:      scenario,
		Title:         fmt.Sprintf("Approval required — %s", scenario),
		Body:          "Pipeline is parked at the Approval Gate.",
	}
	a.Approval.Notify(ctx, notif)

	payload := map[string]any{
		"decision_id": id,
		"severity":    string(sev),
		"scenario":    scenario,
		"risk_score":  risk,
	}
	if autoApprove {
		payload["auto_approved"] = true
	}
	if killSwitch {
		payload["kill_switch"] = true
	}
	if countdownSecs > 0 {
		payload["countdown_secs"] = countdownSecs
	}
	if in.Project != nil {
		payload["project_id"] = in.Project.ID
		if len(in.Project.Policy.ApproverUserIDs) > 0 {
			payload["approver_user_ids"] = in.Project.Policy.ApproverUserIDs
		}
		if in.Project.Selectors.SlackChannelID != "" {
			payload["slack_channel_id"] = in.Project.Selectors.SlackChannelID
		} else if a.SlackDefaultChannel != "" {
			payload["slack_channel_id"] = a.SlackDefaultChannel
		}
	}

	msg := fmt.Sprintf("approval pending — severity=%s scenario=%s", sev, scenario)
	if killSwitch {
		msg = fmt.Sprintf("approval rejected — project kill switch engaged (scenario=%s)", scenario)
	} else if autoApprove {
		msg = fmt.Sprintf("approval auto — severity=%s scenario=%s policy=auto_merge", sev, scenario)
	}

	return domain.ActivityResult{
		AgentRole: domain.AgentApprovalGate,
		Status:    domain.ActSucceeded,
		Message:   msg,
		Payload:   payload,
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
