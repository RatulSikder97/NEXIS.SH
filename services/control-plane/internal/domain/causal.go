package domain

import "context"

// CausalCandidate is one root-cause hypothesis assembled from the Neo4j
// traversal, forwarded to the sidecar's graph-evidence ranking. It maps 1:1
// onto the sidecar's RootCauseCandidate wire model (root_cause_candidates):
// Node -> node, Evidence -> evidence, and the three graph-position fields
// onto in_degree / out_degree / distance_from_symptom.
//
// The degree fields count edges within the retrieved <=MaxHops evidence
// subgraph, not the global codegraph — the traversal is bounded by
// Provider.Limit, so these are local-neighbourhood centrality signals.
// DistanceFromSymptom is the hop count from the crashing frame's symbol
// (0 = the symbol containing the last stack frame itself).
type CausalCandidate struct {
	Node                string
	Evidence            []string
	InDegree            int
	OutDegree           int
	DistanceFromSymptom int
}

// CausalQuery is the input the Pathfinder agent hands to the causal sidecar.
// RootCauseNode is the Symbol name returned by Graph.FindSymbolContaining
// (may be empty when the graph adapter is unavailable); Features is a list
// of free-form evidence strings; Candidates is the graph-derived hypothesis
// set the sidecar ranks (empty when the graph adapter is unavailable, in
// which case the sidecar degrades to its scenario-prior fallback).
type CausalQuery struct {
	IncidentID    string
	Stacktrace    string
	RootCauseNode string
	Features      []string
	Candidates    []CausalCandidate
}

// CausalResult is what the DoWhy sidecar returns. EstimandName is the name
// of the estimand the sidecar used (backdoor_adjustment_on_*, etc.); the
// front-end renders it under the Pathfinder timeline frame.
type CausalResult struct {
	Hypothesis   string
	Confidence   float32 // 0.0..1.0
	Evidence     []string
	EstimandName string
	DurationMs   int64
}

// CausalEngine is the port Pathfinder depends on. The concrete implementation
// is the gRPC client at internal/adapter/causal/grpc.go.
type CausalEngine interface {
	Infer(ctx context.Context, q CausalQuery) (CausalResult, error)
}
