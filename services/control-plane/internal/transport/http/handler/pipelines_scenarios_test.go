package handler

// Catalogue contract for the Live Demo console.
//
// Every card in apps/web/components/live-demo/SCENARIOS.ts posts its `id` to
// POST /v1/workspaces/{ws}/pipelines/demo. A card whose id does not resolve to
// a fixture is a dead button in the product, so this test pins the whole
// catalogue: each id must pass scenarioAllowed and load a non-empty incident
// payload for the agents to reason over.
//
// The id list is duplicated here on purpose — it is the wire contract between
// two languages, and a Go test that reads the TypeScript file would silently
// pass if that file moved.

import (
	"os"
	"testing"
)

var demoCatalogueIDs = []string{
	// application
	"null-deref",
	"unhandled_promise_rejection",
	"regex_catastrophic_backtracking",
	"division_by_zero",
	"string_index_out_of_bounds",
	// data
	"connection_pool_exhausted",
	"query_timeout_p99_spike",
	"migration_failed_mid_deploy",
	"pgvector_index_corrupted",
	"deadlock_detected",
	"airflow_dag_stuck",
	"spark_job_oom",
	"dbt_model_compile_fail",
	"kafka_consumer_lag",
	"snowflake_query_cost_spike",
	// deploy
	"bad_canary_deploy_rollback",
	"oom_kill_loop",
	"image_pull_backoff",
	"pdb_blocks_drain",
	"cert_expiring_soon",
	// observability
	"memory_leak_24h_climb",
	"goroutine_leak",
	"cpu_throttling_spike",
	"latency_p99_breach",
	// security
	"secret_committed_to_repo",
	"failed_auth_brute_force",
	"expired_secret_rotation",
}

// withFixtureDir points the loader at the repo's fixture directory. The
// package's own working directory during `go test` is the handler package, so
// the compose/production candidates in loadFixtureIncident do not resolve.
func withFixtureDir(t *testing.T) {
	t.Helper()
	t.Setenv("FIXTURE_INCIDENTS_DIR", "../../../../fixtures/scenarios")
}

func TestDemoCatalogue_EveryScenarioResolvesToAFixture(t *testing.T) {
	withFixtureDir(t)
	if len(demoCatalogueIDs) != 27 {
		t.Fatalf("catalogue drifted: expected 27 scenarios, have %d", len(demoCatalogueIDs))
	}
	for _, id := range demoCatalogueIDs {
		if !scenarioAllowed(id) {
			t.Errorf("%s: rejected by scenarioAllowed — the Run button would 400", id)
			continue
		}
		inc := loadFixtureIncident(id)
		if inc == nil {
			t.Errorf("%s: no fixture incident — agents would run on a placeholder", id)
			continue
		}
		for _, key := range []string{"title", "stacktrace", "logs", "metadata"} {
			if _, ok := inc[key]; !ok {
				t.Errorf("%s: fixture missing %q", id, key)
			}
		}
		md, _ := inc["metadata"].(map[string]any)
		if md == nil {
			t.Errorf("%s: metadata is not an object", id)
			continue
		}
		// root_cause_node is what Pathfinder matches against the seeded
		// codegraph; without it the causal ranking has nothing to score.
		if s, _ := md["root_cause_node"].(string); s == "" {
			t.Errorf("%s: metadata.root_cause_node is empty", id)
		}
	}
}

func TestScenarioAllowed_RejectsPathTraversalAndUnknowns(t *testing.T) {
	withFixtureDir(t)
	for _, bad := range []string{
		"../../../etc/passwd",
		"..",
		"Null-Deref",
		"scenario with spaces",
		"definitely_not_a_scenario",
		"",
	} {
		if scenarioAllowed(bad) {
			t.Errorf("scenarioAllowed(%q) = true, want false", bad)
		}
	}
}

func TestLoadFixtureIncident_MissingDirIsNotFatal(t *testing.T) {
	t.Setenv("FIXTURE_INCIDENTS_DIR", os.TempDir())
	if inc := loadFixtureIncident("definitely_not_a_scenario"); inc != nil {
		t.Fatalf("expected nil incident for an unknown scenario, got %v", inc)
	}
}
