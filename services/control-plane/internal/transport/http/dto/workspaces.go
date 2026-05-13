package dto

// CreateWorkspaceReq is the body accepted by POST /v1/workspaces. Region must
// be one of the ids in domain.Regions; the service rejects anything else with
// a 400.
type CreateWorkspaceReq struct {
	Name   string `json:"name"`
	Region string `json:"region"`
}

// WorkspaceResp is the JSON wire shape for a workspace. Timestamps are RFC3339
// strings; ReadyAt is empty when the workspace has not yet finished
// provisioning. Slug and ProvisioningStep are surfaced so the dashboard can
// render uptime, region, and the current state-machine label.
type WorkspaceResp struct {
	ID               string `json:"id"`
	OrgID            string `json:"org_id"`
	Name             string `json:"name"`
	Slug             string `json:"slug"`
	Region           string `json:"region"`
	Status           string `json:"status"`
	StatusMessage    string `json:"status_message,omitempty"`
	ProvisioningStep string `json:"provisioning_step,omitempty"`
	CreatedAt        string `json:"created_at"`
	ReadyAt          string `json:"ready_at,omitempty"`
	UpdatedAt        string `json:"updated_at"`
}
