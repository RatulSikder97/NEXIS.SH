package domain

import "context"

// CausalQuery is the input the Pathfinder agent hands to the DoWhy sidecar.
// RootCauseNode is the Symbol name returned by Graph.FindSymbolContaining
// (may be empty when the graph adapter is unavailable); Features is a list
// of free-form evidence strings the sidecar may fold into its estimand.
type CausalQuery struct {
	IncidentID    string
	Stacktrace    string
	RootCauseNode string
	Features      []string
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
