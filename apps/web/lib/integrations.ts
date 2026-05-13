// Phase 3 Stage 7 + Real-integrations Wave 1 — Integrations SDK for the browser.
//
// Thin wrapper around the control-plane /v1/integrations/* endpoints. All
// calls run client-side (Configure dialogs + page revalidation) and rely on
// the `nexis_session` cookie for auth, so each fetch carries
// `credentials: "include"`.
//
// Errors are intentionally surfaced as plain `Error` (not the AuthError used
// by lib/auth.ts) — these calls are invoked from form submissions where the
// caller only needs `.message` to render inline.
//
// Wave 1 adds:
//   * `IntegrationHealth` + new `IntegrationConnection` DTO mirroring the
//     extended DTO landing in Task 1 of the real-integrations plan.
//   * `integrations.connect()` now POSTs `{config: {...}}` (envelope) and
//     returns `void` — matching the new control-plane contract. The old
//     flat-body shape lives on as `connectRaw()` for the legacy per-provider
//     forms (GitHub/Sentry/ArgoCD) until they are folded into the generic
//     ConfigureDialog in Wave 2.

const API =
  typeof window === "undefined"
    ? process.env.API_URL_INTERNAL ?? process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080"
    : process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

// IntegrationProvider mirrors `domain.IntegrationProvider` in the Go
// control-plane. Six providers ship in the real-integrations rollout; keep
// this union in sync with the Go enum + the manifest in
// lib/integrations-config.ts.
export type IntegrationProvider =
  | "github"
  | "sentry"
  | "argocd"
  | "slack"
  | "datadog"
  | "pagerduty";

// Health DTO returned by the new GET /v1/integrations response shape
// (Task 1). `latency_ms` and `last_check_at` are populated when the probe
// runs successfully; `last_error` is populated on degraded/down states.
export type HealthState =
  | "healthy"
  | "degraded"
  | "down"
  | "disconnected"
  | "unknown";

export type IntegrationHealth = {
  state: HealthState;
  latency_ms?: number;
  last_check_at?: string;
  last_error?: string;
};

// IntegrationConnection is the Wave 1 wire shape. The backend layers the new
// `health` field onto the existing connection row. We keep the legacy
// `Integration` alias below for back-compat with components that haven't
// migrated yet.
export type IntegrationConnection = {
  provider: IntegrationProvider;
  connected: boolean;
  health: IntegrationHealth;
  // Legacy fields preserved during the transition. Once Wave 2 lands these
  // can be promoted to non-optional or removed entirely.
  status?: "connected" | "pending" | "error" | "disconnected";
  installation_id?: string;
  metadata?: Record<string, unknown>;
  last_error?: string;
  created_at?: string;
  updated_at?: string;
};

// Integration is the original Phase 3 Stage 7 DTO. Kept as an alias so the
// existing IntegrationCard / *ConfigureForm components compile unchanged.
// New code should use IntegrationConnection.
export type Integration = {
  provider: "github" | "sentry" | "argocd";
  status: "connected" | "pending" | "error" | "disconnected";
  installation_id?: string;
  metadata?: Record<string, unknown>;
  last_error?: string;
  created_at?: string;
  updated_at?: string;
};

async function unwrapError(r: Response): Promise<never> {
  const body = await r.json().catch(() => ({}) as Record<string, unknown>);
  const msg =
    typeof body === "object" && body !== null && "error" in body
      ? String((body as { error: unknown }).error)
      : r.statusText;
  throw new Error(msg);
}

// Normalises the wire response so the UI can rely on `health` always being
// present. Wave 1 ships ahead of the backend, so list endpoints in dev may
// still return the legacy shape without a `health` block; we synthesise one
// from the `connected` (or legacy `status`) flag so the HealthPill doesn't
// crash with `Cannot read properties of undefined (reading 'state')`.
function normaliseConnection(raw: unknown): IntegrationConnection {
  const r = (raw ?? {}) as Partial<IntegrationConnection> & {
    status?: Integration["status"];
  };
  const connected =
    typeof r.connected === "boolean" ? r.connected : r.status === "connected";
  const health: IntegrationHealth = r.health ?? {
    state: connected ? "unknown" : "disconnected",
  };
  return {
    provider: (r.provider ?? "github") as IntegrationProvider,
    connected,
    health,
    status: r.status,
    installation_id: r.installation_id,
    metadata: r.metadata,
    last_error: r.last_error,
    created_at: r.created_at,
    updated_at: r.updated_at,
  };
}

export const integrations = {
  list: async (): Promise<Integration[]> => {
    const r = await fetch(`${API}/v1/integrations`, { credentials: "include" });
    if (!r.ok) await unwrapError(r);
    return r.json();
  },

  // listConnections returns the Wave 1 IntegrationConnection shape with
  // health pre-normalised so the UI never has to null-check `health`.
  listConnections: async (): Promise<IntegrationConnection[]> => {
    const r = await fetch(`${API}/v1/integrations`, { credentials: "include" });
    if (!r.ok) await unwrapError(r);
    const body = (await r.json()) as unknown;
    if (!Array.isArray(body)) return [];
    return body.map(normaliseConnection);
  },

  // Wave 1 connect — POSTs the new envelope `{config: {...}}` and returns
  // void. Used by the generic ConfigureDialog.
  connect: async (provider: string, config: Record<string, string>): Promise<void> => {
    const r = await fetch(`${API}/v1/integrations/${provider}/connect`, {
      method: "POST",
      credentials: "include",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ config }),
    });
    if (!r.ok) await unwrapError(r);
  },

  // Legacy flat-body connect — kept for the existing per-provider forms
  // (GitHubConfigureForm/SentryConfigureForm/ArgoCDConfigureForm) which
  // pre-date the {config: {...}} envelope. Remove when those forms migrate
  // onto ConfigureDialog in Wave 2.
  connectRaw: async (
    provider: string,
    config: Record<string, unknown>,
  ): Promise<Integration> => {
    const r = await fetch(`${API}/v1/integrations/${provider}/connect`, {
      method: "POST",
      credentials: "include",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(config),
    });
    if (!r.ok) await unwrapError(r);
    return r.json();
  },

  disconnect: async (provider: string): Promise<void> => {
    const r = await fetch(`${API}/v1/integrations/${provider}`, {
      method: "DELETE",
      credentials: "include",
    });
    if (!r.ok && r.status !== 204) await unwrapError(r);
  },

  mockInstallGithub: () => {
    // Control-plane will 302 back to /console/integrations?installed=github
    // with the session cookie carried via standard browser navigation.
    window.location.href = `${API}/v1/integrations/github/mock_install`;
  },
};

// generateWebhookSecret produces a 32-byte cryptographically-random value
// encoded as base64url (no padding). Sentry's outbound webhook secret is set
// by the operator, so we generate one on the user's behalf and surface it
// once; the control-plane stores the same value (encrypted by KeyVault) to
// HMAC incoming deliveries.
export function generateWebhookSecret(): string {
  const bytes = new Uint8Array(32);
  crypto.getRandomValues(bytes);
  let binary = "";
  for (let i = 0; i < bytes.length; i++) binary += String.fromCharCode(bytes[i]);
  return btoa(binary)
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=+$/, "");
}
