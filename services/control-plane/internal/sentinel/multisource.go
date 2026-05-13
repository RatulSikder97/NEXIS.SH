// Package sentinel — multi-source detector extensions (Task 8).
//
// Wave 2 added Datadog + PagerDuty adapters that also persist rows to
// incidents_raw via IncidentSink.Insert. After widening the SQL filter in
// IncidentsRepo.PollFatalSince to include ('sentry','datadog','pagerduty')
// the existing detector tick fires recoveries for every source — but that
// introduces a new failure mode: a single root incident observed by multiple
// services (Sentry catches the exception, Datadog alerts on the spike,
// PagerDuty pages oncall) produces three triggers and three workflow runs.
//
// This file adds the dedupe ledger that collapses near-simultaneous triggers
// keyed by (org_id, source_event_id). The window is 60 seconds — long enough
// to swallow the natural skew between three webhook deliveries for the same
// incident, short enough that a second, genuinely separate failure 65 seconds
// later still fires a fresh recovery.
//
// Design notes:
//   - The ledger lives in-process. It does not need to survive a restart: on
//     reboot the lastSeen watermark moves forward and a re-fire of the same
//     trigger after a 60s downtime is the correct behaviour (we want a
//     warm-start to recover the missed work, not silently swallow it).
//   - Triggers with empty SourceEventID (rate-spike rule) are NEVER deduped.
//     Their natural cadence is already gated by SpikeCooldown.
//   - Entries are pruned lazily by recordDedupe; we sweep on each insert so
//     the map stays bounded by `(orgs × in-flight incidents)` rather than
//     growing forever.
package sentinel

import (
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// MultiSourceDedupeWindow is the suppression window for triggers sharing
// (org_id, source_event_id). 60 seconds matches the requirement in Task 8.
const MultiSourceDedupeWindow = 60 * time.Second

// dedupeKey is the composite key into Detector.dedupeLedger. Kept tiny so a
// busy detector with thousands of in-flight incidents stays cache-friendly.
type dedupeKey struct {
	orgID         string
	sourceEventID string
}

// dedupeTriggers filters the input slice down to triggers we have NOT seen
// (by composite key) inside MultiSourceDedupeWindow. Order is preserved so
// the caller's downstream rate-spike logic (which checks "did any fatal fire
// this tick?") still sees a representative sample.
//
// Triggers with empty SourceEventID pass through untouched — rate-spike
// triggers are not source-event-keyed by design.
//
// Concurrency: the ledger map lives on the Detector and is guarded by
// Detector.mu (same mutex as lastSeen/lastTriggered).
func (d *Detector) dedupeTriggers(orgID string, triggers []domain.IncidentTrigger, now time.Time) []domain.IncidentTrigger {
	if len(triggers) == 0 {
		return triggers
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.dedupeLedger == nil {
		d.dedupeLedger = map[dedupeKey]time.Time{}
	}
	out := triggers[:0]
	cutoff := now.Add(-MultiSourceDedupeWindow)
	for _, t := range triggers {
		if t.SourceEventID == "" {
			out = append(out, t)
			continue
		}
		key := dedupeKey{orgID: orgID, sourceEventID: t.SourceEventID}
		seen, ok := d.dedupeLedger[key]
		if ok && seen.After(cutoff) {
			// In-window duplicate — drop.
			continue
		}
		out = append(out, t)
	}
	return out
}

// recordDedupe stamps the (org_id, source_event_id) key with the trigger's
// effective time so subsequent triggers in the same window are dropped.
// Caller holds no lock; recordDedupe acquires it.
//
// Triggers without a SourceEventID (rate-spike) are not stamped — they have
// no natural key and re-firing is the correct behaviour for a sustained
// spike.
//
// recordDedupe also performs an opportunistic sweep of entries older than
// 2× the dedupe window so the map size stays bounded over a long-running
// detector lifetime. Two windows is enough that an in-flight trigger's
// stamp is never accidentally pruned while still being meaningful.
func (d *Detector) recordDedupe(orgID string, t domain.IncidentTrigger, now time.Time) {
	if t.SourceEventID == "" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.dedupeLedger == nil {
		d.dedupeLedger = map[dedupeKey]time.Time{}
	}
	key := dedupeKey{orgID: orgID, sourceEventID: t.SourceEventID}
	d.dedupeLedger[key] = now

	// Opportunistic prune: drop entries older than 2× window.
	pruneCutoff := now.Add(-2 * MultiSourceDedupeWindow)
	for k, v := range d.dedupeLedger {
		if v.Before(pruneCutoff) {
			delete(d.dedupeLedger, k)
		}
	}
}
