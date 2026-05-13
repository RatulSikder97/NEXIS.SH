package dto

// CreateProjectReq is the body accepted by POST /v1/workspaces/{ws_id}/projects.
// All selector fields are optional — a customer may bind only the integrations
// they have connected, and add the rest later via PATCH.
type CreateProjectReq struct {
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	Environment string           `json:"environment"`
	OwnerUserID string           `json:"owner_user_id,omitempty"`
	Selectors   ProjectSelectors `json:"selectors"`
	Policy      *RecoveryPolicy  `json:"recovery_policy,omitempty"`
	SLO         SLOTarget        `json:"slo"`
}

// UpdateProjectReq is the PATCH body. Every field is a pointer so omission ≠
// reset; sending an explicit `null` is treated identically to omission.
// Selectors, when present, replaces the full selector record — partial-field
// patching of selectors is not supported (the surface is small enough that
// resending the full set is cheap).
type UpdateProjectReq struct {
	Name        *string           `json:"name,omitempty"`
	Description *string           `json:"description,omitempty"`
	Environment *string           `json:"environment,omitempty"`
	OwnerUserID *string           `json:"owner_user_id,omitempty"`
	Selectors   *ProjectSelectors `json:"selectors,omitempty"`
	SLO         *SLOTarget        `json:"slo,omitempty"`
}

// ProjectSelectors mirrors domain.ProjectSelectors with snake_case JSON tags.
// The handler maps in both directions to keep the domain type free of JSON
// concerns.
type ProjectSelectors struct {
	GitHubRepo                  string `json:"github_repo,omitempty"`
	GitHubInstallationID        int64  `json:"github_installation_id,omitempty"`
	GitHubDefaultBranch         string `json:"github_default_branch,omitempty"`
	SentryOrganizationSlug      string `json:"sentry_organization_slug,omitempty"`
	SentryProjectSlug           string `json:"sentry_project_slug,omitempty"`
	ArgoCDServerURL             string `json:"argocd_server_url,omitempty"`
	ArgoCDAppName               string `json:"argocd_app_name,omitempty"`
	ArgoCDProject               string `json:"argocd_project,omitempty"`
	PagerDutyServiceID          string `json:"pagerduty_service_id,omitempty"`
	PagerDutyEscalationPolicyID string `json:"pagerduty_escalation_policy_id,omitempty"`
	DatadogServiceTag           string `json:"datadog_service_tag,omitempty"`
	DatadogEnvTag               string `json:"datadog_env_tag,omitempty"`
	SlackChannelID              string `json:"slack_channel_id,omitempty"`
}

// RecoveryPolicy is the JSON shape of the per-project recovery policy stored
// as JSONB. All fields are required on the wire to keep policy semantics
// explicit — the dashboard sends the full object on every change.
type RecoveryPolicy struct {
	AutoMergeLowSeverity    bool     `json:"auto_merge_low_severity"`
	AutoMergeMediumSeverity bool     `json:"auto_merge_medium_severity"`
	MediumCountdownSeconds  int      `json:"medium_countdown_seconds"`
	KillSwitchEnabled       bool     `json:"kill_switch_enabled"`
	ApproverUserIDs         []string `json:"approver_user_ids"`
	MaxConcurrentRecoveries int      `json:"max_concurrent_recoveries"`
	RollbackOnSLOBreach     bool     `json:"rollback_on_slo_breach"`
}

// SLOTarget is the per-project SLO. Pointer fields = "no target" when nil so
// the dashboard can distinguish "unset" from "set to 0".
type SLOTarget struct {
	AvailabilityTarget *float64 `json:"availability_target,omitempty"`
	LatencyP95Ms       *int     `json:"latency_p95_ms,omitempty"`
	ErrorRatePct       *float64 `json:"error_rate_pct,omitempty"`
}

// ProjectResp is the JSON-on-the-wire view of a domain.Project. Timestamps
// are RFC3339; archived_at is omitted unless the row is archived (clients
// can use the field's presence as the soft-delete sentinel).
type ProjectResp struct {
	ID             string           `json:"id"`
	OrgID          string           `json:"org_id"`
	WorkspaceID    string           `json:"workspace_id"`
	Name           string           `json:"name"`
	Slug           string           `json:"slug"`
	Description    string           `json:"description"`
	Environment    string           `json:"environment"`
	OwnerUserID    string           `json:"owner_user_id,omitempty"`
	Selectors      ProjectSelectors `json:"selectors"`
	RecoveryPolicy RecoveryPolicy   `json:"recovery_policy"`
	SLO            SLOTarget        `json:"slo"`
	CreatedAt      string           `json:"created_at"`
	UpdatedAt      string           `json:"updated_at"`
	ArchivedAt     string           `json:"archived_at,omitempty"`
}
