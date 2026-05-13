// project.go — Projects are first-class self-healing targets. Each Project
// bundles integration selector mappings (which GitHub repo, which Sentry
// project, which ArgoCD app...), a JSON recovery policy, and SLO targets.
// Sentinel routes incoming RawIncidents to a Project via selector match;
// the recovery pipeline reads the project's mappings to act on the right
// repo/app/channel.
package domain

import "time"

// Environment is the deployment stage the project represents.
type Environment string

const (
	EnvironmentDev     Environment = "dev"
	EnvironmentStaging Environment = "staging"
	EnvironmentProd    Environment = "prod"
)

// IsValid reports whether s is one of the canonical environment values.
func (e Environment) IsValid() bool {
	switch e {
	case EnvironmentDev, EnvironmentStaging, EnvironmentProd:
		return true
	}
	return false
}

// ProjectSelectors holds the cross-integration mapping that lets Sentinel
// route an upstream incident (Sentry issue, Datadog alert, PagerDuty trigger,
// GitHub event) to the right project. Each field is optional — a project
// links only to the integrations the customer has connected.
type ProjectSelectors struct {
	GitHubRepo                  string // "owner/repo"
	GitHubInstallationID        int64
	GitHubDefaultBranch         string // defaults to "main"
	SentryOrganizationSlug      string
	SentryProjectSlug           string
	ArgoCDServerURL             string
	ArgoCDAppName               string
	ArgoCDProject               string // defaults to "default"
	PagerDutyServiceID          string
	PagerDutyEscalationPolicyID string
	DatadogServiceTag           string // e.g. "service:orders-api"
	DatadogEnvTag               string
	SlackChannelID              string // where approval prompts post
}

// RecoveryPolicy describes how aggressive auto-recovery should be for this
// project. Stored as JSONB so we can evolve without per-knob migrations.
type RecoveryPolicy struct {
	AutoMergeLowSeverity    bool     `json:"auto_merge_low_severity"`
	AutoMergeMediumSeverity bool     `json:"auto_merge_medium_severity"`
	MediumCountdownSeconds  int      `json:"medium_countdown_seconds"`
	KillSwitchEnabled       bool     `json:"kill_switch_enabled"`
	ApproverUserIDs         []string `json:"approver_user_ids"`
	MaxConcurrentRecoveries int      `json:"max_concurrent_recoveries"`
	RollbackOnSLOBreach     bool     `json:"rollback_on_slo_breach"`
}

// DefaultRecoveryPolicy returns the policy applied to new projects when the
// caller omits one. Conservative defaults: no auto-merge, 2-min countdown
// on medium-severity, rollback on SLO breach.
func DefaultRecoveryPolicy() RecoveryPolicy {
	return RecoveryPolicy{
		AutoMergeLowSeverity:    false,
		AutoMergeMediumSeverity: false,
		MediumCountdownSeconds:  120,
		KillSwitchEnabled:       false,
		ApproverUserIDs:         []string{},
		MaxConcurrentRecoveries: 1,
		RollbackOnSLOBreach:     true,
	}
}

// SLOTarget is an optional per-project SLO used by Sentinel for severity
// scoring. Nil fields mean "no SLO defined for this dimension".
type SLOTarget struct {
	AvailabilityTarget *float64 // e.g. 0.9995
	LatencyP95Ms       *int
	ErrorRatePct       *float64
}

// Project is the persisted aggregate.
type Project struct {
	ID          string
	OrgID       string
	WorkspaceID string
	Name        string
	Slug        string
	Description string
	Environment Environment
	OwnerUserID string

	Selectors ProjectSelectors
	Policy    RecoveryPolicy
	SLO       SLOTarget

	CreatedAt  time.Time
	UpdatedAt  time.Time
	ArchivedAt *time.Time
}

// IncidentFingerprint is the minimum data Sentinel needs from an incoming
// event to match it to a project. Filled in by each integration adapter
// when it constructs the RawIncident.
type IncidentFingerprint struct {
	Source                 string // "sentry" | "datadog" | "pagerduty" | "github"
	SentryOrganizationSlug string
	SentryProjectSlug      string
	DatadogServiceTag      string
	PagerDutyServiceID     string
	GitHubRepo             string
}
