package sentinel

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/db"
)

// WorkspacesReader is the narrow port the detector depends on to look up the
// "default" workspace for an org. Implemented by *repo.WorkspacesRepo via
// DefaultForOrg.
type WorkspacesReader interface {
	DefaultForOrg(ctx context.Context, orgID string) (string, error)
}

// IntegrationsReader is the narrow port the detector depends on to enumerate
// the orgs it must poll each tick. After Task 8 (multi-source) the detector
// reads ConnectedIncidentOrgs so it sees Datadog + PagerDuty tenants too.
// ConnectedSentryOrgs stays on the port for backward compatibility with
// fakes / call sites that have not migrated yet; new wiring should target
// ConnectedIncidentOrgs.
//
// *repo.IntegrationsRepo satisfies both. The test fakes in detector_test.go
// implement both methods; production wiring in main.go uses the same repo
// instance.
type IntegrationsReader interface {
	ConnectedSentryOrgs(ctx context.Context) ([]string, error)
	ConnectedIncidentOrgs(ctx context.Context) ([]string, error)
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
	appPool      *pgxpool.Pool
	audit        domain.AuditWriter
	router       *Router
	workflowType string
	interval     time.Duration
	logger       *slog.Logger

	mu            sync.Mutex
	lastSeen      map[string]time.Time
	lastTriggered map[string]time.Time

	// dedupeLedger backs the multi-source dedupe layer (sentinel/multisource.go).
	// Keyed by (org_id, source_event_id) → last-seen time. Same mutex as
	// lastSeen/lastTriggered; lazily initialised on first use.
	dedupeLedger map[dedupeKey]time.Time

	// spikeBaselines is the per-org SPC state for the error-rate-spike rule:
	// an EWMA mean+variance of the CountRecent window aggregate, one entry
	// per org (rules.go's SpikeBaseline). In-memory by design — it resets on
	// restart, which drops the rule into its fixed-threshold warm-up mode
	// for one SpikeWindow (the same trade-off dedupeLedger documents; this
	// service claims no cross-restart statistical memory). Guarded by mu.
	spikeBaselines map[string]SpikeBaseline
}

// Config bundles every Detector dependency. The runtime constructor lives in
// main.go; tests build a Detector inline with fakes.
type Config struct {
	Incidents    domain.IncidentsReader
	Workflows    domain.WorkflowService
	Workspaces   WorkspacesReader
	Integrations IntegrationsReader
	// AppPool is the RLS-bound application pool. The detector runs on a
	// background ticker with no request tx, so workflow_runs INSERTs are
	// refused by the tenant policy unless we open one ourselves and pin
	// app.current_org_id. Nil is tolerated (tests, and any deployment whose
	// pool has RLS disabled) — fire() then calls Start directly.
	AppPool *pgxpool.Pool
	Audit   domain.AuditWriter
	// Router resolves a project_id for each emitted trigger. Optional —
	// when nil the detector skips project routing and every trigger fires
	// with ProjectID="" (workflow falls back to fixtures). Constructed
	// alongside the detector in cmd/server/main.go from the projects repo.
	Router *Router
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
		incidents:      cfg.Incidents,
		workflows:      cfg.Workflows,
		workspaces:     cfg.Workspaces,
		integrations:   cfg.Integrations,
		appPool:        cfg.AppPool,
		audit:          cfg.Audit,
		router:         cfg.Router,
		workflowType:   cfg.WorkflowType,
		interval:       cfg.Interval,
		logger:         cfg.Logger,
		lastSeen:       map[string]time.Time{},
		lastTriggered:  map[string]time.Time{},
		spikeBaselines: map[string]SpikeBaseline{},
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
	orgs, err := d.integrations.ConnectedIncidentOrgs(ctx)
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

// tick performs one detection sweep across every connected incident-source
// org. Errors per org are logged + skipped — they never abort the sweep.
//
// After Task 8 the source set widens to (sentry, datadog, pagerduty); the
// triggers slice produced by Apply is run through dedupeTriggers so a single
// underlying incident observed by multiple sources (or duplicated by the same
// source mid-window) only spawns one workflow run.
func (d *Detector) tick(ctx context.Context, now time.Time) {
	orgs, err := d.integrations.ConnectedIncidentOrgs(ctx)
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
		countOK := err == nil
		if err != nil {
			d.logger.Warn("sentinel.detector.count_recent", "org_id", orgID, "err", err)
			recentCount = 0
		}

		// SPC bookkeeping: this tick is judged against the baseline built
		// from PRIOR ticks only, then the current observation is folded in.
		// A failed CountRecent is not observed — folding the substituted 0
		// would drag the EWMA down and make the next real count look like a
		// spike. The fold happens before the workspace lookup on purpose so
		// orgs without a default workspace still accumulate a baseline and
		// are statistically warm the moment a workspace appears.
		d.mu.Lock()
		baseline := d.spikeBaselines[orgID]
		if countOK {
			d.spikeBaselines[orgID] = baseline.Observe(float64(recentCount))
		}
		d.mu.Unlock()

		wsID, err := d.workspaces.DefaultForOrg(ctx, orgID)
		if err != nil {
			// No workspace → no run to trigger; skip silently in non-prod
			// (every org without a workspace ends up here on boot).
			if !errors.Is(err, domain.ErrNotFound) {
				d.logger.Warn("sentinel.detector.workspace", "org_id", orgID, "err", err)
			}
			continue
		}

		triggers := Apply(orgID, wsID, last, lastTrig, fatals, recentCount, baseline, now)
		triggers = d.dedupeTriggers(orgID, triggers, now)
		// Project routing — resolve a project_id for each trigger so the
		// recovery workflow has the right repo/app/channel mapping. The
		// router stamps trigger.ProjectID in place; failures are logged but
		// don't drop the trigger (workflow falls back to fixtures).
		if d.router != nil {
			for i := range triggers {
				_ = d.router.Route(ctx, &triggers[i])
			}
		}
		for _, t := range triggers {
			d.fire(ctx, t)
			// Stamp the dedupe ledger AFTER fire so a concurrent failure
			// doesn't accidentally suppress the next legitimate try; the fire
			// itself is best-effort and idempotent at the workflow layer.
			d.recordDedupe(orgID, t, now)
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

// TriggerOne is the Phase 8 admin escape hatch that bypasses the poll loop
// and synthesises a single IncidentTrigger directly. Used by the
// POST /v1/admin/sentinel/trigger endpoint so operators can demo a recovery
// without waiting for a real Sentry fatal to land.
//
// The synthetic trigger is stamped Rule="manual" so the detector's bookkeeping
// (lastTriggered cooldown, lastSeen watermark) is NOT touched — manual
// triggers are independent of the rule-based path so they don't suppress a
// real spike that lands seconds later.
//
// incidentID is optional; pass "" when the caller doesn't want to associate
// the run with any incidents_raw row.
func (d *Detector) TriggerOne(ctx context.Context, orgID, incidentID string) (domain.WorkflowRun, error) {
	if d == nil {
		return domain.WorkflowRun{}, errors.New("sentinel: detector not initialised")
	}
	wsID, err := d.workspaces.DefaultForOrg(ctx, orgID)
	if err != nil {
		return domain.WorkflowRun{}, err
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	trig := domain.IncidentTrigger{
		OrgID:       orgID,
		WorkspaceID: wsID,
		IncidentID:  incidentID,
		Rule:        "manual",
		DetectedAt:  now,
		ReceivedAt:  now,
	}
	princ := domain.Principal{Role: "system", OrgID: orgID}
	inputJSON, _ := json.Marshal(map[string]any{
		"triggered_by": "sentinel_admin",
		"incident_id":  trig.IncidentID,
		"rule":         trig.Rule,
		"detected_at":  trig.DetectedAt,
	})
	run, err := d.workflows.Start(ctx, princ, trig.WorkspaceID, d.workflowType, inputJSON)
	if err != nil {
		d.logger.Warn("sentinel.detector.trigger_one.workflow_start",
			"org_id", orgID, "incident_id", incidentID, "err", err)
		return domain.WorkflowRun{}, err
	}
	if d.audit != nil {
		_ = d.audit.Write(ctx, princ, "incident.sentinel_admin_triggered", run.ID, map[string]any{
			"incident_id": trig.IncidentID,
			"rule":        trig.Rule,
		})
	}
	d.logger.Info("sentinel.detector.trigger_one.fired",
		"org_id", orgID, "workspace_id", trig.WorkspaceID, "run_id", run.ID)
	return run, nil
}

// fire kicks off one RecoveryPipeline run for the given trigger. Errors are
// logged at WARN — a Temporal-side failure should not take down the detector.

// startRun invokes the workflow service inside a tenant-pinned transaction.
//
// WorkflowService.Start inserts the workflow_runs row on whatever tx is in
// ctx so RLS can see app.current_org_id. HTTP callers get that binding from
// the RLS middleware; the detector is a background ticker with no request, so
// without this wrapper every autonomous detection died with "new row violates
// row-level security policy for table workflow_runs" — detection worked and
// recovery never started.
func (d *Detector) startRun(ctx context.Context, princ domain.Principal, t domain.IncidentTrigger, input map[string]any) (domain.WorkflowRun, error) {
	if d.appPool == nil {
		inputJSON, _ := json.Marshal(input)
		return d.workflows.Start(ctx, princ, t.WorkspaceID, d.workflowType, inputJSON)
	}
	tx, err := d.appPool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.WorkflowRun{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SELECT set_config('app.current_org_id', $1, true)", t.OrgID); err != nil {
		return domain.WorkflowRun{}, err
	}
	// Tell Pathfinder which codebase to walk. The demo path stamps the
	// fixture's repo_sha explicitly; on the autonomous path we use the most
	// recently indexed one for the org, because that is exactly the code
	// NEXIS has a graph for. Without it the traversal runs against
	// repo_sha="" , finds no Symbol node, and the causal ranking returns no
	// candidate at all.
	if _, ok := input["repo_sha"]; !ok {
		var sha string
		if err := tx.QueryRow(ctx,
			`SELECT repo_sha FROM code_embeddings
			  WHERE org_id = $1 AND repo_sha <> ''
			  ORDER BY created_at DESC LIMIT 1`, t.OrgID).Scan(&sha); err == nil && sha != "" {
			input["repo_sha"] = sha
		}
	}
	inputJSON, _ := json.Marshal(input)
	run, err := d.workflows.Start(db.WithTx(ctx, tx), princ, t.WorkspaceID, d.workflowType, inputJSON)
	if err != nil {
		return domain.WorkflowRun{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.WorkflowRun{}, err
	}
	return run, nil
}

func (d *Detector) fire(ctx context.Context, t domain.IncidentTrigger) {
	princ := domain.Principal{Role: "system", OrgID: t.OrgID, UserID: ""}
	input := map[string]any{
		"triggered_by": "sentinel",
		"incident_id":  t.IncidentID,
		"rule":         t.Rule,
		"detected_at":  t.DetectedAt,
	}
	if t.ProjectID != "" {
		input["project_id"] = t.ProjectID
	}
	// Hand the agents the actual fault. WorkflowService.Start reads this
	// object into PipelineInput.Incident, which is what the Architect,
	// Backend and QA prompts are built from — omit it and each one is asked
	// to plan a repair for an incident it cannot see.
	if t.Title != "" {
		input["incident"] = map[string]any{
			"label":       t.Source,
			"title":       t.Title,
			"service":     t.Service,
			"environment": t.Environment,
			"stacktrace":  t.Stacktrace,
			"logs":        t.Logs,
		}
	}
	run, err := d.startRun(ctx, princ, t, input)
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
