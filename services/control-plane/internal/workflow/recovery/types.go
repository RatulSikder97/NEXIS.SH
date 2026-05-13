// Package recovery hosts the RecoveryPipeline workflow + the 9 stub
// activities matching the agent fleet (Sentinel, Pathfinder, Synthesiser,
// Architect, Backend, QA, DevOps, DataEngineer, ApprovalGate).
//
// Only this package imports the Temporal SDK — adapter / transport layers
// depend on the domain.WorkflowService port instead. The shapes here are
// chosen so Phase 5 can swap activity bodies (real LLM + sandbox) without
// touching the workflow function or its DAG.
package recovery

import "github.com/nexis-eco/nexis/services/control-plane/internal/domain"

// PipelineInput is the StartWorkflow input. Carries the org + workspace +
// optional incident id (Phase 4 uses 'manual' or 'demo'; Phase 6 fills in
// from a Sentinel emission). Phase 5 adds Incident (full payload) + RepoSHA
// (pgvector retrieval scope) + PriorOutputs (DAG fan-in).
type PipelineInput struct {
	OrgID        string                  `json:"org_id"`
	WorkspaceID  string                  `json:"workspace_id"`
	RunID        string                  `json:"run_id"` // also the Temporal WorkflowID
	IncidentID   string                  `json:"incident_id"`
	TriggeredBy  string                  `json:"triggered_by"` // 'manual' | 'demo' | 'sentinel'
	Incident     *domain.IncidentPayload `json:"incident,omitempty"`
	RepoSHA      string                  `json:"repo_sha,omitempty"`
	PriorOutputs map[string]any          `json:"prior_outputs,omitempty"`
}

// PipelineOutput is the workflow return value. Used by GetRun for the
// trailing summary in the timeline header.
type PipelineOutput struct {
	DurationMS int64                   `json:"duration_ms"`
	Results    []domain.ActivityResult `json:"results"`
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
