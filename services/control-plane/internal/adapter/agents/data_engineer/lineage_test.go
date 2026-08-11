package data_engineer

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestTablesFromSQL(t *testing.T) {
	cases := []struct {
		name string
		sql  string
		want []string
	}{
		{"alter", "ALTER TABLE orders ADD COLUMN x INT;", []string{"orders"}},
		{"create if not exists", "CREATE TABLE IF NOT EXISTS audit_log (id uuid);", []string{"audit_log"}},
		{"drop if exists", "DROP TABLE IF EXISTS scratch;", []string{"scratch"}},
		{"quoted", `ALTER TABLE "orders" ALTER COLUMN total TYPE numeric;`, []string{"orders"}},
		{"schema qualified", "ALTER TABLE public.orders ADD COLUMN y INT;", []string{"public.orders"}},
		{"multiple deduped", "ALTER TABLE orders ADD COLUMN a INT; ALTER TABLE orders ADD COLUMN b INT; CREATE TABLE items (id uuid);", []string{"orders", "items"}},
		{"lowercase keywords", "alter table users drop column legacy;", []string{"users"}},
		{"no ddl", "UPDATE orders SET total = 0;", nil},
		{"empty", "", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tablesFromSQL(tc.sql)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("table[%d] = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestMigrationsFromStructured(t *testing.T) {
	cases := []struct {
		name       string
		structured map[string]any
		want       int
	}{
		{"nil map", nil, 0},
		{"missing key", map[string]any{"data_backfill": nil}, 0},
		{"empty array", map[string]any{"migrations": []any{}}, 0},
		{"non-array", map[string]any{"migrations": "nope"}, 0},
		{
			"valid single",
			map[string]any{"migrations": []any{
				map[string]any{"version": "20260810120000", "name": "add_col", "up_sql": "ALTER TABLE t ADD COLUMN c INT;", "down_sql": "ALTER TABLE t DROP COLUMN c;"},
			}},
			1,
		},
		{
			"malformed members skipped",
			map[string]any{"migrations": []any{
				"garbage",
				map[string]any{},
				map[string]any{"version": "20260810120000", "name": "keep", "up_sql": "CREATE TABLE x (id uuid);", "down_sql": "DROP TABLE x;"},
			}},
			1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := MigrationsFromStructured(tc.structured)
			if len(got) != tc.want {
				t.Fatalf("got %d migrations %v, want %d", len(got), got, tc.want)
			}
		})
	}
}

func TestBuildMigrationRunEvent_SpecShape(t *testing.T) {
	at := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	migs := []ProposedMigration{
		{Version: "20260810120000", Name: "widen_orders", UpSQL: "ALTER TABLE orders ALTER COLUMN total TYPE numeric;", DownSQL: "ALTER TABLE orders ALTER COLUMN total TYPE text;"},
		{Version: "20260810120001", Name: "no_ddl_backfill", UpSQL: "UPDATE orders SET total = 0;", DownSQL: ""},
	}
	ev := BuildMigrationRunEvent("wf-run-1", LineageJobPropose, "COMPLETE", migs, at)

	if ev.EventType != "COMPLETE" {
		t.Errorf("eventType = %q", ev.EventType)
	}
	if ev.EventTime != "2026-08-10T12:00:00Z" {
		t.Errorf("eventTime = %q", ev.EventTime)
	}
	if ev.Producer != LineageProducer || ev.SchemaURL != LineageSchemaURL {
		t.Errorf("producer/schemaURL = %q/%q", ev.Producer, ev.SchemaURL)
	}
	if ev.Job.Namespace != LineageJobNamespace || ev.Job.Name != LineageJobPropose {
		t.Errorf("job = %+v", ev.Job)
	}
	if _, err := uuid.Parse(ev.Run.RunID); err != nil {
		t.Errorf("run.runId %q is not a UUID: %v", ev.Run.RunID, err)
	}
	// Deterministic per (workflow run, job); distinct across jobs.
	again := BuildMigrationRunEvent("wf-run-1", LineageJobPropose, "COMPLETE", migs, at)
	if again.Run.RunID != ev.Run.RunID {
		t.Errorf("run id not deterministic: %q vs %q", again.Run.RunID, ev.Run.RunID)
	}
	apply := BuildMigrationRunEvent("wf-run-1", LineageJobApply, "COMPLETE", migs, at)
	if apply.Run.RunID == ev.Run.RunID {
		t.Error("propose + apply must be distinct OpenLineage runs")
	}

	// Outputs: one dataset for the DDL table, one migration-name fallback.
	if len(ev.Outputs) != 2 {
		t.Fatalf("outputs = %+v, want 2 datasets", ev.Outputs)
	}
	names := []string{ev.Outputs[0].Name, ev.Outputs[1].Name}
	if names[0] != "migration/no_ddl_backfill" || names[1] != "orders" {
		t.Errorf("output names = %v", names)
	}
	for _, o := range ev.Outputs {
		if o.Namespace != LineageDatasetNamespace {
			t.Errorf("dataset namespace = %q", o.Namespace)
		}
	}
}

// TestBuildMigrationRunEvent_JSONWire pins the marshalled key set to the
// OpenLineage RunEvent contract — camelCase keys, inputs as [] rather than
// null.
func TestBuildMigrationRunEvent_JSONWire(t *testing.T) {
	ev := BuildMigrationRunEvent("wf-run-2", LineageJobApply, "COMPLETE",
		[]ProposedMigration{{Name: "m", UpSQL: "CREATE TABLE t (id uuid);"}}, time.Now().UTC())
	raw, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"eventType", "eventTime", "producer", "schemaURL", "run", "job", "inputs", "outputs"} {
		if _, ok := m[key]; !ok {
			t.Errorf("wire JSON missing %q key: %s", key, raw)
		}
	}
	if strings.Contains(string(raw), `"inputs":null`) {
		t.Errorf("inputs must marshal as [], got %s", raw)
	}
	run, _ := m["run"].(map[string]any)
	if _, ok := run["runId"].(string); !ok {
		t.Errorf("run.runId missing: %s", raw)
	}
	job, _ := m["job"].(map[string]any)
	if job["namespace"] != LineageJobNamespace || job["name"] != LineageJobApply {
		t.Errorf("job wire shape wrong: %s", raw)
	}
}
