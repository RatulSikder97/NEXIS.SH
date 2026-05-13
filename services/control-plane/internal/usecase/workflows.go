// Package usecase — workflow start dispatch (Phase 7, projects shard).
//
// StartRecovery is the thin usecase wrapper around domain.WorkflowService.Start
// that the HTTP transport + admin CLI call when kicking off a RecoveryPipeline
// run. It exists to:
//
//   - Resolve an optional project_id against the projects repo so the workflow
//     input carries a verified project mapping.
//   - Stamp project_id into the workflow_runs row at insert time (via the
//     workflow adapter, which folds it onto domain.WorkflowRun.ProjectID).
//
// The function deliberately does NOT do tenant ownership checks beyond what
// the projects repo + workflow service already enforce — adding a second
// layer of RLS-aware filtering here would duplicate behaviour and risk
// drift.
package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// RecoveryStartScenario is the wire-shaped trigger metadata that
// StartRecovery folds into the workflow's PipelineInput payload. Today the
// canonical use case is the admin "start a fresh recovery for incident X"
// path; future expansion (e.g. cron-driven rollbacks) lands here too.
type RecoveryStartScenario struct {
	// IncidentID is the incidents_raw id that triggered the run. Optional —
	// when empty the workflow falls back to the legacy "manual" / "demo"
	// path. Should match the upstream rule emit so dedupe stays correct.
	IncidentID string
	// TriggeredBy labels the run's origin: "manual" | "demo" | "sentinel" |
	// "admin". Surfaces in the timeline and audit row.
	TriggeredBy string
	// Incident is the optional full payload (title / service / stacktrace).
	// Passed straight through to the workflow as the PipelineInput.Incident
	// pointer so agents can read it without re-fetching the row.
	Incident *domain.IncidentPayload
	// RepoSHA scopes the retrieval window. Empty falls back to the workspace
	// default fixture SHA.
	RepoSHA string
}

// ProjectsLookupForRecovery is the narrow port StartRecovery needs from the
// projects repo. Defined here so the test layer can pass a fake without
// dragging in pgx. The real *repo.ProjectsRepo satisfies this via Get.
type ProjectsLookupForRecovery interface {
	Get(ctx context.Context, projectID string) (domain.Project, error)
}

// RecoveryDispatcher is the public type the HTTP transport and CLI depend on.
// Construct once at boot in cmd/server/main.go and reuse across requests.
type RecoveryDispatcher struct {
	workflows domain.WorkflowService
	projects  ProjectsLookupForRecovery
}

// NewRecoveryDispatcher wires the dependencies. projects may be nil — when
// unwired any caller-supplied project_id is silently ignored (the run still
// fires with empty PipelineInput.ProjectID).
func NewRecoveryDispatcher(workflows domain.WorkflowService, projects ProjectsLookupForRecovery) *RecoveryDispatcher {
	return &RecoveryDispatcher{workflows: workflows, projects: projects}
}

// StartRecovery kicks off a RecoveryPipeline workflow for the given workspace,
// optionally scoped to a project. Returns the started run so the caller can
// echo the id back in the API response.
//
// When optionalProjectID is non-empty the function:
//
//  1. Reads the project via projectsRepo.Get to verify it exists + belongs to
//     the caller's org.
//  2. Stamps project_id onto the input JSON so the workflow adapter persists
//     it on workflow_runs.project_id.
//
// A misconfigured project_id (cross-tenant, archived, missing) returns
// domain.ErrNotFound — the HTTP layer maps that to 404. Other lookup errors
// surface verbatim so the caller can decide between retry + giving up.
func (d *RecoveryDispatcher) StartRecovery(
	ctx context.Context,
	princ domain.Principal,
	workspaceID string,
	scenario RecoveryStartScenario,
	optionalProjectID string,
) (domain.WorkflowRun, error) {
	if d == nil {
		return domain.WorkflowRun{}, errors.New("usecase: recovery dispatcher not initialised")
	}
	if d.workflows == nil {
		return domain.WorkflowRun{}, errors.New("usecase: workflow service not wired")
	}

	// Project resolution. Optional path — empty id skips the lookup.
	if optionalProjectID != "" {
		if d.projects == nil {
			return domain.WorkflowRun{}, fmt.Errorf("usecase: projects lookup not wired")
		}
		p, err := d.projects.Get(ctx, optionalProjectID)
		if err != nil {
			return domain.WorkflowRun{}, err
		}
		if p.OrgID != princ.OrgID {
			// Cross-tenant probe — masquerade as not-found rather than
			// leaking the existence of the other tenant's row.
			return domain.WorkflowRun{}, domain.ErrNotFound
		}
		if p.ArchivedAt != nil {
			return domain.WorkflowRun{}, fmt.Errorf("usecase: project archived")
		}
	}

	// Build the input JSON. Mirror the wire shape the workflow adapter's
	// Start() already parses out of inputJSON — keys: triggered_by,
	// incident_id, repo_sha, project_id, incident{...}.
	payload := map[string]any{
		"triggered_by": scenario.TriggeredBy,
		"incident_id":  scenario.IncidentID,
	}
	if scenario.RepoSHA != "" {
		payload["repo_sha"] = scenario.RepoSHA
	}
	if optionalProjectID != "" {
		payload["project_id"] = optionalProjectID
	}
	if scenario.Incident != nil {
		payload["incident"] = map[string]any{
			"label":       scenario.Incident.Label,
			"title":       scenario.Incident.Title,
			"service":     scenario.Incident.Service,
			"environment": scenario.Incident.Environment,
			"stacktrace":  scenario.Incident.Stacktrace,
			"logs":        scenario.Incident.Logs,
		}
	}
	inputJSON, err := json.Marshal(payload)
	if err != nil {
		return domain.WorkflowRun{}, fmt.Errorf("usecase: marshal recovery input: %w", err)
	}

	return d.workflows.Start(ctx, princ, workspaceID, "RecoveryPipeline", inputJSON)
}
