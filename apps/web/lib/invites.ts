// Phase 3 Stage 8/10 — invites SDK.
//
// Used by:
//   * Settings → Members & Roles (Stage 8): admin-only list/issue/revoke
//     under /v1/orgs/{id}/invites.
//   * Invite claim flow (Stage 10): public lookup + claim under
//     /v1/invites/{token}{,/claim}.
//
// All admin calls forward the user's session cookie via credentials:"include".
// The public claim endpoints accept no auth — the raw token in the URL is
// the proof of intent.

const API =
  typeof window === "undefined"
    ? process.env.API_URL_INTERNAL ?? process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080"
    : process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

// PendingInvite mirrors the JSON returned by GET /v1/orgs/{id}/invites.
// Fields match the snake_case the Go handler emits via json tags.
export type PendingInvite = {
  token_hash: string;
  email: string;
  role: "admin" | "member";
  expires_at: string;
  claimed_at?: string | null;
};

// InviteInfo matches the public GET /v1/invites/{token} response.
export type InviteInfo = {
  org: { id: string; name: string; slug: string };
  role: "admin" | "member";
  inviter_email: string;
};

async function unwrap(r: Response): Promise<never> {
  const body = await r.json().catch(() => ({}) as Record<string, unknown>);
  const msg =
    typeof body === "object" && body !== null && "error" in body
      ? String((body as { error: unknown }).error)
      : r.statusText;
  throw new Error(msg);
}

export const invites = {
  list: async (orgId: string): Promise<PendingInvite[]> => {
    const r = await fetch(`${API}/v1/orgs/${orgId}/invites`, {
      credentials: "include",
    });
    if (!r.ok) await unwrap(r);
    return r.json();
  },
  issue: async (
    orgId: string,
    email: string,
    role: "admin" | "member",
  ): Promise<{ token_prefix: string }> => {
    const r = await fetch(`${API}/v1/orgs/${orgId}/invites`, {
      method: "POST",
      credentials: "include",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ email, role }),
    });
    if (!r.ok) await unwrap(r);
    return r.json();
  },
  revoke: async (orgId: string, tokenHash: string): Promise<void> => {
    const r = await fetch(`${API}/v1/orgs/${orgId}/invites/${tokenHash}`, {
      method: "DELETE",
      credentials: "include",
    });
    if (!r.ok && r.status !== 204) await unwrap(r);
  },
  // Public — no cookie required. Looks up the org + role + inviter info
  // for the landing page before the invitee picks a password.
  get: async (token: string): Promise<InviteInfo> => {
    const r = await fetch(`${API}/v1/invites/${token}`);
    if (!r.ok) await unwrap(r);
    return r.json();
  },
  // Public — sets the nexis_session cookie on success.
  claim: async (token: string, password: string): Promise<void> => {
    const r = await fetch(`${API}/v1/invites/${token}/claim`, {
      method: "POST",
      credentials: "include",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ password }),
    });
    if (!r.ok) await unwrap(r);
  },
};
