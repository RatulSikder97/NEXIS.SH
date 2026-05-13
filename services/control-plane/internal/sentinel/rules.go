// Package sentinel hosts the always-on detector goroutine that polls
// incidents_raw and triggers a RecoveryPipeline workflow per rule hit. The
// detector is a peer of usecase/ — it owns its own goroutine, has no HTTP
// surface, and depends on domain.IncidentsReader (admin-pool reads),
// usecase.WorkflowService (run start), and narrow Workspaces/Integrations
// readers for cross-org bookkeeping.
//
// Phase 6 ships two rules (fatal_level + error_rate_spike). Rules are pure
// functions over `(rows, now) → []IncidentTrigger` so they are trivially
// unit-testable without standing up Neo4j, Temporal, or the database.
package sentinel

import (
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// SpikeThreshold is the row count over SpikeWindow that constitutes an
// error-rate spike. Phase 6 ships 5/5m as a debug-friendly default; Phase 7's
// real metric pipeline tunes this per-tenant.
const SpikeThreshold = 5

// SpikeWindow is the lookback window the error-rate-spike rule applies. Also
// used as the cooldown the rule respects to avoid double-firing — once a spike
// triggers, the next one for the same org is suppressed until SpikeWindow
// passes.
const SpikeWindow = 5 * time.Minute

// SpikeCooldown is the minimum gap between two error-rate-spike triggers for
// the same org. Held equal to SpikeWindow on purpose: after the window passes
// the rule re-evaluates cleanly without ambiguous overlap.
const SpikeCooldown = 5 * time.Minute

// Apply evaluates both Phase 6 rules against a single tick's inputs and
// returns the triggers to fire.
//
//   - Rule 1 (fatal_level): one trigger per row in fatals. The caller passes
//     in fatals already filtered to rows newer than lastSeen.
//   - Rule 2 (error_rate_spike): one trigger when recentCount >= threshold
//     AND the cooldown has elapsed AND no fatal-level trigger fired this tick
//     (when fatals is non-empty the per-row rule already covers the spike;
//     we don't want a double-fire that creates two workflow runs).
func Apply(
	orgID, workspaceID string,
	lastSeen time.Time,
	lastTriggerAt time.Time,
	fatals []domain.IncidentRow,
	recentCount int,
	now time.Time,
) []domain.IncidentTrigger {
	out := make([]domain.IncidentTrigger, 0, len(fatals)+1)
	for _, r := range fatals {
		out = append(out, domain.IncidentTrigger{
			OrgID: orgID, WorkspaceID: workspaceID, IncidentID: r.ID,
			Rule: "fatal_level", DetectedAt: now, ReceivedAt: r.ReceivedAt,
		})
	}
	cooldownOK := lastTriggerAt.IsZero() || now.Sub(lastTriggerAt) > SpikeCooldown
	if recentCount >= SpikeThreshold && cooldownOK && len(fatals) == 0 {
		out = append(out, domain.IncidentTrigger{
			OrgID: orgID, WorkspaceID: workspaceID,
			Rule: "error_rate_spike", DetectedAt: now, ReceivedAt: now,
		})
	}
	return out
}
