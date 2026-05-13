package domain

import (
	"context"
	"time"
)

// WorkspaceStatus mirrors the CHECK constraint on workspaces.status. Provisioning
// is the initial state; the state machine in the workspace adapter advances
// through provisioning → ready (happy path) or → error (synthetic failure).
// Suspended is set explicitly on DELETE /v1/workspaces/{id}.
type WorkspaceStatus string

const (
	WSProvisioning WorkspaceStatus = "provisioning"
	WSReady        WorkspaceStatus = "ready"
	WSError        WorkspaceStatus = "error"
	WSSuspended    WorkspaceStatus = "suspended"
)

// Workspace is the tenant-visible compute unit. Slug is unique per org and is
// derived from Name by the service; the unique-violation retry loop appends a
// numeric suffix on conflict.
type Workspace struct {
	ID               string
	OrgID            string
	Name             string
	Slug             string
	Region           string
	Status           WorkspaceStatus
	StatusMessage    string
	ProvisioningStep string
	CreatedAt        time.Time
	ReadyAt          *time.Time
	UpdatedAt        time.Time
}

// Region is the static catalog entry returned by GET /v1/workspaces/regions.
// Phase 3.5 ships a fixed slice; later phases may swap in a DB-backed source.
type Region struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Continent string `json:"continent"`
}

// Regions is the whitelist of region ids the WorkspaceService accepts on
// Create. Synthetic and AWS-shaped — Phase 3.5 doesn't allocate real AWS
// capacity.
var Regions = []Region{
	{ID: "us-east-1", Name: "US East (N. Virginia)", Continent: "NA"},
	{ID: "us-west-2", Name: "US West (Oregon)", Continent: "NA"},
	{ID: "eu-west-1", Name: "EU West (Ireland)", Continent: "EU"},
	{ID: "eu-central-1", Name: "EU Central (Frankfurt)", Continent: "EU"},
	{ID: "ap-southeast-1", Name: "Asia Pacific (Singapore)", Continent: "AP"},
	{ID: "ap-south-1", Name: "Asia Pacific (Mumbai)", Continent: "AP"},
}

// ProvisioningStep is one frame in the SSE stream consumed by the onboarding
// wizard. Progress is cumulative [0..1]; Status mirrors the wire enum
// ('in_progress' | 'ready' | 'error'); Message carries the failure reason when
// Status == "error". TS is the wall-clock time the event was published.
type ProvisioningStep struct {
	Step     string    `json:"step"`
	Label    string    `json:"label"`
	Progress float64   `json:"progress"`
	Status   string    `json:"status"`
	Message  string    `json:"message,omitempty"`
	TS       time.Time `json:"ts"`
}

// WorkspaceService is the port the HTTP handlers depend on. The adapter in
// internal/adapter/workspace implements it on top of a *repo.WorkspacesRepo
// and an SSE broker.
type WorkspaceService interface {
	Create(ctx context.Context, p Principal, name, region string) (Workspace, error)
	Get(ctx context.Context, p Principal, id string) (Workspace, error)
	List(ctx context.Context, p Principal) ([]Workspace, error)
	Suspend(ctx context.Context, p Principal, id string) error
	Events(ctx context.Context, workspaceID string) <-chan ProvisioningStep
}
