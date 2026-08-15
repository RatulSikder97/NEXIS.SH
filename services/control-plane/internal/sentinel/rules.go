// Package sentinel hosts the always-on detector goroutine that polls
// incidents_raw and triggers a RecoveryPipeline workflow per rule hit. The
// detector is a peer of usecase/ — it owns its own goroutine, has no HTTP
// surface, and depends on domain.IncidentsReader (admin-pool reads),
// usecase.WorkflowService (run start), and narrow Workspaces/Integrations
// readers for cross-org bookkeeping.
//
// Phase 6 ships two rules (fatal_level + error_rate_spike). Rules are pure
// functions over `(rows, baseline, now) → []IncidentTrigger` so they are
// trivially unit-testable without standing up Neo4j, Temporal, or the
// database.
//
// The error_rate_spike rule is statistical process control (SPC) over the
// Postgres windowed aggregate the detector polls each tick (CountRecent —
// the deliberate Flink descope: "Postgres windowed aggregates + Temporal
// cron", see docs/PROJECT_PLAN.md). Each org carries a SpikeBaseline — an
// exponentially weighted moving average (EWMA) of its recent-window event
// count plus an EWMA variance — and the rule fires only when the current
// window's count breaches the upper control limit mean + SpikeSigma·σ.
package sentinel

import (
	"math"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// SpikeThreshold is the absolute floor for the error-rate-spike rule: a
// window count below this can never fire, no matter how quiet the org's
// baseline is. Without it a near-silent org (mean≈0, σ≈0) would page on a
// single stray event, because any count > 0 breaches a zero control limit.
// It doubles as the warm-up threshold: until a baseline has SpikeMinSamples
// observations the rule falls back to this fixed count — the Phase 6
// behaviour — so a freshly booted detector is not blind.
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

// SpikeSigma is the control-limit multiplier k in `UCL = mean + k·σ`.
//
// 3 is the textbook Shewhart control limit (Shewhart 1931; Montgomery,
// "Introduction to Statistical Quality Control"): under an in-control
// process with approximately normal sampling noise, ~99.865% of window
// counts fall at or below mean + 3σ, so the one-sided false-alarm rate is
// ~0.135% per evaluation. Smaller k pages operators on routine variation;
// larger k misses real regressions — three sigma is the standard compromise
// and matches the c-chart convention for count data.
const SpikeSigma = 3.0

// SpikeEWMAAlpha is the smoothing factor for the per-org baseline EWMA.
// Each observation moves the mean by alpha·(x−mean), so the baseline's
// weighted memory has centre of mass (1−alpha)/alpha ≈ 49 samples back —
// at the detector's default 10s tick that is ≈8 minutes, i.e. the baseline
// summarises roughly the preceding two-to-three SpikeWindows rather than
// chasing the current one. Small enough that a genuine burst stands out
// against the limit before the baseline absorbs it; large enough that the
// baseline tracks slow drift in an org's normal rate without operator
// retuning.
const SpikeEWMAAlpha = 0.02

// SpikeMinSamples is the number of observations a baseline needs before its
// control limit is trusted. 30 ticks at the default 10s interval is one full
// SpikeWindow of history — the minimum for the EWMA variance to reflect the
// org's real dispersion rather than the seed value. Below this the rule
// falls back to the fixed SpikeThreshold (see its doc).
const SpikeMinSamples = 30

// SpikeBaseline is the per-org SPC state behind the error-rate-spike rule:
// an EWMA estimate of the recent-window event count's mean and variance.
//
// It is a small value type on purpose. Observe returns an updated copy, so
// Apply stays a pure function and the Detector owns the only mutable copy
// (in its spikeBaselines map, guarded by Detector.mu). The state is
// in-memory and resets on restart — the same deliberate trade-off as the
// multi-source dedupe ledger (multisource.go): after a reboot the rule runs
// in warm-up (fixed-threshold) mode for one SpikeWindow and then resumes
// statistical limits. Nothing in this service claims cross-restart
// statistical memory.
type SpikeBaseline struct {
	// Mean is the EWMA of observed window counts.
	Mean float64
	// Variance is the EWMA variance of observed window counts, maintained
	// with the exponentially weighted form of Welford's update (Finch 2009,
	// "Incremental calculation of weighted mean and variance").
	Variance float64
	// Samples is how many observations have been folded in.
	Samples int
}

// Observe folds one window-count observation into the baseline and returns
// the updated copy. The first observation seeds the mean directly (instead
// of decaying up from zero, which would bias the limit low for the EWMA's
// whole memory span); later observations apply the standard EWMA updates:
//
//	mean'     = mean + α·(x − mean)
//	variance' = (1−α)·(variance + α·(x − mean)²)
func (b SpikeBaseline) Observe(x float64) SpikeBaseline {
	if b.Samples == 0 {
		return SpikeBaseline{Mean: x, Variance: 0, Samples: 1}
	}
	diff := x - b.Mean
	incr := SpikeEWMAAlpha * diff
	b.Mean += incr
	b.Variance = (1 - SpikeEWMAAlpha) * (b.Variance + diff*incr)
	b.Samples++
	return b
}

// Ready reports whether the baseline has enough history for its control
// limit to be statistically meaningful. See SpikeMinSamples.
func (b SpikeBaseline) Ready() bool {
	return b.Samples >= SpikeMinSamples
}

// UCL is the upper control limit: mean + SpikeSigma·σ.
//
// σ is the larger of the empirically observed EWMA stddev and the Poisson
// floor √mean. Window counts are event counts, and the classic c-chart for
// count data sets its limits at mean + 3·√mean precisely because a Poisson
// process has variance equal to its mean — an org whose observed history
// happens to be under-dispersed (e.g. a perfectly steady synthetic load)
// must not end up with a hair-trigger limit of mean + ε.
func (b SpikeBaseline) UCL() float64 {
	sd := math.Sqrt(b.Variance)
	if floor := math.Sqrt(b.Mean); floor > sd {
		sd = floor
	}
	return b.Mean + SpikeSigma*sd
}

// Exceeds is the SPC verdict for one window count: true when the count is a
// statistically significant excursion above this org's own baseline.
//
//   - Below the absolute floor (SpikeThreshold) → never a spike.
//   - Warm-up (fewer than SpikeMinSamples observations) → fixed-threshold
//     fallback, i.e. the floor alone decides. This is exactly the Phase 6
//     rule, so a zero-value SpikeBaseline reproduces legacy behaviour.
//   - Ready → the count must breach the upper control limit mean + kσ.
func (b SpikeBaseline) Exceeds(count int) bool {
	if count < SpikeThreshold {
		return false
	}
	if !b.Ready() {
		return true
	}
	return float64(count) > b.UCL()
}

// Apply evaluates both rules against a single tick's inputs and returns the
// triggers to fire. It is pure: baseline is passed by value and never
// mutated — the caller (Detector.tick) folds the tick's observation into
// its own copy separately, after evaluation, so each tick is judged against
// the baseline built from prior ticks only.
//
//   - Rule 1 (fatal_level): one trigger per row in fatals. The caller passes
//     in fatals already filtered to rows newer than lastSeen.
//   - Rule 2 (error_rate_spike): one trigger when the current window count is
//     a statistical excursion above the org's baseline (baseline.Exceeds —
//     mean + SpikeSigma·σ control limit with a SpikeThreshold floor) AND the
//     cooldown has elapsed AND no fatal-level trigger fired this tick (when
//     fatals is non-empty the per-row rule already covers the spike; we don't
//     want a double-fire that creates two workflow runs).
func Apply(
	orgID, workspaceID string,
	lastSeen time.Time,
	lastTriggerAt time.Time,
	fatals []domain.IncidentRow,
	recentCount int,
	baseline SpikeBaseline,
	now time.Time,
) []domain.IncidentTrigger {
	out := make([]domain.IncidentTrigger, 0, len(fatals)+1)
	for _, r := range fatals {
		out = append(out, domain.IncidentTrigger{
			OrgID: orgID, WorkspaceID: workspaceID, IncidentID: r.ID,
			IncidentRawID: r.ID,
			SourceEventID: r.SourceEventID,
			Rule:          "fatal_level", DetectedAt: now, ReceivedAt: r.ReceivedAt,
			Title:       r.Title,
			Service:     r.Service,
			Environment: r.Environment,
			Stacktrace:  r.Stacktrace,
			Logs:        r.Logs,
			// Fingerprint carried forward so the router can resolve a
			// project without re-reading the incidents_raw row. Source
			// matches the persisted source string ("sentry"|"datadog"|
			// "pagerduty"); each row only fills its own native fields.
			Source:                 r.Source,
			SentryOrganizationSlug: r.SentryOrganizationSlug,
			SentryProjectSlug:      r.SentryProjectSlug,
			DatadogServiceTag:      r.DatadogServiceTag,
			PagerDutyServiceID:     r.PagerDutyServiceID,
			GitHubRepo:             r.GitHubRepo,
		})
	}
	cooldownOK := lastTriggerAt.IsZero() || now.Sub(lastTriggerAt) > SpikeCooldown
	if baseline.Exceeds(recentCount) && cooldownOK && len(fatals) == 0 {
		out = append(out, domain.IncidentTrigger{
			OrgID: orgID, WorkspaceID: workspaceID,
			Rule: "error_rate_spike", DetectedAt: now, ReceivedAt: now,
		})
	}
	return out
}
