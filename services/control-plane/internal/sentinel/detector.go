package sentinel

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// WorkspacesReader is the narrow port the detector depends on to look up the
// "default" workspace for an org. Implemented by *repo.WorkspacesRepo via
// DefaultForOrg.
type WorkspacesReader interface {
	DefaultForOrg(ctx context.Context, orgID string) (string, error)
}

// IntegrationsReader is the narrow port the detector depends on to enumerate
// the orgs it must poll each tick. Implemented by *repo.IntegrationsRepo via
// ConnectedSentryOrgs.
type IntegrationsReader interface {
	ConnectedSentryOrgs(ctx context.Context) ([]string, error)
}

// Detector is the always-on goroutine that polls incidents_raw + triggers a
// RecoveryPipeline run per rule hit.
//
// Concurrency model: a single goroutine drives the loop. State lives in two
// maps (lastSeen, lastTriggered) protected by mu. The maps are only read +
// written from tick() — the mutex is defence-in-depth for any future fan-out.
type Detector struct {
	incidents    domain.IncidentsReader
	workflows    domain.WorkflowService
	workspaces   WorkspacesReader
	integrations IntegrationsReader
	audit        domain.AuditWriter
	workflowType string
	interval     time.Duration
	logger       *slog.Logger

	mu            sync.Mutex
	lastSeen      map[string]time.Time
	lastTriggered map[string]time.Time
}

// Config bundles every Detector dependency. The runtime constructor lives in
// main.go; tests build a Detector inline with fakes.
type Config struct {
	Incidents    domain.IncidentsReader
	Workflows    domain.WorkflowService
	Workspaces   WorkspacesReader
	Integrations IntegrationsReader
	Audit        domain.AuditWriter
	// WorkflowType selects which Temporal workflow type the detector kicks off.
	// Phase 6 uses recovery.WorkflowType ("RecoveryPipeline").
	WorkflowType string
	Interval     time.Duration
	Logger       *slog.Logger
}

// New constructs a Detector. None of the deps may be nil — the goroutine
// crashes early on a misconfigured boot instead of silently no-op'ing.
func New(cfg Config) *Detector {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 10 * time.Second
	}
	if cfg.WorkflowType == "" {
		cfg.WorkflowType = "RecoveryPipeline"
	}
	return &Detector{
		incidents:     cfg.Incidents,
		workflows:     cfg.Workflows,
		workspaces:    cfg.Workspaces,
		integrations:  cfg.Integrations,
		audit:         cfg.Audit,
		workflowType:  cfg.WorkflowType,
		interval:      cfg.Interval,
		logger:        cfg.Logger,
		lastSeen:      map[string]time.Time{},
		lastTriggered: map[string]time.Time{},
	}
}

// Run blocks until ctx is cancelled. Tick errors slog at WARN and never
// propagate; the loop continues so a single misbehaving org doesn't take the
// whole detector down.
func (d *Detector) Run(ctx context.Context) {
	d.logger.Info("sentinel.detector.start", "interval_ms", d.interval.Milliseconds())
	if err := d.warm(ctx); err != nil {
		d.logger.Warn("sentinel.detector.warm_failed", "err", err)
	}
	t := time.NewTicker(d.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			d.logger.Info("sentinel.detector.stop")
			return
		case now := <-t.C:
			d.tick(ctx, now.UTC().Truncate(time.Microsecond))
		}
	}
}

// warm initialises lastSeen with MAX(received_at) per org so the first tick
// doesn't replay the whole history of fatal events for orgs that have been
// running before the detector spun up.
func (d *Detector) warm(ctx context.Context) error {
	if d.integrations == nil || d.incidents == nil {
		return errors.New("sentinel.warm: nil deps")
	}
	orgs, err := d.integrations.ConnectedSentryOrgs(ctx)
	if err != nil {
		return err
	}
	for _, org := range orgs {
		ts, err := d.incidents.MaxReceivedAt(ctx, org)
		if err != nil {
			d.logger.Warn("sentinel.warm.max_received_at", "org_id", org, "err", err)
			continue
		}
		d.mu.Lock()
		d.lastSeen[org] = ts
		d.mu.Unlock()
	}
	return nil
}

// tick performs one detection sweep across every Sentry-connected org. Errors
// per org are logged + skipped — they never abort the sweep.
func (d *Detector) tick(ctx context.Context, now time.Time) {
	orgs, err := d.integrations.ConnectedSentryOrgs(ctx)
	if err != nil {
		d.logger.Warn("sentinel.detector.tick.orgs", "err", err)
		return
	}
	for _, orgID := range orgs {
		d.mu.Lock()
		last := d.lastSeen[orgID]
		lastTrig := d.lastTriggered[orgID]
		d.mu.Unlock()

		fatals, err := d.incidents.PollFatalSince(ctx, orgID, last)
		if err != nil {
			d.logger.Warn("sentinel.detector.poll", "org_id", orgID, "err", err)
			continue
		}
		recentCount, err := d.incidents.CountRecent(ctx, orgID, SpikeWindow)
		if err != nil {
			d.logger.Warn("sentinel.detector.count_recent", "org_id", orgID, "err", err)
			recentCount = 0
		}

		wsID, err := d.workspaces.DefaultForOrg(ctx, orgID)
		if err != nil {
			// No workspace → no run to trigger; skip silently in non-prod
			// (every org without a workspace ends up here on boot).
			if !errors.Is(err, domain.ErrNotFound) {
				d.logger.Warn("sentinel.detector.workspace", "org_id", orgID, "err", err)
			}
			continue
		}

		triggers := Apply(orgID, wsID, last, lastTrig, fatals, recentCount, now)
		for _, t := range triggers {
			d.fire(ctx, t)
		}
		if len(fatals) > 0 {
			d.mu.Lock()
			d.lastSeen[orgID] = fatals[len(fatals)-1].ReceivedAt
			d.mu.Unlock()
		}
		if len(triggers) > 0 {
			d.mu.Lock()
			d.lastTriggered[orgID] = now
			d.mu.Unlock()
		}
	}
}

// fire kicks off one RecoveryPipeline run for the given trigger. Errors are
// logged at WARN — a Temporal-side failure should not take down the detector.
func (d *Detector) fire(ctx context.Context, t domain.IncidentTrigger) {
	princ := domain.Principal{Role: "system", OrgID: t.OrgID, UserID: ""}
	inputJSON, _ := json.Marshal(map[string]any{
		"triggered_by": "sentinel",
		"incident_id":  t.IncidentID,
		"rule":         t.Rule,
		"detected_at":  t.DetectedAt,
	})
	run, err := d.workflows.Start(ctx, princ, t.WorkspaceID, d.workflowType, inputJSON)
	if err != nil {
		d.logger.Warn("sentinel.detector.workflow_start",
			"org_id", t.OrgID, "incident_id", t.IncidentID, "rule", t.Rule, "err", err)
		return
	}
	d.logger.Info("sentinel.detector.fired",
		"org_id", t.OrgID, "workspace_id", t.WorkspaceID,
		"incident_id", t.IncidentID, "rule", t.Rule, "run_id", run.ID)

	if d.audit != nil {
		// audit.Write takes a Principal — the synthetic system principal we
		// fabricated above is what every Sentinel-originated audit row
		// attributes to. The HMAC chain still works because the writer reads
		// orgID off Principal.OrgID, not from a session row.
		_ = d.audit.Write(ctx, princ, "incident.sentinel_triggered", run.ID, map[string]any{
			"incident_id": t.IncidentID,
			"rule":        t.Rule,
			"detected_at": t.DetectedAt,
		})
	}
}
