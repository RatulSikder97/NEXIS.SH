// Phase 9 — intelligent role recommendation SDK.
//
// Used by Settings → Members & Roles: the owner|admin-gated recommendation
// banner (list/refresh/decide under /v1/orgs/{id}/role-recommendations) and
// the real member table (GET /v1/orgs/{id}/members). All calls forward the
// session cookie via credentials:"include", mirroring lib/invites.ts.

const API =
  typeof window === "undefined"
    ? (process.env.API_URL_INTERNAL ??
      process.env.NEXT_PUBLIC_API_URL ??
      "http://localhost:8080")
    : (process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080");

// RoleRecommendation mirrors the JSON emitted by the Go handler's recToWire —
// snake_case keys, RFC3339 timestamps, empty string for undecided fields.
export type RoleRecommendation = {
  id: string;
  user_id: string;
  email: string;
  current_role: "owner" | "admin" | "member";
  recommended_role: "owner" | "admin" | "member";
  rule: string;
  rationale: string;
  status: "pending" | "accepted" | "dismissed";
  created_at: string;
  decided_at?: string;
  decided_by?: string;
};

// OrgMember mirrors GET /v1/orgs/{id}/members. last_login_at is "" when the
// member has never logged in.
export type OrgMember = {
  user_id: string;
  email: string;
  role: "owner" | "admin" | "member";
  joined_at: string;
  last_login_at?: string;
};

async function unwrap(r: Response): Promise<never> {
  const body = await r.json().catch(() => ({}) as Record<string, unknown>);
  const msg =
    typeof body === "object" && body !== null && "error" in body
      ? String((body as { error: unknown }).error)
      : r.statusText;
  throw new Error(msg);
}

export const roleRecommendations = {
  list: async (orgId: string): Promise<RoleRecommendation[]> => {
    const r = await fetch(`${API}/v1/orgs/${orgId}/role-recommendations`, {
      credentials: "include",
    });
    if (!r.ok) await unwrap(r);
    return r.json();
  },
  // Runs the analyser for the org on demand; returns how many NEW pending
  // recommendations the pass created.
  refresh: async (orgId: string): Promise<{ created: number }> => {
    const r = await fetch(
      `${API}/v1/orgs/${orgId}/role-recommendations/refresh`,
      { method: "POST", credentials: "include" },
    );
    if (!r.ok) await unwrap(r);
    return r.json();
  },
  decide: async (
    orgId: string,
    recId: string,
    action: "accept" | "dismiss",
  ): Promise<RoleRecommendation> => {
    const r = await fetch(
      `${API}/v1/orgs/${orgId}/role-recommendations/${recId}/decide`,
      {
        method: "POST",
        credentials: "include",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ action }),
      },
    );
    if (!r.ok) await unwrap(r);
    return r.json();
  },
};

export const orgMembers = {
  list: async (orgId: string): Promise<OrgMember[]> => {
    const r = await fetch(`${API}/v1/orgs/${orgId}/members`, {
      credentials: "include",
    });
    if (!r.ok) await unwrap(r);
    return r.json();
  },
};
