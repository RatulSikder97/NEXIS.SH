"use client";

// Ops tab — one-click preview deploys via the deploy-engine.
//
// Sits alongside the four inline tabs in client.tsx as the fifth tab of
// the project detail surface. Split into its own file because it owns a
// full fetch/mutate lifecycle (deploy, stop, history) rather than just
// projecting props like the other tabs.
//
// Surface:
//   * "Deploy now" — POST /v1/projects/{id}/deploy. The build + run +
//     health check happen synchronously inside that request, so the call
//     can take 10–60+ seconds; the in-flight state renders an explicit
//     amber "building" panel (not a bare spinner) so the wait doesn't
//     read as a hang.
//   * Latest deployment spotlight — colored status pill (emerald=running,
//     red=failed, amber=building, muted=stopped, matching the console's
//     pill language), live preview URL when running, error + self-healing
//     note when failed (deploy_engine failures raise incidents that the
//     Sentinel→…→QA loop fixes automatically), and collapsible
//     build/container log disclosures. Logs arrive inline on the deploy
//     response — no fetch-on-expand needed (unlike PatchDiffViewer).
//   * History — GET /v1/projects/{id}/deployments, newest first; each row
//     is a collapsed <details> with status/time/stack in the summary.
//
// Degrades to the same EmptyState "Endpoint coming soon" pattern as the
// sibling tabs when the projects backend is missing.

import * as React from "react";
import Link from "next/link";
import type { Route } from "next";
import {
  AlertTriangle,
  ChevronDown,
  ChevronRight,
  ExternalLink,
  History,
  Loader2,
  Rocket,
  ScrollText,
  Square,
} from "lucide-react";

import { Button } from "@/components/ui/Button";
import { EmptyState } from "@/components/empty-state/EmptyState";
import { cn } from "@/lib/utils";
import {
  deployments as deploymentsSdk,
  type Deployment,
  type DeploymentStatus,
} from "@/lib/deployments";
import type { Project } from "@/lib/projects";

// PillStatus extends the wire statuses with the client-only "building"
// state shown while the synchronous deploy request is in flight.
type PillStatus = DeploymentStatus | "building";

function pillClasses(status: PillStatus): string {
  switch (status) {
    case "running":
      return "bg-emerald-500/15 text-emerald-700 ring-emerald-500/30 dark:text-emerald-300";
    case "building":
      return "bg-amber-500/15 text-amber-700 ring-amber-500/30 dark:text-amber-300";
    case "failed":
      return "bg-red-500/15 text-red-700 ring-red-500/30 dark:text-red-300";
    case "stopped":
      return "bg-[var(--color-muted)] text-[var(--color-muted-foreground)] ring-[var(--color-border)]";
  }
}

function StatusPill({ status }: { status: PillStatus }) {
  return (
    <span
      className={cn(
        "inline-flex items-center rounded-full px-2 py-0.5 text-[10px] font-medium uppercase tracking-widest ring-1",
        pillClasses(status),
      )}
    >
      {status}
    </span>
  );
}

// MetaBadge renders the small mono chips for detected_stack /
// dockerfile_source / image_tag.
function MetaBadge({ label, value }: { label: string; value: string }) {
  return (
    <span className="inline-flex items-center gap-1 rounded-full bg-[var(--color-muted)]/60 px-2 py-0.5 ring-1 ring-[var(--color-border)]">
      <span className="text-[10px] uppercase tracking-widest text-[var(--color-muted-foreground)]">
        {label}
      </span>
      <span className="font-mono text-[10px] text-[var(--color-foreground)]">
        {value}
      </span>
    </span>
  );
}

// Same relative-time formatting as the Overview tab's incident table —
// duplicated locally because client.tsx keeps its helpers module-private.
function formatRelative(iso: string, now: number): string {
  const t = Date.parse(iso);
  if (!Number.isFinite(t)) return iso;
  const diff = Math.max(0, now - t);
  const s = Math.round(diff / 1000);
  if (s < 60) return `${s}s ago`;
  const m = Math.round(s / 60);
  if (m < 60) return `${m}m ago`;
  const h = Math.round(m / 60);
  if (h < 24) return `${h}h ago`;
  const d = Math.round(h / 24);
  return `${d}d ago`;
}

// LogDisclosure is a collapsed <details> around one inline log blob. No
// network on expand — the logs ride the deploy/list responses.
function LogDisclosure({ label, log }: { label: string; log: string }) {
  return (
    <details className="group rounded-md border border-[var(--color-border)] bg-[var(--color-card)]">
      <summary
        className={cn(
          "flex cursor-pointer list-none items-center gap-2 px-3 py-2 text-xs",
          "text-[var(--color-foreground)] hover:bg-[var(--color-muted)]/40",
        )}
      >
        <ChevronRight
          className="h-3.5 w-3.5 text-[var(--color-muted-foreground)] group-open:hidden"
          aria-hidden
        />
        <ChevronDown
          className="hidden h-3.5 w-3.5 text-[var(--color-muted-foreground)] group-open:block"
          aria-hidden
        />
        <ScrollText
          className="h-3.5 w-3.5 text-[var(--color-muted-foreground)]"
          aria-hidden
        />
        <span className="font-medium">{label}</span>
        <span className="ml-auto font-mono text-[10px] text-[var(--color-muted-foreground)]">
          {log ? `${log.split("\n").length} lines` : "empty"}
        </span>
      </summary>
      <div className="border-t border-[var(--color-border)] p-3">
        {log ? (
          <pre className="max-h-80 overflow-auto whitespace-pre-wrap break-words rounded-md bg-[var(--color-muted)]/40 p-3 font-mono text-[11px] leading-relaxed text-[var(--color-foreground)]">
            {log}
          </pre>
        ) : (
          <p className="text-xs text-[var(--color-muted-foreground)]">
            No output captured.
          </p>
        )}
      </div>
    </details>
  );
}

// FailedNote renders the error prominently plus the "an incident was
// raised — watch it get fixed" framing: deploy_engine-sourced failures
// are picked up by the Sentinel→Pathfinder→Synthesiser→Backend→QA loop
// automatically, so a failed deploy is the START of an auto-fix, not a
// dead end.
function FailedNote({ error }: { error?: string }) {
  return (
    <div
      role="alert"
      className="space-y-3 rounded-md border border-red-500/30 bg-red-500/10 p-4"
    >
      <div className="flex items-start gap-3 text-sm text-red-700 dark:text-red-300">
        <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" aria-hidden />
        <div className="min-w-0">
          <p className="font-semibold">Deploy failed</p>
          <p className="mt-1 break-words font-mono text-xs">
            {error ?? "The build or health check did not succeed."}
          </p>
        </div>
      </div>
      <div className="rounded-md border border-[var(--color-border)] bg-[var(--color-card)] p-3">
        <p className="text-xs text-[var(--color-foreground)]">
          <span className="font-semibold">
            The self-healing loop has been notified.
          </span>{" "}
          This failure raised a deploy-engine incident, and the recovery
          pipeline (Sentinel → Pathfinder → Synthesiser → Backend → QA) will
          attempt an automatic fix.
        </p>
        <Button asChild variant="outline" size="sm" className="mt-2">
          <Link href={"/console/incidents" as Route}>
            Watch the recovery
            <ExternalLink className="h-3.5 w-3.5" />
          </Link>
        </Button>
      </div>
    </div>
  );
}

// DeploymentRow is one collapsed history entry — status/time/stack in the
// summary, full metadata + logs on expand.
function DeploymentRow({ d, now }: { d: Deployment; now: number }) {
  return (
    <details className="group border-t border-[var(--color-border)] first:border-t-0">
      <summary
        className={cn(
          "flex cursor-pointer list-none items-center gap-3 px-4 py-3",
          "hover:bg-[var(--color-muted)]/40",
        )}
      >
        <ChevronRight
          className="h-3.5 w-3.5 shrink-0 text-[var(--color-muted-foreground)] group-open:hidden"
          aria-hidden
        />
        <ChevronDown
          className="hidden h-3.5 w-3.5 shrink-0 text-[var(--color-muted-foreground)] group-open:block"
          aria-hidden
        />
        <StatusPill status={d.status} />
        <span className="text-xs text-[var(--color-muted-foreground)]">
          {formatRelative(d.started_at, now)}
        </span>
        <span className="font-mono text-[11px] text-[var(--color-foreground)]">
          {d.detected_stack}
        </span>
        <span className="ml-auto truncate font-mono text-[10px] text-[var(--color-muted-foreground)]">
          {d.deployment_id.slice(0, 8)}
        </span>
      </summary>
      <div className="space-y-3 px-4 pb-4 pt-1">
        <div className="flex flex-wrap items-center gap-2">
          <MetaBadge label="stack" value={d.detected_stack} />
          <MetaBadge label="dockerfile" value={d.dockerfile_source} />
          {d.image_tag && <MetaBadge label="image" value={d.image_tag} />}
        </div>
        {d.status === "running" && d.url && (
          <p className="text-xs">
            <a
              href={d.url}
              target="_blank"
              rel="noopener noreferrer"
              className="inline-flex items-center gap-1 font-mono text-[var(--color-primary)] underline-offset-4 hover:underline"
            >
              {d.url}
              <ExternalLink className="h-3 w-3" aria-hidden />
            </a>
          </p>
        )}
        {d.status === "failed" && d.error && (
          <p className="break-words font-mono text-xs text-red-700 dark:text-red-300">
            {d.error}
          </p>
        )}
        <LogDisclosure label="Build log" log={d.build_log} />
        <LogDisclosure label="Container log" log={d.container_log} />
      </div>
    </details>
  );
}

export function OpsTab({
  project,
  backendMissing,
}: {
  project: Project;
  backendMissing: boolean;
}) {
  // history: null = initial fetch still in flight, [] = loaded but empty.
  const [history, setHistory] = React.useState<Deployment[] | null>(null);
  const [deploying, setDeploying] = React.useState(false);
  const [stopping, setStopping] = React.useState(false);
  const [actionError, setActionError] = React.useState<string | null>(null);
  const [now, setNow] = React.useState<number>(() => Date.now());

  React.useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), 5_000);
    return () => window.clearInterval(id);
  }, []);

  React.useEffect(() => {
    if (backendMissing) return;
    let cancelled = false;
    void (async () => {
      const rows = await deploymentsSdk.list(project.id);
      if (!cancelled) setHistory(rows);
    })();
    return () => {
      cancelled = true;
    };
  }, [project.id, backendMissing]);

  const onDeploy = React.useCallback(async () => {
    setDeploying(true);
    setActionError(null);
    try {
      const d = await deploymentsSdk.deploy(project.id);
      // The POST response IS the authoritative new row (200 running or
      // 422 failed) — prepend it rather than refetching so the result
      // shows immediately even if the list read lags.
      setHistory((prev) => [d, ...(prev ?? [])]);
    } catch (err) {
      setActionError(
        err instanceof Error ? err.message : "Deploy request failed.",
      );
    } finally {
      setDeploying(false);
    }
  }, [project.id]);

  const onStop = React.useCallback(
    async (deploymentId: string) => {
      setStopping(true);
      setActionError(null);
      try {
        await deploymentsSdk.stop(project.id, deploymentId);
        // Optimistically flip the row, then reconcile against the
        // control-plane (system of record) in the background.
        setHistory((prev) =>
          (prev ?? []).map((d) =>
            d.deployment_id === deploymentId
              ? { ...d, status: "stopped" as const, url: null, port: null }
              : d,
          ),
        );
        const rows = await deploymentsSdk.list(project.id);
        if (rows.length > 0) setHistory(rows);
      } catch (err) {
        setActionError(
          err instanceof Error ? err.message : "Stop request failed.",
        );
      } finally {
        setStopping(false);
      }
    },
    [project.id],
  );

  if (backendMissing) {
    return (
      <EmptyState
        icon={Rocket}
        title="Endpoint coming soon"
        description="Preview deploys will land here once the control-plane deploy API ships."
      />
    );
  }

  const latest = history?.[0];

  return (
    <div className="space-y-6">
      <section
        aria-labelledby="ops-current-deploy"
        className="rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-5"
      >
        <header className="mb-4 flex flex-wrap items-center justify-between gap-3">
          <div>
            <h2
              id="ops-current-deploy"
              className="text-sm font-semibold text-[var(--color-foreground)]"
            >
              Preview deployment
            </h2>
            <p className="mt-0.5 text-xs text-[var(--color-muted-foreground)]">
              Builds the repo into a container, runs it, and health-checks it
              via the deploy-engine.
            </p>
          </div>
          <Button onClick={onDeploy} disabled={deploying}>
            {deploying ? (
              <>
                <Loader2 className="h-4 w-4 animate-spin" aria-hidden />
                Building…
              </>
            ) : (
              <>
                <Rocket className="h-4 w-4" aria-hidden />
                Deploy now
              </>
            )}
          </Button>
        </header>

        {actionError && (
          <div
            role="alert"
            className="mb-4 rounded-md border border-red-500/30 bg-red-500/10 px-4 py-3 text-sm text-red-700 dark:text-red-300"
          >
            {actionError}
          </div>
        )}

        {deploying ? (
          <div
            role="status"
            className="flex items-start gap-3 rounded-md border border-amber-500/30 bg-amber-500/10 p-4"
          >
            <Loader2
              className="mt-0.5 h-4 w-4 shrink-0 animate-spin text-amber-700 dark:text-amber-300"
              aria-hidden
            />
            <div className="min-w-0">
              <div className="flex items-center gap-2">
                <StatusPill status="building" />
                <p className="text-sm font-semibold text-amber-700 dark:text-amber-300">
                  Building and starting the container…
                </p>
              </div>
              <p className="mt-1 text-xs text-amber-700/90 dark:text-amber-300/90">
                Cloning the repo, building the image, booting the container, and
                waiting for its health check — this can take a minute or more on
                a cold cache. Leave this tab open; the result appears here as
                soon as the build finishes.
              </p>
            </div>
          </div>
        ) : history === null ? (
          <div className="flex items-center gap-2 text-xs text-[var(--color-muted-foreground)]">
            <Loader2 className="h-3.5 w-3.5 animate-spin" aria-hidden />
            Loading deployments…
          </div>
        ) : !latest ? (
          <EmptyState
            icon={Rocket}
            title="No deployments yet"
            description="Hit Deploy now to build this project's repo and spin up a live preview container."
          />
        ) : (
          <div className="space-y-4">
            <div className="flex flex-wrap items-center gap-2">
              <StatusPill status={latest.status} />
              <span className="text-xs text-[var(--color-muted-foreground)]">
                {formatRelative(latest.started_at, now)}
              </span>
              <MetaBadge label="stack" value={latest.detected_stack} />
              <MetaBadge label="dockerfile" value={latest.dockerfile_source} />
              {latest.image_tag && (
                <MetaBadge label="image" value={latest.image_tag} />
              )}
            </div>

            {latest.status === "running" && (
              <div className="flex flex-wrap items-center justify-between gap-3 rounded-md border border-emerald-500/30 bg-emerald-500/10 p-4">
                <div className="min-w-0">
                  <p className="text-xs font-semibold uppercase tracking-widest text-emerald-700 dark:text-emerald-300">
                    Live preview
                  </p>
                  {latest.url && (
                    <a
                      href={latest.url}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="mt-1 inline-flex items-center gap-1.5 break-all font-mono text-sm text-emerald-700 underline-offset-4 hover:underline dark:text-emerald-300"
                    >
                      {latest.url}
                      <ExternalLink
                        className="h-3.5 w-3.5 shrink-0"
                        aria-hidden
                      />
                    </a>
                  )}
                </div>
                <Button
                  variant="outline"
                  size="sm"
                  disabled={stopping}
                  onClick={() => void onStop(latest.deployment_id)}
                >
                  {stopping ? (
                    <Loader2 className="h-3.5 w-3.5 animate-spin" aria-hidden />
                  ) : (
                    <Square className="h-3.5 w-3.5" aria-hidden />
                  )}
                  Stop
                </Button>
              </div>
            )}

            {latest.status === "failed" && <FailedNote error={latest.error} />}

            {latest.status === "stopped" && (
              <p className="text-xs text-[var(--color-muted-foreground)]">
                The last preview container was stopped. Deploy again to bring up
                a fresh one.
              </p>
            )}

            <div className="space-y-2">
              <LogDisclosure label="Build log" log={latest.build_log} />
              <LogDisclosure label="Container log" log={latest.container_log} />
            </div>
          </div>
        )}
      </section>

      <section
        aria-labelledby="ops-history"
        className="rounded-lg border border-[var(--color-border)] bg-[var(--color-card)]"
      >
        <header className="border-b border-[var(--color-border)] p-5">
          <h2
            id="ops-history"
            className="flex items-center gap-2 text-sm font-semibold text-[var(--color-foreground)]"
          >
            <History
              className="h-4 w-4 text-[var(--color-muted-foreground)]"
              aria-hidden
            />
            Deployment history
          </h2>
          <p className="mt-0.5 text-xs text-[var(--color-muted-foreground)]">
            Newest first. Expand a row for its logs and image details.
          </p>
        </header>
        {history === null ? (
          <div className="flex items-center gap-2 p-5 text-xs text-[var(--color-muted-foreground)]">
            <Loader2 className="h-3.5 w-3.5 animate-spin" aria-hidden />
            Loading history…
          </div>
        ) : history.length === 0 ? (
          <div className="p-5">
            <EmptyState
              icon={History}
              title="Nothing deployed yet"
              description="Every deploy — succeeded, failed, or stopped — will be recorded here."
            />
          </div>
        ) : (
          <div>
            {history.map((d) => (
              <DeploymentRow key={d.deployment_id} d={d} now={now} />
            ))}
          </div>
        )}
      </section>
    </div>
  );
}
