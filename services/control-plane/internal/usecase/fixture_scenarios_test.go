package usecase

// Fault-injection fixture library validation. Every scenario JSON under
// services/control-plane/fixtures/scenarios must parse, carry the full
// incident shape (label/title/service/environment/stacktrace/logs +
// metadata.incident_kind), and resolve through the eval runner's fixture
// loader — both via the scenarioFixtureFiles aliases and via the
// "<label>.json" filename fallback.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// scenariosDir is relative to this package directory (internal/usecase).
const scenariosDir = "../../fixtures/scenarios"

// TestScenarioFixtures_ParseAndShape — each fixture is valid JSON, the
// label matches the filename stem, all narrative fields are non-empty, and
// metadata carries an incident_kind tag for the eval harness to group by.
func TestScenarioFixtures_ParseAndShape(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(scenariosDir, "*.json"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	// 2 original (oom, schema-drift) + at least 9 fault-injection classes.
	if len(files) < 11 {
		t.Fatalf("expected >= 11 scenario fixtures, found %d", len(files))
	}
	for _, f := range files {
		body, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("%s: read: %v", f, err)
		}
		var raw map[string]any
		if err := json.Unmarshal(body, &raw); err != nil {
			t.Fatalf("%s: invalid JSON: %v", f, err)
		}
		stem := strings.TrimSuffix(filepath.Base(f), ".json")
		if label, _ := raw["label"].(string); label != stem {
			t.Errorf("%s: label %q must match filename stem %q", f, label, stem)
		}
		for _, k := range []string{"title", "service", "environment", "stacktrace", "logs"} {
			if s, _ := raw[k].(string); s == "" {
				t.Errorf("%s: field %q must be a non-empty string", f, k)
			}
		}
		meta, ok := raw["metadata"].(map[string]any)
		if !ok {
			t.Errorf("%s: metadata object missing", f)
			continue
		}
		if kind, _ := meta["incident_kind"].(string); kind == "" {
			t.Errorf("%s: metadata.incident_kind must be set", f)
		}
	}
}

// TestScenarioFixtures_LoadThroughRunner — every scenario label the CLI /
// demo surface accepts resolves to a populated IncidentPayload.
func TestScenarioFixtures_LoadThroughRunner(t *testing.T) {
	t.Setenv("FIXTURE_INCIDENTS_DIR", scenariosDir)
	labels := []string{
		// Aliases in scenarioFixtureFiles.
		"schema-drift", "null-deref", "oom", "synthetic",
		"zero-div", "api-contract-violation", "dependency-breakage",
		"conn-pool-exhaustion", "deadlock", "memory-leak",
		"rate-limit-cascade", "disk-exhaustion",
		// Direct labels via the "<label>.json" fallback.
		"demo-oom", "demo-schema-drift", "demo-null-pointer",
		"demo-memory-leak", "demo-deadlock",
	}
	for _, label := range labels {
		inc := loadFixtureIncidentForRunner(label)
		if inc == nil {
			t.Errorf("scenario %q must resolve to a fixture incident", label)
			continue
		}
		if inc.Title == "" || inc.Service == "" || inc.Stacktrace == "" {
			t.Errorf("scenario %q: incomplete incident %+v", label, inc)
		}
	}
}

// TestScenarioFixtures_MapEntriesExistOnDisk — no scenarioFixtureFiles
// entry may point at a file that does not exist in the control-plane
// scenarios directory (guards against typos when new classes land).
func TestScenarioFixtures_MapEntriesExistOnDisk(t *testing.T) {
	for scenario, file := range scenarioFixtureFiles {
		if _, err := os.Stat(filepath.Join(scenariosDir, file)); err != nil {
			t.Errorf("scenario %q: fixture %q not found in %s: %v", scenario, file, scenariosDir, err)
		}
	}
}
