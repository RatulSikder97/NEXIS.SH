// Package neo4j hosts the Neo4j-backed implementation of the domain.Graph
// port. The control-plane never imports neo4j-go-driver outside this package
// and the platform/neo4j helper — every other consumer flows through the
// domain.Graph interface so the driver dependency stays scoped.
//
// The hot path for Pathfinder is read-only (FindSymbolContaining + Neighbours).
// Upsert is exercised only by cmd/seed-neo4j at boot; the production server
// never writes.
package neo4j

import (
	"context"
	"fmt"
	"strings"

	neo4jdrv "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// Store is the domain.Graph implementation on top of neo4j-go-driver/v5.
type Store struct {
	drv neo4jdrv.DriverWithContext
}

// New wraps an already-opened driver. Caller (cmd/server/main.go) owns the
// driver lifecycle and closes it at shutdown.
func New(drv neo4jdrv.DriverWithContext) *Store { return &Store{drv: drv} }

// compile-time conformance check.
var _ domain.Graph = (*Store)(nil)

// Ping wraps VerifyConnectivity. Called from main.go startup + activity boot
// to short-circuit Pathfinder's graph-evidence path when Neo4j is unreachable.
func (s *Store) Ping(ctx context.Context) error {
	return s.drv.VerifyConnectivity(ctx)
}

// FindSymbolContaining looks up the Symbol whose synthetic [line_start,line_end]
// range covers the (filePath, line) tuple. Returns domain.ErrNotFound when no
// row matches.
func (s *Store) FindSymbolContaining(ctx context.Context, orgID, repoSHA, filePath string, line int) (domain.GraphNode, error) {
	sess := s.drv.NewSession(ctx, neo4jdrv.SessionConfig{AccessMode: neo4jdrv.AccessModeRead})
	defer sess.Close(ctx)
	res, err := sess.Run(ctx,
		`MATCH (s:Symbol {file_path:$file, org_id:$org, repo_sha:$sha})
		 WHERE s.line_start <= $line AND s.line_end >= $line
		 RETURN s LIMIT 1`,
		map[string]any{"file": filePath, "org": orgID, "sha": repoSHA, "line": line},
	)
	if err != nil {
		return domain.GraphNode{}, fmt.Errorf("FindSymbolContaining: %w", err)
	}
	if !res.Next(ctx) {
		return domain.GraphNode{}, domain.ErrNotFound
	}
	rec := res.Record()
	if len(rec.Values) == 0 {
		return domain.GraphNode{}, domain.ErrNotFound
	}
	node, _ := rec.Values[0].(neo4jdrv.Node)
	return nodeToDomain(node, domain.GraphKindSymbol), nil
}

// Neighbours walks `maxHops` edges of the given kinds outward from `n` and
// returns (edge type, target node) pairs. When kinds is empty Phase 6 defaults
// to (CALLS, RAISED) — the two relationships Pathfinder traverses to assemble
// its evidence chain.
func (s *Store) Neighbours(ctx context.Context, n domain.GraphNode, kinds []domain.GraphEdgeKind, maxHops, limit int) ([]domain.GraphEdge, error) {
	if len(kinds) == 0 {
		kinds = []domain.GraphEdgeKind{domain.GraphEdgeCalls, domain.GraphEdgeRaised}
	}
	if maxHops <= 0 {
		maxHops = 2
	}
	if limit <= 0 {
		limit = 20
	}
	relList := relPattern(kinds)
	sess := s.drv.NewSession(ctx, neo4jdrv.SessionConfig{AccessMode: neo4jdrv.AccessModeRead})
	defer sess.Close(ctx)

	// %s for the relationship pattern is safe — it's built from enum constants,
	// not user input. The traversal depth bound is also a const-derived int.
	// size(r) is the path length in edges — surfaced as GraphEdge.Hops so
	// Pathfinder can report each candidate's real distance from the symptom
	// symbol to the causal sidecar's ranking.
	cypher := fmt.Sprintf(
		`MATCH (s:Symbol {name:$name, org_id:$org})-[r:%s*1..%d]-(t)
		 RETURN type(r[0]) AS rel, t, size(r) AS hops LIMIT %d`,
		relList, maxHops, limit,
	)
	res, err := sess.Run(ctx, cypher, map[string]any{"name": n.Name, "org": n.OrgID})
	if err != nil {
		return nil, fmt.Errorf("Neighbours: %w", err)
	}
	var out []domain.GraphEdge
	for res.Next(ctx) {
		rec := res.Record()
		if len(rec.Values) < 2 {
			continue
		}
		rel, _ := rec.Values[0].(string)
		target, _ := rec.Values[1].(neo4jdrv.Node)
		hops := 1
		if len(rec.Values) > 2 {
			if h, ok := rec.Values[2].(int64); ok && h > 0 {
				hops = int(h)
			}
		}
		out = append(out, domain.GraphEdge{
			From: n,
			To:   nodeToDomain(target, kindFromLabels(target.Labels)),
			Kind: domain.GraphEdgeKind(rel),
			Hops: hops,
		})
	}
	return out, res.Err()
}

// Upsert writes a batch of edges + their endpoints. Used by SeedFromFixtures
// (and cmd/seed-neo4j) only — the production hot path is read-only.
func (s *Store) Upsert(ctx context.Context, batch []domain.GraphEdge) error {
	if len(batch) == 0 {
		return nil
	}
	sess := s.drv.NewSession(ctx, neo4jdrv.SessionConfig{AccessMode: neo4jdrv.AccessModeWrite})
	defer sess.Close(ctx)
	for _, e := range batch {
		cypher, params := upsertCypher(e)
		if _, err := sess.Run(ctx, cypher, params); err != nil {
			return fmt.Errorf("upsert %s -[%s]-> %s: %w", e.From.Name, e.Kind, e.To.Name, err)
		}
	}
	return nil
}

// relPattern joins a list of GraphEdgeKind values with the Cypher `|`
// alternation operator (e.g. "CALLS|RAISED").
func relPattern(kinds []domain.GraphEdgeKind) string {
	parts := make([]string, 0, len(kinds))
	for _, k := range kinds {
		parts = append(parts, string(k))
	}
	return strings.Join(parts, "|")
}

// nodeToDomain converts a Neo4j driver Node into our domain projection. The
// `kind` argument is the label inferred by the caller (FindSymbol… knows it's
// a Symbol; Neighbours uses kindFromLabels for the target node).
func nodeToDomain(n neo4jdrv.Node, kind domain.GraphNodeKind) domain.GraphNode {
	props := n.Props
	out := domain.GraphNode{Kind: kind, Props: map[string]any{}}
	if v, ok := props["org_id"].(string); ok {
		out.OrgID = v
	}
	if v, ok := props["repo_sha"].(string); ok {
		out.RepoSHA = v
	}
	if v, ok := props["file_path"].(string); ok {
		out.FilePath = v
	}
	if v, ok := props["name"].(string); ok {
		out.Name = v
	}
	out.LineStart = propInt(props, "line_start")
	out.LineEnd = propInt(props, "line_end")
	// Copy the unrecognised props into Props so callers can introspect them.
	for k, v := range props {
		switch k {
		case "org_id", "repo_sha", "file_path", "name", "line_start", "line_end":
			continue
		default:
			out.Props[k] = v
		}
	}
	return out
}

// propInt extracts an int property tolerating int / int64 / float64 (neo4j-go-
// driver returns numeric properties as int64 by default).
func propInt(props map[string]any, key string) int {
	switch v := props[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	}
	return 0
}

// kindFromLabels picks the first known node kind from the label slice. Falls
// back to Symbol because that is the most common neighbour returned during the
// Pathfinder traversal.
func kindFromLabels(labels []string) domain.GraphNodeKind {
	for _, l := range labels {
		switch domain.GraphNodeKind(l) {
		case domain.GraphKindModule:
			return domain.GraphKindModule
		case domain.GraphKindSymbol:
			return domain.GraphKindSymbol
		case domain.GraphKindExceptionType:
			return domain.GraphKindExceptionType
		}
	}
	return domain.GraphKindSymbol
}

// upsertCypher returns the MERGE statement + params for a single edge. One
// template per edge kind keeps each query parameterised — no string-concat of
// values, just constant label/relationship names.
func upsertCypher(e domain.GraphEdge) (string, map[string]any) {
	params := map[string]any{
		"an":  e.From.Name,
		"ap":  e.From.FilePath,
		"as":  int64(e.From.LineStart),
		"ae":  int64(e.From.LineEnd),
		"bp":  e.To.FilePath,
		"bn":  e.To.Name,
		"org": e.From.OrgID,
		"sha": e.From.RepoSHA,
	}
	switch e.Kind {
	case domain.GraphEdgeDefinedIn:
		return `
			MERGE (b:Module {file_path:$bp, org_id:$org, repo_sha:$sha})
			MERGE (a:Symbol {name:$an, org_id:$org, repo_sha:$sha})
			SET a.file_path=$ap, a.line_start=$as, a.line_end=$ae
			MERGE (a)-[:DEFINED_IN]->(b)
			`, params
	case domain.GraphEdgeCalls:
		return `
			MERGE (a:Symbol {name:$an, org_id:$org, repo_sha:$sha})
			MERGE (b:Symbol {name:$bn, org_id:$org, repo_sha:$sha})
			SET a.file_path=$ap, a.line_start=$as, a.line_end=$ae
			MERGE (a)-[:CALLS]->(b)
			`, params
	case domain.GraphEdgeImports:
		return `
			MERGE (a:Symbol {name:$an, org_id:$org, repo_sha:$sha})
			MERGE (b:Module {file_path:$bp, org_id:$org, repo_sha:$sha})
			MERGE (a)-[:IMPORTS]->(b)
			`, params
	case domain.GraphEdgeRaised:
		// To name an ExceptionType the seeder uses the To.Name slot; the file
		// path on B is empty (ExceptionType is org-scoped, not file-scoped).
		return `
			MERGE (a:Symbol {name:$an, org_id:$org, repo_sha:$sha})
			MERGE (b:ExceptionType {name:$bn, org_id:$org, repo_sha:$sha})
			MERGE (a)-[:RAISED]->(b)
			`, params
	}
	// Default — defensive; should be unreachable.
	return `RETURN 0`, params
}
