// Deployments SDK — preview deploys via the deploy-engine.
//
// Wraps the control-plane's project deployment HTTP surface for the
// console UI:
//
//   POST /v1/projects/{id}/deploy                              → Deployment (building)
//   GET  /v1/projects/{id}/deployments                         → Deployment[] (newest first)
//   POST /v1/projects/{id}/deployments/{deployment_id}/stop    → { status: "stopped" }
//
// The control-plane proxies these to the deploy-engine service, which
// clones the project's repo, builds an image (repo Dockerfile or a
// generated one), runs the container, and health-checks it. That work is
// NOT in the request/response cycle: `deploy()` returns a "building" row
// within milliseconds, and the same row transitions to running/failed in
// the background — poll list() (or pollUntilTerminal below) to watch it
// resolve. This changed from a synchronous design (a deploy could take
// 10+ minutes on a cold cache, and the platform's own 60-second request
// timeout was killing it mid-build) — see the control-plane's
// handler/deployments.go package doc for the full story.
//
// Wire convention (mirrors the validator adapter): a 4xx/5xx from the POST
// means the request itself was rejected (bad binding, no GitHub App, auth) —
// nothing was queued. A 202 means a row now exists and must be polled for
// its outcome; "building" is a real, persisted status now, not a
// client-only placeholder.
//
// JSON shapes are snake_case end-to-end, matching the Go DTOs verbatim —
// same as lib/pipelines.ts. `list()` fails soft to [] because the FE
// ships ahead of the backend (same FE-first pattern as lib/projects.ts).

const API =
  typeof window === "undefined"
    ? (process.env.API_URL_INTERNAL ??
      process.env.NEXT_PUBLIC_API_URL ??
      "http://localhost:8080")
    : (process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080");

// DeploymentStatus is the lifecycle of one preview deployment.
//   building → row created, clone/build/run/health-check in progress
//   running  → container is up and passed its health check
//   failed   → build failed, health check failed, or the engine call
//              itself never reached the sidecar (a transport error still
//              resolves the row to failed rather than leaving it stuck)
//   stopped  → container was stopped/removed via the stop endpoint
export type DeploymentStatus = "building" | "running" | "failed" | "stopped";

// DetectedStack is the deploy-engine's language/runtime detection result,
// used to pick or generate a Dockerfile.
export type DetectedStack = "node" | "python" | "go" | "static" | "unknown";

// DockerfileSource records whether the image was built from a Dockerfile
// committed in the repo or one the deploy-engine generated for the
// detected stack.
export type DockerfileSource = "repo" | "generated";

// Deployment is the wire shape shared by all three endpoints. `url` and
// `port` are null unless the deployment is running; `error` is set on
// failed deploys with a short machine-readable-ish summary.
export type Deployment = {
  deployment_id: string;
  project_id: string;
  status: DeploymentStatus;
  url: string | null;
  port: number | null;
  image_tag: string;
  dockerfile_source: DockerfileSource;
  detected_stack: DetectedStack;
  build_log: string;
  container_log: string;
  error?: string;
  started_at: string;
  finished_at: string;
};

async function throwTransport(r: Response): Promise<never> {
  const body = (await r
    .json()
    .catch(() => ({}) as Record<string, unknown>)) as { error?: string };
  throw new Error(body.error ?? r.statusText);
}

// terminalStatuses are the statuses pollUntilTerminal stops on.
const terminalStatuses = new Set<DeploymentStatus>([
  "running",
  "failed",
  "stopped",
]);

export const deployments = {
  // deploy queues a build and resolves almost immediately with the
  // "building" row — it does NOT wait for the container to come up.
  // Callers must poll (see pollUntilTerminal) to learn the outcome.
  // Throws on transport/auth errors (401/500/network) or a request the
  // control-plane rejected outright (no GitHub App bound, etc.) — those
  // never got as far as creating a row.
  deploy: async (projectId: string): Promise<Deployment> => {
    const r = await fetch(`${API}/v1/projects/${projectId}/deploy`, {
      method: "POST",
      credentials: "include",
      cache: "no-store",
    });
    if (r.ok) {
      return (await r.json()) as Deployment;
    }
    return throwTransport(r);
  },

  // pollUntilTerminal watches one deployment id via list() until it leaves
  // "building", or until timeoutMs elapses. Resolves with the last-seen row
  // either way (never throws on a timeout — the caller decides how to
  // present "still building after N minutes", since that is not the same
  // failure as a rejected request). Resolves immediately if the row is
  // already terminal or has disappeared from the list (defensive — should
  // not happen, but a caller awaiting forever on a vanished row would be a
  // worse bug than returning null).
  pollUntilTerminal: async (
    projectId: string,
    deploymentId: string,
    {
      intervalMs = 3_000,
      timeoutMs = 11 * 60_000, // slightly past the server's own ~10-minute budget
      signal,
    }: { intervalMs?: number; timeoutMs?: number; signal?: AbortSignal } = {},
  ): Promise<Deployment | null> => {
    const deadline = Date.now() + timeoutMs;
    for (;;) {
      const rows = await deployments.list(projectId);
      const row = rows.find((d) => d.deployment_id === deploymentId);
      if (!row || terminalStatuses.has(row.status)) return row ?? null;
      if (signal?.aborted || Date.now() >= deadline) return row;
      await new Promise((resolve) => setTimeout(resolve, intervalMs));
    }
  },

  // list returns the project's deployments, most recent first. Fails soft
  // to [] when the endpoint isn't live yet (FE ships ahead of the BE).
  list: async (projectId: string): Promise<Deployment[]> => {
    if (!projectId) return [];
    try {
      const r = await fetch(`${API}/v1/projects/${projectId}/deployments`, {
        credentials: "include",
        cache: "no-store",
      });
      if (!r.ok) return [];
      const body = (await r.json()) as unknown;
      return Array.isArray(body) ? (body as Deployment[]) : [];
    } catch {
      return [];
    }
  },

  // stop stops + removes the deployment's container (docker rm -f on the
  // deploy-engine side). Throws on non-2xx so the caller can surface the
  // error inline; on success the caller should refetch the list.
  stop: async (
    projectId: string,
    deploymentId: string,
  ): Promise<{ status: "stopped" }> => {
    const r = await fetch(
      `${API}/v1/projects/${projectId}/deployments/${deploymentId}/stop`,
      {
        method: "POST",
        credentials: "include",
        cache: "no-store",
      },
    );
    if (!r.ok) return throwTransport(r);
    return (await r.json()) as { status: "stopped" };
  },
};
