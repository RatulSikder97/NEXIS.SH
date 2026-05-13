"use client";

// WorkflowsClient — org-wide workflow runs table.
//
// Each row expands inline to show an OperationalSegments panel summarising
// the pipeline stages. A "Detailed timeline" link routes to the workspace-
// scoped /console/incidents/{id} detail page where the operator can drive
// the SSE stream + approvals.
//
// Polling: 5s while any row is queued|running, idle otherwise. The interval
// re-fans the per-workspace list endpoint.

import * as React from "react";
import Link from "next/link";
import {
  ChevronDown,
  ChevronRight,
  Loader2,
  RefreshCw,
  Workflow,
} from "lucide-react";

import { EmptyState } from "@/components/empty-state/EmptyState";
import {
  OperationalSegments,
  type OperationalSegment,
  type SegmentStatus,
} from "@/components/console/OperationalSegments";
import {
  formatDurationMs,
  formatRelative,
} from "@/lib/agents-format";
import { cn } from "@/lib/utils";
import {
  AGENTS_IN_ORDER,
  AGENT_LABELS,
  pipelines,
  type WorkflowRun,
  type WorkflowRunStatus,
} from "@/lib/pipelines";

const POLL_MS = 5000;
const TICK_MS = 1000;

export type WorkflowRowSeed = {
  run: WorkflowRun;
  workspace_id: string;
  workspace_name: string;
};

const STATUS_PILL: Record<WorkflowRunStatus, string> = {
  queued:
    "bg-zinc-500/15 text-zinc-700 ring-zinc-500/30 dark:text-zinc-300",
  running:
    "bg-blue-500/15 text-blue-700 ring-blue-500/30 dark:text-blue-300",
  succeeded:
    "bg-emerald-500/15 text-emerald-700 ring-emerald-500/30 dark:text-emerald-300",
  failed:
    "bg-red-500/15 text-red-700 ring-red-500/30 dark:text-red-300",
  timed_out:
    "bg-red-500/15 text-red-700 ring-red-500/30 dark:text-red-300",
  cancelled:
    "bg-zinc-500/10 text-zinc-600 ring-zinc-500/20 dark:text-zinc-400",
};

function hasInFlight(rows: WorkflowRowSeed[]): boolean {
  return rows.some(
    (r) => r.run.status === "queued" || r.run.status === "running",
  );
}

function durationFor(run: WorkflowRun, now: number): number | undefined {
  if (typeof run.duration_ms === "number") return run.duration_ms;
  const start = Date.parse(run.started_at);
  if (!Number.isFinite(start)) return undefined;
  return now - start;
}

function scenarioFrom(run: WorkflowRun): string {
  // workflow_type is the workflow class name (e.g. RecoveryPipeline). We
  // surface that plus the current_step for in-flight runs.
  if (run.status === "running" && run.current_step) {
    return `${run.workflow_type} · ${run.current_step}`;
  }
  return run.workflow_type;
}

// segmentsFor builds an OperationalSegments-ready preview from the synchronous
// pipeline run row. We can't compute per-stage durations without the events
// list, so the preview shows lifecycle stages: started → running → finished.
function segmentsFor(run: WorkflowRun): OperationalSegment[] {
  const out: OperationalSegment[] = [];
  out.push({
    label: "Pipeline accepted",
    started_at: run.started_at,
    finished_at: run.started_at,
    duration_ms: 0,
    status: "succeeded",
    detail: `Workflow ${run.workflow_type} accepted by Temporal.`,
  });
  if (run.current_step) {
    const stepKey = run.current_step.toLowerCase();
    out.push({
      label: AGENT_LABELS[stepKey as never] ?? run.current_step,
      started_at: run.started_at,
      duration_ms: undefined,
      status: run.status === "running" ? "running" : "succeeded",
      detail: `Current activity: ${run.current_step}`,
    });
  }
  // Terminal segment
  let terminalStatus: SegmentStatus = "pending";
  if (run.status === "succeeded") terminalStatus = "succeeded";
  else if (run.status === "failed" || run.status === "timed_out")
    terminalStatus = "failed";
  else if (run.status === "cancelled") terminalStatus = "skipped";
  else if (run.status === "running") terminalStatus = "running";
  out.push({
    label: "Pipeline terminal",
    started_at: run.completed_at ?? run.started_at,
    finished_at: run.completed_at,
    duration_ms: run.duration_ms,
    status: terminalStatus,
    detail:
      run.error ??
      (terminalStatus === "succeeded"
        ? "Pipeline completed cleanly."
        : terminalStatus === "running"
          ? "Pipeline is still in flight."
          : terminalStatus === "failed"
            ? "Pipeline did not complete."
            : "Pipeline status pending."),
  });
  void AGENTS_IN_ORDER; // referenced to keep the symbol stable across refactors
  return out;
}

function Row({
  seed,
  now,
  isOpen,
  onToggle,
}: {
  seed: WorkflowRowSeed;
  now: number;
  isOpen: boolean;
  onToggle: () => void;
}) {
  const run = seed.run;
  const d = durationFor(run, now);
  return (
    <>
      <tr
        className={cn(
          "border-t border-[var(--color-border)] transition-colors",
          isOpen ? "bg-[var(--color-muted)]/30" : "hover:bg-[var(--color-muted)]/20",
        )}
      >
        <td className="px-3 py-2">
          <button
            type="button"
            onClick={onToggle}
            aria-expanded={isOpen}
            aria-label={isOpen ? "Collapse row" : "Expand row"}
            className="inline-flex h-6 w-6 items-center justify-center rounded-md text-[var(--color-muted-foreground)] hover:bg-[var(--color-muted)] hover:text-[var(--color-foreground)]"
          >
            {isOpen ? (
              <ChevronDown className="h-3 w-3" />
            ) : (
              <ChevronRight className="h-3 w-3" />
            )}
          </button>
        </td>
        <td className="px-3 py-2 font-mono text-[11px] text-[var(--color-muted-foreground)]">
          {formatRelative(run.started_at)}
        </td>
        <td className="px-3 py-2 text-xs text-[var(--color-foreground)]">
          {seed.workspace_name}
        </td>
        <td className="px-3 py-2 text-xs text-[var(--color-foreground)]">
          {scenarioFrom(run)}
        </td>
        <td className="px-3 py-2">
          <span
            className={cn(
              "inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[10px] font-medium uppercase tracking-widest ring-1",
              STATUS_PILL[run.status],
            )}
          >
            {run.status === "running" && (
              <Loader2 className="h-2.5 w-2.5 animate-spin" aria-hidden />
            )}
            {run.status}
          </span>
        </td>
        <td className="px-3 py-2 font-mono text-[11px] text-[var(--color-muted-foreground)]">
          {formatDurationMs(d)}
        </td>
        <td className="px-3 py-2 text-xs text-[var(--color-muted-foreground)]">
          {run.current_step ?? "—"}
        </td>
        <td className="px-3 py-2 text-right">
          <Link
            href={`/console/incidents/${run.id}` as never}
            className="text-xs font-medium text-[var(--color-primary)] hover:underline"
          >
            Detail →
          </Link>
        </td>
      </tr>
      {isOpen && (
        <tr className="border-t border-[var(--color-border)] bg-[var(--color-background)]">
          <td colSpan={8} className="px-3 py-4">
            <OperationalSegments
              title="Operational segments"
              description={`Workspace ${seed.workspace_name} · run ${run.id.slice(
                0,
                8,
              )}…`}
              segments={segmentsFor(run)}
            />
          </td>
        </tr>
      )}
    </>
  );
}

export function WorkflowsClient({
  initial,
  workspaces,
}: {
  initial: WorkflowRowSeed[];
  workspaces: Array<{ id: string; name: string }>;
}) {
  const [rows, setRows] = React.useState<WorkflowRowSeed[]>(initial);
  const [now, setNow] = React.useState<number>(() => Date.now());
  const [openId, setOpenId] = React.useState<string | null>(null);
  const [refreshing, setRefreshing] = React.useState(false);

  React.useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), TICK_MS);
    return () => window.clearInterval(id);
  }, []);

  const inFlight = hasInFlight(rows);

  const refetch = React.useCallback(async () => {
    if (workspaces.length === 0) return;
    setRefreshing(true);
    try {
      const settled = await Promise.all(
        workspaces.map(async (w) => {
          try {
            const list = await pipelines.list(w.id, 25);
            return list.map(
              (r): WorkflowRowSeed => ({
                run: r,
                workspace_id: w.id,
                workspace_name: w.name,
              }),
            );
          } catch {
            return [];
          }
        }),
      );
      const merged = settled
        .flat()
        .sort(
          (a, b) =>
            Date.parse(b.run.started_at) - Date.parse(a.run.started_at),
        );
      setRows(merged);
    } finally {
      setRefreshing(false);
    }
  }, [workspaces]);

  React.useEffect(() => {
    if (!inFlight) return;
    const id = window.setInterval(() => {
      void refetch();
    }, POLL_MS);
    return () => window.clearInterval(id);
  }, [inFlight, refetch]);

  return (
    <div className="space-y-6">
      <div className="flex items-end justify-between gap-4">
        <div className="space-y-1">
          <p className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]">
            Fleet
          </p>
          <h1 className="text-2xl font-semibold">Workflows</h1>
          <p className="max-w-2xl text-sm text-[var(--color-muted-foreground)]">
            Every workflow run across all your workspaces. Includes recovery
            pipelines, evals, and any future scheduled flows. Rows expand
            inline to show operational segments; the detail link opens the
            full activity stream.
          </p>
        </div>
        <button
          type="button"
          onClick={() => void refetch()}
          disabled={refreshing}
          className="inline-flex items-center gap-1.5 rounded-md border border-[var(--color-border)] bg-[var(--color-card)] px-3 py-1.5 text-xs text-[var(--color-muted-foreground)] hover:text-[var(--color-foreground)] disabled:opacity-50"
        >
          <RefreshCw
            className={cn("h-3 w-3", refreshing && "animate-spin")}
          />
          Refresh
        </button>
      </div>

      {rows.length === 0 ? (
        <EmptyState
          icon={Workflow}
          title="No workflow runs yet"
          description="Trigger a sample incident from the dashboard or kick off an eval run to see workflow rows here."
        />
      ) : (
        <div className="overflow-hidden rounded-lg border border-[var(--color-border)] bg-[var(--color-card)]">
          <table className="w-full text-sm">
            <thead className="bg-[var(--color-muted)]/40 text-left text-[10px] uppercase tracking-widest text-[var(--color-muted-foreground)]">
              <tr>
                <th className="w-8 px-3 py-2" aria-label="Expand" />
                <th className="px-3 py-2 font-medium">Started</th>
                <th className="px-3 py-2 font-medium">Workspace</th>
                <th className="px-3 py-2 font-medium">Scenario</th>
                <th className="px-3 py-2 font-medium">Status</th>
                <th className="px-3 py-2 font-medium">Duration</th>
                <th className="px-3 py-2 font-medium">Current step</th>
                <th className="px-3 py-2 font-medium" />
              </tr>
            </thead>
            <tbody>
              {rows.map((r) => (
                <Row
                  key={r.run.id}
                  seed={r}
                  now={now}
                  isOpen={openId === r.run.id}
                  onToggle={() =>
                    setOpenId((curr) => (curr === r.run.id ? null : r.run.id))
                  }
                />
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
