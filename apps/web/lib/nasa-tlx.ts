// Phase 8 — NASA-TLX SDK.
//
// Two endpoints touched:
//   * GET  /v1/me/org-stats     → returns the current org's recovery counter.
//     The frontend reads this after a successful high-severity recovery to
//     decide whether to open the TLX modal (gated on the 3rd recovery).
//   * POST /v1/nasa-tlx         → submits a TLX response. The server adds
//     user_id + org_id from the session; the client supplies the six
//     21-point scores + an optional notes string + the recovery run id.

const API = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

export type OrgStats = {
  successful_recoveries_count: number;
};

// SubScale is the canonical NASA-TLX six-axis structure. Each value is a
// 0..20 integer (the 21-point scale). The Performance axis is inverted
// (0 = "Perfect", 20 = "Failure") per the NASA TLX manual — the slider UI
// handles that inversion visually.
export type NasaTlxSubmission = {
  mental_demand: number;
  physical_demand: number;
  temporal_demand: number;
  performance: number;
  effort: number;
  frustration: number;
  notes?: string;
  recovery_run_id?: string;
};

async function unwrap(r: Response): Promise<never> {
  const body = await r.json().catch(() => ({}) as Record<string, unknown>);
  const msg =
    typeof body === "object" && body !== null && "error" in body
      ? String((body as { error: unknown }).error)
      : r.statusText;
  throw new Error(msg);
}

export const nasaTlx = {
  orgStats: async (): Promise<OrgStats> => {
    const r = await fetch(`${API}/v1/me/org-stats`, {
      credentials: "include",
    });
    if (!r.ok) await unwrap(r);
    return r.json();
  },
  submit: async (input: NasaTlxSubmission): Promise<void> => {
    const r = await fetch(`${API}/v1/nasa-tlx`, {
      method: "POST",
      credentials: "include",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(input),
    });
    if (!r.ok && r.status !== 204 && r.status !== 201) await unwrap(r);
  },
};
