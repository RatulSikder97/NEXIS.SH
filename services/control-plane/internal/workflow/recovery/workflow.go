package recovery

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// ApprovalSignalName is the channel the workflow listens on for the human
// approve/reject signal. Mirrors approval.SignalName — we duplicate the
// constant here (rather than importing the approval package) because the
// arch lint rule allows workflow→adapter but cycle-avoidance keeps this
// package import-cycle-free relative to the approval shard.
const ApprovalSignalName = "approval.decision"

// ApprovalMediumTimeout is the auto-timeout for MEDIUM severity decisions.
// Matches the value passed into approval.WaitForDecision; lifted to package
// scope so tests can shorten it via build-tag overrides if needed in future.
const ApprovalMediumTimeout = 2 * time.Minute

// stdActivityOpts is the default retry + timeout policy applied to every
// stub activity in Phase 4. Phase 5's LLM-bound activities override
// StartToCloseTimeout to 5 minutes via a per-call ActivityOptions context.
var stdActivityOpts = workflow.ActivityOptions{
	StartToCloseTimeout:    30 * time.Second,
	ScheduleToCloseTimeout: 2 * time.Minute,
	RetryPolicy: &temporal.RetryPolicy{
		InitialInterval:        1 * time.Second,
		BackoffCoefficient:     2.0,
		MaximumInterval:        30 * time.Second,
		MaximumAttempts:        3,
		NonRetryableErrorTypes: []string{"ValidationError", "ForbiddenError"},
	},
}

// llmActivityOpts overrides start-to-close for codegen/test-gen activities.
// Phase 5 caps MaximumAttempts at 2 — schema retries already happen inside
// the agent layer, so the workflow-level retry would multiply token cost on
// LLM-bound activities.
var llmActivityOpts = func() workflow.ActivityOptions {
	o := stdActivityOpts
	o.StartToCloseTimeout = 5 * time.Minute
	if o.RetryPolicy != nil {
		rp := *o.RetryPolicy
		rp.MaximumAttempts = 2
		o.RetryPolicy = &rp
	}
	return o
}()

// recorderOpts is the tight policy for RecordActivityEvent — fast, retries
// transient DB hiccups, but never the workflow-blocking 5-minute LLM window.
var recorderOpts = workflow.ActivityOptions{
	StartToCloseTimeout: 5 * time.Second,
	RetryPolicy: &temporal.RetryPolicy{
		InitialInterval: 200 * time.Millisecond,
		MaximumAttempts: 3,
	},
}

// RecoveryPipeline is the 9-step DAG. The shape MUST match the spec table
// (3 sequential L2 → 3 sequential L1 → DevOps||DataEngineer parallel → ApprovalGate
// join) — Phases 5+6 only swap the activity bodies.
//
// We use *Activities methods so a single activity object owns all dependencies
// (repo, broker, patchstore, validator). The Temporal SDK resolves the method
// name from the registered activity at runtime; the underscore arg in the
// method's "_ PipelineInput" parameter ensures every stub has the same
// signature.
func RecoveryPipeline(ctx workflow.Context, in PipelineInput) (PipelineOutput, error) {
	start := workflow.Now(ctx)
	results := make([]domain.ActivityResult, 0, 9)
	prior := map[string]any{}
	if in.PriorOutputs == nil {
		in.PriorOutputs = prior
	} else {
		prior = in.PriorOutputs
	}

	// Phase 7 — Projects (self-healing). LoadProject fills in.Project from
	// the projects repo when ProjectID is set; nil + error are both non-
	// fatal so the legacy fixture path still works for stub runs.
	if in.ProjectID != "" {
		loadCtx := workflow.WithActivityOptions(ctx, stdActivityOpts)
		var loaded LoadProjectOutput
		if err := workflow.ExecuteActivity(loadCtx, (*Activities).LoadProject, LoadProjectInput{
			OrgID:     in.OrgID,
			ProjectID: in.ProjectID,
		}).Get(loadCtx, &loaded); err != nil {
			workflow.GetLogger(ctx).Warn("recovery.load_project_failed",
				"project_id", in.ProjectID, "err", err)
		} else if loaded.Project != nil {
			in.Project = loaded.Project
			// Kill-switch short-circuit. If the project's policy has the kill
			// switch engaged, fail closed before any agent fires. Saves token
			// burn and stops the pipeline at the earliest possible moment.
			if loaded.Project.Policy.KillSwitchEnabled {
				recordEvent(ctx, in, domain.AgentPipeline, "Pipeline.KillSwitch", domain.ActFailed,
					"project kill switch engaged", map[string]interface{}{
						"project_id": loaded.Project.ID,
					}, 1)
				return PipelineOutput{}, temporal.NewNonRetryableApplicationError(
					"project kill switch engaged", "KillSwitchError", nil,
				)
			}
		}
	}

	// foldPrior pulls structured payload of a completed activity into the
	// prior map under its agent name. This is what each L1 agent reads via
	// AgentInput.PriorOutputs.
	foldPrior := func(name domain.AgentName, payload map[string]any) {
		if payload == nil {
			return
		}
		if s, ok := payload["structured"].(map[string]any); ok {
			prior[string(name)] = s
		}
	}

	// runActivity records started → executes → records terminal status.
	// Failures cause the workflow to return early; the reaper goroutine on
	// the adapter side observes the workflow failure and updates the
	// workflow_runs row to status=failed.
	runActivity := func(role domain.AgentRole, activityName string, fn any, opts workflow.ActivityOptions, agentName domain.AgentName) (domain.ActivityResult, error) {
		c := workflow.WithActivityOptions(ctx, opts)
		recordEvent(ctx, in, role, activityName, domain.ActStarted, "", nil, 1)
		// Snapshot a fresh PipelineInput so each ExecuteActivity carries
		// the current prior map without races (Temporal serialises history;
		// passing a shared pointer here is fine but explicit is safer).
		stepIn := in
		stepIn.PriorOutputs = clonePrior(prior)
		var r domain.ActivityResult
		if err := workflow.ExecuteActivity(c, fn, stepIn).Get(c, &r); err != nil {
			recordEvent(ctx, in, role, activityName, domain.ActFailed, err.Error(), nil, 1)
			return domain.ActivityResult{}, err
		}
		recordEvent(ctx, in, role, activityName, domain.ActSucceeded, r.Message, r.Payload, 1)
		if agentName != "" {
			foldPrior(agentName, r.Payload)
		}
		return r, nil
	}

	// ---- L2 detect → diagnose → plan (sequential) ----
	for _, step := range []struct {
		role  domain.AgentRole
		name  string
		fn    any
		opts  workflow.ActivityOptions
		agent domain.AgentName
	}{
		{domain.AgentSentinel, "Sentinel.Detect", (*Activities).SentinelDetect, stdActivityOpts, ""},
		{domain.AgentPathfinder, "Pathfinder.Diagnose", (*Activities).PathfinderDiagnose, stdActivityOpts, ""},
		{domain.AgentSynthesiser, "Synthesiser.Plan", (*Activities).SynthesiserPlan, stdActivityOpts, ""},
		// ---- L1 architect → backend → qa (sequential) ----
		{domain.AgentArchitect, "Architect.Solution", (*Activities).ArchitectSolution, llmActivityOpts, domain.AgentNameArchitect},
		{domain.AgentBackend, "Backend.Codegen", (*Activities).BackendCodegen, llmActivityOpts, domain.AgentNameBackend},
		{domain.AgentQA, "QA.TestGen", (*Activities).QATestGen, llmActivityOpts, domain.AgentNameQA},
	} {
		r, err := runActivity(step.role, step.name, step.fn, step.opts, step.agent)
		if err != nil {
			return PipelineOutput{}, err
		}
		results = append(results, r)
	}

	// ---- DevOps || DataEngineer (parallel) ----
	recordEvent(ctx, in, domain.AgentDevOps, "DevOps.Pipeline", domain.ActStarted, "", nil, 1)
	recordEvent(ctx, in, domain.AgentDataEngineer, "DataEngineer.Migrations", domain.ActStarted, "", nil, 1)

	parIn := in
	parIn.PriorOutputs = clonePrior(prior)
	devopsFut := workflow.ExecuteActivity(workflow.WithActivityOptions(ctx, llmActivityOpts),
		(*Activities).DevOpsPipeline, parIn)
	dataFut := workflow.ExecuteActivity(workflow.WithActivityOptions(ctx, llmActivityOpts),
		(*Activities).DataEngineerMigrate, parIn)

	var rDev, rData domain.ActivityResult
	if err := devopsFut.Get(ctx, &rDev); err != nil {
		recordEvent(ctx, in, domain.AgentDevOps, "DevOps.Pipeline", domain.ActFailed, err.Error(), nil, 1)
		return PipelineOutput{}, err
	}
	recordEvent(ctx, in, domain.AgentDevOps, "DevOps.Pipeline", domain.ActSucceeded, rDev.Message, rDev.Payload, 1)
	foldPrior(domain.AgentNameDevOps, rDev.Payload)
	results = append(results, rDev)

	if err := dataFut.Get(ctx, &rData); err != nil {
		recordEvent(ctx, in, domain.AgentDataEngineer, "DataEngineer.Migrations", domain.ActFailed, err.Error(), nil, 1)
		return PipelineOutput{}, err
	}
	recordEvent(ctx, in, domain.AgentDataEngineer, "DataEngineer.Migrations", domain.ActSucceeded, rData.Message, rData.Payload, 1)
	foldPrior(domain.AgentNameDataEngineer, rData.Payload)
	results = append(results, rData)

	// ---- ApprovalGate.Route (join) ----
	rApprove, err := runActivity(domain.AgentApprovalGate, "ApprovalGate.Route", (*Activities).ApprovalGateRoute, stdActivityOpts, "")
	if err != nil {
		return PipelineOutput{}, err
	}
	results = append(results, rApprove)

	// Resolve severity from the ApprovalGate activity payload. The activity
	// emits "low" | "medium" | "high" only on the Phase 6 production path;
	// the Phase 4 stub path returns no payload, which we treat as "skip the
	// signal race" so the existing test suite + dev environment still works.
	severityStr := ""
	if sev, ok := rApprove.Payload["severity"].(string); ok {
		severityStr = sev
	}
	decisionID := ""
	if id, ok := rApprove.Payload["decision_id"].(string); ok {
		decisionID = id
	}
	// Phase 7 — project policy overrides. The ApprovalGateRoute activity
	// folds these into the payload so the workflow can short-circuit
	// without re-reading the project here.
	policyKillSwitch, _ := rApprove.Payload["kill_switch"].(bool)
	policyAutoApprove, _ := rApprove.Payload["auto_approved"].(bool)
	policyCountdown := 0
	if v, ok := rApprove.Payload["countdown_secs"].(int); ok {
		policyCountdown = v
	} else if v, ok := rApprove.Payload["countdown_secs"].(int64); ok {
		policyCountdown = int(v)
	} else if v, ok := rApprove.Payload["countdown_secs"].(float64); ok {
		policyCountdown = int(v)
	}

	// Kill-switch — fail closed regardless of severity.
	if policyKillSwitch {
		out := PipelineOutput{
			DurationMS:         workflow.Now(ctx).Sub(start).Milliseconds(),
			Results:            results,
			ApprovalDecisionID: decisionID,
		}
		recordEvent(ctx, in, domain.AgentPipeline, "Pipeline.Complete", domain.ActFailed,
			"project kill switch engaged",
			map[string]interface{}{
				"duration_ms": out.DurationMS,
				"decision":    "kill_switch",
			}, 1)
		return out, temporal.NewNonRetryableApplicationError(
			"project kill switch engaged", "KillSwitchError", nil,
		)
	}

	if severityStr != "" {
		// Policy auto-approval — force LOW path so awaitApprovalDecision
		// short-circuits without a signal/timer race.
		if policyAutoApprove {
			severityStr = string(domain.SeverityLow)
		}
		// Signal/timer race. workflow.GetSignalChannel + workflow.NewTimer +
		// workflow.NewSelector are all replay-safe — running the race inside
		// the workflow body (rather than an activity) is required to keep
		// determinism on Temporal history replay.
		severity := domain.Severity(severityStr)
		mediumTimeout := ApprovalMediumTimeout
		if policyCountdown > 0 && severity == domain.SeverityMedium {
			mediumTimeout = time.Duration(policyCountdown) * time.Second
		}
		sig, sigErr := awaitApprovalDecisionWithTimeout(ctx, severity, mediumTimeout)
		if sigErr != nil {
			recordEvent(ctx, in, domain.AgentApprovalGate, "ApprovalGate.Finalize", domain.ActFailed, sigErr.Error(), nil, 1)
			return PipelineOutput{}, sigErr
		}

		// Finalise activity persists the decision + audit. Runs even on
		// rejection so the row is up-to-date when the UI re-reads it.
		finIn := ApprovalFinalizeInput{
			OrgID:         in.OrgID,
			WorkflowRunID: in.RunID,
			Signal:        sig,
		}
		finCtx := workflow.WithActivityOptions(ctx, stdActivityOpts)
		recordEvent(ctx, in, domain.AgentApprovalGate, "ApprovalGate.Finalize", domain.ActStarted, "", nil, 1)
		var rFinal domain.ActivityResult
		if err := workflow.ExecuteActivity(finCtx, (*Activities).ApprovalGateFinalize, finIn).Get(finCtx, &rFinal); err != nil {
			recordEvent(ctx, in, domain.AgentApprovalGate, "ApprovalGate.Finalize", domain.ActFailed, err.Error(), nil, 1)
			return PipelineOutput{}, err
		}
		recordEvent(ctx, in, domain.AgentApprovalGate, "ApprovalGate.Finalize", domain.ActSucceeded, rFinal.Message, rFinal.Payload, 1)
		results = append(results, rFinal)

		// Rejected / timed out terminates the workflow with a non-retryable
		// application error so workflow_runs.status flips to failed.
		if sig.Decision == domain.ApprovalRejected || sig.Decision == domain.ApprovalTimeoutRejected {
			out := PipelineOutput{
				DurationMS:         workflow.Now(ctx).Sub(start).Milliseconds(),
				Results:            results,
				ApprovalDecisionID: decisionID,
			}
			recordEvent(ctx, in, domain.AgentPipeline, "Pipeline.Complete", domain.ActFailed,
				fmt.Sprintf("rejected: %s", sig.Decision),
				map[string]interface{}{"duration_ms": out.DurationMS, "decision": string(sig.Decision)}, 1)
			return out, temporal.NewNonRetryableApplicationError(
				fmt.Sprintf("approval %s", sig.Decision), "ApprovalRejectedError", nil,
			)
		}
	}

	out := PipelineOutput{
		DurationMS:         workflow.Now(ctx).Sub(start).Milliseconds(),
		Results:            results,
		ApprovalDecisionID: decisionID,
	}
	// Terminal pipeline event — UI uses this to close EventSource.
	recordEvent(ctx, in, domain.AgentPipeline, "Pipeline.Complete", domain.ActSucceeded,
		fmt.Sprintf("duration=%s", time.Duration(out.DurationMS)*time.Millisecond),
		map[string]interface{}{"duration_ms": out.DurationMS}, 1)
	return out, nil
}

// clonePrior shallow-copies the prior outputs map so each ExecuteActivity
// gets a stable snapshot. Temporal serialises the input to history; a
// shallow copy is sufficient because the inner values are themselves
// JSON-decoded maps (no shared pointers further down).
func clonePrior(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// awaitApprovalDecision is the workflow-side implementation of the
// signal/timer race spec'd in §9.2:
//
//   - LOW    → return ApprovalAutoApproved immediately (no signal/timer wait).
//   - MEDIUM → race the ApprovalSignalName channel against a 2-minute timer.
//   - HIGH   → block on the signal channel until it lands (or the workflow
//     timeout — 10m — fires upstream).
//
// All primitives used here (GetSignalChannel, NewTimer, NewSelector) are
// replay-safe. The selector closes over local vars by reference; we never
// mutate workflow state from outside the selector callbacks.
func awaitApprovalDecision(ctx workflow.Context, severity domain.Severity) (domain.ApprovalSignal, error) {
	return awaitApprovalDecisionWithTimeout(ctx, severity, ApprovalMediumTimeout)
}

// awaitApprovalDecisionWithTimeout accepts a per-call medium countdown so
// the workflow can honour a project policy's MediumCountdownSeconds. Kept
// distinct from awaitApprovalDecision so existing call sites + tests that
// rely on the default 2-min timer don't have to change.
func awaitApprovalDecisionWithTimeout(ctx workflow.Context, severity domain.Severity, mediumTimeout time.Duration) (domain.ApprovalSignal, error) {
	if severity == domain.SeverityLow {
		return domain.ApprovalSignal{
			Decision:  domain.ApprovalAutoApproved,
			DecidedBy: "",
			Notes:     "auto-approved (low severity)",
		}, nil
	}

	sigCh := workflow.GetSignalChannel(ctx, ApprovalSignalName)
	var sig domain.ApprovalSignal
	var ok bool

	switch severity {
	case domain.SeverityMedium:
		if mediumTimeout <= 0 {
			mediumTimeout = ApprovalMediumTimeout
		}
		timerCtx, cancelTimer := workflow.WithCancel(ctx)
		timerFut := workflow.NewTimer(timerCtx, mediumTimeout)
		sel := workflow.NewSelector(ctx)
		sel.AddReceive(sigCh, func(c workflow.ReceiveChannel, _ bool) {
			c.Receive(ctx, &sig)
			cancelTimer() // stop the timer — selector returns once any branch fires
			ok = true
		})
		sel.AddFuture(timerFut, func(f workflow.Future) {
			// Drain any timer error (timer cancellation reports an error
			// when cancelled before firing — ignored here).
			_ = f.Get(ctx, nil)
			if !ok {
				sig = domain.ApprovalSignal{
					Decision:  domain.ApprovalTimeoutRejected,
					DecidedBy: "",
					Notes:     "timer fired before signal",
				}
				ok = true
			}
		})
		sel.Select(ctx)
		return sig, nil

	case domain.SeverityHigh:
		sigCh.Receive(ctx, &sig)
		return sig, nil
	}

	// Unknown severity — fail closed so the gate doesn't silently approve.
	return domain.ApprovalSignal{}, fmt.Errorf("approval: unknown severity %q", severity)
}

// recordEvent is the workflow-side helper that calls RecordActivityEvent as
// a side-effect activity. Using an activity (rather than a SideEffect or
// direct DB call) keeps the workflow deterministic + replay-safe.
func recordEvent(ctx workflow.Context, in PipelineInput, role domain.AgentRole, name string, status domain.ActivityStatus, message string, payload map[string]interface{}, attempt int) {
	recCtx := workflow.WithActivityOptions(ctx, recorderOpts)
	_ = workflow.ExecuteActivity(recCtx, (*Activities).RecordActivityEvent, RecordEventInput{
		OrgID:         in.OrgID,
		WorkflowRunID: in.RunID,
		AgentRole:     role,
		ActivityName:  name,
		Status:        status,
		Attempt:       attempt,
		Message:       message,
		Payload:       payload,
	}).Get(ctx, nil)
}
