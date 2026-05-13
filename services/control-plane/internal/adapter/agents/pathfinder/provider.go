// Package pathfinder implements the L2 Pathfinder agent. It walks the
// Neo4j codegraph from the incident's last stack frame to a candidate
// root-cause symbol, asks the DoWhy sidecar (via domain.CausalEngine) for a
// hypothesis + confidence, and returns the result for the Synthesiser to
// route on. The LLM refinement layer is optional and gated by
// PATHFINDER_LLM_REFINE=1.
//
// Architectural note: this adapter depends only on the domain.Graph and
// domain.CausalEngine ports — the concrete Neo4j driver and the gRPC/HTTP
// causal client live in internal/adapter/graphstore/ and
// internal/adapter/causal/. The go-arch-lint config forbids those adapter
// packages from being imported here; everything flows through the port.
package pathfinder

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/agents"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// Provider is the concrete L2 Pathfinder implementation. Wire one of these
// in cmd/server/main.go after the Graph + CausalEngine adapters are
// constructed, then register under domain.AgentNamePathfinder via
// init_l2.NewL2Agents(...).
//
// Graph may be nil — the Pathfinder degrades to "causal-only" evidence and
// flags graph_available=false in the structured output. CausalEngine may
// also be nil (CAUSAL_ENABLED=0), in which case the agent returns a
// scenario:"unknown" placeholder with confidence=0 so the Synthesiser still
// has a deterministic route to fall back on.
type Provider struct {
	LLM       *agents.LLMClient // shared LLMClient used for the optional refinement pass + budget plumbing
	Graph     domain.Graph      // may be nil — Pathfinder must tolerate
	Causal    domain.CausalEngine // may be nil — Pathfinder must tolerate
	Model     string            // resolved per-provider by main.go; only used when LLMRefine is true
	LLMRefine bool              // mirrors config.PathfinderLLMRefine
	MaxHops   int               // edge traversal depth; default 2 (matches spec §6.2)
	Limit     int               // edge traversal cap; default 20 (matches spec §6.2)
}

// New returns a Provider with safe defaults. Both graph and causal are
// optional — main.go passes nil for either when their adapters fail to
// boot. The traversal knobs are exposed for the eval harness to vary in
// future phases.
func New(llm *agents.LLMClient, graph domain.Graph, causal domain.CausalEngine, model string, llmRefine bool) *Provider {
	return &Provider{
		LLM:       llm,
		Graph:     graph,
		Causal:    causal,
		Model:     model,
		LLMRefine: llmRefine,
		MaxHops:   2,
		Limit:     20,
	}
}

func (p *Provider) Name() domain.AgentName { return domain.AgentNamePathfinder }

// Run executes the Phase 6 §6.2 flow end-to-end. The output Structured map
// is always populated (even when the graph + causal adapters are absent),
// so the Synthesiser fast-path can read evidence_chain unconditionally.
func (p *Provider) Run(ctx context.Context, in domain.AgentInput) (domain.AgentOutput, error) {
	start := time.Now()
	inc := in.Incident
	if inc == nil {
		inc = &domain.IncidentPayload{Title: "Unspecified incident"}
	}

	// ----- 1) Parse stack frame: last (file_path, line) pair from the trace.
	filePath, line := extractLastPythonFrame(inc.Stacktrace)

	// ----- 2 + 3) Walk the graph for the containing symbol + 1-2 hop neighbours.
	var (
		rootCauseNode  string
		evidence       []string
		graphAvailable = p.Graph != nil
	)
	if p.Graph != nil && filePath != "" {
		sym, err := p.Graph.FindSymbolContaining(ctx, in.OrgID, in.RepoSHA, filePath, line)
		if err == nil {
			rootCauseNode = sym.Name
			evidence = append(evidence, fmt.Sprintf("symbol=%s in %s:%d-%d", sym.Name, sym.FilePath, sym.LineStart, sym.LineEnd))

			neigh, nerr := p.Graph.Neighbours(ctx, sym, []domain.GraphEdgeKind{domain.GraphEdgeCalls, domain.GraphEdgeRaised}, p.MaxHops, p.Limit)
			if nerr == nil {
				for _, e := range neigh {
					evidence = append(evidence, fmt.Sprintf("%s->%s(%s)", e.From.Name, e.To.Name, e.Kind))
				}
			}
		} else if !errors.Is(err, domain.ErrNotFound) {
			// Real driver error — record it as evidence but do not abort.
			evidence = append(evidence, fmt.Sprintf("graph_error=%v", err))
		}
	}

	// Always include the raw stacktrace tokens as fallback evidence; the
	// Synthesiser's fast-path regex matches against these. Without this,
	// a graph-disabled deployment would always route to "unknown".
	evidence = append(evidence, stacktraceTokens(inc.Stacktrace)...)

	// ----- 4) Call the causal sidecar.
	var (
		hypothesis      string
		confidence      float32
		estimandName    string
		causalAvailable = p.Causal != nil
	)
	if p.Causal != nil {
		cres, cerr := p.Causal.Infer(ctx, domain.CausalQuery{
			IncidentID:    in.WorkflowRunID,
			Stacktrace:    inc.Stacktrace,
			RootCauseNode: rootCauseNode,
			Features:      evidence,
		})
		if cerr != nil {
			// Sidecar unavailable: degrade gracefully. The agent still
			// succeeds so the workflow can take the "unknown" route.
			evidence = append(evidence, fmt.Sprintf("causal_error=%v", cerr))
			causalAvailable = false
		} else {
			hypothesis = cres.Hypothesis
			confidence = cres.Confidence
			estimandName = cres.EstimandName
			if len(cres.Evidence) > 0 {
				evidence = append(evidence, cres.Evidence...)
			}
		}
	}
	if hypothesis == "" {
		hypothesis = "no causal hypothesis available"
	}

	// ----- 5) Optional LLM refinement (off by default).
	var (
		llmRes        agents.InvokeResult
		llmErr        error
		systemPrompt  string
		userPrompt    string
		schemaRetries int
	)
	if p.LLMRefine && p.LLM != nil {
		var ubuf bytes.Buffer
		if terr := template.Must(template.New("u").Parse(UserTemplate)).Execute(&ubuf, map[string]any{
			"Hypothesis": hypothesis,
			"Confidence": confidence,
			"Evidence":   strings.Join(evidence, "\n- "),
			"Stacktrace": inc.Stacktrace,
		}); terr != nil {
			return domain.AgentOutput{}, terr
		}
		systemPrompt = SystemPrompt
		userPrompt = ubuf.String()
		llmRes, llmErr = p.LLM.Invoke(ctx, agents.InvokeRequest{
			Agent:             domain.AgentNamePathfinder,
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
				return agents.Validate(SchemaRefineJSON, content)
			},
		})
		if llmErr == nil {
			if h, ok := llmRes.Structured["hypothesis"].(string); ok && h != "" {
				hypothesis = h
			}
			if c, ok := llmRes.Structured["confidence"].(float64); ok {
				confidence = float32(c)
			}
			schemaRetries = llmRes.SchemaRetries
		}
		// On LLM error we keep the canned hypothesis — refinement is opt-in
		// and best-effort by design.
	}

	structured := map[string]any{
		"root_cause_node":   rootCauseNode,
		"hypothesis":        hypothesis,
		"confidence":        float64(confidence),
		"evidence_chain":    evidence,
		"estimand_name":     estimandName,
		"graph_available":   graphAvailable,
		"causal_available":  causalAvailable,
	}

	return domain.AgentOutput{
		Success:       true,
		Content:       llmRes.Content, // empty unless refinement ran
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

// extractLastPythonFrame walks the stacktrace and returns the (file, line)
// pair from the deepest `File "...", line N` frame. Returns ("", 0) when
// the trace is non-Python or empty. The fixture incidents all use
// CPython's traceback format; non-Python stacks fall through to a
// causal-only path.
//
// Phase 7 swaps this for a multi-language parser.
var pyFrameRE = regexp.MustCompile(`File "([^"]+)", line (\d+)`)

func extractLastPythonFrame(trace string) (string, int) {
	if trace == "" {
		return "", 0
	}
	matches := pyFrameRE.FindAllStringSubmatch(trace, -1)
	if len(matches) == 0 {
		return "", 0
	}
	last := matches[len(matches)-1]
	line, _ := strconv.Atoi(last[2])
	return last[1], line
}

// stacktraceTokens splits the trace into the small set of identifiers the
// Synthesiser's fast-path classifier matches against. We deliberately keep
// the token set minimal — the goal is to surface error class names and
// suspicious phrases (NoneType, OperationalError, MemoryError, etc.) so
// the regex routing in synthesiser.routes.classify can fire.
func stacktraceTokens(trace string) []string {
	if trace == "" {
		return nil
	}
	// Collect every word that looks like CamelCase or contains a colon —
	// both forms cover Python exception names ("ZeroDivisionError",
	// "psycopg2.errors.UndefinedColumn") and Linux kernel signals
	// ("OOMKilled", "Killed").
	tokenRE := regexp.MustCompile(`[A-Z][A-Za-z0-9_.]+`)
	uniq := make(map[string]struct{}, 8)
	out := make([]string, 0, 8)
	for _, t := range tokenRE.FindAllString(trace, -1) {
		if _, seen := uniq[t]; seen {
			continue
		}
		uniq[t] = struct{}{}
		out = append(out, t)
	}
	return out
}
