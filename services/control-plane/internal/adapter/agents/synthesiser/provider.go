// Package synthesiser implements the L2 Synthesiser agent. It reads the
// Pathfinder output, classifies the incident into one of four scenarios
// (null_deref, schema_drift, oom, unknown), and emits the ordered list of
// L1 agents the workflow should invoke.
//
// Classification is two-stage:
//
//  1. Fast path — token-regex matches on pathfinder.evidence_chain (and the
//     stacktrace fallback baked in by Pathfinder). Cheap, deterministic,
//     covers the three demo scenarios.
//  2. LLM fallback — when no fast-path token matches, invoke the Phase 5
//     LLM spine with the schema-validated classification prompt. Falls back
//     to scenario:"unknown" if the LLM is unavailable or returns malformed
//     JSON after retries.
//
// The package depends only on the agents.LLMClient + the domain layer — no
// cross-adapter imports.
package synthesiser

import (
	"bytes"
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"text/template"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/agents"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// Scenario is the canonical classification label. The string values match
// the JSON wire shape (snake_case) and the SchemaJSON enum exactly.
type Scenario string

const (
	ScenarioNullDeref   Scenario = "null_deref"
	ScenarioSchemaDrift Scenario = "schema_drift"
	ScenarioOOM         Scenario = "oom"
	ScenarioUnknown     Scenario = "unknown"
)

// routingTable is the scenario → ordered L1 agent list defined by Phase 6
// §7.1. The workflow consumes selected_agents in order; skipped_agents is
// the canonical-AllL1Agents \ selected set, used by the timeline UI to
// grey out unused agents.
var routingTable = map[Scenario][]domain.AgentName{
	ScenarioNullDeref:   {domain.AgentNameArchitect, domain.AgentNameBackend, domain.AgentNameQA},
	ScenarioSchemaDrift: {domain.AgentNameArchitect, domain.AgentNameDataEngineer, domain.AgentNameBackend, domain.AgentNameQA},
	ScenarioOOM:         {domain.AgentNameArchitect, domain.AgentNameDevOps, domain.AgentNameBackend},
	ScenarioUnknown:     {domain.AgentNameArchitect, domain.AgentNameBackend, domain.AgentNameQA},
}

// estimatedDurations is a rough per-scenario duration the UI uses for the
// progress bar before the L1 agents start emitting their own timings. The
// values are Phase 6 placeholders; Phase 8 calibrates against the eval
// harness median wall-clock.
var estimatedDurations = map[Scenario]int{
	ScenarioNullDeref:   180_000,
	ScenarioSchemaDrift: 240_000,
	ScenarioOOM:         200_000,
	ScenarioUnknown:     180_000,
}

// Fast-path regex set. Each rule pairs (scenario, compiled token regex).
// classify() walks the rules in order and returns the first match. Order
// matters when scenarios overlap (schema_drift before null_deref because
// SQL errors usually print NoneType-looking traces too).
var fastPathRules = []struct {
	scenario Scenario
	pattern  *regexp.Regexp
}{
	// schema_drift — psycopg2 + SQLAlchemy column / relation errors.
	{ScenarioSchemaDrift, regexp.MustCompile(`(?i)(UndefinedColumn|UndefinedTable|OperationalError.*column|column .* does not exist|relation .* does not exist|psycopg2\.errors)`)},
	// oom — kernel and Python memory exhaustion.
	{ScenarioOOM, regexp.MustCompile(`(?i)(MemoryError|OOMKilled|out of memory|memory_pressure)`)},
	// null_deref — Python NoneType + Go nil pointer.
	{ScenarioNullDeref, regexp.MustCompile(`(?i)(NoneType.*attribute|AttributeError.*NoneType|nil pointer dereference|NullReferenceException)`)},
}

// Provider is the concrete Synthesiser L2 implementation. LLM may be nil
// (eval harness, tests) — in that case the fast-path failures fall through
// directly to ScenarioUnknown rather than attempting the model call.
type Provider struct {
	LLM   *agents.LLMClient
	Model string
}

func New(llm *agents.LLMClient, model string) *Provider {
	return &Provider{LLM: llm, Model: model}
}

func (p *Provider) Name() domain.AgentName { return domain.AgentNameSynthesiser }

// Run is the Synthesiser activity entry point. AgentInput.PriorOutputs
// must contain the Pathfinder structured map under the "pathfinder" key.
// When absent, the agent routes to ScenarioUnknown with source="fallback"
// so the workflow still has a deterministic L1 ordering.
func (p *Provider) Run(ctx context.Context, in domain.AgentInput) (domain.AgentOutput, error) {
	start := time.Now()

	pathfinder := extractPathfinder(in.PriorOutputs)

	// 1) Fast path on evidence_chain + the stacktrace fallback.
	scenario, fastRationale, fastConf, matched := classifyFast(pathfinder)
	source := "fast_path"

	// 2) LLM fallback when the fast path didn't fire.
	var (
		llmRes        agents.InvokeResult
		llmErr        error
		systemPrompt  string
		userPrompt    string
		llmRationale  string
		llmConfidence float64
		schemaRetries int
	)
	if !matched {
		if p.LLM != nil {
			systemPrompt = SystemPrompt
			var ubuf bytes.Buffer
			if terr := template.Must(template.New("u").Parse(UserTemplate)).Execute(&ubuf, map[string]any{
				"Hypothesis":    pathfinder.Hypothesis,
				"Confidence":    pathfinder.Confidence,
				"RootCauseNode": pathfinder.RootCauseNode,
				"Evidence":      strings.Join(pathfinder.Evidence, "\n- "),
			}); terr != nil {
				return domain.AgentOutput{}, terr
			}
			userPrompt = ubuf.String()
			llmRes, llmErr = p.LLM.Invoke(ctx, agents.InvokeRequest{
				Agent:             domain.AgentNameSynthesiser,
				Model:             p.Model,
				System:            SystemPrompt,
				User:              userPrompt,
				MaxTokens:         400,
				JSONResponse:      true,
				CacheSystem:       true,
				WorkflowRunID:     in.WorkflowRunID,
				OrgID:             in.OrgID,
				EstimatedTokensIn: 600,
				Validate: func(content string) (map[string]any, error) {
					return agents.Validate(SchemaLLMJSON, content)
				},
			})
			if llmErr == nil {
				if s, ok := llmRes.Structured["scenario"].(string); ok {
					scenario = Scenario(s)
				}
				if c, ok := llmRes.Structured["confidence"].(float64); ok {
					llmConfidence = c
				}
				if r, ok := llmRes.Structured["rationale"].(string); ok {
					llmRationale = r
				}
				schemaRetries = llmRes.SchemaRetries
				source = "llm"
			} else {
				// LLM either ran out of retries or hit a budget cap.
				scenario = ScenarioUnknown
				llmRationale = "LLM classification failed; routed to unknown"
				source = "fallback"
			}
		} else {
			scenario = ScenarioUnknown
			llmRationale = "no LLM configured; routed to unknown"
			source = "fallback"
		}
	}

	selected := routingTable[scenario]
	if selected == nil {
		// Defence-in-depth: schema validation should already catch any
		// non-enum scenario but we don't want a nil map panic.
		scenario = ScenarioUnknown
		selected = routingTable[ScenarioUnknown]
	}
	skipped := skippedAgents(selected)

	// Confidence picks the strongest of (fast-path baseline, LLM output).
	confidence := fastConf
	if llmConfidence > confidence {
		confidence = llmConfidence
	}
	rationale := fastRationale
	if !matched {
		rationale = llmRationale
	}

	structured := map[string]any{
		"scenario":              string(scenario),
		"confidence":            confidence,
		"selected_agents":       toStringSlice(selected),
		"skipped_agents":        toStringSlice(skipped),
		"rationale":             rationale,
		"estimated_duration_ms": estimatedDurations[scenario],
		"source":                source,
	}

	return domain.AgentOutput{
		Success:       true,
		Content:       llmRes.Content,
		Structured:    structured,
		TokensIn:      llmRes.TokensIn,
		TokensOut:     llmRes.TokensOut,
		CachedTokens:  llmRes.CachedTokens,
		CostCents:     llmRes.CostCents,
		DurationMs:    time.Since(start).Milliseconds(),
		Model:         llmRes.Model,
		Provider:      llmRes.Provider,
		SchemaRetries: schemaRetries,
		SystemPrompt:  systemPrompt,
		UserPrompt:    userPrompt,
	}, nil
}

// pathfinderView is the subset of Pathfinder structured output the
// Synthesiser cares about. Lifted out of the raw map so the classifier
// signature stays small.
type pathfinderView struct {
	Hypothesis    string
	Confidence    float64
	RootCauseNode string
	Evidence      []string
}

func extractPathfinder(prior map[string]any) pathfinderView {
	if prior == nil {
		return pathfinderView{}
	}
	raw, ok := prior["pathfinder"]
	if !ok {
		return pathfinderView{}
	}
	// AgentOutput.Structured arrives as map[string]any; AgentOutput as a
	// whole may also arrive (post-JSON-roundtrip the workflow stores the
	// full AgentOutput). Handle both — pull the Structured submap when
	// present, otherwise treat raw as Structured directly.
	asMap, _ := raw.(map[string]any)
	if asMap == nil {
		// Defensive JSON round-trip for callers that hand the workflow
		// the whole AgentOutput as a typed struct.
		bs, err := json.Marshal(raw)
		if err != nil {
			return pathfinderView{}
		}
		_ = json.Unmarshal(bs, &asMap)
	}
	if asMap == nil {
		return pathfinderView{}
	}
	if structured, ok := asMap["structured"].(map[string]any); ok {
		asMap = structured
	}

	view := pathfinderView{}
	if s, ok := asMap["hypothesis"].(string); ok {
		view.Hypothesis = s
	}
	if c, ok := asMap["confidence"].(float64); ok {
		view.Confidence = c
	}
	if s, ok := asMap["root_cause_node"].(string); ok {
		view.RootCauseNode = s
	}
	switch ev := asMap["evidence_chain"].(type) {
	case []string:
		view.Evidence = append(view.Evidence, ev...)
	case []any:
		for _, e := range ev {
			if s, ok := e.(string); ok {
				view.Evidence = append(view.Evidence, s)
			}
		}
	}
	return view
}

// classifyFast runs the regex rules against the joined evidence chain.
// Returns (scenario, rationale, confidence, matched). On match the
// confidence is a conservative 0.7 floor — high enough to skip the LLM
// fallback but not pretending to be model-grade.
func classifyFast(pv pathfinderView) (Scenario, string, float64, bool) {
	if len(pv.Evidence) == 0 && pv.Hypothesis == "" {
		return ScenarioUnknown, "", 0, false
	}
	haystack := strings.Join(pv.Evidence, "\n") + "\n" + pv.Hypothesis
	for _, rule := range fastPathRules {
		if rule.pattern.MatchString(haystack) {
			return rule.scenario, "fast-path token match: " + rule.scenario.toRationaleTag(), 0.7, true
		}
	}
	return ScenarioUnknown, "", 0, false
}

func (s Scenario) toRationaleTag() string {
	switch s {
	case ScenarioSchemaDrift:
		return "psycopg2 column/relation mismatch"
	case ScenarioNullDeref:
		return "NoneType / nil-pointer attribute access"
	case ScenarioOOM:
		return "memory exhaustion (MemoryError / OOMKilled)"
	default:
		return string(s)
	}
}

// skippedAgents returns AllL1Agents minus the selected set. Order follows
// the canonical DAG order so the timeline UI greys out greyed-out agents
// in the same order they would otherwise have run.
func skippedAgents(selected []domain.AgentName) []domain.AgentName {
	picked := make(map[domain.AgentName]struct{}, len(selected))
	for _, n := range selected {
		picked[n] = struct{}{}
	}
	out := make([]domain.AgentName, 0, len(domain.AllL1Agents))
	for _, n := range domain.AllL1Agents {
		if _, ok := picked[n]; ok {
			continue
		}
		out = append(out, n)
	}
	return out
}

func toStringSlice(names []domain.AgentName) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = string(n)
	}
	return out
}
