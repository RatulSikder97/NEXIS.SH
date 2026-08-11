package pathfinder

import (
	"context"
	"errors"
	"testing"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/agents"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// fakeLLM mirrors the architect test's stub: sequential canned responses,
// per-call counting. Reused here for the refinement-path test.
type fakeLLM struct {
	responses []domain.CompletionResponse
	calls     int
	last      domain.CompletionRequest
}

func (f *fakeLLM) Name() string { return "openai" }
func (f *fakeLLM) Info(_ context.Context) (domain.LLMInfo, error) {
	return domain.LLMInfo{Provider: "openai"}, nil
}
func (f *fakeLLM) Complete(_ context.Context, req domain.CompletionRequest) (domain.CompletionResponse, error) {
	f.last = req
	idx := f.calls
	f.calls++
	if idx >= len(f.responses) {
		idx = len(f.responses) - 1
	}
	return f.responses[idx], nil
}

// fakeGraph implements domain.Graph deterministically for unit tests.
type fakeGraph struct {
	sym       domain.GraphNode
	symErr    error
	neigh     []domain.GraphEdge
	neighErr  error
	findCalls int
}

func (g *fakeGraph) FindSymbolContaining(_ context.Context, _, _, _ string, _ int) (domain.GraphNode, error) {
	g.findCalls++
	if g.symErr != nil {
		return domain.GraphNode{}, g.symErr
	}
	return g.sym, nil
}
func (g *fakeGraph) Neighbours(_ context.Context, _ domain.GraphNode, _ []domain.GraphEdgeKind, _, _ int) ([]domain.GraphEdge, error) {
	if g.neighErr != nil {
		return nil, g.neighErr
	}
	return g.neigh, nil
}
func (g *fakeGraph) Upsert(_ context.Context, _ []domain.GraphEdge) error { return nil }
func (g *fakeGraph) Ping(_ context.Context) error                         { return nil }

// fakeCausal implements domain.CausalEngine. Returns the canned result and
// optionally an error.
type fakeCausal struct {
	result domain.CausalResult
	err    error
	calls  int
	last   domain.CausalQuery
}

func (c *fakeCausal) Infer(_ context.Context, q domain.CausalQuery) (domain.CausalResult, error) {
	c.calls++
	c.last = q
	if c.err != nil {
		return domain.CausalResult{}, c.err
	}
	return c.result, nil
}

const sampleStack = `Traceback (most recent call last):
  File "src/nexis_fixture/api.py", line 14, in get_user
    cur.execute('SELECT email_verified_at FROM users WHERE id=%s', (uid,))
psycopg2.errors.UndefinedColumn: column users.email_verified_at does not exist`

const nullDerefStack = `Traceback (most recent call last):
  File "src/nexis_fixture/api.py", line 22, in render
    return user.profile.name
AttributeError: 'NoneType' object has no attribute 'profile'`

const oomStack = `MemoryError: out of memory while loading 4GB tensor
container terminated with OOMKilled signal`

// TestPathfinder_HappyPath_GraphAndCausalWired runs the full
// graph + causal pipeline end-to-end and asserts the Structured output
// shape the Synthesiser expects.
func TestPathfinder_HappyPath_GraphAndCausalWired(t *testing.T) {
	graph := &fakeGraph{
		sym: domain.GraphNode{
			Kind: domain.GraphKindSymbol, Name: "get_user",
			FilePath: "src/nexis_fixture/api.py", LineStart: 10, LineEnd: 20,
		},
		neigh: []domain.GraphEdge{
			{
				From: domain.GraphNode{Name: "get_user"},
				To:   domain.GraphNode{Name: "UndefinedColumn"},
				Kind: domain.GraphEdgeRaised,
				Hops: 1,
			},
			{
				From: domain.GraphNode{Name: "get_user"},
				To:   domain.GraphNode{Kind: domain.GraphKindSymbol, Name: "load_columns"},
				Kind: domain.GraphEdgeCalls,
				Hops: 2,
			},
			{
				From: domain.GraphNode{Name: "get_user"},
				To:   domain.GraphNode{Kind: domain.GraphKindExceptionType, Name: "OperationalError"},
				Kind: domain.GraphEdgeRaised,
				Hops: 2,
			},
		},
	}
	causal := &fakeCausal{result: domain.CausalResult{
		Hypothesis:   "schema drift: users.email_verified_at column missing",
		Confidence:   0.91,
		EstimandName: "backdoor_adjustment_on_schema_state",
		Evidence:     []string{"column users.email_verified_at not present"},
	}}

	p := New(nil, graph, causal, "gpt-4o-mini", false)
	out, err := p.Run(context.Background(), domain.AgentInput{
		OrgID:         "org",
		WorkflowRunID: "wf-1",
		Incident: &domain.IncidentPayload{
			Title:      "schema-drift",
			Stacktrace: sampleStack,
		},
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !out.Success {
		t.Fatalf("expected success, got %+v", out)
	}
	if graph.findCalls != 1 {
		t.Errorf("expected graph.find called once, got %d", graph.findCalls)
	}
	if causal.calls != 1 {
		t.Errorf("expected causal.infer called once, got %d", causal.calls)
	}
	if causal.last.RootCauseNode != "get_user" {
		t.Errorf("rootCauseNode not propagated to causal: %+v", causal.last)
	}

	// Candidate set: symptom symbol at distance 0 with its subgraph degree,
	// Symbol-kind neighbours with hop distance + path multiplicity, and
	// ExceptionType neighbours excluded (they are evidence, not hypotheses).
	cands := causal.last.Candidates
	if len(cands) != 3 {
		t.Fatalf("expected 3 candidates (get_user, UndefinedColumn, load_columns), got %+v", cands)
	}
	root := cands[0]
	if root.Node != "get_user" || root.DistanceFromSymptom != 0 {
		t.Errorf("first candidate should be symptom symbol at distance 0: %+v", root)
	}
	if root.OutDegree != 1 {
		t.Errorf("root subgraph degree should count only 1-hop edges, got %d", root.OutDegree)
	}
	if len(root.Evidence) == 0 {
		t.Errorf("root candidate must carry its own evidence: %+v", root)
	}
	byName := map[string]domain.CausalCandidate{}
	for _, c := range cands {
		byName[c.Node] = c
	}
	uc, ok := byName["UndefinedColumn"]
	if !ok {
		t.Fatalf("UndefinedColumn (empty Kind -> Symbol fallback) missing: %+v", cands)
	}
	if uc.DistanceFromSymptom != 1 || uc.InDegree != 1 {
		t.Errorf("UndefinedColumn should be distance=1 in_degree=1, got %+v", uc)
	}
	lc, ok := byName["load_columns"]
	if !ok {
		t.Fatalf("load_columns Symbol neighbour missing: %+v", cands)
	}
	if lc.DistanceFromSymptom != 2 {
		t.Errorf("load_columns hop distance should be 2, got %+v", lc)
	}
	if _, present := byName["OperationalError"]; present {
		t.Errorf("ExceptionType neighbour must not become a candidate: %+v", cands)
	}

	rcn, _ := out.Structured["root_cause_node"].(string)
	if rcn != "get_user" {
		t.Errorf("expected root_cause_node=get_user, got %q", rcn)
	}
	hyp, _ := out.Structured["hypothesis"].(string)
	if hyp != "schema drift: users.email_verified_at column missing" {
		t.Errorf("hypothesis not propagated: %q", hyp)
	}
	conf, _ := out.Structured["confidence"].(float64)
	if conf < 0.9 {
		t.Errorf("confidence not propagated: %v", conf)
	}
	ev, _ := out.Structured["evidence_chain"].([]string)
	if len(ev) == 0 {
		t.Fatalf("evidence_chain empty: %+v", out.Structured)
	}
	if !out.Structured["graph_available"].(bool) {
		t.Errorf("graph_available should be true")
	}
	if !out.Structured["causal_available"].(bool) {
		t.Errorf("causal_available should be true")
	}
}

// TestPathfinder_GraphMissing — Neo4j adapter is nil at boot. Pathfinder
// must still succeed using stacktrace tokens + causal evidence so the
// Synthesiser fast-path can route on the trace alone.
func TestPathfinder_GraphMissing(t *testing.T) {
	causal := &fakeCausal{result: domain.CausalResult{
		Hypothesis: "null deref in attribute access",
		Confidence: 0.78,
	}}
	p := New(nil, nil, causal, "", false)
	out, err := p.Run(context.Background(), domain.AgentInput{
		OrgID:    "org",
		Incident: &domain.IncidentPayload{Title: "null-deref", Stacktrace: nullDerefStack},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.Structured["graph_available"].(bool) {
		t.Errorf("graph_available should be false when graph is nil")
	}
	if !out.Structured["causal_available"].(bool) {
		t.Errorf("causal_available should be true")
	}
	if causal.last.RootCauseNode != "" {
		t.Errorf("rootCauseNode should be empty when graph is nil, got %q", causal.last.RootCauseNode)
	}
	if len(causal.last.Candidates) != 0 {
		t.Errorf("candidates should be empty when graph is nil (sidecar falls back to its prior), got %+v", causal.last.Candidates)
	}
	ev, _ := out.Structured["evidence_chain"].([]string)
	if len(ev) == 0 {
		t.Fatalf("evidence_chain should still contain stacktrace tokens")
	}
}

// TestPathfinder_CausalMissing — the gRPC sidecar is disabled. Pathfinder
// returns confidence=0 and an "unknown" placeholder so the Synthesiser
// routes to the unknown scenario.
func TestPathfinder_CausalMissing(t *testing.T) {
	graph := &fakeGraph{sym: domain.GraphNode{Name: "render"}}
	p := New(nil, graph, nil, "", false)
	out, err := p.Run(context.Background(), domain.AgentInput{
		OrgID:    "org",
		Incident: &domain.IncidentPayload{Title: "oom", Stacktrace: oomStack},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.Structured["causal_available"].(bool) {
		t.Errorf("causal_available should be false when causal is nil")
	}
	conf, _ := out.Structured["confidence"].(float64)
	if conf != 0 {
		t.Errorf("expected confidence=0 when causal disabled, got %v", conf)
	}
	hyp, _ := out.Structured["hypothesis"].(string)
	if hyp == "" {
		t.Errorf("hypothesis placeholder should be set")
	}
}

// TestPathfinder_GraphNotFound — symbol lookup returns ErrNotFound; agent
// must continue without surfacing the error in evidence_chain.
func TestPathfinder_GraphNotFound(t *testing.T) {
	graph := &fakeGraph{symErr: domain.ErrNotFound}
	causal := &fakeCausal{result: domain.CausalResult{Confidence: 0.5}}
	p := New(nil, graph, causal, "", false)
	out, err := p.Run(context.Background(), domain.AgentInput{
		OrgID:    "org",
		Incident: &domain.IncidentPayload{Stacktrace: sampleStack},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	rcn, _ := out.Structured["root_cause_node"].(string)
	if rcn != "" {
		t.Errorf("root_cause_node should be empty when symbol not found, got %q", rcn)
	}
	ev, _ := out.Structured["evidence_chain"].([]string)
	for _, e := range ev {
		if e == "graph_error=not found" {
			t.Errorf("ErrNotFound should not be surfaced as graph_error: %q", e)
		}
	}
}

// TestPathfinder_GraphDriverError — non-ErrNotFound errors are surfaced as
// evidence so we have a paper trail in the timeline UI, but the agent
// still succeeds.
func TestPathfinder_GraphDriverError(t *testing.T) {
	graph := &fakeGraph{symErr: errors.New("bolt: connection refused")}
	p := New(nil, graph, nil, "", false)
	out, err := p.Run(context.Background(), domain.AgentInput{
		OrgID:    "org",
		Incident: &domain.IncidentPayload{Stacktrace: sampleStack},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	ev, _ := out.Structured["evidence_chain"].([]string)
	found := false
	for _, e := range ev {
		if e == "graph_error=bolt: connection refused" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected graph_error in evidence_chain, got %v", ev)
	}
}

// TestPathfinder_LLMRefine — refinement toggle rewrites the hypothesis
// using the LLM stub. Verifies the canned hypothesis is replaced and that
// schema retries propagate.
func TestPathfinder_LLMRefine(t *testing.T) {
	graph := &fakeGraph{sym: domain.GraphNode{Name: "render"}}
	causal := &fakeCausal{result: domain.CausalResult{
		Hypothesis: "raw causal sentence",
		Confidence: 0.8,
	}}
	llm := &fakeLLM{responses: []domain.CompletionResponse{
		{Content: `{"hypothesis":"polished sentence","confidence":0.83}`, Model: "gpt-4o-mini", InputTokens: 50, OutputTokens: 20, CostCents: 0.01},
	}}
	client := &agents.LLMClient{Provider: llm, SchemaRetryMax: 1}
	p := New(client, graph, causal, "gpt-4o-mini", true)

	out, err := p.Run(context.Background(), domain.AgentInput{
		OrgID:    "org",
		Incident: &domain.IncidentPayload{Stacktrace: nullDerefStack},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	hyp, _ := out.Structured["hypothesis"].(string)
	if hyp != "polished sentence" {
		t.Errorf("expected polished hypothesis, got %q", hyp)
	}
	conf, _ := out.Structured["confidence"].(float64)
	if conf < 0.82 || conf > 0.84 {
		t.Errorf("expected confidence ~0.83, got %v", conf)
	}
	if llm.calls != 1 {
		t.Errorf("expected 1 LLM call, got %d", llm.calls)
	}
	if !llm.last.JSONResponse {
		t.Errorf("expected JSONResponse=true")
	}
	if !llm.last.CacheSystem {
		t.Errorf("expected CacheSystem=true (prompt caching)")
	}
	if out.Model != "gpt-4o-mini" {
		t.Errorf("model not propagated: %q", out.Model)
	}
}

// TestPathfinder_LLMRefine_BadSchemaKeepsCanned — if the LLM returns
// junk JSON the agent must fall through to the canned hypothesis rather
// than fail outright. Refinement is best-effort.
func TestPathfinder_LLMRefine_BadSchemaKeepsCanned(t *testing.T) {
	causal := &fakeCausal{result: domain.CausalResult{
		Hypothesis: "canned hypothesis",
		Confidence: 0.7,
	}}
	llm := &fakeLLM{responses: []domain.CompletionResponse{
		{Content: `not json`, Model: "gpt-4o-mini"},
	}}
	client := &agents.LLMClient{Provider: llm, SchemaRetryMax: 1}
	p := New(client, nil, causal, "gpt-4o-mini", true)

	out, err := p.Run(context.Background(), domain.AgentInput{
		OrgID:    "org",
		Incident: &domain.IncidentPayload{Stacktrace: nullDerefStack},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	hyp, _ := out.Structured["hypothesis"].(string)
	if hyp != "canned hypothesis" {
		t.Errorf("expected canned hypothesis to survive LLM failure, got %q", hyp)
	}
}

// TestPathfinder_StacktraceTokens — token extraction reaches both Python
// exception names and Linux container signals (OOMKilled). The
// Synthesiser fast-path depends on these tokens existing in
// evidence_chain.
func TestPathfinder_StacktraceTokens(t *testing.T) {
	toks := stacktraceTokens(oomStack)
	wantedAny := []string{"MemoryError", "OOMKilled"}
	for _, w := range wantedAny {
		found := false
		for _, t := range toks {
			if t == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected token %q in %v", w, toks)
		}
	}
}

// TestPathfinder_ExtractLastPythonFrame_LastWins — multiple frames; the
// deepest frame is the candidate. This is the key contract the graph
// lookup depends on.
func TestPathfinder_ExtractLastPythonFrame_LastWins(t *testing.T) {
	trace := `File "src/a.py", line 1, in outer
    inner()
  File "src/b.py", line 42, in inner
    raise X`
	f, l := extractLastPythonFrame(trace)
	if f != "src/b.py" || l != 42 {
		t.Errorf("expected last frame src/b.py:42, got %q:%d", f, l)
	}
}

// TestPathfinder_ExtractLastPythonFrame_NonPython — non-Python traces
// return ("", 0) without panicking.
func TestPathfinder_ExtractLastPythonFrame_NonPython(t *testing.T) {
	f, l := extractLastPythonFrame("runtime error: nil pointer dereference\ngoroutine 1 [running]")
	if f != "" || l != 0 {
		t.Errorf("expected zero-value for non-Python trace, got %q:%d", f, l)
	}
}
