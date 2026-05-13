"use client";

// RecoveryClient — active recovery pipelines table with per-row stage tracker.
//
// Each row shows: incident scenario, severity pill, started_at, current
// stage, progress %, and a "Detail" link to the workspace incidents page.
// Inline expansion renders an OperationalSegments preview built from the
// 8-stage pipeline contract; current_step drives which stage is "running"
// and any prior stage is treated as "succeeded".

import * as React from "react";
import Link from "next/link";
import { Loader2, RefreshCw, ShieldAlert } from "lucide-react";

import { cn } from "@/lib/utils";
import { EmptyState } from "@/components/empty-state/EmptyState";
import {
  OperationalSegments,
  type OperationalSegment,
  type SegmentStatus,
} from "@/components/console/OperationalSegments";
import {
  AGENTS_IN_ORDER,
  AGENT_LABELS,
  pipelines,
  type AgentRole,
  type IncidentSeverity,
  type WorkflowRun,
  type WorkflowRunStatus,
} from "@/lib/pipelines";
import { formatDurationMs, formatRelative } from "@/lib/agents-format";

const POLL_MS = 5000;

export type RecoveryRunSeed = {
  run: WorkflowRun;
  workspace_id: string;
  workspace_name: string;
};

const SEVERITY_PILL: Record<IncidentSeverity, string> = {
  none: "bg-zinc-500/15 text-zinc-700 ring-zinc-500/30 dark:text-zinc-300",
  low: "bg-blue-500/15 text-blue-700 ring-blue-500/30 dark:text-blue-300",
  medium:
    "bg-amber-500/15 text-amber-700 ring-amber-500/30 dark:text-amber-300",
  high: "bg-red-500/15 text-red-700 ring-red-500/30 dark:text-red-300",
};

const STATUS_DOT: Record<WorkflowRunStatus, string> = {
  queued: "bg-zinc-400",
  running: "bg-blue-500",
  succeeded: "bg-emerald-500",
  failed: "bg-red-500",
  timed_out: "bg-red-500",
  cancelled: "bg-zinc-300",
};

function stageIndex(currentStep?: string): number {
  if (!currentStep) return -1;
  const key = currentStep.toLowerCase();
  for (let i = 0; i < AGENTS_IN_ORDER.length; i++) {
    if (AGENTS_IN_ORDER[i] === (key as AgentRole)) return i;
  }
  return -1;
}

function progressFor(run: WorkflowRun): number {
  if (run.status === "succeeded") return 100;
  if (run.status === "failed" || run.status === "timed_out") return 100;
  if (run.status === "cancelled") return 100;
  const idx = stageIndex(run.current_step);
  const total = AGENTS_IN_ORDER.length + 1; // +1 for validator
  if (idx < 0) return 5;
  return Math.round(((idx + 1) / total) * 100);
}

function pipelineSegments(run: WorkflowRun): OperationalSegment[] {
  const currentIdx = stageIndex(run.current_step);
  const stages: OperationalSegment[] = [];
  AGENTS_IN_ORDER.forEach((agent, idx) => {
    let status: SegmentStatus;
    if (run.status === "failed" || run.status === "timed_out") {
      if (idx < currentIdx) status = "succeeded";
      else if (idx === currentIdx) status = "failed";
      else status = "skipped";
    } else if (run.status === "succeeded") {
      status = "succeeded";
    } else if (run.status === "cancelled") {
      status = idx < currentIdx ? "succeeded" : "skipped";
    } else {
      if (idx < currentIdx) status = "succeeded";
      else if (idx === currentIdx) status = "running";
      else status = "pending";
    }
    stages.push({
      label: AGENT_LABELS[agent],
      started_at: run.started_at,
      finished_at:
        status === "succeeded" || status === "failed"
          ? run.completed_at ?? run.started_at
          : undefined,
      status,
      detail:
        status === "running"
          ? `Currently executing ${agent}.`
          : status === "failed"
            ? run.error ?? `Stage ${agent} did not complete.`
            : status === "succeeded"
              ? `Stage ${agent} completed.`
              : status === "skipped"
                ? `Stage ${agent} skipped (pipeline ${run.status}).`
                : `Stage ${agent} not yet started.`,
    });
  });
  // Validator stage
  stages.push({
    label: "Validator · Sandbox",
    started_at: run.completed_at ?? run.started_at,
    finished_at: run.completed_at,
    status:
      run.status === "succeeded"
        ? "succeeded"
        : run.status === "running"
          ? "pending"
          : "skipped",
    detail:
      run.status === "succeeded"
        ? "Validator approved the patch."
        : "Validator runs after Approval Gate passes.",
  });
  return stages;
}

function Row({ seed }: { seed: RecoveryRunSeed }) {
  const [open, setOpen] = React.useState(false);
  const run = seed.run;
  const progress = progressFor(run);
  const severity = run.severity ?? "none";
  return (
    <>
      <tr
        className={cn(
          "border-t border-[var(--color-border)] transition-colors",
          open ? "bg-[var(--color-muted)]/30" : "hover:bg-[var(--color-muted)]/20",
        )}
      >
        <td className="px-3 py-2">
          <button
            type="button"
            onClick={() => setOpen((v) => !v)}
            aria-expanded={open}
            className="inline-flex items-center gap-1 text-[11px] text-[var(--color-muted-foreground)] hover:text-[var(--color-foreground)]"
          >
            {open ? "Hide" : "Stages"}
          </button>
        </td>
        <td className="px-3 py-2 text-xs">
          <p className="font-medium text-[var(--color-foreground)]">
            Incident · {seed.workspace_name}
          </p>
          <p className="font-mono text-[10px] text-[var(--color-muted-foreground)]">
            {run.id.slice(0, 12)}
          </p>
        </td>
        <td className="px-3 py-2">
          <span
            className={cn(
              "inline-flex items-center rounded-full px-2 py-0.5 text-[10px] font-medium uppercase tracking-widest ring-1",
              SEVERITY_PILL[severity],
            )}
          >
            {severity}
          </span>
        </td>
        <td className="px-3 py-2 font-mono text-[11px] text-[var(--color-muted-foreground)]">
          {formatRelative(run.started_at)}
        </td>
        <td className="px-3 py-2 text-xs text-[var(--color-foreground)]">
          {run.current_step
            ? AGENT_LABELS[run.current_step.toLowerCase() as AgentRole] ??
              run.current_step
            : "—"}
        </td>
        <td className="px-3 py-2">
          <div className="flex items-center gap-2">
            <div className="h-1.5 w-32 overflow-hidden rounded-full bg-[var(--color-muted)]">
              <div
                className={cn(
                  "h-full transition-all",
                  STATUS_DOT[run.status],
                )}
                style={{ width: `${progress}%` }}
                aria-hidden
              />
            </div>
            <span className="font-mono text-[10px] text-[var(--color-muted-foreground)]">
              {progress}%
            </span>
            {run.status === "running" && (
              <Loader2
                className="h-3 w-3 animate-spin text-blue-500"
                aria-hidden
              />
            )}
          </div>
        </td>
        <td className="px-3 py-2 font-mono text-[11px] text-[var(--color-muted-foreground)]">
          {formatDurationMs(run.duration_ms)}
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
      {open && (
        <tr className="border-t border-[var(--color-border)] bg-[var(--color-background)]">
          <td colSpan={8} className="px-3 py-4">
            <OperationalSegments
              title="Stage tracker"
              description={`Workspace ${seed.workspace_name} · run ${run.id.slice(0, 8)}`}
              segments={pipelineSegments(run)}
            />
          </td>
        </tr>
      )}
    </>
  );
}

export function RecoveryClient({
  initial,
  workspaces,
}: {
  initial: RecoveryRunSeed[];
  workspaces: Array<{ id: string; name: string }>;
}) {
  const [rows, setRows] = React.useState<RecoveryRunSeed[]>(initial);
  const [refreshing, setRefreshing] = React.useState(false);

  const refetch = React.useCallback(async () => {
    if (workspaces.length === 0) return;
    setRefreshing(true);
    try {
      const settled = await Promise.all(
        workspaces.map(async (w) => {
          try {
            const list = await pipelines.list(w.id, 25);
            return list
              .filter((r) => r.workflow_type === "RecoveryPipeline")
              .map(
                (r): RecoveryRunSeed => ({
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

  const inFlight = rows.some(
    (r) => r.run.status === "queued" || r.run.status === "running",
  );

  React.useEffect(() => {
    if (!inFlight) return;
    const id = window.setInterval(() => {
      void refetch();
    }, POLL_MS);
    return () => window.clearInterval(id);
  }, [inFlight, refetch]);

  const active = rows.filter(
    (r) => r.run.status === "running" || r.run.status === "queued",
  );

  return (
    <div className="space-y-6">
      <div className="flex items-end justify-between gap-4">
        <div className="space-y-1">
          <p className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]">
            Operations
          </p>
          <h1 className="text-2xl font-semibold flex items-center gap-2">
            <ShieldAlert className="h-5 w-5 text-[var(--color-primary)]" />
            Recovery pipeline
          </h1>
          <p className="max-w-2xl text-sm text-[var(--color-muted-foreground)]">
            Every recovery pipeline run across your workspaces, ordered by
            most recent. {active.length > 0 ? `${active.length} pipeline${active.length === 1 ? "" : "s"} active.` : "All quiet."}
          </p>
        </div>
        <button
          type="button"
          onClick={() => void refetch()}
          disabled={refreshing}
          className="inline-flex items-center gap-1.5 rounded-md border border-[var(--color-border)] bg-[var(--color-card)] px-3 py-1.5 text-xs text-[var(--color-muted-foreground)] hover:text-[var(--color-foreground)] disabled:opacity-50"
        >
          <RefreshCw className={cn("h-3 w-3", refreshing && "animate-spin")} />
          Refresh
        </button>
      </div>

      {rows.length === 0 ? (
        <EmptyState
          icon={ShieldAlert}
          title="No recovery pipelines yet"
          description="Trigger a sample incident from the dashboard to see the full 8-stage agentic recovery flow in motion."
        />
      ) : (
        <section className="overflow-hidden rounded-lg border border-[var(--color-border)] bg-[var(--color-card)]">
          <table className="w-full text-sm">
            <thead className="bg-[var(--color-muted)]/40 text-left text-[10px] uppercase tracking-widest text-[var(--color-muted-foreground)]">
              <tr>
                <th className="w-16 px-3 py-2" aria-label="Stages" />
                <th className="px-3 py-2 font-medium">Incident</th>
                <th className="px-3 py-2 font-medium">Severity</th>
                <th className="px-3 py-2 font-medium">Started</th>
                <th className="px-3 py-2 font-medium">Current stage</th>
                <th className="px-3 py-2 font-medium">Progress</th>
                <th className="px-3 py-2 font-medium">Duration</th>
                <th className="px-3 py-2 font-medium" />
              </tr>
            </thead>
            <tbody>
              {rows.map((r) => (
                <Row key={r.run.id} seed={r} />
              ))}
            </tbody>
          </table>
        </section>
      )}
    </div>
  );
}
