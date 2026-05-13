// Command seed-neo4j seeds a synthetic codegraph for the fixture repo. It is
// intended to be invoked once at bootstrap time inside the dev cluster:
//
//	docker compose run --rm control-plane /app/seed-neo4j \
//	    --org-id $(uuidgen) --uri bolt://neo4j:7687
//
// The seed walks services/validator/fixtures (configurable via --fixtures),
// regex-extracts Module + Symbol nodes, and writes Module + DEFINED_IN + CALLS
// + RAISED edges. It is idempotent on (org_id, repo_sha): re-runs MERGE onto
// existing nodes instead of duplicating.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"

	graphstore "github.com/nexis-eco/nexis/services/control-plane/internal/adapter/graphstore/neo4j"
	platformneo4j "github.com/nexis-eco/nexis/services/control-plane/internal/platform/neo4j"
)

func main() {
	var (
		uri      = flag.String("uri", envOr("NEO4J_URI", "bolt://neo4j:7687"), "Neo4j bolt URI")
		user     = flag.String("user", envOr("NEO4J_USER", "neo4j"), "Neo4j user")
		pass     = flag.String("pass", envOr("NEO4J_PASS", "nexis_dev_password"), "Neo4j password")
		orgID    = flag.String("org-id", os.Getenv("SEED_NEO4J_ORG_ID"), "Tenant org id (uuid) — required")
		repoSHA  = flag.String("repo-sha", envOr("SEED_NEO4J_REPO_SHA", "fixture-seed-001"), "Synthetic repo SHA tag")
		fixtures = flag.String("fixtures", envOr("SEED_NEO4J_FIXTURES", "services/validator/fixtures"), "Fixture repo root")
	)
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if *orgID == "" {
		fmt.Fprintln(os.Stderr, "seed-neo4j: --org-id (or SEED_NEO4J_ORG_ID) required")
		os.Exit(2)
	}

	ctx := context.Background()
	drv, err := platformneo4j.New(ctx, platformneo4j.Config{URI: *uri, User: *user, Password: *pass})
	if err != nil {
		logger.Error("driver init failed", "err", err)
		os.Exit(1)
	}
	defer func() { _ = drv.Close(context.Background()) }()
	if err := platformneo4j.Verify(ctx, drv); err != nil {
		logger.Error("driver verify failed", "err", err)
		os.Exit(1)
	}

	store := graphstore.New(drv)
	n, err := store.SeedFromFixtures(ctx, *fixtures, *orgID, *repoSHA)
	if err != nil {
		logger.Error("seed failed", "err", err)
		os.Exit(1)
	}
	fmt.Printf("seeded %d edges for org=%s repo_sha=%s root=%s\n", n, *orgID, *repoSHA, *fixtures)
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
