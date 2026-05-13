// Phase 3.5 Stage 6 — Workspaces SDK.
//
// Client-side wrapper over the control-plane workspace endpoints. All calls
// run with credentials: "include" so the browser carries the nexis_session
// cookie. SSE provisioning events come back as JSON-encoded frames via
// EventSource — withCredentials is required for the cookie to ride along.

const API = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

export type Region = { id: string; name: string; continent: string };
export type WorkspaceStatus =
  | "provisioning"
  | "ready"
  | "error"
  | "suspended";

export type Workspace = {
  id: string;
  org_id: string;
  name: string;
  slug: string;
  region: string;
  status: WorkspaceStatus;
  status_message?: string;
  provisioning_step?: string;
  created_at: string;
  ready_at?: string;
  updated_at: string;
};

export type ProvisioningStep = {
  step: string;
  label: string;
  progress: number;
  status: "in_progress" | "ready" | "error";
  message?: string;
  ts: string;
};

export const workspaces = {
  regions: async (): Promise<Region[]> => {
    const r = await fetch(`${API}/v1/workspaces/regions`, {
      credentials: "include",
    });
    if (!r.ok) throw new Error(r.statusText);
    return r.json();
  },
  list: async (): Promise<Workspace[]> => {
    const r = await fetch(`${API}/v1/workspaces`, {
      credentials: "include",
    });
    if (!r.ok) throw new Error(r.statusText);
    return r.json();
  },
  get: async (id: string): Promise<Workspace> => {
    const r = await fetch(`${API}/v1/workspaces/${id}`, {
      credentials: "include",
    });
    if (!r.ok) throw new Error(r.statusText);
    return r.json();
  },
  create: async (name: string, region: string): Promise<Workspace> => {
    const r = await fetch(`${API}/v1/workspaces`, {
      method: "POST",
      credentials: "include",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ name, region }),
    });
    if (!r.ok) {
      const body = (await r.json().catch(() => ({}))) as { error?: string };
      throw new Error(body.error ?? r.statusText);
    }
    return r.json();
  },
  suspend: async (id: string): Promise<void> => {
    await fetch(`${API}/v1/workspaces/${id}`, {
      method: "DELETE",
      credentials: "include",
    });
  },
  events: (
    id: string,
    onEvent: (ev: ProvisioningStep) => void,
  ): EventSource => {
    const es = new EventSource(`${API}/v1/workspaces/${id}/events`, {
      withCredentials: true,
    });
    es.onmessage = (e) => {
      try {
        onEvent(JSON.parse(e.data));
      } catch {
        /* ignore malformed frames + keep-alives */
      }
    };
    return es;
  },
};
