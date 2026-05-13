// Phase 3 Stage 9 — Audit SDK.
//
// Wraps GET /v1/audit + GET /v1/audit.csv. Filter parameters are forwarded
// verbatim as query string; absent values are stripped so the URL stays
// short. The CSV helper returns a URL string (not a fetch) — the audit page
// links to it via a plain <a target="_blank"> so the browser carries the
// session cookie and triggers a download.

const API =
  typeof window === "undefined"
    ? process.env.API_URL_INTERNAL ?? process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080"
    : process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

// AuditRow mirrors the JSON shape emitted by handler.AuditList. Field names
// are snake_case; metadata is opaque JSON.
export type AuditRow = {
  id: string;
  org_id: string;
  actor: string;
  action: string;
  target: string;
  metadata?: Record<string, unknown> | null;
  created_at: string;
};

export type AuditListResp = {
  rows: AuditRow[];
  total: number;
};

export type AuditFilters = {
  since?: string; // RFC3339
  until?: string; // RFC3339
  actor?: string;
  action?: string;
  limit?: number;
  offset?: number;
};

function buildQuery(f: AuditFilters): string {
  const sp = new URLSearchParams();
  if (f.since) sp.set("since", f.since);
  if (f.until) sp.set("until", f.until);
  if (f.actor) sp.set("actor", f.actor);
  if (f.action) sp.set("action", f.action);
  if (f.limit !== undefined && f.limit > 0) sp.set("limit", String(f.limit));
  if (f.offset !== undefined && f.offset > 0) sp.set("offset", String(f.offset));
  const s = sp.toString();
  return s ? `?${s}` : "";
}

async function unwrap(r: Response): Promise<never> {
  const body = await r.json().catch(() => ({}) as Record<string, unknown>);
  const msg =
    typeof body === "object" && body !== null && "error" in body
      ? String((body as { error: unknown }).error)
      : r.statusText;
  throw new Error(msg);
}

export const audit = {
  list: async (f: AuditFilters = {}): Promise<AuditListResp> => {
    const r = await fetch(`${API}/v1/audit${buildQuery(f)}`, {
      credentials: "include",
    });
    if (!r.ok) await unwrap(r);
    return r.json();
  },
  // csvUrl is the link the page wires to a plain <a>. The browser navigates
  // with the session cookie automatically, so the control-plane's RLS sees
  // the right tenant and the response triggers a download.
  csvUrl: (f: AuditFilters = {}): string => `${API}/v1/audit.csv${buildQuery(f)}`,
};
