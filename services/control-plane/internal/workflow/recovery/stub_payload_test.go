package recovery

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// TestStubToolCalls_ShapePerAgent verifies every agent role gets a non-empty
// tool_calls array with the expected keys. The drill-down UI keys off this
// list, so a regression here would surface as blank panels.
func TestStubToolCalls_ShapePerAgent(t *testing.T) {
	roles := []domain.AgentRole{
		domain.AgentBackend, domain.AgentQA, domain.AgentArchitect,
		domain.AgentDevOps, domain.AgentDataEngineer,
		domain.AgentPathfinder, domain.AgentSynthesiser,
		domain.AgentRole("validator_l2"),
		domain.AgentSentinel, domain.AgentApprovalGate,
	}
	for _, role := range roles {
		role := role
		t.Run(string(role), func(t *testing.T) {
			calls := stubToolCalls(role, "handler/foo.go:42 something went wrong")
			require.NotEmpty(t, calls, "expected non-empty tool_calls for %s", role)
			for _, c := range calls {
				require.Contains(t, c, "tool", "missing tool key for %s", role)
				require.Contains(t, c, "args", "missing args key for %s", role)
				require.Contains(t, c, "result", "missing result key for %s", role)
				assert.NotEmpty(t, c["tool"], "empty tool name for %s", role)
			}
		})
	}
}

// TestStubTokenEstimate_PerLayer asserts the constant token bucket per the
// spec — 256 for L1 specialists, 128 for L2 detectors, 64 for routers.
func TestStubTokenEstimate_PerLayer(t *testing.T) {
	cases := []struct {
		role domain.AgentRole
		want int
	}{
		{domain.AgentArchitect, 256},
		{domain.AgentBackend, 256},
		{domain.AgentQA, 256},
		{domain.AgentDevOps, 256},
		{domain.AgentDataEngineer, 256},
		{domain.AgentPathfinder, 128},
		{domain.AgentSynthesiser, 128},
		{domain.AgentSentinel, 128},
		{domain.AgentApprovalGate, 64},
		{domain.AgentPipeline, 64},
	}
	for _, c := range cases {
		c := c
		t.Run(string(c.role), func(t *testing.T) {
			assert.Equal(t, c.want, stubTokenEstimate(c.role))
		})
	}
}

// TestEnrichStubFallback_FillsRequiredKeys verifies every required key per
// the spec lands on a stub-fallback payload — agent_role, degraded,
// degrade_reason, tokens_in, tokens_out, cost_cents, duration_ms, model,
// tool_calls, output_summary, input_summary.
func TestEnrichStubFallback_FillsRequiredKeys(t *testing.T) {
	payload := map[string]any{}
	start := time.Now().Add(-50 * time.Millisecond)
	enrichStubFallback(payload, domain.AgentBackend,
		"NullPointerException in handler/foo.go:42",
		"openai 401 unauthorized", start, domain.AgentNameBackend)

	required := []string{
		"agent_role", "degraded", "degrade_reason",
		"tokens_in", "tokens_out", "cost_cents", "duration_ms",
		"model", "tool_calls", "output_summary", "input_summary",
	}
	for _, key := range required {
		_, ok := payload[key]
		assert.Truef(t, ok, "missing required key %q on stub-fallback payload", key)
	}
	assert.Equal(t, "stub-fallback", payload["model"])
	assert.Equal(t, "backend", payload["agent_role"])
	assert.Equal(t, true, payload["degraded"])
	assert.Equal(t, "openai 401 unauthorized", payload["degrade_reason"])
	assert.Equal(t, 256, payload["tokens_out"])
	assert.Equal(t, 0, payload["cost_cents"])
	assert.NotEmpty(t, payload["tool_calls"])
	assert.NotEmpty(t, payload["output_summary"])
	durMs, _ := payload["duration_ms"].(int64)
	assert.GreaterOrEqual(t, durMs, int64(50), "duration_ms should reflect real elapsed wallclock")
}

// TestSummariseAgentInput_Truncates verifies the input_summary stays under
// 200 chars even when the incident description is huge.
func TestSummariseAgentInput_Truncates(t *testing.T) {
	huge := strings.Repeat("A", 1000)
	in := PipelineInput{
		Incident: &domain.IncidentPayload{
			Title:   huge,
			Service: "checkout-api",
		},
	}
	got := summariseAgentInput(in, domain.AgentNameBackend)
	assert.LessOrEqual(t, len(got), 200, "summary should be <=200 chars")
	assert.True(t, strings.HasSuffix(got, "..."), "long summaries should end with ellipsis")
}

// TestSummariseAgentInput_FallbackWhenIncidentNil verifies the fallback path
// still produces a useful summary (so input_summary is never empty).
func TestSummariseAgentInput_FallbackWhenIncidentNil(t *testing.T) {
	in := PipelineInput{
		RunID:      "run-abc",
		IncidentID: "demo",
		TriggeredBy: "demo",
	}
	got := summariseAgentInput(in, domain.AgentNameBackend)
	assert.Contains(t, got, "demo")
	assert.NotEmpty(t, got)
}

// TestEstimateTokensFromText_LinearScale sanity checks the len/4 heuristic
// matches the spec.
func TestEstimateTokensFromText_LinearScale(t *testing.T) {
	assert.Equal(t, 0, estimateTokensFromText(""))
	assert.Equal(t, 25, estimateTokensFromText(strings.Repeat("a", 100)))
}
