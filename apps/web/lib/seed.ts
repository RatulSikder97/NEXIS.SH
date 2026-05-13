// Phase 8 — Sample-repo seed SDK.
//
// Single endpoint: POST /v1/workspaces/{ws}/seed-sample copies the Phase 4
// validator fixture into a fresh `incidents_raw` row, fires a
// RecoveryPipeline workflow, and returns the new pipeline run id so the
// caller can deep-link to its detail page (typically with `?live=1` to
// flip the timeline into guided-demo mode).

const API = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

export type SeedSampleResp = { run_id: string };

async function unwrap(r: Response): Promise<never> {
  const body = await r.json().catch(() => ({}) as Record<string, unknown>);
  const msg =
    typeof body === "object" && body !== null && "error" in body
      ? String((body as { error: unknown }).error)
      : r.statusText;
  throw new Error(msg);
}

export const seed = {
  // sample triggers the synthetic-fixture flow for a workspace. The control-
  // plane returns 202 with `{ run_id }` once the workflow has been queued;
  // the caller is expected to navigate to the run's detail page where the
  // existing SSE timeline takes over.
  sample: async (workspaceId: string): Promise<SeedSampleResp> => {
    const r = await fetch(
      `${API}/v1/workspaces/${workspaceId}/seed-sample`,
      {
        method: "POST",
        credentials: "include",
        headers: { "content-type": "application/json" },
      },
    );
    if (!r.ok) await unwrap(r);
    return r.json();
  },
};
