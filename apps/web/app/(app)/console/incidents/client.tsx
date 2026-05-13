"use client";

// Phase 4 Stage 7 — Incidents (pipeline runs) list client.
//
// Owns: the runs array, a "now" tick (1s) for live duration columns, a 5s
// polling refetch while any row is still in motion (queued|running), and
// the "run a synthetic incident" CTA.
//
// Polling cadence: 5s. We only poll while there's at least one queued or
// running row — once everything terminates the table is frozen and we stop
// hitting the API to keep idle tabs cheap. The user can switch tabs / come
// back and React will re-mount the page, which re-runs the server fetch.
//
// CTA: posts to /v1/workspaces/{ws}/pipelines/demo and navigates to the
// new run's detail page. We use the SDK so credentials + JSON parsing are
// consistent with the rest of the surface.

import * as React from "react";
import { useRouter } from "next/navigation";
import type { Route } from "next";
import { Loader2, PlayCircle, RefreshCw } from "lucide-react";

import { Button } from "@/components/ui/Button";
import { PipelineRunsTable } from "@/components/pipelines/PipelineRunsTable";
import { pipelines, type WorkflowRun } from "@/lib/pipelines";

const POLL_MS = 5000;
const TICK_MS = 1000;

function hasInFlight(runs: WorkflowRun[]): boolean {
  return runs.some((r) => r.status === "queued" || r.status === "running");
}

export function IncidentsClient({
  workspaceId,
  initial,
}: {
  workspaceId: string;
  initial: WorkflowRun[];
}) {
  const router = useRouter();
  const [runs, setRuns] = React.useState<WorkflowRun[]>(initial);
  const [now, setNow] = React.useState<number>(() => Date.now());
  const [pending, setPending] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);
  const [demoSubmitting, setDemoSubmitting] = React.useState(false);

  // Mirror runs in a ref so the interval callback always sees the latest
  // value without re-creating the interval on every state change.
  const runsRef = React.useRef(runs);
  React.useEffect(() => {
    runsRef.current = runs;
  }, [runs]);

  // 1s ticker for live-duration + relative-time. Cheap enough to leave
  // running while the page is mounted; we don't gate it on hasInFlight
  // because the relative-time on past rows also wants to drift forward.
  React.useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), TICK_MS);
    return () => window.clearInterval(id);
  }, []);

  // 5s poll while any row is in motion. The effect re-runs when the
  // in-flight signal flips, so once everything terminates the interval
  // is torn down and we go idle.
  const inFlight = hasInFlight(runs);
  React.useEffect(() => {
    if (!workspaceId) return;
    if (!inFlight) return;
    const id = window.setInterval(async () => {
      try {
        const next = await pipelines.list(workspaceId, 50);
        setRuns(next);
      } catch {
        // swallow — the next tick will retry.
      }
    }, POLL_MS);
    return () => window.clearInterval(id);
  }, [workspaceId, inFlight]);

  async function refresh() {
    if (!workspaceId) return;
    setPending(true);
    setError(null);
    try {
      const next = await pipelines.list(workspaceId, 50);
      setRuns(next);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load");
    } finally {
      setPending(false);
    }
  }

  async function runDemo() {
    if (!workspaceId) return;
    setDemoSubmitting(true);
    setError(null);
    try {
      const run = await pipelines.createDemo(workspaceId);
      router.push(`/console/incidents/${run.id}` as Route);
    } catch (err) {
      setError(
        err instanceof Error ? err.message : "Failed to start synthetic run",
      );
      setDemoSubmitting(false);
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold">Incidents</h1>
          <p className="mt-1 text-sm text-[var(--color-muted-foreground)]">
            Recovery pipeline runs for this workspace. Open a row to watch the
            agent timeline live.
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={refresh}
            disabled={pending || !workspaceId}
          >
            {pending ? (
              <Loader2 className="h-4 w-4 animate-spin" />
            ) : (
              <RefreshCw className="h-4 w-4" />
            )}
            Refresh
          </Button>
          <Button
            size="sm"
            onClick={runDemo}
            disabled={demoSubmitting || !workspaceId}
          >
            {demoSubmitting ? (
              <Loader2 className="h-4 w-4 animate-spin" />
            ) : (
              <PlayCircle className="h-4 w-4" />
            )}
            Run synthetic incident
          </Button>
        </div>
      </div>

      {error && (
        <div
          role="alert"
          className="rounded-md border border-red-500/30 bg-red-500/10 px-4 py-3 text-sm text-red-700 dark:text-red-300"
        >
          {error}
        </div>
      )}

      {!workspaceId && (
        <div
          role="status"
          className="rounded-md border border-amber-500/30 bg-amber-500/10 px-4 py-3 text-sm text-amber-700 dark:text-amber-300"
        >
          No workspace selected. Pick one in the sidebar to view pipeline runs.
        </div>
      )}

      {runs.length === 0 ? (
        <div className="rounded-lg border border-dashed border-[var(--color-border)] bg-[var(--color-card)] p-10 text-center">
          <p className="text-base font-medium text-[var(--color-foreground)]">
            No incidents yet
          </p>
          <p className="mt-1 text-sm text-[var(--color-muted-foreground)]">
            Trigger a synthetic incident to walk through the recovery loop
            end-to-end.
          </p>
          <div className="mt-4 flex justify-center">
            <Button
              size="sm"
              onClick={runDemo}
              disabled={demoSubmitting || !workspaceId}
            >
              {demoSubmitting ? (
                <Loader2 className="h-4 w-4 animate-spin" />
              ) : (
                <PlayCircle className="h-4 w-4" />
              )}
              Run synthetic incident
            </Button>
          </div>
        </div>
      ) : (
        <PipelineRunsTable runs={runs} now={now} />
      )}
    </div>
  );
}
