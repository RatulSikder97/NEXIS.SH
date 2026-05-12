// Package dto — integration-shaped request/response bodies.
//
// IntegrationResp mirrors domain.Connection but stringifies the timestamps so
// the JSON wire form is RFC3339 rather than the Go zero-value-friendly default.
// Status is emitted as the IntegrationStatus string constants (connected,
// pending, error, disconnected).
package dto

// IntegrationResp is the JSON shape returned by /v1/integrations endpoints.
// LastError is the empty string when the integration is healthy; the omitempty
// keeps the field out of the wire body in the happy path.
type IntegrationResp struct {
	Provider       string         `json:"provider"`
	Status         string         `json:"status"`
	InstallationID string         `json:"installation_id,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	LastError      string         `json:"last_error,omitempty"`
	CreatedAt      string         `json:"created_at"`
	UpdatedAt      string         `json:"updated_at"`
}

// ConnectReq is the body of POST /v1/integrations/{provider}/connect. The
// concrete shape varies per-provider (github needs installation_id,
// sentry needs webhook_secret, etc.) so we deserialise to a free-form map and
// hand it to the adapter, which validates required fields.
type ConnectReq map[string]any
