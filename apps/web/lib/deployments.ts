// Deployments SDK — preview deploys via the deploy-engine.
//
// Wraps the control-plane's project deployment HTTP surface for the
// console UI:
//
//   POST /v1/projects/{id}/deploy                              → Deployment
//   GET  /v1/projects/{id}/deployments                         → Deployment[] (newest first)
//   POST /v1/projects/{id}/deployments/{deployment_id}/stop    → { status: "stopped" }
//
// The control-plane proxies these to the deploy-engine service, which
// clones the project's repo, builds an image (repo Dockerfile or a
// generated one), runs the container, and health-checks it — all
// synchronously. A deploy call can therefore legitimately take 10–60+
// seconds; callers must render a "building…" state rather than a bare
// spinner.
//
// Wire convention (mirrors the validator adapter): BOTH 200 and 422
// carry the structured Deployment result — 422 means "the build or
// health check failed", not a transport error, and the body still has
// build_log / container_log / error for the UI to render. Any other
// non-2xx (401/500/…) is a real transport/auth error and throws.
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
//   running → container is up and passed its health check
//   failed  → build failed OR the container never became healthy
//   stopped → container was stopped/removed via the stop endpoint
// There is no persisted "building" state — the build is synchronous
// inside the POST /deploy call, so "building" only exists client-side
// while that request is in flight.
export type DeploymentStatus = "running" | "failed" | "stopped";

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

export const deployments = {
  // deploy triggers a synchronous build + run + health check and resolves
  // with the resulting Deployment. Resolves (does NOT throw) on 422 —
  // that's the "build failed" result and still carries the logs the UI
  // needs. Throws only on transport/auth errors (401/500/network).
  deploy: async (projectId: string): Promise<Deployment> => {
    const r = await fetch(`${API}/v1/projects/${projectId}/deploy`, {
      method: "POST",
      credentials: "include",
      cache: "no-store",
    });
    if (r.ok || r.status === 422) {
      return (await r.json()) as Deployment;
    }
    return throwTransport(r);
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
