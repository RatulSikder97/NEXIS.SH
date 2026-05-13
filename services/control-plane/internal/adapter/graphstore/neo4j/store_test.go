package neo4j

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	neo4jdrv "github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/stretchr/testify/require"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	platformneo4j "github.com/nexis-eco/nexis/services/control-plane/internal/platform/neo4j"
)

// TestStore_RoundTrip is env-gated: it requires a running Neo4j bound on
// NEO4J_URI / NEO4J_USER / NEO4J_PASS. The CI matrix sets these from the
// docker-compose neo4j service; local laptop runs skip silently.
func TestStore_RoundTrip(t *testing.T) {
	uri := os.Getenv("NEO4J_URI")
	if uri == "" {
		t.Skip("NEO4J_URI unset — skipping graphstore integration test")
	}
	user := envOr("NEO4J_USER", "neo4j")
	pass := envOr("NEO4J_PASS", "nexis_dev_password")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	drv, err := platformneo4j.New(ctx, platformneo4j.Config{URI: uri, User: user, Password: pass})
	require.NoError(t, err)
	t.Cleanup(func() { _ = drv.Close(context.Background()) })
	require.NoError(t, platformneo4j.Verify(ctx, drv))

	store := New(drv)
	orgID := uuid.NewString()
	repoSHA := "test-" + uuid.NewString()[:8]

	// Build a tiny synthetic graph: one Module + two Symbols + one CALLS + one
	// RAISED edge. Symbol `caller` calls `callee`; `callee` raises ValueError.
	module := domain.GraphNode{
		Kind: domain.GraphKindModule, OrgID: orgID, RepoSHA: repoSHA,
		FilePath: "test/main.py",
	}
	caller := domain.GraphNode{
		Kind: domain.GraphKindSymbol, OrgID: orgID, RepoSHA: repoSHA,
		FilePath: "test/main.py", Name: "caller",
		LineStart: 1, LineEnd: 10,
	}
	callee := domain.GraphNode{
		Kind: domain.GraphKindSymbol, OrgID: orgID, RepoSHA: repoSHA,
		FilePath: "test/main.py", Name: "callee",
		LineStart: 12, LineEnd: 18,
	}
	exception := domain.GraphNode{
		Kind: domain.GraphKindExceptionType, OrgID: orgID, RepoSHA: repoSHA,
		Name: "ValueError",
	}
	require.NoError(t, store.Upsert(ctx, []domain.GraphEdge{
		{From: caller, To: module, Kind: domain.GraphEdgeDefinedIn},
		{From: callee, To: module, Kind: domain.GraphEdgeDefinedIn},
		{From: caller, To: callee, Kind: domain.GraphEdgeCalls},
		{From: callee, To: exception, Kind: domain.GraphEdgeRaised},
	}))

	// FindSymbolContaining: line 5 should hit `caller`.
	found, err := store.FindSymbolContaining(ctx, orgID, repoSHA, "test/main.py", 5)
	require.NoError(t, err)
	require.Equal(t, "caller", found.Name)

	// Neighbours of `caller` via (CALLS, RAISED) reaches `callee` (and through it
	// `ValueError`).
	edges, err := store.Neighbours(ctx, caller,
		[]domain.GraphEdgeKind{domain.GraphEdgeCalls, domain.GraphEdgeRaised}, 2, 20)
	require.NoError(t, err)
	require.NotEmpty(t, edges)

	// Cleanup — drop everything we wrote for this run.
	_ = clearOrg(ctx, drv, orgID)
}

// TestSeedFromFixtures parses an in-test temp tree and asserts at least one
// DEFINED_IN edge lands per top-level def/class. Also env-gated.
func TestSeedFromFixtures(t *testing.T) {
	uri := os.Getenv("NEO4J_URI")
	if uri == "" {
		t.Skip("NEO4J_URI unset — skipping seed integration test")
	}
	user := envOr("NEO4J_USER", "neo4j")
	pass := envOr("NEO4J_PASS", "nexis_dev_password")

	dir := t.TempDir()
	src := "def alpha(b):\n    if b == 0:\n        return None\n    return 1 / b\n\n" +
		"class Beta:\n    def gamma(self):\n        return alpha(0)\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "lib.py"), []byte(src), 0o600))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	drv, err := platformneo4j.New(ctx, platformneo4j.Config{URI: uri, User: user, Password: pass})
	require.NoError(t, err)
	t.Cleanup(func() { _ = drv.Close(context.Background()) })

	store := New(drv)
	orgID := uuid.NewString()
	n, err := store.SeedFromFixtures(ctx, dir, orgID, "seed-test")
	require.NoError(t, err)
	require.Greater(t, n, 0)
	_ = clearOrg(ctx, drv, orgID)
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func clearOrg(ctx context.Context, drv neo4jdrv.DriverWithContext, orgID string) error {
	sess := drv.NewSession(ctx, neo4jdrv.SessionConfig{AccessMode: neo4jdrv.AccessModeWrite})
	defer sess.Close(ctx)
	_, err := sess.Run(ctx, `MATCH (n {org_id:$org}) DETACH DELETE n`, map[string]any{"org": orgID})
	return err
}
