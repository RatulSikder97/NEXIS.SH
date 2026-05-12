// Phase 3 Stage 8 — API Keys SDK.
//
// Wraps the control-plane endpoints under /v1/apikeys. Plaintext is returned
// exactly once at creation and never persisted client-side beyond the
// originating component's lifecycle. Errors propagate as plain Error so the
// caller can render `.message` inline.

const API = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

export type APIKey = {
  id: string;
  prefix: string;
  name: string;
  scopes: string[];
  created_at: string;
  last_used_at?: string | "";
};

export type APIKeyCreated = APIKey & { plaintext_once: string };

async function unwrap(r: Response): Promise<never> {
  const body = await r.json().catch(() => ({}) as Record<string, unknown>);
  const msg =
    typeof body === "object" && body !== null && "error" in body
      ? String((body as { error: unknown }).error)
      : r.statusText;
  throw new Error(msg);
}

export const apikeys = {
  list: async (): Promise<APIKey[]> => {
    const r = await fetch(`${API}/v1/apikeys`, { credentials: "include" });
    if (!r.ok) await unwrap(r);
    return r.json();
  },
  create: async (
    name: string,
    scopes: string[],
  ): Promise<APIKeyCreated> => {
    const r = await fetch(`${API}/v1/apikeys`, {
      method: "POST",
      credentials: "include",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ name, scopes }),
    });
    if (!r.ok) await unwrap(r);
    return r.json();
  },
  revoke: async (id: string): Promise<void> => {
    const r = await fetch(`${API}/v1/apikeys/${id}`, {
      method: "DELETE",
      credentials: "include",
    });
    if (!r.ok && r.status !== 204) await unwrap(r);
  },
};
