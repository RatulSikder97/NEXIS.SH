package recovery

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

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
// Phase 4 stubs don't need the 5-minute window but we set it now so Phase 5
// inherits the orchestration unchanged.
var llmActivityOpts = func() workflow.ActivityOptions {
	o := stdActivityOpts
	o.StartToCloseTimeout = 5 * time.Minute
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

	// runActivity records started → executes → records terminal status.
	// Failures cause the workflow to return early; the reaper goroutine on
	// the adapter side observes the workflow failure and updates the
	// workflow_runs row to status=failed.
	runActivity := func(role domain.AgentRole, activityName string, fn any, opts workflow.ActivityOptions) (domain.ActivityResult, error) {
		c := workflow.WithActivityOptions(ctx, opts)
		recordEvent(ctx, in, role, activityName, domain.ActStarted, "", nil, 1)
		var r domain.ActivityResult
		if err := workflow.ExecuteActivity(c, fn, in).Get(c, &r); err != nil {
			recordEvent(ctx, in, role, activityName, domain.ActFailed, err.Error(), nil, 1)
			return domain.ActivityResult{}, err
		}
		recordEvent(ctx, in, role, activityName, domain.ActSucceeded, r.Message, r.Payload, 1)
		return r, nil
	}

	// ---- L2 detect → diagnose → plan (sequential) ----
	for _, step := range []struct {
		role domain.AgentRole
		name string
		fn   any
		opts workflow.ActivityOptions
	}{
		{domain.AgentSentinel, "Sentinel.Detect", (*Activities).SentinelDetect, stdActivityOpts},
		{domain.AgentPathfinder, "Pathfinder.Diagnose", (*Activities).PathfinderDiagnose, stdActivityOpts},
		{domain.AgentSynthesiser, "Synthesiser.Plan", (*Activities).SynthesiserPlan, stdActivityOpts},
		// ---- L1 architect → backend → qa (sequential) ----
		{domain.AgentArchitect, "Architect.Solution", (*Activities).ArchitectSolution, stdActivityOpts},
		{domain.AgentBackend, "Backend.Codegen", (*Activities).BackendCodegen, llmActivityOpts},
		{domain.AgentQA, "QA.TestGen", (*Activities).QATestGen, llmActivityOpts},
	} {
		r, err := runActivity(step.role, step.name, step.fn, step.opts)
		if err != nil {
			return PipelineOutput{}, err
		}
		results = append(results, r)
	}

	// ---- DevOps || DataEngineer (parallel) ----
	recordEvent(ctx, in, domain.AgentDevOps, "DevOps.Pipeline", domain.ActStarted, "", nil, 1)
	recordEvent(ctx, in, domain.AgentDataEngineer, "DataEngineer.Migrations", domain.ActStarted, "", nil, 1)

	devopsFut := workflow.ExecuteActivity(workflow.WithActivityOptions(ctx, stdActivityOpts),
		(*Activities).DevOpsPipeline, in)
	dataFut := workflow.ExecuteActivity(workflow.WithActivityOptions(ctx, stdActivityOpts),
		(*Activities).DataEngineerMigrate, in)

	var rDev, rData domain.ActivityResult
	if err := devopsFut.Get(ctx, &rDev); err != nil {
		recordEvent(ctx, in, domain.AgentDevOps, "DevOps.Pipeline", domain.ActFailed, err.Error(), nil, 1)
		return PipelineOutput{}, err
	}
	recordEvent(ctx, in, domain.AgentDevOps, "DevOps.Pipeline", domain.ActSucceeded, rDev.Message, rDev.Payload, 1)
	results = append(results, rDev)

	if err := dataFut.Get(ctx, &rData); err != nil {
		recordEvent(ctx, in, domain.AgentDataEngineer, "DataEngineer.Migrations", domain.ActFailed, err.Error(), nil, 1)
		return PipelineOutput{}, err
	}
	recordEvent(ctx, in, domain.AgentDataEngineer, "DataEngineer.Migrations", domain.ActSucceeded, rData.Message, rData.Payload, 1)
	results = append(results, rData)

	// ---- ApprovalGate.Route (join) ----
	rApprove, err := runActivity(domain.AgentApprovalGate, "ApprovalGate.Route", (*Activities).ApprovalGateRoute, stdActivityOpts)
	if err != nil {
		return PipelineOutput{}, err
	}
	results = append(results, rApprove)

	out := PipelineOutput{
		DurationMS: workflow.Now(ctx).Sub(start).Milliseconds(),
		Results:    results,
	}
	// Terminal pipeline event — UI uses this to close EventSource.
	recordEvent(ctx, in, domain.AgentPipeline, "Pipeline.Complete", domain.ActSucceeded,
		fmt.Sprintf("duration=%s", time.Duration(out.DurationMS)*time.Millisecond),
		map[string]interface{}{"duration_ms": out.DurationMS}, 1)
	return out, nil
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
