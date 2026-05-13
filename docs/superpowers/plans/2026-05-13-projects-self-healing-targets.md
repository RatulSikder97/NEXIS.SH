# Projects — Self-Healing Targets

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development`. Steps use checkbox (`- [ ]`) syntax for tracking.

**Should this exist?** Yes. Today the data model jumps from `workspace` → `incident` directly. There is no `project` row that says "this GitHub repo + this Sentry project + this ArgoCD app + this PagerDuty service collectively describe the thing we're auto-healing." Without it:

- Recovery has nowhere to look up *which* repo to open a PR against.
- Sentinel can't route a Datadog alert to the right service.
- The dashboard can't show per-service MTTR.
- A workspace with five services has to share one set of integration mappings — meaning auto-recovery is workspace-wide on/off, not per-service.

A **Project** is the first-class self-healing target. One row per service/codebase the customer wants NEXIS to watch and fix.

**Architecture:** A new `projects` table under each workspace. Each project bundles its integration mappings (GitHub repo, Sentry project_slug, ArgoCD app_name, PagerDuty service_id, Datadog tag selector), recovery policy (auto-merge thresholds, kill-switch, approver list), runtime environment, and SLOs. Sentinel routes incoming `RawIncident` rows to a project by fingerprint match on the project's selectors. Recovery pipeline gains a `project_id` column and uses the project's integration mappings to act.

**Tech stack:** Same as the rest of the platform — Go + Postgres + Next.js + RLS via `org_id` and `workspace_id`. New domain types, repo, usecase service, HTTP surface, console UI.

---

## Why this changes everything downstream

1. **Sentinel routing.** Incoming Sentry events carry `project.slug`; Datadog carries `service` tag; PagerDuty carries `service.id`. Sentinel maps each to a `project_id` via project selectors. Today it doesn't — every event hits the workspace-wide recovery flow.

2. **Recovery pipeline.** Architect/Backend agents need to know *which repo* to read graphs from and *which branch* to open PRs against. Currently hard-coded to a fixture repo. With projects, the recovery activity reads `project.github_installation_id + project.github_repo` and acts on that.

3. **Per-service KPIs.** MTTR, fix-precision, NASA-TLX — all of these are per-service measurements in industry research. We can't publish credible numbers without project-level aggregation.

4. **Auto-recovery policy.** Today: workspace-wide "low=auto, medium=2min countdown, high=human". Customers will want per-project policy ("auto-merge OK for the cache service, never for the payments service").

5. **Onboarding UX.** "Connect your first repo" is the natural first-action after sign-up. Today the user picks a workspace and… nothing concrete to connect. Projects give the wizard a target.

---

## File structure

### Backend
- `services/control-plane/internal/domain/project.go` — `Project` aggregate + `ProjectSelectors` + `RecoveryPolicy`.
- `services/control-plane/internal/adapter/repo/projects_repo.go` — `Create/Get/List/Update/Delete/MatchByFingerprint`.
- `services/control-plane/internal/usecase/projects.go` — service: validate selectors against connected integrations, enforce per-tenant project cap.
- `services/control-plane/internal/transport/http/handler/projects.go` — REST + audit.
- `services/control-plane/internal/transport/http/dto/projects.go` — wire shapes.
- `services/control-plane/internal/sentinel/router.go` — match `RawIncident → project_id` using selectors.
- `services/control-plane/migrations/0024_projects.up.sql` + `.down.sql`.

### Frontend
- `apps/web/app/(app)/console/projects/page.tsx` + `client.tsx` — list grid.
- `apps/web/app/(app)/console/projects/new/page.tsx` + `client.tsx` — connect-a-project wizard.
- `apps/web/app/(app)/console/projects/[id]/page.tsx` + `client.tsx` — detail page.
- `apps/web/app/(app)/console/projects/[id]/settings/page.tsx` — recovery policy editor.
- `apps/web/lib/projects.ts` — SDK.
- `apps/web/components/projects/ProjectCard.tsx`, `ProjectConnectWizard.tsx`, `RecoveryPolicyForm.tsx`, `IntegrationLinkRow.tsx`.

---

## Task 1: Schema + domain

**Files:**
- Create: `services/control-plane/migrations/0024_projects.up.sql`
- Create: `services/control-plane/migrations/0024_projects.down.sql`
- Create: `services/control-plane/internal/domain/project.go`

- [ ] **Step 1: Write the migration**

```sql
-- 0024_projects.up.sql
CREATE TABLE projects (
  id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id          uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  workspace_id    uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  name            text NOT NULL,
  slug            text NOT NULL,             -- url-safe: derived from name
  description     text NOT NULL DEFAULT '',
  environment     text NOT NULL CHECK (environment IN ('dev','staging','prod')),
  owner_user_id   uuid REFERENCES users(id) ON DELETE SET NULL,

  -- Integration mappings (nullable — each is optional)
  github_repo               text,           -- "owner/repo"
  github_installation_id    bigint,
  github_default_branch     text DEFAULT 'main',
  sentry_organization_slug  text,
  sentry_project_slug       text,
  argocd_server_url         text,
  argocd_app_name           text,
  argocd_project            text DEFAULT 'default',
  pagerduty_service_id      text,
  pagerduty_escalation_policy_id text,
  datadog_service_tag       text,           -- e.g. "service:orders-api"
  datadog_env_tag           text,
  slack_channel_id          text,           -- where approval prompts post

  -- Recovery policy (JSONB so we can evolve schema without migrations)
  recovery_policy           jsonb NOT NULL DEFAULT '{
    "auto_merge_low_severity": false,
    "auto_merge_medium_severity": false,
    "medium_countdown_seconds": 120,
    "kill_switch_enabled": false,
    "approver_user_ids": [],
    "max_concurrent_recoveries": 1,
    "rollback_on_slo_breach": true
  }'::jsonb,

  -- SLO targets (optional — used by Sentinel for severity scoring)
  slo_availability_target   numeric(5,4),   -- e.g. 0.9995
  slo_latency_p95_ms        int,
  slo_error_rate_pct        numeric(5,2),

  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now(),
  archived_at     timestamptz,
  UNIQUE (workspace_id, slug)
);

CREATE INDEX idx_projects_org_workspace ON projects (org_id, workspace_id) WHERE archived_at IS NULL;
CREATE INDEX idx_projects_github_repo ON projects (github_repo) WHERE github_repo IS NOT NULL;
CREATE INDEX idx_projects_sentry ON projects (sentry_organization_slug, sentry_project_slug) WHERE sentry_project_slug IS NOT NULL;
CREATE INDEX idx_projects_pagerduty_service ON projects (pagerduty_service_id) WHERE pagerduty_service_id IS NOT NULL;
CREATE INDEX idx_projects_datadog_service ON projects (datadog_service_tag) WHERE datadog_service_tag IS NOT NULL;

ALTER TABLE projects ENABLE ROW LEVEL SECURITY;
CREATE POLICY projects_tenant ON projects
  USING (org_id = NULLIF(current_setting('app.current_org_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE, DELETE ON projects TO nexis_app;

-- Add project_id to the downstream tables so recovery + incidents are project-scoped.
ALTER TABLE workflow_runs       ADD COLUMN project_id uuid REFERENCES projects(id) ON DELETE SET NULL;
ALTER TABLE incidents_raw       ADD COLUMN project_id uuid REFERENCES projects(id) ON DELETE SET NULL;
ALTER TABLE approvals           ADD COLUMN project_id uuid REFERENCES projects(id) ON DELETE SET NULL;
ALTER TABLE webhook_deliveries  ADD COLUMN project_id uuid REFERENCES projects(id) ON DELETE SET NULL;
CREATE INDEX idx_workflow_runs_project ON workflow_runs (project_id) WHERE project_id IS NOT NULL;
CREATE INDEX idx_incidents_raw_project ON incidents_raw (project_id) WHERE project_id IS NOT NULL;

-- updated_at trigger
CREATE OR REPLACE FUNCTION projects_set_updated_at() RETURNS trigger AS $$
BEGIN NEW.updated_at = now(); RETURN NEW; END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_projects_updated_at BEFORE UPDATE ON projects
  FOR EACH ROW EXECUTE FUNCTION projects_set_updated_at();
```

- [ ] **Step 2: Write the down migration** mirroring the above (drop in reverse order).

- [ ] **Step 3: Implement the domain type**

```go
// services/control-plane/internal/domain/project.go
package domain

import "time"

type Environment string
const (
    EnvironmentDev     Environment = "dev"
    EnvironmentStaging Environment = "staging"
    EnvironmentProd    Environment = "prod"
)

type ProjectSelectors struct {
    GitHubRepo              string
    GitHubInstallationID    int64
    GitHubDefaultBranch     string
    SentryOrganizationSlug  string
    SentryProjectSlug       string
    ArgoCDServerURL         string
    ArgoCDAppName           string
    ArgoCDProject           string
    PagerDutyServiceID      string
    PagerDutyEscalationPolicyID string
    DatadogServiceTag       string
    DatadogEnvTag           string
    SlackChannelID          string
}

type RecoveryPolicy struct {
    AutoMergeLowSeverity    bool     `json:"auto_merge_low_severity"`
    AutoMergeMediumSeverity bool     `json:"auto_merge_medium_severity"`
    MediumCountdownSeconds  int      `json:"medium_countdown_seconds"`
    KillSwitchEnabled       bool     `json:"kill_switch_enabled"`
    ApproverUserIDs         []string `json:"approver_user_ids"`
    MaxConcurrentRecoveries int      `json:"max_concurrent_recoveries"`
    RollbackOnSLOBreach     bool     `json:"rollback_on_slo_breach"`
}

type SLOTarget struct {
    AvailabilityTarget *float64 // 0.9995
    LatencyP95Ms       *int
    ErrorRatePct       *float64
}

type Project struct {
    ID            string
    OrgID         string
    WorkspaceID   string
    Name          string
    Slug          string
    Description   string
    Environment   Environment
    OwnerUserID   string
    Selectors     ProjectSelectors
    Policy        RecoveryPolicy
    SLO           SLOTarget
    CreatedAt     time.Time
    UpdatedAt     time.Time
    ArchivedAt    *time.Time
}
```

- [ ] **Step 4: Commit** `feat(projects): schema + domain type`.

---

## Task 2: Repository

**Files:**
- Create: `services/control-plane/internal/adapter/repo/projects_repo.go`
- Create: `services/control-plane/internal/adapter/repo/projects_repo_test.go`

- [ ] **Step 1: Write a failing test** `TestProjectsRepo_CreateAndGet`.
- [ ] **Step 2: Implement Create + Get** using the dual-pool pattern.
- [ ] **Step 3: Test + impl `List(ctx, workspaceID)`**.
- [ ] **Step 4: Test + impl `Update(ctx, id, fields)`**.
- [ ] **Step 5: Test + impl `Archive(ctx, id)`** — soft-delete by setting `archived_at`.
- [ ] **Step 6: Test + impl `MatchByFingerprint(ctx, orgID, RawIncident) (project_id, ok)`** — matches by Sentry slug, Datadog service tag, PagerDuty service id, GitHub repo. Returns the first match.
- [ ] **Step 7: Test + impl `CountByWorkspace(ctx, wsID)`** — for the per-tenant cap enforcement.
- [ ] **Step 8: Commit** `feat(projects): repository with selector matching`.

---

## Task 3: Usecase service

**Files:**
- Create: `services/control-plane/internal/usecase/projects.go`
- Create: `services/control-plane/internal/usecase/projects_test.go`

Surface:
```go
type ProjectsService struct {
    repo      *repo.ProjectsRepo
    intRepo   *repo.IntegrationsRepo
    workspaceRepo *repo.WorkspaceRepo
    audit     domain.AuditWriter
}

func (s *ProjectsService) Create(ctx, princ, workspaceID, in CreateProjectInput) (Project, error)
func (s *ProjectsService) Update(ctx, princ, projectID, in UpdateProjectInput) (Project, error)
func (s *ProjectsService) Archive(ctx, princ, projectID) error
func (s *ProjectsService) Get(ctx, princ, projectID) (Project, error)
func (s *ProjectsService) List(ctx, princ, workspaceID) ([]Project, error)
```

Validations:
- Project cap per workspace = 25 (free) / unlimited (paid). Read tenant tier from entitlements.
- `github_repo` MUST belong to a connected GitHub installation for this org (validate via the integration's installation token).
- `sentry_project_slug` MUST exist in the connected Sentry org (validate via API).
- `argocd_app_name` MUST be reachable (validate via API).
- `datadog_service_tag` MUST follow `service:<value>` shape.
- `pagerduty_service_id` MUST be a valid PD service (validate via API).
- Slug auto-generated from name: lowercase, alphanumeric + dashes, unique within workspace.

Audit every create/update/archive with `project.created` / `project.updated` / `project.archived`.

- [ ] **Step 1: Write failing test** `TestCreate_ValidatesGitHubRepoBelongsToInstallation`.
- [ ] **Step 2-6: Implement each validation incrementally.**
- [ ] **Step 7: Commit** `feat(projects): usecase service with cross-integration validation`.

---

## Task 4: HTTP surface

**Files:**
- Create: `services/control-plane/internal/transport/http/handler/projects.go`
- Create: `services/control-plane/internal/transport/http/dto/projects.go`
- Modify: `services/control-plane/internal/transport/http/server.go`

Endpoints:
- `POST   /v1/workspaces/{ws}/projects` (owner|admin) — create
- `GET    /v1/workspaces/{ws}/projects` (any) — list
- `GET    /v1/projects/{id}` (any) — get
- `PATCH  /v1/projects/{id}` (owner|admin) — update
- `DELETE /v1/projects/{id}` (owner|admin) — archive
- `GET    /v1/projects/{id}/recovery-policy` (any)
- `PUT    /v1/projects/{id}/recovery-policy` (owner|admin)

Wire shape — snake_case throughout. Recovery policy as a nested object.

- [ ] **Step 1-7: Each endpoint as its own task; TDD per endpoint.**
- [ ] **Step 8: Commit** `feat(projects): REST surface`.

---

## Task 5: Sentinel routing

**Files:**
- Modify: `services/control-plane/internal/sentinel/multisource.go`
- Create: `services/control-plane/internal/sentinel/router.go`
- Create: `services/control-plane/internal/sentinel/router_test.go`

When `dedupeTriggers` accepts an `IncidentTrigger`:
1. Build a fingerprint key from the source-specific fields (Sentry: `org_slug/project_slug`; Datadog: `service` tag from payload; PagerDuty: `service.id` from payload; GitHub: `repo` from payload).
2. Call `projectsRepo.MatchByFingerprint(ctx, orgID, fingerprint)` → `project_id` or nil.
3. Stamp the trigger with `project_id`. Recovery pipeline picks it up and uses the project's integration mappings.

If no project matches:
- If the org has zero projects → log warn, emit `IncidentDetected` without project_id (fall back to workspace-wide recovery).
- If the org has projects but none matched → log warn, emit anyway with `project_id=nil` + reason "no project matched".

- [ ] **Step 1: Test** `TestRouter_MatchesSentryEventToProject`.
- [ ] **Step 2: Implement matcher.**
- [ ] **Step 3-6: Same for Datadog, PagerDuty, GitHub.**
- [ ] **Step 7: Commit** `feat(sentinel): route incidents to project via selector match`.

---

## Task 6: Recovery pipeline scoping

**Files:**
- Modify: `services/control-plane/internal/workflow/recovery/activities.go`
- Modify: `services/control-plane/internal/workflow/recovery/workflow.go`

When a workflow starts with a `project_id`:
- Architect/Backend agents pull `project.github_repo + installation_id + default_branch` from `domain.Project` (passed as workflow input).
- GitOps PR opener uses these for `OpenPullRequest`.
- Approval Gate consults `project.recovery_policy` (auto-merge thresholds, approver list, kill-switch).
- ArgoCD sync targets `project.argocd_app_name`.
- Slack notifier posts to `project.slack_channel_id` (falls back to workspace-default channel).
- Failure rollback honours `project.recovery_policy.rollback_on_slo_breach`.

- [ ] **Step 1: Test** `TestRecovery_UsesProjectGitHubRepo`.
- [ ] **Step 2: Plumb `project_id` through `WorkflowInput`.**
- [ ] **Step 3: Each agent activity reads the project (passed in via context).**
- [ ] **Step 4-6: Update approval gate, ArgoCD activity, Slack notifier.**
- [ ] **Step 7: Commit** `feat(recovery): per-project recovery flow`.

---

## Task 7: Frontend — list + detail

**Files:**
- Create: `apps/web/app/(app)/console/projects/page.tsx` + `client.tsx`
- Create: `apps/web/app/(app)/console/projects/[id]/page.tsx` + `client.tsx`
- Create: `apps/web/lib/projects.ts`
- Create: `apps/web/components/projects/ProjectCard.tsx`
- Modify: `apps/web/components/console/Sidebar.tsx` — add "Projects" under WORKSPACE

The list page is a card grid (project name + environment chip + connected-integrations row of icons + 7-day MTTR + open-incidents badge). Empty state CTA → "Connect your first project".

The detail page has tabs:
- Overview — KPI strip + recent incidents + recovery success rate.
- Integrations — per-integration link with edit/disconnect.
- Recovery policy — read-only summary linking to the policy editor.
- Activity — project-scoped activity stream.

- [ ] **Step 1-6: Each tab as its own commit increment.**
- [ ] **Step 7: Commit** `feat(web): projects list + detail surface`.

---

## Task 8: Frontend — Connect Project Wizard

**Files:**
- Create: `apps/web/app/(app)/console/projects/new/page.tsx` + `client.tsx`
- Create: `apps/web/components/projects/ProjectConnectWizard.tsx`

5-step wizard:
1. **Basics** — name + environment + description.
2. **Connect GitHub** — pick from the installed repos list (`GET /v1/integrations/github/repos`).
3. **Connect Sentry** — pick from the connected org's projects list.
4. **Connect ArgoCD + PagerDuty + Datadog + Slack** — optional, can skip.
5. **Recovery policy** — auto-merge thresholds, approvers, kill-switch toggle.

Each step validates server-side before letting the user move on.

- [ ] **Step 1: Write the wizard scaffold.**
- [ ] **Step 2-6: Each step.**
- [ ] **Step 7: Commit** `feat(web): connect-project wizard`.

---

## Task 9: Onboarding integration

**Files:**
- Modify: `apps/web/components/onboarding/SampleRepoStep.tsx`
- Modify: `apps/web/app/onboarding/workspace/client.tsx`

After workspace creation, Phase 8 already adds a "Try the synthetic fixture" vs "Connect your own GitHub repo" step. Wire the "Connect your own GitHub repo" CTA to the new Project Connect Wizard at `/console/projects/new`.

- [ ] **Step 1: Update the CTA.**
- [ ] **Step 2: Commit.**

---

## Task 10: Per-project KPIs on the dashboard

**Files:**
- Modify: `apps/web/app/(app)/console/page.tsx`
- Create: `apps/web/components/console/ProjectsHealthGrid.tsx`

On the dashboard, below the KPI strip, add a "Projects health" grid showing each project's MTTR (7-day), open incidents, current recovery status, and a link to the project detail page.

- [ ] **Step 1: Add the grid.**
- [ ] **Step 2: Commit.**

---

## Task 11: End-to-end demo + Definition of Done

A fully-wired customer journey:

1. User signs up + creates workspace.
2. User installs the GitHub App.
3. Wizard pulls the list of accessible repos.
4. User picks `acme/orders-api` + sets environment=staging.
5. Wizard prompts for Sentry: user picks `acme/orders-staging` project.
6. User skips ArgoCD/PD/Datadog/Slack for now.
7. Recovery policy: auto-merge=off, approver=themselves.
8. Project created.
9. User triggers the synthetic fault on the fixture repo (also linked as a project).
10. Sentinel routes the incident to `orders-api` project via Sentry slug match.
11. Recovery pipeline opens a PR against `acme/orders-api` on `main`.
12. Slack DM to the approver. Approve → ArgoCD sync triggers.
13. NASA-TLX modal appears after success.
14. Dashboard shows `orders-api` MTTR = 4m32s.

Acceptance:
- [ ] Create-project E2E test (Playwright) passes.
- [ ] Recovery routes to the right project (integration test in `tests/integration/`).
- [ ] Build + test + vet + arch green.
- [ ] Migration 0024 applied to running stack.
- [ ] No regressions on existing flows.

---

## Effort estimate

| Task | Wall-clock with parallel dispatching |
|---|---|
| 1 schema + domain | 30 min |
| 2 repo | 45 min |
| 3 usecase | 60 min |
| 4 HTTP surface | 60 min |
| 5 sentinel router | 45 min |
| 6 recovery scoping | 90 min |
| 7 FE list + detail | 90 min |
| 8 FE wizard | 90 min |
| 9 onboarding wire | 15 min |
| 10 dashboard grid | 30 min |
| 11 E2E + DoD | 60 min |
| **Total** | **~10 hours** wall-clock |

Parallel BE+FE on tasks 4+7 cuts another 60 min. The sequential bottleneck is tasks 5+6 (sentinel + recovery scoping must wait for project_id plumbing).

---

## Non-goals

- Multi-repo projects (a project = one repo for v1; mono-repo with `path_pattern` filter ships v2).
- Manual selector overrides ("treat any incident with X in title as this project") — v2.
- Auto-discovery of projects from connected installations ("we see you have 12 repos; should we create projects for them?") — v2.
- Project templates for common stacks (Node+Postgres, Go+Redis) — v2.
- Sharing projects across workspaces — never (org boundary is the privacy boundary).

---

## Risk register

| # | Risk | Mitigation |
|---|---|---|
| 1 | Selector matching is brittle if customers tag inconsistently | Add a "test fingerprint" feature in the wizard — paste a sample Sentry/Datadog event, show which project it would route to. |
| 2 | Recovery policy schema drift as we add knobs | JSONB column + versioned migration helper that defaults missing keys. |
| 3 | Cross-integration validation calls slow down project creation | Make validations parallel via errgroup; cap each at 5s. |
| 4 | Customers want to manage 100s of projects | Pagination + search + tag-based filtering on the list page from day 1. |
