package synthesiser

// Classification contract for the fault-injection fixture library. Each
// fixture under services/control-plane/fixtures/scenarios declares the
// scenario its author expects the fast-path classifier to produce in
// metadata.expected_scenario. This test feeds the fixture's stacktrace +
// logs through classifyFast the same way the Pathfinder fallback surfaces
// them as evidence, and asserts:
//
//   - enum'd classes (null_deref / oom / schema_drift) fast-path match to
//     the declared scenario, and
//   - "unknown" classes do NOT accidentally trip a fast-path rule (they
//     must fall through to the LLM / unknown routing on purpose).
//
// Fixtures that predate the expected_scenario field are skipped.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

const fixtureScenariosDir = "../../../../fixtures/scenarios"

func TestFixtureLibrary_FastPathClassification(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(fixtureScenariosDir, "*.json"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(files) == 0 {
		t.Fatalf("no scenario fixtures found in %s", fixtureScenariosDir)
	}

	checked := 0
	for _, f := range files {
		body, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("%s: read: %v", f, err)
		}
		var fx struct {
			Label      string         `json:"label"`
			Stacktrace string         `json:"stacktrace"`
			Logs       string         `json:"logs"`
			Metadata   map[string]any `json:"metadata"`
		}
		if err := json.Unmarshal(body, &fx); err != nil {
			t.Fatalf("%s: invalid JSON: %v", f, err)
		}
		expRaw, ok := fx.Metadata["expected_scenario"].(string)
		if !ok {
			continue // legacy fixture without a declared expectation
		}
		checked++

		pv := pathfinderView{Evidence: []string{fx.Stacktrace, fx.Logs}}
		got, _, _, matched := classifyFast(pv)

		switch expected := Scenario(expRaw); expected {
		case ScenarioUnknown:
			if matched {
				t.Errorf("%s: expected NO fast-path match (unknown class), but matched %q", fx.Label, got)
			}
		case ScenarioNullDeref, ScenarioSchemaDrift, ScenarioOOM:
			if !matched {
				t.Errorf("%s: expected fast-path match %q, got no match", fx.Label, expected)
			} else if got != expected {
				t.Errorf("%s: expected scenario %q, got %q", fx.Label, expected, got)
			}
		default:
			t.Errorf("%s: metadata.expected_scenario %q is not a valid Scenario enum value", fx.Label, expRaw)
		}
	}
	if checked < 9 {
		t.Fatalf("expected >= 9 fixtures declaring expected_scenario, checked %d", checked)
	}
}
