package dto

// WorkflowRunResp is the wire shape for a workflow_runs row. Timestamps are
// RFC3339 strings; nullable columns drop out when empty so the client doesn't
// have to handle nulls. snake_case tags match the rest of the JSON surface.
type WorkflowRunResp struct {
	ID           string `json:"id"`
	OrgID        string `json:"org_id"`
	WorkspaceID  string `json:"workspace_id"`
	WorkflowType string `json:"workflow_type"`
	Status       string `json:"status"`
	CurrentStep  string `json:"current_step,omitempty"`
	StartedAt    string `json:"started_at"`
	CompletedAt  string `json:"completed_at,omitempty"`
	DurationMs   int64  `json:"duration_ms,omitempty"`
	Error        string `json:"error,omitempty"`
}

// ActivityEventResp is the wire shape for an activity_events row + the SSE
// payload. Payload is rendered as a JSON object (or omitted when nil) so the
// client can read structured fields like duration_ms / coverage without
// re-parsing strings.
type ActivityEventResp struct {
	WorkflowRunID string         `json:"workflow_run_id"`
	Seq           int            `json:"seq"`
	AgentRole     string         `json:"agent_role"`
	ActivityName  string         `json:"activity_name"`
	Status        string         `json:"status"`
	Attempt       int            `json:"attempt"`
	Message       string         `json:"message,omitempty"`
	Payload       map[string]any `json:"payload,omitempty"`
	TS            string         `json:"ts"`
}

// PipelineGetResp is the response shape for GET /v1/workspaces/{ws}/pipelines/{run}.
// `Run` is the snapshot row; `Events` are every activity_event ordered by seq.
type PipelineGetResp struct {
	Run    WorkflowRunResp     `json:"run"`
	Events []ActivityEventResp `json:"events"`
}

// CreatePipelineReq is the body shape for POST /v1/workspaces/{ws}/pipelines.
// Phase 4 only supports workflow_type="RecoveryPipeline"; the value is on the
// wire so future workflow types (Phase 5+) don't break the handler signature.
type CreatePipelineReq struct {
	WorkflowType string                 `json:"workflow_type"`
	Input        map[string]interface{} `json:"input,omitempty"`
}
