package neo4j

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// defRE matches a top-level Python `def name` or `class name` declaration.
// Phase 6 keeps the parser intentionally tiny — Phase 7 swaps to a real AST
// (libcst / tree-sitter) once the demo fixture set grows beyond a handful of
// files.
var defRE = regexp.MustCompile(`^\s*(?:def|class)\s+([A-Za-z_][A-Za-z0-9_]*)`)

// callRE finds bare-identifier function calls inside a Symbol body. Used by
// computeCalls to derive synthetic CALLS edges between symbols that share the
// same fixture root. Matches names followed by `(`.
var callRE = regexp.MustCompile(`\b([A-Za-z_][A-Za-z0-9_]*)\(`)

// raisedTable hard-codes the (symbol name → exception type) edges Phase 6
// surfaces for the demo fixtures. Each entry becomes a synthetic RAISED edge
// after the symbol pass.
var raisedTable = map[string][]string{
	"safe_div":     {"ValueError"},
	"load_orders":  {"OperationalError"},
	"image_resize": {"MemoryError"},
}

// SeedFromFixtures walks `root` (typically services/validator/fixtures), parses
// every .py file with a regex-level AST, and writes Module + Symbol +
// DEFINED_IN/CALLS/RAISED edges via Upsert.
//
// Returns the number of edges written. Idempotent: re-running with the same
// (org, repo_sha) MERGEs onto the existing nodes/edges rather than duplicating.
func (s *Store) SeedFromFixtures(ctx context.Context, root, orgID, repoSHA string) (int, error) {
	var symbols []domain.GraphNode
	moduleByPath := map[string]domain.GraphNode{}
	bodies := map[string]string{} // symbol name → body text for the calls pass

	err := filepath.Walk(root, func(p string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info == nil || info.IsDir() || !strings.HasSuffix(p, ".py") {
			return nil
		}
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			return nil // skip unreadable files; demo seed is best-effort
		}
		modulePath, _ := filepath.Rel(root, p)
		modulePath = filepath.ToSlash(modulePath)
		module := domain.GraphNode{
			Kind: domain.GraphKindModule, OrgID: orgID, RepoSHA: repoSHA,
			FilePath: modulePath,
		}
		moduleByPath[modulePath] = module

		lines := strings.Split(string(data), "\n")
		var curSym *domain.GraphNode
		var curBody []string
		flush := func(endLine int) {
			if curSym == nil {
				return
			}
			curSym.LineEnd = endLine
			symbols = append(symbols, *curSym)
			bodies[curSym.Name] = strings.Join(curBody, "\n")
			curSym = nil
			curBody = nil
		}
		for i, ln := range lines {
			if m := defRE.FindStringSubmatch(ln); len(m) == 2 {
				flush(i)
				curSym = &domain.GraphNode{
					Kind: domain.GraphKindSymbol, OrgID: orgID, RepoSHA: repoSHA,
					FilePath: modulePath, Name: m[1], LineStart: i + 1,
				}
				continue
			}
			if curSym != nil {
				curBody = append(curBody, ln)
			}
		}
		flush(len(lines))
		return nil
	})
	if err != nil {
		return 0, err
	}

	// Build the edge slice deterministically — sort symbols by (FilePath, Name)
	// so the seeder output is reproducible across runs.
	sort.SliceStable(symbols, func(i, j int) bool {
		if symbols[i].FilePath != symbols[j].FilePath {
			return symbols[i].FilePath < symbols[j].FilePath
		}
		return symbols[i].Name < symbols[j].Name
	})

	var edges []domain.GraphEdge
	// 1) DEFINED_IN: every symbol points to its module.
	for _, sym := range symbols {
		mod, ok := moduleByPath[sym.FilePath]
		if !ok {
			continue
		}
		edges = append(edges, domain.GraphEdge{
			From: sym, To: mod, Kind: domain.GraphEdgeDefinedIn,
		})
	}
	// 2) CALLS: scan each body for known symbol names.
	edges = append(edges, computeCalls(symbols, bodies)...)
	// 3) RAISED: static table (Phase 7 swaps to real `raise` inspection).
	edges = append(edges, raisedEdges(symbols, orgID, repoSHA)...)

	if err := s.Upsert(ctx, edges); err != nil {
		return 0, err
	}
	return len(edges), nil
}

// computeCalls iterates over each symbol's body looking for invocations of any
// other symbol in the slice. Skips self-edges; dedupes (from,to).
func computeCalls(symbols []domain.GraphNode, bodies map[string]string) []domain.GraphEdge {
	known := map[string]domain.GraphNode{}
	for _, s := range symbols {
		known[s.Name] = s
	}
	seen := map[string]bool{}
	var out []domain.GraphEdge
	for _, s := range symbols {
		body := bodies[s.Name]
		if body == "" {
			continue
		}
		matches := callRE.FindAllStringSubmatch(body, -1)
		for _, m := range matches {
			callee, ok := known[m[1]]
			if !ok || callee.Name == s.Name {
				continue
			}
			key := s.Name + "→" + callee.Name
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, domain.GraphEdge{
				From: s, To: callee, Kind: domain.GraphEdgeCalls,
			})
		}
	}
	return out
}

// raisedEdges materialises the static raisedTable into GraphEdge values. The
// destination ExceptionType node is anonymous on file_path — exceptions are
// org-scoped, not file-scoped.
func raisedEdges(symbols []domain.GraphNode, orgID, repoSHA string) []domain.GraphEdge {
	known := map[string]domain.GraphNode{}
	for _, s := range symbols {
		known[s.Name] = s
	}
	var out []domain.GraphEdge
	// Sort the table keys for deterministic emission.
	keys := make([]string, 0, len(raisedTable))
	for k := range raisedTable {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, symName := range keys {
		sym, ok := known[symName]
		if !ok {
			continue
		}
		for _, exc := range raisedTable[symName] {
			out = append(out, domain.GraphEdge{
				From: sym,
				To: domain.GraphNode{
					Kind: domain.GraphKindExceptionType, OrgID: orgID, RepoSHA: repoSHA,
					Name: exc,
				},
				Kind: domain.GraphEdgeRaised,
			})
		}
	}
	return out
}
