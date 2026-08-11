package domain

import "context"

// GraphNodeKind enumerates the node labels in the Neo4j codegraph. Phase 6
// covers Module, Symbol, ExceptionType — Phase 7+ extends with Service,
// Deployment, etc.
type GraphNodeKind string

const (
	GraphKindModule        GraphNodeKind = "Module"
	GraphKindSymbol        GraphNodeKind = "Symbol"
	GraphKindExceptionType GraphNodeKind = "ExceptionType"
)

// GraphEdgeKind enumerates the relationship types Phase 6 reasons about.
type GraphEdgeKind string

const (
	GraphEdgeDefinedIn GraphEdgeKind = "DEFINED_IN"
	GraphEdgeCalls     GraphEdgeKind = "CALLS"
	GraphEdgeImports   GraphEdgeKind = "IMPORTS"
	GraphEdgeRaised    GraphEdgeKind = "RAISED"
)

// GraphNode is the codegraph node projection consumed by Pathfinder. Props
// holds any kind-specific attributes that don't fit the fixed slots.
type GraphNode struct {
	Kind      GraphNodeKind
	OrgID     string
	RepoSHA   string
	FilePath  string
	Name      string
	LineStart int
	LineEnd   int
	Props     map[string]any
}

// GraphEdge is the directed (From → To) relationship used by both the
// neighbours traversal and the seeder upsert path.
//
// Hops is populated only by the Neighbours traversal: the path length (in
// edges) from the queried symbol to To. The seeder leaves it zero — a
// direct upsert edge has no traversal context. Pathfinder folds Hops into
// the distance_from_symptom metadata it forwards to the causal sidecar's
// candidate ranking.
type GraphEdge struct {
	From GraphNode
	To   GraphNode
	Kind GraphEdgeKind
	Hops int
}

// Graph is the port the Pathfinder agent depends on. Implementations live in
// internal/adapter/graphstore/neo4j/store.go. A nil Graph means the adapter
// boot couldn't reach Neo4j; the Pathfinder agent must tolerate that and
// fall through to the causal-only evidence path.
type Graph interface {
	// FindSymbolContaining returns the Symbol node whose [line_start..line_end]
	// range covers the (file_path, line) tuple. Returns ErrNotFound when no
	// match.
	FindSymbolContaining(ctx context.Context, orgID, repoSHA, filePath string, line int) (GraphNode, error)

	// Neighbours walks `maxHops` of the given edge kinds outward from `node`
	// and returns the visited nodes paired with the edge type that reached
	// them. Limit caps the traversal at a hard count.
	Neighbours(ctx context.Context, node GraphNode, kinds []GraphEdgeKind, maxHops, limit int) ([]GraphEdge, error)

	// Upsert is used by cmd/seed-neo4j only; the production hot path is
	// read-only.
	Upsert(ctx context.Context, batch []GraphEdge) error

	// Ping verifies driver connectivity at startup.
	Ping(ctx context.Context) error
}
