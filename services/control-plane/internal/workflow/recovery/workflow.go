package recovery

import (
	"fmt"
	"strings"
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
	// ScheduleToClose is the whole-activity budget — queue time plus every
	// attempt — so inheriting the 2-minute default from stdActivityOpts
	// capped these activities below their own 5-minute StartToClose, and the
	// extra window could never actually be used. Any model slower than ~2
	// minutes died with "Not enough time to schedule next retry", which is
	// easy to misread as a bad response rather than a timeout. Budget two
	// full attempts plus scheduling slack.
	o.ScheduleToCloseTimeout = 12 * time.Minute
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

// gitopsActivityOpts governs the deploy-side activities (GitOpsDeploy,
// RollbackDeploy). Opening a PR walks the GitHub API through the gitops
// sidecar — slower than a DB write, far faster than an LLM call — so it
// gets its own 2-minute start-to-close budget on the standard retry ladder.
var gitopsActivityOpts = func() workflow.ActivityOptions {
	o := stdActivityOpts
	o.StartToCloseTimeout = 2 * time.Minute
	o.ScheduleToCloseTimeout = 5 * time.Minute
	return o
}()

// deployEngineActivityOpts governs DeployEngineRedeploy. The engine clones,
// builds a Docker image, and waits for the container health check with a
// 120s in-request budget, so the start-to-close must clear that plus network
// slack. Retries capped at 2 — a retry re-runs a full image build.
var deployEngineActivityOpts = func() workflow.ActivityOptions {
	o := stdActivityOpts
	o.StartToCloseTimeout = 3 * time.Minute
	o.ScheduleToCloseTimeout = 7 * time.Minute
	if o.RetryPolicy != nil {
		rp := *o.RetryPolicy
		rp.MaximumAttempts = 2
		o.RetryPolicy = &rp
	}
	return o
}()

// healthActivityOpts covers PostDeployHealthCheck, which deliberately blocks
// for the whole post-deploy observation window (default 30s, capped well
// below the 90s start-to-close so the probe never times out mid-poll).
// Retries capped at 2 — a retry re-runs the full window, so the ladder
// mustn't multiply the wait the way stdActivityOpts' 3 attempts would.
var healthActivityOpts = func() workflow.ActivityOptions {
	o := stdActivityOpts
	o.StartToCloseTimeout = 90 * time.Second
	o.ScheduleToCloseTimeout = 4 * time.Minute
	if o.RetryPolicy != nil {
		rp := *o.RetryPolicy
		rp.MaximumAttempts = 2
		o.RetryPolicy = &rp
	}
	return o
}()

// RecoveryPipeline is the 9-step DAG. The shape MUST match the spec table
// (3 sequential L2 → 3 sequential L1 → DevOps||DataEngineer parallel → ApprovalGate
// join) — Phases 5+6 only swap the activity bodies.
//
// Phase 8 closes the loop on top of that shape: the Synthesiser plan's
// selected_agents gates which L1 steps actually execute (skips get their own
// timeline frames), an approved/auto-approved decision triggers the GitOps
// deploy tail (open PR → post-deploy SLO probe → policy-gated ArgoCD
// rollback), and a Backend patch that strays outside the Architect's
// affected_files escalates severity to HIGH via the contract-violation flag.
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

	// incidentSource is the triggering incident's origin ("sentry",
	// "deploy_engine", ...), captured from the Sentinel.Detect result payload.
	// The deploy-engine redeploy tail below only fires when the incident that
	// started this pipeline came from a failed preview deploy. Reading it off
	// the activity result (history-backed) keeps the branch replay-safe.
	incidentSource := ""

	// ---- L2 detect → diagnose → plan (sequential) ----
	// Pathfinder + Synthesiser carry their agent names so foldPrior lifts
	// their structured plans into the prior map — the Synthesiser's
	// selected_agents is what gates the L1 steps below.
	for _, step := range []struct {
		role  domain.AgentRole
		name  string
		fn    any
		opts  workflow.ActivityOptions
		agent domain.AgentName
	}{
		{domain.AgentSentinel, "Sentinel.Detect", (*Activities).SentinelDetect, stdActivityOpts, ""},
		{domain.AgentPathfinder, "Pathfinder.Diagnose", (*Activities).PathfinderDiagnose, stdActivityOpts, domain.AgentNamePathfinder},
		{domain.AgentSynthesiser, "Synthesiser.Plan", (*Activities).SynthesiserPlan, stdActivityOpts, domain.AgentNameSynthesiser},
		// ---- L1 architect → backend → qa (sequential) ----
		{domain.AgentArchitect, "Architect.Solution", (*Activities).ArchitectSolution, llmActivityOpts, domain.AgentNameArchitect},
		{domain.AgentBackend, "Backend.Codegen", (*Activities).BackendCodegen, llmActivityOpts, domain.AgentNameBackend},
		{domain.AgentQA, "QA.TestGen", (*Activities).QATestGen, llmActivityOpts, domain.AgentNameQA},
	} {
		// Phase 8 — Synthesiser delegation enforcement. Once the plan is in
		// the prior map, L1 steps outside selected_agents don't run; the
		// skip is still recorded so the timeline explains why the agent
		// never fired. agentRoleOf returns "" for the L2 names, so only the
		// L1 subset is ever gated. A nil set (stub path, degraded plan)
		// preserves the legacy run-everything DAG.
		if agentRoleOf(step.agent) != "" {
			if sel := selectedAgentSet(prior); sel != nil {
				if _, ok := sel[step.agent]; !ok {
					recordSkippedAgent(ctx, in, step.role, step.name, prior)
					continue
				}
			}
		}
		r, err := runActivity(step.role, step.name, step.fn, step.opts, step.agent)
		if err != nil {
			return PipelineOutput{}, err
		}
		results = append(results, r)

		if step.name == "Sentinel.Detect" {
			if src, ok := r.Payload["source"].(string); ok {
				incidentSource = src
			}
		}

		// Phase 8 — Architect contract enforcement. Right after Backend's
		// patch lands, diff its actual changed files against the Architect's
		// declared affected_files. A violation is an escalation signal, not
		// a pipeline failure: the verdict rides the prior map into
		// ApprovalGateRoute, which forces HIGH severity so a human must
		// approve the out-of-contract patch.
		if step.agent == domain.AgentNameBackend {
			if bad := contractViolationFiles(prior); len(bad) > 0 {
				prior["contract_violation"] = map[string]any{
					"violation":          true,
					"unauthorized_files": bad,
				}
				recordEvent(ctx, in, domain.AgentArchitect, "Architect.Violation", domain.ActFailed,
					fmt.Sprintf("backend patch touched %d file(s) outside the architect's affected_files: %s",
						len(bad), strings.Join(bad, ", ")),
					map[string]interface{}{
						"unauthorized_files": bad,
						"escalation":         "severity forced to high",
					}, 1)
			}
		}
	}

	// ---- DevOps || DataEngineer (parallel) ----
	// The Synthesiser plan gates the parallel pair exactly like the
	// sequential L1 steps: unselected agents get a skipped frame instead of
	// an execution. A nil future below means "was skipped".
	sel := selectedAgentSet(prior)
	runDevOps := true
	runData := true
	if sel != nil {
		_, runDevOps = sel[domain.AgentNameDevOps]
		_, runData = sel[domain.AgentNameDataEngineer]
	}

	parIn := in
	parIn.PriorOutputs = clonePrior(prior)
	var devopsFut, dataFut workflow.Future
	if runDevOps {
		recordEvent(ctx, in, domain.AgentDevOps, "DevOps.Pipeline", domain.ActStarted, "", nil, 1)
		devopsFut = workflow.ExecuteActivity(workflow.WithActivityOptions(ctx, llmActivityOpts),
			(*Activities).DevOpsPipeline, parIn)
	} else {
		recordSkippedAgent(ctx, in, domain.AgentDevOps, "DevOps.Pipeline", prior)
	}
	if runData {
		recordEvent(ctx, in, domain.AgentDataEngineer, "DataEngineer.Migrations", domain.ActStarted, "", nil, 1)
		dataFut = workflow.ExecuteActivity(workflow.WithActivityOptions(ctx, llmActivityOpts),
			(*Activities).DataEngineerMigrate, parIn)
	} else {
		recordSkippedAgent(ctx, in, domain.AgentDataEngineer, "DataEngineer.Migrations", prior)
	}

	if devopsFut != nil {
		var rDev domain.ActivityResult
		if err := devopsFut.Get(ctx, &rDev); err != nil {
			recordEvent(ctx, in, domain.AgentDevOps, "DevOps.Pipeline", domain.ActFailed, err.Error(), nil, 1)
			return PipelineOutput{}, err
		}
		recordEvent(ctx, in, domain.AgentDevOps, "DevOps.Pipeline", domain.ActSucceeded, rDev.Message, rDev.Payload, 1)
		foldPrior(domain.AgentNameDevOps, rDev.Payload)
		results = append(results, rDev)
	}

	if dataFut != nil {
		var rData domain.ActivityResult
		if err := dataFut.Get(ctx, &rData); err != nil {
			recordEvent(ctx, in, domain.AgentDataEngineer, "DataEngineer.Migrations", domain.ActFailed, err.Error(), nil, 1)
			return PipelineOutput{}, err
		}
		recordEvent(ctx, in, domain.AgentDataEngineer, "DataEngineer.Migrations", domain.ActSucceeded, rData.Message, rData.Payload, 1)
		foldPrior(domain.AgentNameDataEngineer, rData.Payload)
		results = append(results, rData)
	}

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

	// Phase 8 — GitOps deploy outcome, surfaced on PipelineOutput + the
	// Pipeline.Complete payload so the incident timeline can show the PR
	// link (or why there isn't one).
	prURL := ""
	gitopsErr := ""

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
		// Scenario + patch ride along so the activity can fold the terminal
		// decision into feedback_examples (RLHF pipeline).
		finScenario := ""
		if syn, ok := prior["synthesiser"].(map[string]any); ok {
			finScenario, _ = syn["scenario"].(string)
		}
		finIn := ApprovalFinalizeInput{
			OrgID:         in.OrgID,
			WorkflowRunID: in.RunID,
			Signal:        sig,
			IncidentID:    in.IncidentID,
			Scenario:      finScenario,
			PatchDiff:     backendPatchFromPrior(prior),
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

		// ---- GitOps deploy (approve / auto-approve / modified only) ----
		// Phase 8 — close the loop. Reaching here means the gate landed on
		// ApprovalApproved, ApprovalAutoApproved, or ApprovalModified
		// (reject/timeout returned above, kill-switch even earlier). Only
		// runs when the Backend agent actually produced a patch — the stub
		// demo loop keeps its exact event trail otherwise. Deploy failure is
		// deliberately soft: the patch was already approved + validated, so
		// we record the failure and let Pipeline.Complete still succeed.
		//
		// RLHF — a 'modified' decision means the engineer edited the patch
		// before approving, so the edited diff REPLACES the Backend agent's
		// original in the prior map: the PR that opens ships exactly what
		// the human signed off on. We rebuild the backend entry (rather
		// than mutating in place) so earlier history frames keep the
		// original diff for the feedback example.
		if sig.Decision == domain.ApprovalModified && sig.ModifiedDiff != "" {
			merged := map[string]any{}
			if prev, ok := prior["backend"].(map[string]any); ok {
				for k, v := range prev {
					merged[k] = v
				}
			}
			merged["patch_diff"] = sig.ModifiedDiff
			prior["backend"] = merged
		}
		if patch := backendPatchFromPrior(prior); patch != "" &&
			(sig.Decision == domain.ApprovalApproved || sig.Decision == domain.ApprovalAutoApproved ||
				sig.Decision == domain.ApprovalModified) {
			deployIn := in
			deployIn.PriorOutputs = clonePrior(prior)
			deployCtx := workflow.WithActivityOptions(ctx, gitopsActivityOpts)
			recordEvent(ctx, in, domain.AgentPipeline, "GitOps.Deploy", domain.ActStarted, "", nil, 1)
			var rDeploy domain.ActivityResult
			if err := workflow.ExecuteActivity(deployCtx, (*Activities).GitOpsDeploy, deployIn).Get(deployCtx, &rDeploy); err != nil {
				recordEvent(ctx, in, domain.AgentPipeline, "GitOps.Deploy", domain.ActFailed, err.Error(), nil, 1)
				gitopsErr = err.Error()
			} else {
				recordEvent(ctx, in, domain.AgentPipeline, "GitOps.Deploy", domain.ActSucceeded, rDeploy.Message, rDeploy.Payload, 1)
				results = append(results, rDeploy)
				if u, ok := rDeploy.Payload["pr_url"].(string); ok {
					prURL = u
				}
				// ---- Post-deploy SLO probe + policy rollback ----
				// Only after a real PR (not the client-unwired skip path):
				// watch the Sentinel incident feed for a fresh fatal, and
				// roll the org's ArgoCD app back when the project policy
				// says so. Probe + rollback both fail soft — a broken probe
				// must never sink an already-approved recovery.
				if skipped, _ := rDeploy.Payload["skipped"].(bool); !skipped {
					service := ""
					if in.Incident != nil {
						service = in.Incident.Service
					}
					hcIn := HealthCheckInput{
						OrgID:      in.OrgID,
						Service:    service,
						DeployedAt: workflow.Now(ctx),
					}
					hcCtx := workflow.WithActivityOptions(ctx, healthActivityOpts)
					recordEvent(ctx, in, domain.AgentPipeline, "GitOps.HealthCheck", domain.ActStarted, "", nil, 1)
					var rHealth domain.ActivityResult
					if err := workflow.ExecuteActivity(hcCtx, (*Activities).PostDeployHealthCheck, hcIn).Get(hcCtx, &rHealth); err != nil {
						recordEvent(ctx, in, domain.AgentPipeline, "GitOps.HealthCheck", domain.ActFailed, err.Error(), nil, 1)
					} else {
						recordEvent(ctx, in, domain.AgentPipeline, "GitOps.HealthCheck", rHealth.Status, rHealth.Message, rHealth.Payload, 1)
						results = append(results, rHealth)
						healthy, _ := rHealth.Payload["healthy"].(bool)
						if !healthy {
							if in.Project != nil && in.Project.Policy.RollbackOnSLOBreach {
								rbIn := in
								rbIn.PriorOutputs = clonePrior(prior)
								rbCtx := workflow.WithActivityOptions(ctx, gitopsActivityOpts)
								recordEvent(ctx, in, domain.AgentPipeline, "GitOps.Rollback", domain.ActStarted, "", nil, 1)
								var rRollback domain.ActivityResult
								if err := workflow.ExecuteActivity(rbCtx, (*Activities).RollbackDeploy, rbIn).Get(rbCtx, &rRollback); err != nil {
									recordEvent(ctx, in, domain.AgentPipeline, "GitOps.Rollback", domain.ActFailed, err.Error(), nil, 1)
								} else {
									recordEvent(ctx, in, domain.AgentPipeline, "GitOps.Rollback", rRollback.Status, rRollback.Message, rRollback.Payload, 1)
									results = append(results, rRollback)
								}
							} else {
								// The rollback DECISION is a timeline event
								// either way — an analyst must see that the
								// breach was noticed and why nothing moved.
								reason := "no project bound to this run"
								if in.Project != nil {
									reason = "rollback_on_slo_breach disabled by project policy"
								}
								recordEvent(ctx, in, domain.AgentPipeline, "GitOps.Rollback", domain.ActSkipped, reason,
									map[string]interface{}{"skipped": true, "reason": reason}, 1)
							}
						}
					}
					// ---- Deploy-engine redeploy (retry after fix) ----
					// Only when the incident that started this pipeline was a
					// failed preview deploy: re-invoke POST /v1/deploy for the
					// bound project (fresh deployment id, same repo/branch —
					// now presumably fixed on the branch HEAD post-merge).
					// Fail-soft like every other post-deploy step — a redeploy
					// hiccup must never sink an already-approved recovery.
					if incidentSource == "deploy_engine" {
						rdIn := in
						rdIn.PriorOutputs = clonePrior(prior)
						rdCtx := workflow.WithActivityOptions(ctx, deployEngineActivityOpts)
						recordEvent(ctx, in, domain.AgentPipeline, "DeployEngine.Redeploy", domain.ActStarted, "", nil, 1)
						var rRedeploy domain.ActivityResult
						if err := workflow.ExecuteActivity(rdCtx, (*Activities).DeployEngineRedeploy, rdIn).Get(rdCtx, &rRedeploy); err != nil {
							recordEvent(ctx, in, domain.AgentPipeline, "DeployEngine.Redeploy", domain.ActFailed, err.Error(), nil, 1)
						} else {
							recordEvent(ctx, in, domain.AgentPipeline, "DeployEngine.Redeploy", rRedeploy.Status, rRedeploy.Message, rRedeploy.Payload, 1)
							results = append(results, rRedeploy)
						}
					}
				}
			}
		}
	}

	out := PipelineOutput{
		DurationMS:         workflow.Now(ctx).Sub(start).Milliseconds(),
		Results:            results,
		ApprovalDecisionID: decisionID,
		PRURL:              prURL,
		GitOpsError:        gitopsErr,
	}
	// Terminal pipeline event — UI uses this to close EventSource.
	completePayload := map[string]interface{}{"duration_ms": out.DurationMS}
	if prURL != "" {
		completePayload["pr_url"] = prURL
	}
	if gitopsErr != "" {
		completePayload["gitops_error"] = gitopsErr
	}
	recordEvent(ctx, in, domain.AgentPipeline, "Pipeline.Complete", domain.ActSucceeded,
		fmt.Sprintf("duration=%s", time.Duration(out.DurationMS)*time.Millisecond),
		completePayload, 1)
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

// selectedAgentSet extracts the Synthesiser plan's selected_agents from the
// prior map. Returns nil when no plan landed (stub path, degraded run, or a
// plan without the field) — callers treat nil as "run every L1 agent",
// preserving the legacy DAG. Pure function of the prior map, so calling it
// inside the workflow body stays replay-deterministic.
func selectedAgentSet(prior map[string]any) map[domain.AgentName]struct{} {
	syn, ok := prior["synthesiser"].(map[string]any)
	if !ok {
		return nil
	}
	names := stringsFromAny(syn["selected_agents"])
	if len(names) == 0 {
		return nil
	}
	out := make(map[domain.AgentName]struct{}, len(names))
	for _, n := range names {
		out[domain.AgentName(n)] = struct{}{}
	}
	return out
}

// recordSkippedAgent emits the ActSkipped timeline frame for an L1 agent the
// Synthesiser plan left out. The frame carries the scenario + selected list
// so the UI can explain WHY the agent never ran instead of showing a
// permanently-pending row.
func recordSkippedAgent(ctx workflow.Context, in PipelineInput, role domain.AgentRole, name string, prior map[string]any) {
	scenario := ""
	var selected []string
	if syn, ok := prior["synthesiser"].(map[string]any); ok {
		scenario, _ = syn["scenario"].(string)
		selected = stringsFromAny(syn["selected_agents"])
	}
	recordEvent(ctx, in, role, name, domain.ActSkipped,
		fmt.Sprintf("skipped by synthesiser plan (scenario=%s)", scenario),
		map[string]interface{}{
			"skipped_by":      "synthesiser",
			"scenario":        scenario,
			"selected_agents": selected,
		}, 1)
}

// contractViolationFiles compares the Backend agent's actual changed files
// (structured.files_changed, produced by backend.ExtractDiff) against the
// Architect's declared affected_files. Returns the files Backend touched
// without a matching declaration — the Architect contract violation set.
// Either side missing (stub path, degraded agent) disables the check and
// returns nil; an Architect that DID declare affected_files is held to it,
// even when the declared list is empty.
func contractViolationFiles(prior map[string]any) []string {
	arch, ok := prior["architect"].(map[string]any)
	if !ok {
		return nil
	}
	affRaw, ok := arch["affected_files"]
	if !ok {
		return nil
	}
	be, ok := prior["backend"].(map[string]any)
	if !ok {
		return nil
	}
	changed := stringsFromAny(be["files_changed"])
	if len(changed) == 0 {
		return nil
	}
	allowed := make(map[string]struct{})
	for _, f := range stringsFromAny(affRaw) {
		allowed[normalizeRepoPath(f)] = struct{}{}
	}
	var out []string
	for _, f := range changed {
		if _, ok := allowed[normalizeRepoPath(f)]; !ok {
			out = append(out, f)
		}
	}
	return out
}

// normalizeRepoPath strips the "./" prefix + surrounding whitespace so the
// Architect's declared paths and the diff's "+++ b/<path>" headers compare
// on the same form.
func normalizeRepoPath(p string) string {
	return strings.TrimPrefix(strings.TrimSpace(p), "./")
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
