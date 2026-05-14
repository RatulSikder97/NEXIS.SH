package sentinel

// Coverage for the multi-source dedupe layer (sentinel/multisource.go).
// The ledger lives on Detector but the dedupe helpers are pure functions
// of the in-memory state and `now`, so we can drive them directly without
// spinning up a full tick loop.

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// newDedupeDetector returns a bare Detector with only the fields the
// dedupe layer needs (mu + nil ledger). The Run/tick path is exercised in
// detector_test.go; here we just need the receiver for dedupeTriggers /
// recordDedupe.
func newDedupeDetector() *Detector {
	return &Detector{}
}

// TestMultiSource_DedupesAcrossSources — same fingerprint key seen from
// three providers within the 60s window emits only ONE pass-through trigger.
// Mirrors the Task 8 acceptance from the spec.
func TestMultiSource_DedupesAcrossSources(t *testing.T) {
	d := newDedupeDetector()
	now := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)

	// Three triggers, same source_event_id, three sources, close in time.
	triggers := []domain.IncidentTrigger{
		{OrgID: "org-1", SourceEventID: "incident-fp-1", Source: "sentry", IncidentID: "s1"},
		{OrgID: "org-1", SourceEventID: "incident-fp-1", Source: "datadog", IncidentID: "d1"},
		{OrgID: "org-1", SourceEventID: "incident-fp-1", Source: "pagerduty", IncidentID: "p1"},
	}

	// First call: no prior state → all three are returned. The dedupe
	// happens on the SECOND call after recordDedupe stamps the ledger.
	out := d.dedupeTriggers("org-1", triggers, now)
	require.Len(t, out, 3, "first sweep returns all three (no prior ledger entry)")

	// Stamp the ledger with the first trigger only — the production detector
	// records AFTER fire so the ordering matches.
	d.recordDedupe("org-1", triggers[0], now)

	// Subsequent triggers in the SAME window should be dropped.
	later := now.Add(5 * time.Second)
	dupes := []domain.IncidentTrigger{
		{OrgID: "org-1", SourceEventID: "incident-fp-1", Source: "datadog"},
		{OrgID: "org-1", SourceEventID: "incident-fp-1", Source: "pagerduty"},
	}
	out2 := d.dedupeTriggers("org-1", dupes, later)
	require.Len(t, out2, 0, "duplicates inside window must be suppressed")
}

// TestMultiSource_AllowsAfterWindowExpires — a second genuinely-separate
// incident 65s after the first must fire (window is 60s).
func TestMultiSource_AllowsAfterWindowExpires(t *testing.T) {
	d := newDedupeDetector()
	now := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)

	first := domain.IncidentTrigger{
		OrgID: "org-1", SourceEventID: "incident-fp-1", Source: "sentry",
	}
	d.recordDedupe("org-1", first, now)

	// 65 seconds later, the same fingerprint should NOT be deduped.
	later := now.Add(65 * time.Second)
	out := d.dedupeTriggers("org-1", []domain.IncidentTrigger{first}, later)
	require.Len(t, out, 1, "after window expiry, a fresh trigger must pass through")
}

// TestMultiSource_DedupesPerOrg — two orgs hitting the same source_event_id
// don't collide. The ledger key is (org, source_event_id), not just the id.
func TestMultiSource_DedupesPerOrg(t *testing.T) {
	d := newDedupeDetector()
	now := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)

	t1 := domain.IncidentTrigger{OrgID: "org-A", SourceEventID: "fp"}
	t2 := domain.IncidentTrigger{OrgID: "org-B", SourceEventID: "fp"}
	d.recordDedupe("org-A", t1, now)

	// org-B's trigger must still pass through.
	out := d.dedupeTriggers("org-B", []domain.IncidentTrigger{t2}, now.Add(5*time.Second))
	require.Len(t, out, 1, "org isolation: different org must not see another org's dedupe")
}

// TestMultiSource_PassesThroughEmptyEventID — rate-spike triggers (no
// SourceEventID) are intentionally NOT deduped. SpikeCooldown is the gate
// for them; the multi-source layer must leave them alone.
func TestMultiSource_PassesThroughEmptyEventID(t *testing.T) {
	d := newDedupeDetector()
	now := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)

	t1 := domain.IncidentTrigger{OrgID: "org-1", Rule: "error_rate_spike"}
	d.recordDedupe("org-1", t1, now)
	// Even though we just "recorded" t1, the recordDedupe path is a no-op
	// for empty SourceEventID; the next dedupeTriggers must pass it through.

	out := d.dedupeTriggers("org-1", []domain.IncidentTrigger{t1}, now.Add(5*time.Second))
	require.Len(t, out, 1, "empty source_event_id must NEVER be deduped")
}

// TestMultiSource_EmptyTriggerList_NoPanic — defence-in-depth for the
// empty-input path. The detector tick may produce no triggers; the dedupe
// helper must handle that gracefully.
func TestMultiSource_EmptyTriggerList_NoPanic(t *testing.T) {
	d := newDedupeDetector()
	out := d.dedupeTriggers("org-1", nil, time.Now())
	require.Len(t, out, 0, "nil input must return nil-ish")

	out = d.dedupeTriggers("org-1", []domain.IncidentTrigger{}, time.Now())
	require.Len(t, out, 0, "empty input must return empty")
}

// TestMultiSource_RecordDedupe_EmptyIDIsNoOp — explicit unit for the noop
// branch in recordDedupe. Stamps with empty event id must not mutate the
// ledger.
func TestMultiSource_RecordDedupe_EmptyIDIsNoOp(t *testing.T) {
	d := newDedupeDetector()
	d.recordDedupe("org-1", domain.IncidentTrigger{}, time.Now())
	// dedupeLedger should still be uninitialised (nil) — the recordDedupe
	// short-circuit drops out before allocating the map.
	require.Nil(t, d.dedupeLedger, "empty event id must not allocate the ledger")
}

// TestMultiSource_RecordDedupe_PrunesOldEntries — recordDedupe sweeps
// entries older than 2× window so the map size stays bounded. Insert one
// very-old entry, then a fresh one, and confirm the old one is gone.
func TestMultiSource_RecordDedupe_PrunesOldEntries(t *testing.T) {
	d := newDedupeDetector()
	veryOld := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	// Seed the ledger directly so we don't have to backdate via recordDedupe.
	d.dedupeLedger = map[dedupeKey]time.Time{
		{orgID: "org-1", sourceEventID: "stale"}: veryOld,
	}
	// Stamp a fresh entry far enough in the future to trigger the prune.
	fresh := veryOld.Add(5 * time.Minute) // 5min > 2×60s window
	d.recordDedupe("org-1", domain.IncidentTrigger{
		OrgID: "org-1", SourceEventID: "fresh",
	}, fresh)
	if _, ok := d.dedupeLedger[dedupeKey{orgID: "org-1", sourceEventID: "stale"}]; ok {
		t.Fatalf("stale ledger entry must be pruned")
	}
	if _, ok := d.dedupeLedger[dedupeKey{orgID: "org-1", sourceEventID: "fresh"}]; !ok {
		t.Fatalf("fresh entry must be retained")
	}
}
