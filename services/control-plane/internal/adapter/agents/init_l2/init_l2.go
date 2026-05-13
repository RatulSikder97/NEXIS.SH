// Package init_l2 wires the L2 agent providers (Pathfinder, Synthesiser)
// into a map ready for the coordinator at cmd/server/main.go to merge into
// the master agents.Registry. It lives in its own package to avoid an
// import cycle: the parent agents package owns the Registry shape +
// LLMClient that pathfinder and synthesiser both import, so the wiring
// helper has to live one level down.
//
// validator_l2 is intentionally omitted — Stage 6 (a separate adapter)
// owns its construction and merges into the same map at the call site.
package init_l2

import (
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/agents"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/agents/pathfinder"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/agents/synthesiser"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// Config is the assembly bundle main.go hands to New. Models + flags
// resolve via the existing config.Config struct; the Graph + Causal ports
// may be nil when their adapters fail to boot (the L2 agents tolerate
// missing dependencies — see pathfinder.Provider for the degradation
// matrix).
type Config struct {
	// LLM is the shared LLMClient used by both the optional Pathfinder
	// refinement pass and the Synthesiser LLM fallback. Required for any
	// LLM-driven behaviour; pass nil to force fast-path / fallback paths
	// in eval harness mode.
	LLM *agents.LLMClient

	// Graph is the Neo4j-backed codegraph port consumed by Pathfinder.
	// Nil → "causal-only" Pathfinder evidence path.
	Graph domain.Graph

	// Causal is the DoWhy sidecar port consumed by Pathfinder. Nil →
	// scenario:"unknown" with confidence 0; the Synthesiser still routes
	// deterministically.
	Causal domain.CausalEngine

	// PathfinderModel is the OpenAI/Ollama model used when refinement is
	// enabled. Resolved by main.go from AGENT_MODEL_PATHFINDER_* env var
	// equivalents (Phase 6 reuses the architect model by default).
	PathfinderModel string

	// PathfinderLLMRefine mirrors config.PathfinderLLMRefine. False by
	// default — Phase 6 ships with refinement disabled so the demo runs
	// are deterministic across providers.
	PathfinderLLMRefine bool

	// SynthesiserModel is the model used for the LLM classification
	// fallback. Same provider-resolution pattern as PathfinderModel.
	SynthesiserModel string
}

// New returns the {AgentName → Agent} map that main.go merges into the
// master Registry. Returning a map (instead of registering directly)
// keeps the Registry assembly point in main.go where the L1 map is also
// composed.
//
// Sample wire-up:
//
//	l1 := buildL1Agents(...)
//	l2 := init_l2.New(init_l2.Config{LLM: client, Graph: graph, Causal: causal})
//	registry := agents.NewRegistry(mergeMaps(l1, l2))
func New(cfg Config) map[domain.AgentName]domain.Agent {
	pf := pathfinder.New(cfg.LLM, cfg.Graph, cfg.Causal, cfg.PathfinderModel, cfg.PathfinderLLMRefine)
	syn := synthesiser.New(cfg.LLM, cfg.SynthesiserModel)
	return map[domain.AgentName]domain.Agent{
		domain.AgentNamePathfinder:  pf,
		domain.AgentNameSynthesiser: syn,
	}
}
