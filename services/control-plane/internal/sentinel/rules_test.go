package sentinel

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

var t0 = time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)

// readyBaseline builds a Ready SpikeBaseline with the given EWMA mean and
// variance, bypassing Observe so tests can pin exact control limits.
func readyBaseline(mean, variance float64) SpikeBaseline {
	return SpikeBaseline{Mean: mean, Variance: variance, Samples: SpikeMinSamples}
}

func TestApply_FatalLevel_PerRow(t *testing.T) {
	fatals := []domain.IncidentRow{
		{ID: "i1", ReceivedAt: t0},
		{ID: "i2", ReceivedAt: t0.Add(time.Second)},
	}
	out := Apply("o1", "w1", t0.Add(-time.Hour), time.Time{}, fatals, 0, SpikeBaseline{}, t0.Add(time.Minute))
	require.Len(t, out, 2)
	require.Equal(t, "fatal_level", out[0].Rule)
	require.Equal(t, "i1", out[0].IncidentID)
	require.Equal(t, "o1", out[0].OrgID)
	require.Equal(t, "w1", out[0].WorkspaceID)
}

func TestApply_FatalLevel_EmptyNoTriggers(t *testing.T) {
	out := Apply("o1", "w1", t0, time.Time{}, nil, 0, SpikeBaseline{}, t0)
	require.Empty(t, out)
}

func TestApply_Spike_TriggersWhenAboveThresholdAndCooldownClear(t *testing.T) {
	// recentCount above the warm-up threshold, no fatals, lastTrigger far in
	// the past, zero-value baseline (warm-up → fixed-threshold fallback).
	out := Apply("o1", "w1",
		t0, t0.Add(-10*time.Minute), nil, SpikeThreshold+2, SpikeBaseline{}, t0)
	require.Len(t, out, 1)
	require.Equal(t, "error_rate_spike", out[0].Rule)
	require.Equal(t, "", out[0].IncidentID) // spike rule has no row-level id
}

func TestApply_Spike_SuppressedDuringCooldown(t *testing.T) {
	// Same input as above except lastTriggerAt is 1 minute ago — inside the
	// 5-minute cooldown.
	out := Apply("o1", "w1",
		t0, t0.Add(-1*time.Minute), nil, SpikeThreshold+2, SpikeBaseline{}, t0)
	require.Empty(t, out)
}

func TestApply_Spike_BelowThreshold(t *testing.T) {
	out := Apply("o1", "w1",
		t0, time.Time{}, nil, SpikeThreshold-1, SpikeBaseline{}, t0)
	require.Empty(t, out)
}

func TestApply_Spike_SuppressedWhenFatalsAlsoFire(t *testing.T) {
	// When fatals are present the per-row rule already covers the spike — no
	// double-fire.
	fatals := []domain.IncidentRow{{ID: "i1", ReceivedAt: t0}}
	out := Apply("o1", "w1",
		t0.Add(-time.Hour), time.Time{}, fatals, SpikeThreshold+2, SpikeBaseline{}, t0)
	require.Len(t, out, 1)
	require.Equal(t, "fatal_level", out[0].Rule)
}

func TestApply_Spike_CooldownZeroValueAllowed(t *testing.T) {
	// The Detector struct zero-initialises lastTriggered[org] to time.Time{};
	// the Apply function must treat that as "never fired" and not require a
	// 5-minute cooldown gap from the unix epoch.
	out := Apply("o1", "w1",
		t0, time.Time{}, nil, SpikeThreshold, SpikeBaseline{}, t0)
	require.Len(t, out, 1)
	require.Equal(t, "error_rate_spike", out[0].Rule)
}

// --- SPC behaviour -----------------------------------------------------------

func TestApply_Spike_SPCSuppressedForNoisyBaseline(t *testing.T) {
	// The key change vs the Phase 6 fixed threshold: an org whose NORMAL rate
	// is far above SpikeThreshold must not page on a routine window. Baseline
	// mean 50, σ 10 (variance 100, above the √50 Poisson floor) → UCL = 80.
	// A count of 60 is well over the old fixed threshold of 5 but inside the
	// control limit — no trigger.
	out := Apply("o1", "w1",
		t0, time.Time{}, nil, 60, readyBaseline(50, 100), t0)
	require.Empty(t, out)
}

func TestApply_Spike_SPCFiresAboveControlLimit(t *testing.T) {
	// Same baseline as above (UCL = 50 + 3·10 = 80); a count of 81 is a
	// three-sigma excursion → trigger.
	out := Apply("o1", "w1",
		t0, time.Time{}, nil, 81, readyBaseline(50, 100), t0)
	require.Len(t, out, 1)
	require.Equal(t, "error_rate_spike", out[0].Rule)
}

func TestApply_Spike_SPCRespectsCooldown(t *testing.T) {
	// A statistically valid excursion is still suppressed inside the cooldown.
	out := Apply("o1", "w1",
		t0, t0.Add(-time.Minute), nil, 500, readyBaseline(50, 100), t0)
	require.Empty(t, out)
}

func TestSpikeBaseline_PoissonFloorForUnderdispersedHistory(t *testing.T) {
	// A perfectly steady history (variance 0) must not produce a hair-trigger
	// limit of mean+ε: the c-chart Poisson floor √mean applies, so for mean
	// 100 the UCL is 100 + 3·√100 = 130.
	b := readyBaseline(100, 0)
	require.InDelta(t, 130.0, b.UCL(), 1e-9)
	require.False(t, b.Exceeds(130))
	require.True(t, b.Exceeds(131))
}

func TestSpikeBaseline_FloorAppliesEvenWhenReady(t *testing.T) {
	// Quiet org: mean≈0 ⇒ UCL≈0, so any count breaches the statistical limit —
	// but counts below the absolute floor must never fire.
	b := readyBaseline(0, 0)
	require.False(t, b.Exceeds(SpikeThreshold-1))
	require.True(t, b.Exceeds(SpikeThreshold))
}

func TestSpikeBaseline_WarmupFallsBackToFixedThreshold(t *testing.T) {
	// Below SpikeMinSamples the control limit is not trusted: the fixed
	// threshold decides, even when the partial mean is high.
	b := SpikeBaseline{Mean: 50, Variance: 100, Samples: SpikeMinSamples - 1}
	require.False(t, b.Ready())
	require.True(t, b.Exceeds(SpikeThreshold))
	require.False(t, b.Exceeds(SpikeThreshold-1))
}

func TestSpikeBaseline_ObserveSeedsOnFirstSample(t *testing.T) {
	// The first observation seeds the mean directly instead of decaying up
	// from zero — otherwise the baseline would sit far below the true rate
	// for the EWMA's whole memory span.
	b := SpikeBaseline{}.Observe(50)
	require.Equal(t, 1, b.Samples)
	require.InDelta(t, 50.0, b.Mean, 1e-9)
	require.InDelta(t, 0.0, b.Variance, 1e-9)
}

func TestSpikeBaseline_ObserveEWMAUpdate(t *testing.T) {
	// Hand-computed exponentially weighted Welford step from a seeded state:
	//   diff = 60 − 50 = 10; incr = α·diff
	//   mean'     = 50 + α·10
	//   variance' = (1−α)·(0 + 10·α·10)
	b := SpikeBaseline{}.Observe(50).Observe(60)
	require.Equal(t, 2, b.Samples)
	require.InDelta(t, 50+SpikeEWMAAlpha*10, b.Mean, 1e-9)
	require.InDelta(t, (1-SpikeEWMAAlpha)*(SpikeEWMAAlpha*100), b.Variance, 1e-9)
}

func TestSpikeBaseline_ConvergesToSustainedRate(t *testing.T) {
	// Feeding a constant rate drives the mean to that rate and the variance
	// to zero — the definition of "this is the org's normal".
	b := SpikeBaseline{}
	for i := 0; i < 500; i++ {
		b = b.Observe(40)
	}
	require.True(t, b.Ready())
	require.InDelta(t, 40.0, b.Mean, 1e-6)
	require.InDelta(t, 0.0, b.Variance, 1e-6)
	// UCL collapses to the Poisson floor: 40 + 3·√40.
	require.InDelta(t, 40+SpikeSigma*math.Sqrt(40), b.UCL(), 1e-6)
}
