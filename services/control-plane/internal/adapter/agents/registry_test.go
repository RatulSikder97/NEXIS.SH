package agents

// Coverage for the agents Registry. The package's individual agent
// providers live in subpackages with their own tests; this file covers the
// pure dispatch layer.

import (
	"context"
	"errors"
	"sort"
	"testing"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// fakeAgent is the minimal Agent implementation the registry test needs.
type fakeAgent struct {
	name domain.AgentName
}

func (f *fakeAgent) Name() domain.AgentName { return f.name }

func (f *fakeAgent) Run(_ context.Context, _ domain.AgentInput) (domain.AgentOutput, error) {
	return domain.AgentOutput{
		Success: true,
		Structured: map[string]any{
			"ran": true,
		},
	}, nil
}

// erroringAgent always fails its Run — used to verify Registry.Run surfaces
// the underlying error.
type erroringAgent struct{ failErr error }

func (e *erroringAgent) Name() domain.AgentName { return "boom" }

func (e *erroringAgent) Run(_ context.Context, _ domain.AgentInput) (domain.AgentOutput, error) {
	return domain.AgentOutput{}, e.failErr
}

// TestNewRegistry_NamesAndGet — round-trips every registered agent name
// through Names() + Get().
func TestNewRegistry_NamesAndGet(t *testing.T) {
	a := &fakeAgent{name: domain.AgentNameArchitect}
	b := &fakeAgent{name: domain.AgentNameBackend}
	r := NewRegistry(map[domain.AgentName]domain.Agent{
		a.name: a,
		b.name: b,
	})

	names := r.Names()
	sort.Strings(names)
	if len(names) != 2 {
		t.Fatalf("Names len: %d", len(names))
	}
	if names[0] != "architect" || names[1] != "backend" {
		t.Fatalf("names: %+v", names)
	}

	got, err := r.Get(domain.AgentNameArchitect)
	if err != nil {
		t.Fatalf("Get architect: %v", err)
	}
	if got != a {
		t.Fatalf("Get returned wrong agent: %v", got)
	}
}

// TestRegistry_GetUnknown — unknown agent name returns a descriptive error.
func TestRegistry_GetUnknown(t *testing.T) {
	r := NewRegistry(map[domain.AgentName]domain.Agent{})
	_, err := r.Get(domain.AgentName("not-real"))
	if err == nil {
		t.Fatalf("expected error for missing agent")
	}
}

// TestRegistry_RunDispatches — Run wraps Get + Agent.Run.
func TestRegistry_RunDispatches(t *testing.T) {
	a := &fakeAgent{name: domain.AgentNameArchitect}
	r := NewRegistry(map[domain.AgentName]domain.Agent{a.name: a})
	out, err := r.Run(context.Background(), domain.AgentNameArchitect, domain.AgentInput{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !out.Success {
		t.Fatalf("expected success: %+v", out)
	}
}

// TestRegistry_RunUnknownReturnsErr — unknown agent surfaces the Get error.
func TestRegistry_RunUnknownReturnsErr(t *testing.T) {
	r := NewRegistry(map[domain.AgentName]domain.Agent{})
	_, err := r.Run(context.Background(), domain.AgentName("nope"), domain.AgentInput{})
	if err == nil {
		t.Fatalf("expected error for missing agent")
	}
}

// TestRegistry_RunSurfacesAgentError — the Agent.Run error path returns
// the underlying error.
func TestRegistry_RunSurfacesAgentError(t *testing.T) {
	e := &erroringAgent{failErr: errors.New("synthesis failed")}
	r := NewRegistry(map[domain.AgentName]domain.Agent{e.Name(): e})
	_, err := r.Run(context.Background(), e.Name(), domain.AgentInput{})
	if err == nil {
		t.Fatalf("expected error")
	}
	if err.Error() != "synthesis failed" {
		t.Fatalf("error: %v", err)
	}
}

// TestNewRegistry_CopiesMap — mutating the input map after construction
// must not affect the registry's contents.
func TestNewRegistry_CopiesMap(t *testing.T) {
	a := &fakeAgent{name: domain.AgentNameArchitect}
	src := map[domain.AgentName]domain.Agent{a.name: a}
	r := NewRegistry(src)

	// Mutate the source after construction.
	delete(src, a.name)

	got, err := r.Get(a.name)
	if err != nil {
		t.Fatalf("expected agent still present, got %v", err)
	}
	if got != a {
		t.Fatalf("agent: %v", got)
	}
}

// TestDecodeJSONOrError_Happy — valid JSON decodes into a map.
func TestDecodeJSONOrError_Happy(t *testing.T) {
	got, err := DecodeJSONOrError(`{"foo":"bar","n":1}`)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got["foo"] != "bar" {
		t.Fatalf("decoded: %+v", got)
	}
}

// TestDecodeJSONOrError_InvalidWrapsErr — non-JSON wraps the
// ErrAgentSchemaMismatch sentinel so the LLMClient retry loop recognises it.
func TestDecodeJSONOrError_InvalidWrapsErr(t *testing.T) {
	_, err := DecodeJSONOrError(`not really json`)
	if err == nil {
		t.Fatalf("expected error for bad json")
	}
	if !errors.Is(err, domain.ErrAgentSchemaMismatch) {
		t.Fatalf("err must wrap ErrAgentSchemaMismatch: %v", err)
	}
}

// TestValidate_Happy — schema + matching payload returns the parsed map.
func TestValidate_Happy(t *testing.T) {
	schema := `{
		"type": "object",
		"required": ["name"],
		"properties": {
			"name": {"type": "string"}
		}
	}`
	got, err := Validate(schema, `{"name": "alice"}`)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got["name"] != "alice" {
		t.Fatalf("decoded: %+v", got)
	}
}

// TestValidate_SchemaMismatchWrapsErr — schema validation failure returns
// ErrAgentSchemaMismatch.
func TestValidate_SchemaMismatchWrapsErr(t *testing.T) {
	schema := `{
		"type": "object",
		"required": ["name"],
		"properties": {"name": {"type": "string"}}
	}`
	_, err := Validate(schema, `{"name": 42}`) // wrong type
	if err == nil {
		t.Fatalf("expected schema mismatch")
	}
	if !errors.Is(err, domain.ErrAgentSchemaMismatch) {
		t.Fatalf("err must wrap ErrAgentSchemaMismatch: %v", err)
	}
}

// TestValidate_InvalidJSONFailsEarly — malformed JSON also surfaces the
// schema-mismatch sentinel (gojsonschema treats parse-failure as invalid).
func TestValidate_InvalidJSONFailsEarly(t *testing.T) {
	_, err := Validate(`{"type":"object"}`, `not even json`)
	if err == nil {
		t.Fatalf("expected error")
	}
}

// TestRetrievalClient_ContextFor_NilStoreOrEmbedReturnsEmpty — every nil
// guard returns empty string + nil chunks without erroring so the agent
// prompt still runs.
func TestRetrievalClient_ContextFor_NilStoreOrEmbedReturnsEmpty(t *testing.T) {
	r := &RetrievalClient{}
	s, chunks, err := r.ContextFor(context.Background(), "org", "sha", "query")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if s != "" || chunks != nil {
		t.Fatalf("empty expected: %q %+v", s, chunks)
	}
}

// TestRetrievalClient_ContextFor_NilReceiver — the helper is nil-safe.
func TestRetrievalClient_ContextFor_NilReceiver(t *testing.T) {
	var r *RetrievalClient
	s, chunks, err := r.ContextFor(context.Background(), "org", "sha", "query")
	if err != nil || s != "" || chunks != nil {
		t.Fatalf("nil receiver: %v %q %+v", err, s, chunks)
	}
}

// TestRetrievalClient_ContextFor_EmptyQueryShortCircuits — whitespace-only
// query also returns empty (no embed call).
func TestRetrievalClient_ContextFor_EmptyQueryShortCircuits(t *testing.T) {
	r := &RetrievalClient{
		Store: stubStore{},
		Embed: stubEmbed{},
	}
	s, chunks, err := r.ContextFor(context.Background(), "org", "sha", "   ")
	if err != nil || s != "" || chunks != nil {
		t.Fatalf("empty query: %v %q %+v", err, s, chunks)
	}
}

// TestRetrievalClient_ContextFor_RendersChunks — happy path returns a
// markdown block whose contents include each chunk's metadata.
func TestRetrievalClient_ContextFor_RendersChunks(t *testing.T) {
	r := &RetrievalClient{
		Store: stubStore{chunks: []domain.Chunk{
			{FilePath: "main.go", ChunkStart: 1, ChunkEnd: 10, Similarity: 0.9, Content: "package main"},
		}},
		Embed: stubEmbed{vectors: [][]float32{{0.1, 0.2}}},
		K:     5,
	}
	s, chunks, err := r.ContextFor(context.Background(), "org", "sha", "query")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("chunks: %d", len(chunks))
	}
	if !contains(s, "main.go") || !contains(s, "package main") {
		t.Fatalf("rendered context missing details: %q", s)
	}
}

// stubStore + stubEmbed satisfy the retrieval ports without a real
// pgvector / embedding model.
type stubStore struct {
	chunks []domain.Chunk
	err    error
}

func (s stubStore) Insert(_ context.Context, _ []domain.Chunk) error { return nil }

func (s stubStore) TopK(_ context.Context, _, _ string, _ []float32, _ int) ([]domain.Chunk, error) {
	return s.chunks, s.err
}

// FileChunks returns the same fixture chunks filtered to the requested paths,
// which is enough for the prompt-shape assertions in this package.
func (s stubStore) FileChunks(_ context.Context, _, _ string, paths []string) ([]domain.Chunk, error) {
	if s.err != nil {
		return nil, s.err
	}
	want := map[string]bool{}
	for _, p := range paths {
		want[p] = true
	}
	out := []domain.Chunk{}
	for _, c := range s.chunks {
		if want[c.FilePath] {
			out = append(out, c)
		}
	}
	return out, nil
}

type stubEmbed struct {
	vectors [][]float32
	err     error
}

func (s stubEmbed) Name() string { return "stub" }

func (s stubEmbed) Embed(_ context.Context, _ string, _ []string) ([][]float32, error) {
	return s.vectors, s.err
}

func (s stubEmbed) EmbeddingDims() int { return 2 }

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
