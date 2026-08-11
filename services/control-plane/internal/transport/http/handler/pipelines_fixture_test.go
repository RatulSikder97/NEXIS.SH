package handler

// Consistency checks between the demo-scenario whitelist, the fixture map,
// and the fixture files on disk — every scenario the demo endpoint accepts
// must load a complete incident payload.

import "testing"

// handlerScenariosDir is the control-plane fixtures directory relative to
// this package (internal/transport/http/handler).
const handlerScenariosDir = "../../../../fixtures/scenarios"

func TestDemoScenarios_AllResolveToFixtures(t *testing.T) {
	t.Setenv("FIXTURE_INCIDENTS_DIR", handlerScenariosDir)
	for scenario := range demoScenarios {
		file, ok := scenarioToFixture[scenario]
		if !ok {
			t.Errorf("scenario %q whitelisted but missing from scenarioToFixture", scenario)
			continue
		}
		inc := loadFixtureIncident(scenario)
		if inc == nil {
			t.Errorf("scenario %q: fixture %q failed to load", scenario, file)
			continue
		}
		for _, k := range []string{"title", "service", "environment", "stacktrace", "logs"} {
			if s, _ := inc[k].(string); s == "" {
				t.Errorf("scenario %q: fixture %q field %q must be non-empty", scenario, file, k)
			}
		}
	}
}

func TestScenarioToFixture_NoOrphanEntries(t *testing.T) {
	for scenario := range scenarioToFixture {
		if _, ok := demoScenarios[scenario]; !ok {
			t.Errorf("scenarioToFixture entry %q is not whitelisted in demoScenarios", scenario)
		}
	}
}
