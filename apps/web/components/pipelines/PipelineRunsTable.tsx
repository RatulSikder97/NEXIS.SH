"use client";

// Phase 4 Stage 7 — Pipeline runs table.
// Phase 6 Stage 9 — added severity column + "awaiting approval" indicator.
//
// Six columns:
//   Run ID (mono, last 8 chars, click → detail page)
//   Started (relative time, "2m ago")
//   Severity (SeverityPill — "none/low/medium/high"; "—" until classified)
//   Status  (StatusPill, optional "Awaiting approval" amber dot when the
//            run is running with medium|high severity — a hint the approval
//            gate is blocked on a human signal)
//   Current step (AgentIcon + label)
//   Duration (server-provided duration_ms or live elapsed for running rows)
//
// Empty state delegates to the parent — we just render an empty <tbody>.
// The row click target is the run id <Link>; the rest of the row is
// inert to keep keyboard navigation predictable (one focus target per row).

import * as React from "react";
import Link from "next/link";
import type { Route } from "next";

import { AgentIcon } from "@/components/pipelines/AgentIcon";
import { StatusPill } from "@/components/pipelines/StatusPill";
import { SeverityPill } from "@/components/incidents/SeverityPill";
import { AGENT_LABELS, type WorkflowRun } from "@/lib/pipelines";

// formatDuration turns milliseconds into a short human label. Inputs are
// expected to be sub-day in Phase 4 — anything larger is shown in minutes
// without overflow guards because we don't have those cases in practice yet.
function formatDuration(ms: number): string {
  if (!Number.isFinite(ms) || ms < 0) return "—";
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)}s`;
  const m = Math.floor(ms / 60_000);
  const s = Math.round((ms % 60_000) / 1000);
  return s === 0 ? `${m}m` : `${m}m ${s}s`;
}

// formatRelative renders an RFC3339 timestamp as a short "Xm ago" label,
// falling back to a localised datetime when the delta is large. We
// re-render on parent updates so this stays current as the list polls.
function formatRelative(iso: string): string {
  if (!iso) return "—";
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return iso;
  const now = Date.now();
  const delta = Math.max(0, now - t);
  if (delta < 5_000) return "just now";
  if (delta < 60_000) return `${Math.floor(delta / 1000)}s ago`;
  if (delta < 3_600_000) return `${Math.floor(delta / 60_000)}m ago`;
  if (delta < 86_400_000) return `${Math.floor(delta / 3_600_000)}h ago`;
  try {
    return new Date(t).toLocaleString(undefined, {
      month: "short",
      day: "numeric",
      hour: "2-digit",
      minute: "2-digit",
    });
  } catch {
    return iso;
  }
}

function runDurationMs(run: WorkflowRun, now: number): number | null {
  if (typeof run.duration_ms === "number" && run.duration_ms > 0) {
    return run.duration_ms;
  }
  if (run.status === "running" || run.status === "queued") {
    const started = Date.parse(run.started_at);
    if (Number.isFinite(started)) return Math.max(0, now - started);
  }
  return null;
}

export function PipelineRunsTable({
  runs,
  now,
}: {
  runs: WorkflowRun[];
  // `now` is supplied by the parent so the relative-time + live-duration
  // columns update on the parent's heartbeat (every second) without each
  // row owning a tick of its own.
  now: number;
}) {
  return (
    <div className="overflow-hidden rounded-lg border border-[var(--color-border)] bg-[var(--color-card)]">
      <table className="w-full text-sm">
        <thead className="bg-[var(--color-muted)]/40 text-left text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]">
          <tr>
            <th className="px-4 py-2 font-medium">Run</th>
            <th className="px-4 py-2 font-medium">Started</th>
            <th className="px-4 py-2 font-medium">Severity</th>
            <th className="px-4 py-2 font-medium">Status</th>
            <th className="px-4 py-2 font-medium">Current step</th>
            <th className="px-4 py-2 font-medium">Duration</th>
            <th className="px-4 py-2 font-medium" aria-label="Open" />
          </tr>
        </thead>
        <tbody>
          {runs.length === 0 ? (
            <tr>
              <td
                colSpan={7}
                className="px-4 py-8 text-center text-sm text-[var(--color-muted-foreground)]"
              >
                No pipeline runs yet.
              </td>
            </tr>
          ) : (
            runs.map((run) => {
              const short = run.id.slice(0, 8);
              const dur = runDurationMs(run, now);
              const stepRole = run.current_step?.trim() ?? "";
              // "Awaiting approval" surfaces only while the run is still
              // in motion AND severity is medium|high — i.e. the approval
              // gate likely fired and is waiting on a human. Once the run
              // terminates we drop the indicator (the final StatusPill
              // colour carries the outcome on its own).
              const awaitingApproval =
                run.status === "running" &&
                (run.severity === "medium" || run.severity === "high");
              return (
                <tr
                  key={run.id}
                  className="border-t border-[var(--color-border)]"
                >
                  <td className="px-4 py-3">
                    <Link
                      href={`/console/incidents/${run.id}` as Route}
                      className="font-mono text-xs text-[var(--color-foreground)] hover:underline"
                    >
                      {short}
                    </Link>
                  </td>
                  <td className="whitespace-nowrap px-4 py-3 text-[var(--color-muted-foreground)]">
                    {formatRelative(run.started_at)}
                  </td>
                  <td className="px-4 py-3">
                    <SeverityPill severity={run.severity} />
                  </td>
                  <td className="px-4 py-3">
                    <span className="inline-flex items-center gap-2">
                      <StatusPill status={run.status} />
                      {awaitingApproval && (
                        <span
                          className="inline-flex items-center gap-1 text-[10px] font-medium text-amber-700 dark:text-amber-300"
                          title="Approval gate is blocked on a human signal"
                        >
                          <span
                            className="inline-block h-1.5 w-1.5 rounded-full bg-amber-500 animate-pulse"
                            aria-hidden
                          />
                          Awaiting approval
                        </span>
                      )}
                    </span>
                  </td>
                  <td className="px-4 py-3">
                    {stepRole ? (
                      <span className="inline-flex items-center gap-2 text-[var(--color-foreground)]">
                        <AgentIcon role={stepRole} />
                        <span className="text-sm">
                          {AGENT_LABELS[stepRole as keyof typeof AGENT_LABELS] ??
                            stepRole}
                        </span>
                      </span>
                    ) : (
                      <span className="text-[var(--color-muted-foreground)]">
                        —
                      </span>
                    )}
                  </td>
                  <td className="whitespace-nowrap px-4 py-3 text-[var(--color-muted-foreground)]">
                    {dur === null ? "—" : formatDuration(dur)}
                  </td>
                  <td className="px-4 py-3 text-right">
                    <Link
                      href={`/console/incidents/${run.id}` as Route}
                      className="text-xs text-[var(--color-primary)] hover:underline"
                    >
                      Open →
                    </Link>
                  </td>
                </tr>
              );
            })
          )}
        </tbody>
      </table>
    </div>
  );
}
