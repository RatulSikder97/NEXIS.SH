// Package recovery hosts the RecoveryPipeline workflow + the 9 stub
// activities matching the agent fleet (Sentinel, Pathfinder, Synthesiser,
// Architect, Backend, QA, DevOps, DataEngineer, ApprovalGate).
//
// Only this package imports the Temporal SDK — adapter / transport layers
// depend on the domain.WorkflowService port instead. The shapes here are
// chosen so Phase 5 can swap activity bodies (real LLM + sandbox) without
// touching the workflow function or its DAG.
package recovery

import (
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// PipelineInput is the StartWorkflow input. Carries the org + workspace +
// optional incident id (Phase 4 uses 'manual' or 'demo'; Phase 6 fills in
// from a Sentinel emission). Phase 5 adds Incident (full payload) + RepoSHA
// (pgvector retrieval scope) + PriorOutputs (DAG fan-in).
//
// ProjectID (Phase 7 — projects/self-healing) is the optional project this
// run targets. When set, recovery activities load the project's selectors +
// recovery policy + SLO at workflow start and route every external action
// (PR open, ArgoCD sync, Slack notify) through those mappings. Empty —
// either because the trigger had no fingerprint match, or the call site is a
// legacy stub — falls back to the existing fixture path (acme/orders-api-
// fixture).
//
// Project is the loaded project snapshot, filled in by the workflow function
// after LoadProject succeeds. Activities downstream of LoadProject read this
// pointer (nil = no project bound; fixture path). Carrying the project on
// PipelineInput is the simplest way to thread it through Temporal — each
// activity already takes a snapshot of PipelineInput in clonePrior, so the
// project propagates without a parallel state struct.
type PipelineInput struct {
	OrgID        string                  `json:"org_id"`
	WorkspaceID  string                  `json:"workspace_id"`
	ProjectID    string                  `json:"project_id,omitempty"`
	Project      *domain.Project         `json:"project,omitempty"`
	RunID        string                  `json:"run_id"` // also the Temporal WorkflowID
	IncidentID   string                  `json:"incident_id"`
	TriggeredBy  string                  `json:"triggered_by"` // 'manual' | 'demo' | 'sentinel'
	Incident     *domain.IncidentPayload `json:"incident,omitempty"`
	RepoSHA      string                  `json:"repo_sha,omitempty"`
	PriorOutputs map[string]any          `json:"prior_outputs,omitempty"`

	// Phase 6 — populated by Synthesiser activity; consumed by the workflow
	// to drive L1 ordering. Empty when the workflow starts; filled in mid-run
	// via foldPrior. Kept as a typed *domain.SynthesiserPlan so the workflow
	// can branch on Scenario without re-decoding the prior map.
	SynthesiserPlan *domain.SynthesiserPlan `json:"synthesiser_plan,omitempty"`
}

// PipelineOutput is the workflow return value. Used by GetRun for the
// trailing summary in the timeline header.
//
// PRURL / GitOpsError (Phase 8 — close the loop) carry the GitOps deploy
// outcome: the opened PR's web link on success, or the failure reason when
// the deploy failed soft (the pipeline still succeeds — the patch was
// already approved + validated, so a PR hiccup must not sink the run).
type PipelineOutput struct {
	DurationMS         int64                   `json:"duration_ms"`
	Results            []domain.ActivityResult `json:"results"`
	ApprovalDecisionID string                  `json:"approval_decision_id,omitempty"`
	PRURL              string                  `json:"pr_url,omitempty"`
	GitOpsError        string                  `json:"gitops_error,omitempty"`
}

// RecordEventInput is the input shape for RecordActivityEvent. We keep it as
// a tiny named struct (not bare args) because Temporal serialises activity
// inputs to history — typed structs stay auditable.
type RecordEventInput struct {
	OrgID         string                 `json:"org_id"`
	WorkflowRunID string                 `json:"workflow_run_id"`
	AgentRole     domain.AgentRole       `json:"agent_role"`
	ActivityName  string                 `json:"activity_name"`
	Status        domain.ActivityStatus  `json:"status"`
	Attempt       int                    `json:"attempt"`
	Message       string                 `json:"message"`
	Payload       map[string]interface{} `json:"payload,omitempty"`
}

// ApprovalFinalizeInput is the input shape for the ApprovalGateFinalize
// activity that runs after the workflow's signal/timer race resolves. It
// carries the terminal ApprovalSignal so the activity can persist the
// decision + write the audit row.
//
// IncidentID / Scenario / PatchDiff (RLHF pipeline) carry the training-
// example context the activity folds into feedback_examples: the scenario
// the Synthesiser classified, the Backend agent's original diff, and the
// incident that triggered the run. All best-effort — empty values simply
// produce a thinner example (or skip it entirely when both scenario and
// patch are empty).
type ApprovalFinalizeInput struct {
	OrgID         string                `json:"org_id"`
	WorkflowRunID string                `json:"workflow_run_id"`
	Signal        domain.ApprovalSignal `json:"signal"`
	IncidentID    string                `json:"incident_id,omitempty"`
	Scenario      string                `json:"scenario,omitempty"`
	PatchDiff     string                `json:"patch_diff,omitempty"`
}

// HealthCheckInput is the input for the PostDeployHealthCheck activity —
// the bounded post-deploy SLO probe. DeployedAt is workflow.Now at the
// moment the GitOps deploy succeeded (replay-deterministic); the probe only
// counts incidents received strictly after it. Service narrows the match to
// the incident's service when known; empty matches any service in the org.
type HealthCheckInput struct {
	OrgID      string    `json:"org_id"`
	Service    string    `json:"service,omitempty"`
	DeployedAt time.Time `json:"deployed_at"`
}

// LoadProjectInput is the input for the LoadProject activity. The activity
// reads the projects table via the projects repo (admin pool) and returns a
// snapshot of the row for downstream activities.
type LoadProjectInput struct {
	OrgID     string `json:"org_id"`
	ProjectID string `json:"project_id"`
}

// LoadProjectOutput is the workflow-visible projection of a domain.Project.
// We keep a flat shape (rather than passing domain.Project through Temporal
// history directly) so the JSON serialisation of the activity input/output
// stays stable across domain refactors. Pointer-valued Project signals
// "no project bound for this run" — every downstream activity falls back to
// the fixture path in that case.
type LoadProjectOutput struct {
	Project *domain.Project `json:"project,omitempty"`
}

// ApprovalRouteOutput is the wire shape of ApprovalGateRoute's payload —
// duplicated as a typed key set so the workflow can branch on policy
// outcomes without re-decoding map[string]any inside the workflow function.
//
// Today we still write into ActivityResult.Payload (map[string]any) for
// backward compatibility with the existing tests; the typed keys here are
// the supported subset.
type ApprovalRouteOutput struct {
	DecisionID    string  `json:"decision_id"`
	Severity      string  `json:"severity"`
	Scenario      string  `json:"scenario"`
	RiskScore     float64 `json:"risk_score"`
	AutoApproved  bool    `json:"auto_approved,omitempty"`
	KillSwitch    bool    `json:"kill_switch,omitempty"`
	CountdownSecs int     `json:"countdown_secs,omitempty"`
}
