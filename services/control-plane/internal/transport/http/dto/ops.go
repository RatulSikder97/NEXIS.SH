package dto

// Wire shapes for the operational-surface endpoints added in the
// "ops surfaces" Stage. Snake_case JSON across the board.
//
// These types are intentionally close to the SQL row shapes from the new
// webhook_deliveries table + the existing token_ledger / activity_events
// rollups. They keep the handler layer slim — one row → one response struct
// → JSON-encode.

// OrgActivityResp is the response body for GET /v1/orgs/{org_id}/activity.
// Wraps the page in {events, total} so the frontend can render "N of M".
type OrgActivityResp struct {
	Events []ActivityEventResp `json:"events"`
	Total  int                 `json:"total"`
}

// SystemHealthCheck is the per-dependency shape for /v1/system-health.
// Status is one of "healthy" / "degraded" / "down" / "disabled".
type SystemHealthCheck struct {
	Status    string `json:"status"`
	LatencyMs int64  `json:"latency_ms"`
	CheckedAt string `json:"checked_at"`
	LastError string `json:"last_error,omitempty"`
}

// SystemHealthResp is the full response for GET /v1/system-health. Each
// field's omitempty drops checks the deployment hasn't wired (e.g. neo4j
// disabled in the dev compose).
type SystemHealthResp struct {
	ControlPlane SystemHealthCheck `json:"control_plane"`
	Postgres     SystemHealthCheck `json:"postgres"`
	Redis        SystemHealthCheck `json:"redis"`
	Neo4j        SystemHealthCheck `json:"neo4j"`
	MinIO        SystemHealthCheck `json:"minio"`
	Temporal     SystemHealthCheck `json:"temporal"`
}

// WebhookDeliveryResp is one row of GET /v1/integrations/webhooks. Headers
// and payload are surfaced verbatim (post-truncation in the repo) so an
// operator can debug a delivery without leaving the dashboard.
type WebhookDeliveryResp struct {
	ID          string            `json:"id"`
	Provider    string            `json:"provider"`
	EventType   string            `json:"event_type"`
	Status      string            `json:"status"`
	LatencyMs   int               `json:"latency_ms"`
	PayloadSize int               `json:"payload_size"`
	SourceIP    string            `json:"source_ip,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	Payload     map[string]any    `json:"payload,omitempty"`
	Error       string            `json:"error,omitempty"`
	TS          string            `json:"ts"`
}

// WebhookDeliveriesResp wraps the page.
type WebhookDeliveriesResp struct {
	Deliveries []WebhookDeliveryResp `json:"deliveries"`
	Total      int                   `json:"total"`
}

// IntegrationProbeResp is the inline response of POST
// /v1/integrations/{provider}/probe. Mirrors the IntegrationResp shape so the
// dashboard can swap the row in place without a separate refresh.
type IntegrationProbeResp struct {
	Provider       string         `json:"provider"`
	Status         string         `json:"status"`
	InstallationID string         `json:"installation_id,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	LastError      string         `json:"last_error,omitempty"`
	LatencyMs      int64          `json:"latency_ms"`
	ProbedAt       string         `json:"probed_at"`
}

// ValidatorRunResp is one row of GET /v1/validator/runs. PatchSHA is a short
// hash (8-12 chars) when available; otherwise blank.
type ValidatorRunResp struct {
	ID         string `json:"id"`
	PatchSHA   string `json:"patch_sha,omitempty"`
	Status     string `json:"status"`
	DurationMs int64  `json:"duration_ms"`
	StdoutHead string `json:"stdout_head,omitempty"`
	StderrHead string `json:"stderr_head,omitempty"`
	TS         string `json:"ts"`
}

// ValidatorRunsResp wraps the page. Note is an optional human-readable
// banner that the handler surfaces when validator data is not yet wired —
// the frontend can render it in place of the empty-state message.
type ValidatorRunsResp struct {
	Runs []ValidatorRunResp `json:"runs"`
	Note string             `json:"note,omitempty"`
}

// CostByAgentRole is one slice of the org-cost rollup keyed by agent_role.
type CostByAgentRole struct {
	AgentRole string  `json:"agent_role"`
	TokensIn  int64   `json:"tokens_in"`
	TokensOut int64   `json:"tokens_out"`
	CostCents float64 `json:"cost_cents"`
}

// CostByDay is one slice of the org-cost rollup keyed by UTC day.
type CostByDay struct {
	Day       string  `json:"day"`
	CostCents float64 `json:"cost_cents"`
	Tokens    int64   `json:"tokens"`
}

// OrgCostResp is the response body for GET /v1/orgs/{org_id}/cost.
type OrgCostResp struct {
	MTDTotalCentsExact float64           `json:"mtd_total_cents_exact"`
	TokensIn           int64             `json:"tokens_in"`
	TokensOut          int64             `json:"tokens_out"`
	CacheHitRate       float64           `json:"cache_hit_rate"`
	ByAgentRole        []CostByAgentRole `json:"by_agent_role"`
	ByDay              []CostByDay       `json:"by_day"`
}

// KnowledgeWorkspaceStatus is one row of GET /v1/knowledge/status. The repo
// keys embeddings by `repo_sha` rather than workspace_id today, so the
// handler maps repo_sha → workspace via the most-recent workflow_run with
// input.repo_sha or falls back to the synthetic repo_sha as the workspace
// label.
type KnowledgeWorkspaceStatus struct {
	WorkspaceID   string `json:"workspace_id"`
	Name          string `json:"name"`
	ChunkCount    int    `json:"chunk_count"`
	LastIndexedAt string `json:"last_indexed_at,omitempty"`
}

// KnowledgeStatusResp is the full response for /v1/knowledge/status.
type KnowledgeStatusResp struct {
	TotalChunks int                        `json:"total_chunks"`
	Workspaces  []KnowledgeWorkspaceStatus `json:"workspaces"`
	LastQueryMs int64                      `json:"last_query_ms"`
}

// SystemStatusResp is the response body for GET /v1/system-status — the
// sidebar pill. Overall is one of "healthy" / "degraded" / "down".
type SystemStatusResp struct {
	Overall               string `json:"overall"`
	IntegrationsConnected int    `json:"integrations_connected"`
	IntegrationsTotal     int    `json:"integrations_total"`
	IntegrationsDegraded  int    `json:"integrations_degraded"`
	IncidentsOpen         int    `json:"incidents_open"`
	ApprovalsPending      int    `json:"approvals_pending"`
}
