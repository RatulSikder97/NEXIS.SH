// Package sentinel — project routing (Task 5 of the projects/self-healing
// plan).
//
// The Router sits between dedupeTriggers and Detector.fire in the tick loop.
// For every trigger that carries a fingerprint (Sentry org+project,
// Datadog service tag, PagerDuty service id, GitHub repo) the router asks
// ProjectMatcher to resolve a project_id, then:
//
//  1. Stamps trigger.ProjectID so downstream usecases + the workflow input
//     carry the project context end-to-end.
//  2. Persists project_id onto the incidents_raw row via
//     IncidentUpdater.UpdateProjectID — best-effort: a failure logs at WARN
//     but does NOT drop the trigger (the workflow still runs against the
//     stamped trigger).
//
// Triggers without a fingerprint (rate-spike rule, manual admin triggers)
// pass through untouched with ProjectID="" — the recovery workflow falls back
// to the legacy fixture path in that case.
//
// Concurrency: the router is stateless. The Detector creates one instance at
// boot via NewRouter and reuses it across ticks; each tick calls Route
// synchronously from the tick goroutine.
package sentinel

import (
	"context"
	"log/slog"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// ProjectMatcher is the narrow port the router consumes from the projects
// repo. The real *repo.ProjectsRepo satisfies this naturally because its
// MatchByFingerprint method signature matches exactly. Defining the
// interface here lets the router compile and be tested ahead of BE-A
// landing the concrete method body.
type ProjectMatcher interface {
	MatchByFingerprint(ctx context.Context, orgID string, fp domain.IncidentFingerprint) (projectID string, ok bool, err error)
}

// IncidentUpdater is the narrow port the router uses to write project_id back
// onto the incidents_raw row after a successful match. The real
// *repo.IncidentsRepo satisfies this via UpdateProjectID.
type IncidentUpdater interface {
	UpdateProjectID(ctx context.Context, incidentRawID, projectID string) error
}

// Router resolves a project for each emitted IncidentTrigger. Construct via
// NewRouter with a matcher + incidents updater; either may be nil in tests
// that exercise pass-through behaviour only.
type Router struct {
	matcher  ProjectMatcher
	incident IncidentUpdater
	logger   *slog.Logger
}

// RouterConfig bundles the router's deps. Mirrors the Config-style boot
// pattern used elsewhere in this package.
type RouterConfig struct {
	Matcher  ProjectMatcher
	Incident IncidentUpdater
	Logger   *slog.Logger
}

// NewRouter constructs a Router. A nil matcher is allowed for the boot path
// before the projects table is wired — Route is then a no-op, leaving every
// trigger's ProjectID="".
func NewRouter(cfg RouterConfig) *Router {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Router{
		matcher:  cfg.Matcher,
		incident: cfg.Incident,
		logger:   cfg.Logger,
	}
}

// Route resolves a project_id for the trigger and stamps both the trigger
// in-memory and the incidents_raw row on disk. Returns nil on success AND on
// no-match (the trigger still fires, just project-less). The only error
// surface is a router that has been mis-wired and lacks a matcher — even
// then we return nil to keep the detector tick loop flowing.
func (r *Router) Route(ctx context.Context, trigger *domain.IncidentTrigger) error {
	if r == nil || trigger == nil {
		return nil
	}
	if r.matcher == nil {
		// No matcher wired — boot path before projects landed. Triggers
		// fall through with ProjectID="" so the workflow uses fixtures.
		return nil
	}
	fp := domain.IncidentFingerprint{
		Source:                 trigger.Source,
		SentryOrganizationSlug: trigger.SentryOrganizationSlug,
		SentryProjectSlug:      trigger.SentryProjectSlug,
		DatadogServiceTag:      trigger.DatadogServiceTag,
		PagerDutyServiceID:     trigger.PagerDutyServiceID,
		GitHubRepo:             trigger.GitHubRepo,
	}
	if !fingerprintHasSelector(fp) {
		// Rate-spike or manual trigger — nothing to match on. Pass through.
		return nil
	}
	projectID, ok, err := r.matcher.MatchByFingerprint(ctx, trigger.OrgID, fp)
	if err != nil {
		r.logger.Warn("sentinel.router.match_failed",
			"org_id", trigger.OrgID,
			"source", fp.Source,
			"err", err,
		)
		return nil
	}
	if !ok {
		r.logger.Warn("sentinel.router.no_match",
			"org_id", trigger.OrgID,
			"source", fp.Source,
			"sentry_org", fp.SentryOrganizationSlug,
			"sentry_project", fp.SentryProjectSlug,
			"datadog_tag", fp.DatadogServiceTag,
			"pagerduty_service", fp.PagerDutyServiceID,
			"github_repo", fp.GitHubRepo,
		)
		return nil
	}
	trigger.ProjectID = projectID
	if r.incident != nil && trigger.IncidentRawID != "" {
		if uerr := r.incident.UpdateProjectID(ctx, trigger.IncidentRawID, projectID); uerr != nil {
			r.logger.Warn("sentinel.router.persist_failed",
				"org_id", trigger.OrgID,
				"incident_raw_id", trigger.IncidentRawID,
				"project_id", projectID,
				"err", uerr,
			)
			// Persistence is best-effort — the in-memory stamp already lets
			// the workflow do the right thing. Don't drop the trigger.
		}
	}
	return nil
}

// fingerprintHasSelector reports whether at least one source-specific field
// is filled. Triggers without any selector (rate-spike, manual) skip the
// lookup entirely.
func fingerprintHasSelector(fp domain.IncidentFingerprint) bool {
	return fp.SentryOrganizationSlug != "" ||
		fp.SentryProjectSlug != "" ||
		fp.DatadogServiceTag != "" ||
		fp.PagerDutyServiceID != "" ||
		fp.GitHubRepo != ""
}
