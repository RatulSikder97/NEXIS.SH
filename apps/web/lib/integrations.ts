// Phase 3 Stage 7 — Integrations SDK for the browser.
//
// Thin wrapper around the control-plane /v1/integrations/* endpoints. All
// calls run client-side (Configure dialogs + page revalidation) and rely on
// the `nexis_session` cookie for auth, so each fetch carries
// `credentials: "include"`.
//
// Errors are intentionally surfaced as plain `Error` (not the AuthError used
// by lib/auth.ts) — these calls are invoked from form submissions where the
// caller only needs `.message` to render inline.

const API = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

// Integration mirrors the JSON shape returned by GET /v1/integrations on the
// control-plane. Keep the union literals in sync with the Go enum.
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

export const integrations = {
  list: async (): Promise<Integration[]> => {
    const r = await fetch(`${API}/v1/integrations`, { credentials: "include" });
    if (!r.ok) await unwrapError(r);
    return r.json();
  },
  connect: async (
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
