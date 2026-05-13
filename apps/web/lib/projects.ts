// Phase 3.5 — Projects SDK.
//
// Wraps the control-plane's project + recovery-policy HTTP surface for the
// console UI:
//
//   POST   /v1/workspaces/{ws}/projects        → Project           (owner|admin)
//   GET    /v1/workspaces/{ws}/projects        → Project[]         (any)
//   GET    /v1/projects/{id}                    → Project           (any)
//   PATCH  /v1/projects/{id}                    → Project           (owner|admin)
//   DELETE /v1/projects/{id}                    → 204               (owner|admin)
//   GET    /v1/projects/{id}/recovery-policy    → RecoveryPolicy    (any)
//   PUT    /v1/projects/{id}/recovery-policy    → Project           (owner|admin)
//
// FE-first: this SDK ships ahead of the backend so non-2xx responses fail
// soft. `list()` returns []; `get()` / `getPolicy()` return null; mutations
// throw so the form can show the error inline.
//
// Wire shapes are snake_case end-to-end to match the Go domain layer; the FE
// never camelCases them. Optional selector fields are typed as `?:` so the
// UI can render "—" placeholders for unconnected integrations.

const API =
  typeof window === "undefined"
    ? process.env.API_URL_INTERNAL ?? process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080"
    : process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

export type ProjectEnvironment = "dev" | "staging" | "prod";

// ProjectSelectors maps to the six integration providers we wire on a
// project. Each block is independent — a project can connect Sentry but
// skip ArgoCD entirely. The integration provider for each field mirrors
// the IntegrationProvider union in `lib/integrations.ts`.
export type ProjectSelectors = {
  github_repo?: string;
  github_installation_id?: number;
  github_default_branch?: string;
  sentry_organization_slug?: string;
  sentry_project_slug?: string;
  argocd_server_url?: string;
  argocd_app_name?: string;
  argocd_project?: string;
  pagerduty_service_id?: string;
  pagerduty_escalation_policy_id?: string;
  datadog_service_tag?: string;
  datadog_env_tag?: string;
  slack_channel_id?: string;
};

// RecoveryPolicy mirrors the 7-field DefaultRecoveryPolicy on the Go side.
// `medium_countdown_seconds` is the auto-merge cooldown for medium-severity
// PRs; `kill_switch_enabled` is the global emergency stop for the project.
export type RecoveryPolicy = {
  auto_merge_low_severity: boolean;
  auto_merge_medium_severity: boolean;
  medium_countdown_seconds: number;
  kill_switch_enabled: boolean;
  approver_user_ids: string[];
  max_concurrent_recoveries: number;
  rollback_on_slo_breach: boolean;
};

// ProjectSLO is best-effort: backends that don't yet project SLOs will send
// an empty object — the cards should render "—" for missing fields.
export type ProjectSLO = {
  availability_target?: number;
  latency_p95_ms?: number;
  error_rate_pct?: number;
};

export type Project = {
  id: string;
  org_id: string;
  workspace_id: string;
  name: string;
  slug: string;
  description: string;
  environment: ProjectEnvironment;
  owner_user_id: string;
  selectors: ProjectSelectors;
  recovery_policy: RecoveryPolicy;
  slo: ProjectSLO;
  created_at: string;
  updated_at: string;
};

// DEFAULT_POLICY mirrors `domain.DefaultRecoveryPolicy` — conservative
// settings: no auto-merge, no rollback, kill-switch off, 10 minute medium
// countdown, single concurrent recovery, empty approver list.
//
// Used by the wizard's Step 5 to seed the form so the user sees the same
// defaults the backend would apply if they skipped this step entirely.
export const DEFAULT_POLICY: RecoveryPolicy = {
  auto_merge_low_severity: false,
  auto_merge_medium_severity: false,
  medium_countdown_seconds: 600,
  kill_switch_enabled: false,
  approver_user_ids: [],
  max_concurrent_recoveries: 1,
  rollback_on_slo_breach: false,
};

// CreateProjectInput is the body the wizard POSTs to
// /v1/workspaces/{ws}/projects. We keep the shape loose for fields the
// backend may not validate yet — selectors + recovery_policy default to
// the conservative DEFAULT_POLICY server-side when omitted.
export type CreateProjectInput = {
  name: string;
  slug?: string;
  description?: string;
  environment: ProjectEnvironment;
  owner_user_id?: string;
  selectors?: ProjectSelectors;
  recovery_policy?: RecoveryPolicy;
  slo?: ProjectSLO;
};

// UpdateProjectInput is the body PATCHed to /v1/projects/{id}. Every
// field is optional — pass only the keys you intend to change. The
// backend merges into the existing row.
export type UpdateProjectInput = Partial<CreateProjectInput>;

// ServerFetchOptions carries the cookie header for server-side fetches
// (page.tsx loaders). Client-side calls pass nothing and rely on the
// browser's session cookie via credentials: "include".
export type ServerFetchOptions = {
  cookie?: string;
};

function fetchInit(opts: ServerFetchOptions | undefined, init: RequestInit = {}): RequestInit {
  if (opts?.cookie) {
    return {
      ...init,
      headers: {
        ...(init.headers ?? {}),
        cookie: opts.cookie,
      },
      cache: "no-store",
    };
  }
  return {
    ...init,
    credentials: "include",
    cache: "no-store",
  };
}

async function unwrapError(r: Response): Promise<never> {
  const body = (await r.json().catch(() => ({}))) as { error?: string };
  throw new Error(body.error ?? r.statusText);
}

export const projects = {
  // list returns all projects in the workspace. Fails soft to [] when the
  // endpoint isn't ready yet (BE-A ships in parallel).
  list: async (
    wsId: string,
    opts?: ServerFetchOptions,
  ): Promise<Project[]> => {
    if (!wsId) return [];
    try {
      const r = await fetch(
        `${API}/v1/workspaces/${wsId}/projects`,
        fetchInit(opts),
      );
      if (!r.ok) return [];
      const body = (await r.json()) as unknown;
      return Array.isArray(body) ? (body as Project[]) : [];
    } catch {
      return [];
    }
  },

  // get returns one project by id. Returns null on 404 or transient errors
  // so the page can render the EmptyState fallback.
  get: async (
    id: string,
    opts?: ServerFetchOptions,
  ): Promise<Project | null> => {
    if (!id) return null;
    try {
      const r = await fetch(`${API}/v1/projects/${id}`, fetchInit(opts));
      if (!r.ok) return null;
      return (await r.json()) as Project;
    } catch {
      return null;
    }
  },

  // create POSTs a new project. Throws on non-2xx so the wizard can show
  // server-side validation errors (e.g. "github repo not in installation").
  create: async (
    wsId: string,
    input: CreateProjectInput,
  ): Promise<Project> => {
    const r = await fetch(`${API}/v1/workspaces/${wsId}/projects`, {
      method: "POST",
      credentials: "include",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(input),
    });
    if (!r.ok) await unwrapError(r);
    return (await r.json()) as Project;
  },

  // update PATCHes one project. Pass only the fields you intend to change.
  update: async (id: string, input: UpdateProjectInput): Promise<Project> => {
    const r = await fetch(`${API}/v1/projects/${id}`, {
      method: "PATCH",
      credentials: "include",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(input),
    });
    if (!r.ok) await unwrapError(r);
    return (await r.json()) as Project;
  },

  // archive soft-deletes the project (204 on success). Named `archive`
  // rather than `delete` because the backend marks the row as archived
  // instead of hard-deleting — same shape as workspaces.suspend().
  archive: async (id: string): Promise<void> => {
    const r = await fetch(`${API}/v1/projects/${id}`, {
      method: "DELETE",
      credentials: "include",
    });
    if (!r.ok && r.status !== 204) await unwrapError(r);
  },

  // getPolicy returns the project's RecoveryPolicy. Returns null on
  // non-2xx so the policy tab can show "Endpoint coming soon".
  getPolicy: async (
    id: string,
    opts?: ServerFetchOptions,
  ): Promise<RecoveryPolicy | null> => {
    if (!id) return null;
    try {
      const r = await fetch(
        `${API}/v1/projects/${id}/recovery-policy`,
        fetchInit(opts),
      );
      if (!r.ok) return null;
      return (await r.json()) as RecoveryPolicy;
    } catch {
      return null;
    }
  },

  // updatePolicy PUTs the new policy and returns the project. The PUT
  // semantic is "replace the whole policy block" — pass every field.
  updatePolicy: async (
    id: string,
    policy: RecoveryPolicy,
  ): Promise<Project> => {
    const r = await fetch(`${API}/v1/projects/${id}/recovery-policy`, {
      method: "PUT",
      credentials: "include",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(policy),
    });
    if (!r.ok) await unwrapError(r);
    return (await r.json()) as Project;
  },
};

// ConnectedIntegration describes which integration providers a project's
// selectors point at — used by the list cards' icon row to dim unconnected
// providers. The mapping is fully derived (no extra API call).
export type ConnectedIntegrationProvider =
  | "github"
  | "sentry"
  | "argocd"
  | "slack"
  | "datadog"
  | "pagerduty";

export function connectedProviders(p: Project): Set<ConnectedIntegrationProvider> {
  const s = new Set<ConnectedIntegrationProvider>();
  const sel = p.selectors ?? {};
  if (sel.github_repo) s.add("github");
  if (sel.sentry_organization_slug || sel.sentry_project_slug) s.add("sentry");
  if (sel.argocd_app_name || sel.argocd_server_url) s.add("argocd");
  if (sel.slack_channel_id) s.add("slack");
  if (sel.datadog_service_tag || sel.datadog_env_tag) s.add("datadog");
  if (sel.pagerduty_service_id || sel.pagerduty_escalation_policy_id) {
    s.add("pagerduty");
  }
  return s;
}
