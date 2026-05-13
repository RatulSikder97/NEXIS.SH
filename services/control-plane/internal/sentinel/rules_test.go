package sentinel

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

var t0 = time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)

func TestApply_FatalLevel_PerRow(t *testing.T) {
	fatals := []domain.IncidentRow{
		{ID: "i1", ReceivedAt: t0},
		{ID: "i2", ReceivedAt: t0.Add(time.Second)},
	}
	out := Apply("o1", "w1", t0.Add(-time.Hour), time.Time{}, fatals, 0, t0.Add(time.Minute))
	require.Len(t, out, 2)
	require.Equal(t, "fatal_level", out[0].Rule)
	require.Equal(t, "i1", out[0].IncidentID)
	require.Equal(t, "o1", out[0].OrgID)
	require.Equal(t, "w1", out[0].WorkspaceID)
}

func TestApply_FatalLevel_EmptyNoTriggers(t *testing.T) {
	out := Apply("o1", "w1", t0, time.Time{}, nil, 0, t0)
	require.Empty(t, out)
}

func TestApply_Spike_TriggersWhenAboveThresholdAndCooldownClear(t *testing.T) {
	// recentCount above threshold, no fatals, lastTrigger far in the past.
	out := Apply("o1", "w1",
		t0, t0.Add(-10*time.Minute), nil, SpikeThreshold+2, t0)
	require.Len(t, out, 1)
	require.Equal(t, "error_rate_spike", out[0].Rule)
	require.Equal(t, "", out[0].IncidentID) // spike rule has no row-level id
}

func TestApply_Spike_SuppressedDuringCooldown(t *testing.T) {
	// Same input as above except lastTriggerAt is 1 minute ago — inside the
	// 5-minute cooldown.
	out := Apply("o1", "w1",
		t0, t0.Add(-1*time.Minute), nil, SpikeThreshold+2, t0)
	require.Empty(t, out)
}

func TestApply_Spike_BelowThreshold(t *testing.T) {
	out := Apply("o1", "w1",
		t0, time.Time{}, nil, SpikeThreshold-1, t0)
	require.Empty(t, out)
}

func TestApply_Spike_SuppressedWhenFatalsAlsoFire(t *testing.T) {
	// When fatals are present the per-row rule already covers the spike — no
	// double-fire.
	fatals := []domain.IncidentRow{{ID: "i1", ReceivedAt: t0}}
	out := Apply("o1", "w1",
		t0.Add(-time.Hour), time.Time{}, fatals, SpikeThreshold+2, t0)
	require.Len(t, out, 1)
	require.Equal(t, "fatal_level", out[0].Rule)
}

func TestApply_Spike_CooldownZeroValueAllowed(t *testing.T) {
	// The Detector struct zero-initialises lastTriggered[org] to time.Time{};
	// the Apply function must treat that as "never fired" and not require a
	// 5-minute cooldown gap from the unix epoch.
	out := Apply("o1", "w1",
		t0, time.Time{}, nil, SpikeThreshold, t0)
	require.Len(t, out, 1)
	require.Equal(t, "error_rate_spike", out[0].Rule)
}
