// Phase 3 Stage 8 — User preferences SDK.
//
// Thin wrapper around /v1/me/preferences. Only the `theme` key is recognised
// today; future keys (compact mode, default landing, etc.) pass through as
// the control-plane stores the entire object. The PATCH replaces — the
// browser sends the full preferences object on each save.

const API =
  typeof window === "undefined"
    ? process.env.API_URL_INTERNAL ?? process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080"
    : process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

export type Preferences = {
  theme?: "light" | "dark" | "system";
  [k: string]: unknown;
};

async function unwrap(r: Response): Promise<never> {
  const body = await r.json().catch(() => ({}) as Record<string, unknown>);
  const msg =
    typeof body === "object" && body !== null && "error" in body
      ? String((body as { error: unknown }).error)
      : r.statusText;
  throw new Error(msg);
}

export const preferences = {
  get: async (): Promise<Preferences> => {
    const r = await fetch(`${API}/v1/me/preferences`, {
      credentials: "include",
    });
    if (!r.ok) await unwrap(r);
    return r.json();
  },
  patch: async (next: Preferences): Promise<void> => {
    const r = await fetch(`${API}/v1/me/preferences`, {
      method: "PATCH",
      credentials: "include",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(next),
    });
    if (!r.ok && r.status !== 204) await unwrap(r);
  },
};
