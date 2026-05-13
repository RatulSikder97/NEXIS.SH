package domain

import (
	"context"
	"time"
)

// AgentRole names the 9-step agent fleet exactly once. The repo writes
// activity_events.agent_role from this string; the timeline UI keys icons by
// the same value. The synthetic AgentPipeline role is used for workflow-level
// frames (Pipeline.Complete) that don't belong to any single agent.
type AgentRole string

const (
	AgentSentinel     AgentRole = "sentinel"
	AgentPathfinder   AgentRole = "pathfinder"
	AgentSynthesiser  AgentRole = "synthesiser"
	AgentArchitect    AgentRole = "architect"
	AgentBackend      AgentRole = "backend"
	AgentQA           AgentRole = "qa"
	AgentDevOps       AgentRole = "devops"
	AgentDataEngineer AgentRole = "data_engineer"
	AgentApprovalGate AgentRole = "approval_gate"
	AgentPipeline     AgentRole = "pipeline" // synthetic — workflow-level events
)

// AllAgents returns the 9 in-order roles used to render an empty timeline
// before any events arrive. Order matches the DAG fan-out:
//
//	1 → 2 → 3 → 4 → 5 → 6 → (7 || 8) → 9
var AllAgents = []AgentRole{
	AgentSentinel, AgentPathfinder, AgentSynthesiser,
	AgentArchitect, AgentBackend, AgentQA,
	AgentDevOps, AgentDataEngineer, AgentApprovalGate,
}

// WorkflowRunStatus mirrors the CHECK constraint on workflow_runs.status.
type WorkflowRunStatus string

const (
	WRQueued    WorkflowRunStatus = "queued"
	WRRunning   WorkflowRunStatus = "running"
	WRSucceeded WorkflowRunStatus = "succeeded"
	WRFailed    WorkflowRunStatus = "failed"
	WRTimedOut  WorkflowRunStatus = "timed_out"
	WRCancelled WorkflowRunStatus = "cancelled"
)

// ActivityStatus mirrors the CHECK constraint on activity_events.status.
type ActivityStatus string

const (
	ActStarted   ActivityStatus = "started"
	ActSucceeded ActivityStatus = "succeeded"
	ActFailed    ActivityStatus = "failed"
	ActRetrying  ActivityStatus = "retrying"
	ActTimedOut  ActivityStatus = "timed_out"
)

// WorkflowRun is one tenant-scoped execution of a workflow type. ID equals
// the Temporal WorkflowID (we generate it client-side); TemporalRunID is
// the Temporal-assigned per-attempt id.
//
// ProjectID (Phase 7 — projects/self-healing) is the optional project this
// run targets. Persisted on workflow_runs.project_id by the workflow
// adapter via InsertRun; downstream dashboards filter by it.
type WorkflowRun struct {
	ID            string
	OrgID         string
	WorkspaceID   string
	ProjectID     string
	WorkflowType  string
	TemporalRunID string
	TemporalWfID  string
	Status        WorkflowRunStatus
	CurrentStep   string
	Input         []byte
	Output        []byte
	Error         string
	StartedAt     time.Time
	CompletedAt   *time.Time
	DurationMs    *int64
	CreatedBy     string
}

// ActivityEvent is one row in the activity_events table and one frame in the
// SSE stream. Seq is monotonic per run; SSE consumers dedup on (RunID, Seq).
type ActivityEvent struct {
	ID            string
	OrgID         string
	WorkflowRunID string
	Seq           int
	AgentRole     AgentRole
	ActivityName  string
	Status        ActivityStatus
	Attempt       int
	Message       string
	Payload       []byte
	TS            time.Time
}

// ActivityResult is the in-process value the workflow function reads back
// from each activity. It does NOT hit Postgres directly; the workflow
// brackets each call with a RecordActivityEvent activity that persists +
// publishes to the SSE broker.
type ActivityResult struct {
	AgentRole AgentRole              `json:"agent_role"`
	Status    ActivityStatus         `json:"status"`
	Message   string                 `json:"message"`
	Payload   map[string]interface{} `json:"payload,omitempty"`
}

// WorkflowService is the port HTTP handlers depend on. The adapter in
// internal/adapter/workflow implements it on top of a Temporal client + the
// workflow repo + an SSE broker.
type WorkflowService interface {
	Start(ctx context.Context, p Principal, workspaceID, workflowType string, input []byte) (WorkflowRun, error)
	Get(ctx context.Context, p Principal, runID string) (WorkflowRun, []ActivityEvent, error)
	List(ctx context.Context, p Principal, workspaceID string, limit int, before time.Time) ([]WorkflowRun, error)
	Subscribe(ctx context.Context, runID string) <-chan ActivityEvent
}

// SynthesiserPlan is the Phase 6 plan the Synthesiser L2 agent produces.
// SelectedAgents drives the L1 fan-out the workflow uses; SkippedAgents is
// surfaced in the timeline so an analyst sees "QA was skipped because the
// scenario was schema_drift". Rationale carries the human-readable why.
type SynthesiserPlan struct {
	Scenario       string      `json:"scenario"`
	Confidence     float64     `json:"confidence"`
	SelectedAgents []AgentName `json:"selected_agents"`
	SkippedAgents  []AgentName `json:"skipped_agents"`
	Rationale      string      `json:"rationale"`
}
