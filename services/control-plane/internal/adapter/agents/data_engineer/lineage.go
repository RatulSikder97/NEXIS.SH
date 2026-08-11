package data_engineer

import (
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// OpenLineage RunEvent emission — the FYP-spec Pathfinder/Data Engineer
// lineage contract. OpenLineage is an event schema, not an SDK: a RunEvent
// is JSON with eventType/eventTime/run{runId}/job{namespace,name}/inputs[]/
// outputs[] plus producer + schemaURL. The types below marshal to exactly
// that shape; persistence lives in repo.LineageRepo (lineage_events,
// migration 0028) and the emission hook in the recovery activities
// (DataEngineerMigrate proposes, GitOpsDeploy applies).

const (
	// LineageProducer identifies this codebase as the event producer, per the
	// OpenLineage spec's producer URI convention.
	LineageProducer = "https://github.com/nexis-eco/nexis/services/control-plane"
	// LineageSchemaURL pins the spec version the emitted JSON conforms to.
	LineageSchemaURL = "https://openlineage.io/spec/2-0-2/OpenLineage.json#/definitions/RunEvent"
	// LineageJobNamespace scopes every Data Engineer job name.
	LineageJobNamespace = "nexis.data_engineer"
	// LineageJobPropose is the job name for "the agent proposed migrations".
	LineageJobPropose = "migration.propose"
	// LineageJobApply is the job name for "the approved migrations shipped in
	// the recovery PR" (the GitOps deploy).
	LineageJobApply = "migration.apply"
	// LineageDatasetNamespace is the dataset namespace for tables touched by
	// a proposed migration, per the OpenLineage postgres naming convention.
	LineageDatasetNamespace = "postgres://control-plane"
)

// RunEvent is an OpenLineage-spec-compatible run state update.
type RunEvent struct {
	EventType string       `json:"eventType"`
	EventTime string       `json:"eventTime"`
	Producer  string       `json:"producer"`
	SchemaURL string       `json:"schemaURL"`
	Run       RunRef       `json:"run"`
	Job       JobRef       `json:"job"`
	Inputs    []DatasetRef `json:"inputs"`
	Outputs   []DatasetRef `json:"outputs"`
}

// RunRef is the spec's run object — runId is a UUID.
type RunRef struct {
	RunID string `json:"runId"`
}

// JobRef is the spec's job object.
type JobRef struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

// DatasetRef is the spec's dataset object, used for both inputs and outputs.
type DatasetRef struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

// ProposedMigration is the typed view of one entry in the Data Engineer's
// structured "migrations" output (see SchemaJSON).
type ProposedMigration struct {
	Version string
	Name    string
	UpSQL   string
	DownSQL string
}

// MigrationsFromStructured pulls the migrations array out of the Data
// Engineer's schema-validated structured output. Tolerates the two shapes an
// activity payload takes on either side of a Temporal JSON round-trip
// (map[string]any members only — the schema guarantees objects). Returns nil
// when the map carries no migrations, which callers treat as "nothing to
// emit".
func MigrationsFromStructured(structured map[string]any) []ProposedMigration {
	if structured == nil {
		return nil
	}
	raw, ok := structured["migrations"].([]any)
	if !ok || len(raw) == 0 {
		return nil
	}
	out := make([]ProposedMigration, 0, len(raw))
	for _, m := range raw {
		mm, ok := m.(map[string]any)
		if !ok {
			continue
		}
		mig := ProposedMigration{}
		mig.Version, _ = mm["version"].(string)
		mig.Name, _ = mm["name"].(string)
		mig.UpSQL, _ = mm["up_sql"].(string)
		mig.DownSQL, _ = mm["down_sql"].(string)
		if mig.Name == "" && mig.UpSQL == "" {
			continue
		}
		out = append(out, mig)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// BuildMigrationRunEvent renders the RunEvent for a set of proposed
// migrations. eventType is one of the spec's RunState values (the emission
// hooks use COMPLETE — each hook records a finished fact, not a live run).
//
// run.runId is a deterministic UUIDv5 of (workflowRunID, jobName), so
// re-emissions after a Temporal activity retry collapse onto the same
// OpenLineage run while propose + apply for one workflow stay distinct runs.
//
// Outputs carry one dataset per table the migrations' up_sql touches
// (best-effort DDL parse); a migration whose SQL yields no table names falls
// back to a migration/<name> dataset so the event is never output-less.
// Inputs is always the empty array — the spec wants [] rather than null.
func BuildMigrationRunEvent(workflowRunID, jobName, eventType string, migs []ProposedMigration, at time.Time) RunEvent {
	seen := map[string]struct{}{}
	outputs := []DatasetRef{}
	add := func(name string) {
		if name == "" {
			return
		}
		if _, dup := seen[name]; dup {
			return
		}
		seen[name] = struct{}{}
		outputs = append(outputs, DatasetRef{Namespace: LineageDatasetNamespace, Name: name})
	}
	for _, m := range migs {
		tables := tablesFromSQL(m.UpSQL)
		if len(tables) == 0 {
			add("migration/" + m.Name)
			continue
		}
		for _, t := range tables {
			add(t)
		}
	}
	sort.Slice(outputs, func(i, j int) bool { return outputs[i].Name < outputs[j].Name })

	return RunEvent{
		EventType: eventType,
		EventTime: at.UTC().Format(time.RFC3339),
		Producer:  LineageProducer,
		SchemaURL: LineageSchemaURL,
		Run:       RunRef{RunID: lineageRunID(workflowRunID, jobName)},
		Job:       JobRef{Namespace: LineageJobNamespace, Name: jobName},
		Inputs:    []DatasetRef{},
		Outputs:   outputs,
	}
}

// lineageRunID derives the spec-required UUID run id deterministically from
// our workflow run + job name (UUIDv5 over the URL namespace).
func lineageRunID(workflowRunID, jobName string) string {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("nexis:lineage:"+jobName+":"+workflowRunID)).String()
}

// ddlTablePattern matches the table name following CREATE/ALTER/DROP TABLE,
// tolerating IF [NOT] EXISTS and quoted or schema-qualified identifiers.
var ddlTablePattern = regexp.MustCompile(
	`(?i)\b(?:CREATE|ALTER|DROP)\s+TABLE\s+(?:IF\s+(?:NOT\s+)?EXISTS\s+)?("?[A-Za-z_][A-Za-z0-9_]*"?(?:\."?[A-Za-z_][A-Za-z0-9_]*"?)?)`)

// tablesFromSQL extracts the distinct table names a migration's DDL touches,
// in first-appearance order. Best-effort by design: lineage datasets are
// telemetry, so an exotic statement the regex misses degrades to the
// migration-name fallback rather than an error.
func tablesFromSQL(sql string) []string {
	matches := ddlTablePattern.FindAllStringSubmatch(sql, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		name := strings.ReplaceAll(m[1], `"`, "")
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}
