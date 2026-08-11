package data_engineer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/db"
)

// DefaultTrackedTables is the core control-plane surface the drift detector
// watches when the caller doesn't configure its own set. Chosen as the tables
// whose shape every phase of the pipeline depends on — a drifted column here
// breaks agents, approvals, or billing long before a human notices.
var DefaultTrackedTables = []string{
	"organizations",
	"users",
	"workspaces",
	"workflow_runs",
	"activity_events",
	"incidents_raw",
	"approval_decisions",
	"projects",
}

// ColumnShape is one (table, column, type) triple — the unit both the live
// information_schema introspection and the stored baseline reduce to, so the
// diff is a pure set comparison.
type ColumnShape struct {
	TableName  string
	ColumnName string
	DataType   string
}

// DriftKind classifies a single divergence between baseline and live schema.
type DriftKind string

const (
	// DriftColumnRemoved — the column exists in the baseline but not live.
	DriftColumnRemoved DriftKind = "column_removed"
	// DriftColumnAdded — the column exists live but not in the baseline.
	DriftColumnAdded DriftKind = "column_added"
	// DriftTypeChanged — the column exists in both but the data_type differs.
	DriftTypeChanged DriftKind = "type_changed"
)

// Drift is one detected divergence. BaselineType / LiveType are filled per
// kind: removed carries only BaselineType, added only LiveType, type-changed
// both.
type Drift struct {
	Kind         DriftKind
	Table        string
	Column       string
	BaselineType string
	LiveType     string
}

// Detail renders the drift as the one-line evidence string embedded in the
// synthetic incident's stacktrace. The phrasing is deliberate: every variant
// matches the Synthesiser's schema-drift fast-path regex
// (`column .* does not exist`, see synthesiser/provider.go fastPathRules), so
// a REAL detection classifies as scenario="schema_drift" — and therefore
// rides the severity-HIGH escalation in approval.Classify — through exactly
// the same path as the canned demo fixture.
func (d Drift) Detail() string {
	switch d.Kind {
	case DriftColumnRemoved:
		return fmt.Sprintf("column %q.%q does not exist in live schema (baseline type %s)",
			d.Table, d.Column, d.BaselineType)
	case DriftColumnAdded:
		return fmt.Sprintf("column %q.%q does not exist in baseline snapshot (live type %s)",
			d.Table, d.Column, d.LiveType)
	case DriftTypeChanged:
		return fmt.Sprintf("column %q.%q with type %s does not exist in live schema (live type is %s)",
			d.Table, d.Column, d.BaselineType, d.LiveType)
	}
	return fmt.Sprintf("column %q.%q drifted", d.Table, d.Column)
}

// DriftReport is the outcome of one DriftDetector.Check pass.
//
// BaselineCaptured=true means no baseline existed yet, so this pass captured
// one instead of diffing — first-boot bootstrap, never a drift alert.
// BaselineColumns is how many (table, column) rows the pass captured or
// loaded, so the cron log line carries a sanity signal either way.
type DriftReport struct {
	BaselineCaptured bool
	BaselineColumns  int
	Drifts           []Drift
}

// DriftDetector introspects information_schema.columns for the tracked
// tables and diffs the live shape against the schema_baseline_snapshots
// rows (migration 0027).
//
// Pool is the fallback db.Querier used when ctx carries no RLS tx — the
// db.FromCtx pattern, so the same detector works from the cron goroutine
// (bare pool) and from a per-request RLS tx alike. The baseline table has no
// RLS (it describes the system's own schema), so either pool works; the cron
// wiring in cmd/server passes the admin pool.
type DriftDetector struct {
	Pool   db.Querier
	Tables []string
}

// NewDriftDetector builds a detector over the given fallback Querier. A nil
// or empty tables slice falls back to DefaultTrackedTables.
func NewDriftDetector(pool db.Querier, tables []string) *DriftDetector {
	if len(tables) == 0 {
		tables = DefaultTrackedTables
	}
	return &DriftDetector{Pool: pool, Tables: tables}
}

// SnapshotLive reads the current shape of the tracked tables from
// information_schema.columns. Rows come back ordered so downstream diffs and
// fingerprints are deterministic.
func (d *DriftDetector) SnapshotLive(ctx context.Context) ([]ColumnShape, error) {
	q := db.FromCtx(ctx, d.Pool)
	rows, err := q.Query(ctx, `
		SELECT table_name, column_name, data_type
		FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = ANY($1)
		ORDER BY table_name, column_name
	`, d.Tables)
	if err != nil {
		return nil, fmt.Errorf("DriftDetector.SnapshotLive: %w", err)
	}
	defer rows.Close()
	out := []ColumnShape{}
	for rows.Next() {
		var c ColumnShape
		if err := rows.Scan(&c.TableName, &c.ColumnName, &c.DataType); err != nil {
			return nil, fmt.Errorf("DriftDetector.SnapshotLive: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// LoadBaseline reads the stored baseline rows for the tracked tables.
// Returns an empty slice (not an error) when no baseline has been captured
// yet — the caller treats that as the bootstrap case.
func (d *DriftDetector) LoadBaseline(ctx context.Context) ([]ColumnShape, error) {
	q := db.FromCtx(ctx, d.Pool)
	rows, err := q.Query(ctx, `
		SELECT table_name, column_name, data_type
		FROM schema_baseline_snapshots
		WHERE table_name = ANY($1)
		ORDER BY table_name, column_name
	`, d.Tables)
	if err != nil {
		return nil, fmt.Errorf("DriftDetector.LoadBaseline: %w", err)
	}
	defer rows.Close()
	out := []ColumnShape{}
	for rows.Next() {
		var c ColumnShape
		if err := rows.Scan(&c.TableName, &c.ColumnName, &c.DataType); err != nil {
			return nil, fmt.Errorf("DriftDetector.LoadBaseline: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CaptureBaseline re-snapshots the tracked tables' live shape into
// schema_baseline_snapshots, replacing any previous baseline for those
// tables. DELETE-then-INSERT in two statements (rather than upsert) so
// columns dropped since the last capture disappear from the baseline too.
// Returns the number of (table, column) rows captured.
//
// Called automatically on the first Check (no baseline yet); after that,
// re-blessing a deliberately-migrated schema is an operator action — the
// detector never silently absorbs drift into the baseline.
func (d *DriftDetector) CaptureBaseline(ctx context.Context) (int, error) {
	q := db.FromCtx(ctx, d.Pool)
	if _, err := q.Exec(ctx, `
		DELETE FROM schema_baseline_snapshots WHERE table_name = ANY($1)
	`, d.Tables); err != nil {
		return 0, fmt.Errorf("DriftDetector.CaptureBaseline: clear: %w", err)
	}
	tag, err := q.Exec(ctx, `
		INSERT INTO schema_baseline_snapshots (table_name, column_name, data_type)
		SELECT c.table_name, c.column_name, c.data_type
		FROM information_schema.columns c
		WHERE c.table_schema = 'public' AND c.table_name = ANY($1)
	`, d.Tables)
	if err != nil {
		return 0, fmt.Errorf("DriftDetector.CaptureBaseline: insert: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// Check is the single entry point the cron checker drives: bootstrap the
// baseline when none exists, otherwise diff live against baseline.
func (d *DriftDetector) Check(ctx context.Context) (DriftReport, error) {
	baseline, err := d.LoadBaseline(ctx)
	if err != nil {
		return DriftReport{}, err
	}
	if len(baseline) == 0 {
		n, err := d.CaptureBaseline(ctx)
		if err != nil {
			return DriftReport{}, err
		}
		return DriftReport{BaselineCaptured: true, BaselineColumns: n}, nil
	}
	live, err := d.SnapshotLive(ctx)
	if err != nil {
		return DriftReport{}, err
	}
	return DriftReport{
		BaselineColumns: len(baseline),
		Drifts:          DiffShapes(baseline, live),
	}, nil
}

// DiffShapes is the pure diff between a baseline snapshot and the live
// shape: removed columns, added columns, and type changes, in deterministic
// (table, column) order so fingerprints are stable across runs.
func DiffShapes(baseline, live []ColumnShape) []Drift {
	type key struct{ table, column string }
	baseByKey := make(map[key]string, len(baseline))
	for _, c := range baseline {
		baseByKey[key{c.TableName, c.ColumnName}] = c.DataType
	}
	liveByKey := make(map[key]string, len(live))
	for _, c := range live {
		liveByKey[key{c.TableName, c.ColumnName}] = c.DataType
	}

	out := []Drift{}
	for _, c := range baseline {
		k := key{c.TableName, c.ColumnName}
		liveType, ok := liveByKey[k]
		if !ok {
			out = append(out, Drift{Kind: DriftColumnRemoved, Table: c.TableName, Column: c.ColumnName, BaselineType: c.DataType})
			continue
		}
		if liveType != c.DataType {
			out = append(out, Drift{Kind: DriftTypeChanged, Table: c.TableName, Column: c.ColumnName, BaselineType: c.DataType, LiveType: liveType})
		}
	}
	for _, c := range live {
		if _, ok := baseByKey[key{c.TableName, c.ColumnName}]; !ok {
			out = append(out, Drift{Kind: DriftColumnAdded, Table: c.TableName, Column: c.ColumnName, LiveType: c.DataType})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Table != out[j].Table {
			return out[i].Table < out[j].Table
		}
		if out[i].Column != out[j].Column {
			return out[i].Column < out[j].Column
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}

// DriftSource is the narrow port DriftChecker drives — one Check per tick.
// *DriftDetector satisfies it directly; tests substitute a fake so the
// checker's incident plumbing is unit-testable without Postgres.
type DriftSource interface {
	Check(ctx context.Context) (DriftReport, error)
}

// DriftChecker is the cron-shaped wrapper that turns a detected drift into a
// synthetic incidents_raw row via the same domain.IncidentSink port the
// webhook adapters use. The row lands with source='schema_drift' and
// level='fatal', so the Sentinel detector's normal PollFatalSince →
// rules.Apply → workflow-start path fires a RecoveryPipeline for it — no
// bespoke trigger plumbing.
//
// The Name/Interval/Run trio matches cron.Job so cmd/server can register it
// alongside usage_ticker:
//
//	cron.Job{Name: "schema_drift_checker", Interval: checker.Interval, Run: checker.Run}
//
// Dedupe: SourceEventID is a fingerprint of the sorted drift details, and
// incidents_raw's (org_id, source, source_event_id) unique index makes the
// insert idempotent — a drift that persists across ticks lands exactly one
// incident until its shape changes.
type DriftChecker struct {
	Source DriftSource
	// Sink persists the synthetic incident. Wire an IncidentsRepo whose
	// fallback pool bypasses RLS (repo.NewIncidentsRepo(adminPool)) — the
	// cron goroutine has no request tx, so an RLS-aware fallback would
	// insert zero rows.
	Sink domain.IncidentSink
	// OrgID is the tenant the synthetic incident is attributed to — the org
	// whose Sentinel + approval surface handles control-plane drift.
	OrgID    string
	Interval time.Duration
	Logger   *slog.Logger
}

// Run performs one drift sweep. Matches the cron.Job Run signature; errors
// propagate so the cron dispatcher logs them without stopping the loop.
func (c *DriftChecker) Run(ctx context.Context) error {
	if c == nil || c.Source == nil || c.Sink == nil || c.OrgID == "" {
		return fmt.Errorf("data_engineer.DriftChecker: misconfigured (source/sink/org required)")
	}
	logger := c.Logger
	if logger == nil {
		logger = slog.Default()
	}
	report, err := c.Source.Check(ctx)
	if err != nil {
		return fmt.Errorf("data_engineer.DriftChecker: check: %w", err)
	}
	if report.BaselineCaptured {
		logger.Info("schema_drift.baseline_captured", "columns", report.BaselineColumns)
		return nil
	}
	if len(report.Drifts) == 0 {
		return nil
	}
	raw := driftIncident(report.Drifts, time.Now().UTC())
	if err := c.Sink.Insert(ctx, c.OrgID, raw); err != nil {
		return fmt.Errorf("data_engineer.DriftChecker: insert incident: %w", err)
	}
	logger.Warn("schema_drift.detected",
		"org_id", c.OrgID, "drifts", len(report.Drifts), "source_event_id", raw.SourceEventID)
	return nil
}

// driftIncident builds the synthetic RawIncident for a set of drifts. The
// stacktrace payload key carries the regex-friendly Detail lines because
// that is the field PollFatalSince projects into IncidentRow.Stacktrace and
// Pathfinder folds into the evidence chain the Synthesiser classifies.
func driftIncident(drifts []Drift, now time.Time) domain.RawIncident {
	details := make([]string, 0, len(drifts))
	structured := make([]map[string]string, 0, len(drifts))
	for _, d := range drifts {
		details = append(details, d.Detail())
		structured = append(structured, map[string]string{
			"kind":          string(d.Kind),
			"table":         d.Table,
			"column":        d.Column,
			"baseline_type": d.BaselineType,
			"live_type":     d.LiveType,
		})
	}
	return domain.RawIncident{
		Source:        "schema_drift",
		SourceEventID: driftFingerprint(details),
		Title:         fmt.Sprintf("Schema drift detected: %d column(s) diverged from baseline", len(drifts)),
		Level:         "fatal",
		Service:       "control-plane-db",
		Environment:   "production",
		Payload: map[string]any{
			"stacktrace":  strings.Join(details, "\n"),
			"logs":        fmt.Sprintf("ts=%s level=error service=control-plane-db msg=schema_drift_detected drift_count=%d", now.Format(time.RFC3339), len(drifts)),
			"drift":       structured,
			"detected_at": now.Format(time.RFC3339),
		},
	}
}

// driftFingerprint hashes the sorted detail lines into the source_event_id
// used for idempotent inserts. Sorting makes the id order-insensitive so two
// sweeps that observe the same drift set always collide onto one row.
func driftFingerprint(details []string) string {
	sorted := make([]string, len(details))
	copy(sorted, details)
	sort.Strings(sorted)
	sum := sha256.Sum256([]byte(strings.Join(sorted, "\n")))
	return "drift-" + hex.EncodeToString(sum[:])[:16]
}
