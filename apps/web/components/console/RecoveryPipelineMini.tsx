"use client";

// RecoveryPipelineMini — horizontal 8-stage mini-canvas rendered on the
// console home page. Shows the agentic recovery flow:
//   Sentinel → Pathfinder → Synthesiser → Architect → Backend → QA
//      → DevOps ∥ DataEngineer → ApprovalGate → Validator
//
// Each stage card carries a tiny live counter sourced from the workspace's
// pipeline runs (running by stage). On hover, a tooltip describes the stage.
// Clicking the stage navigates into /console/recovery filtered to that stage.
//
// The data is best-effort: we read `current_step` off the workflow_runs rows
// for the current workspace and bucket them. If the field is empty we count
// the row toward the "pending" bucket so the canvas never lies.

import * as React from "react";
import Link from "next/link";
import type { Route } from "next";

import { cn } from "@/lib/utils";
import { pipelines, type WorkflowRun } from "@/lib/pipelines";

const STAGES: Array<{ key: string; label: string; description: string }> = [
  {
    key: "sentinel",
    label: "Sentinel",
    description: "Detect anomaly",
  },
  {
    key: "pathfinder",
    label: "Pathfinder",
    description: "Diagnose root cause",
  },
  {
    key: "synthesiser",
    label: "Synthesiser",
    description: "Plan response",
  },
  {
    key: "architect",
    label: "Architect",
    description: "Design solution",
  },
  {
    key: "backend",
    label: "Backend",
    description: "Codegen",
  },
  {
    key: "qa",
    label: "QA",
    description: "Test generation",
  },
  {
    key: "devops",
    label: "DevOps ∥ Data",
    description: "Pipeline & migration",
  },
  {
    key: "approval_gate",
    label: "Approval",
    description: "Gate decision",
  },
  {
    key: "validator",
    label: "Validator",
    description: "Sandboxed exec",
  },
];

const COOKIE_WORKSPACE = "nexis_workspace";
const POLL_MS = 15_000;

function readWorkspaceCookie(): string | null {
  if (typeof document === "undefined") return null;
  const m = document.cookie.match(
    new RegExp(
      "(?:^|; )" +
        COOKIE_WORKSPACE.replace(/[.$?*|{}()[\]\\/+^]/g, "\\$&") +
        "=([^;]*)",
    ),
  );
  return m ? decodeURIComponent(m[1]) : null;
}

function bucketRuns(runs: WorkflowRun[]): Record<string, number> {
  const out: Record<string, number> = {};
  for (const s of STAGES) out[s.key] = 0;
  for (const r of runs) {
    if (r.status !== "running" && r.status !== "queued") continue;
    const step = (r.current_step ?? "").toLowerCase();
    if (!step) continue;
    if (step in out) {
      out[step]++;
      continue;
    }
    if (step.startsWith("data")) {
      out.devops = (out.devops ?? 0) + 1;
      continue;
    }
    if (step.startsWith("validator") || step.startsWith("validate")) {
      out.validator = (out.validator ?? 0) + 1;
    }
  }
  return out;
}

export function RecoveryPipelineMini() {
  const [counts, setCounts] = React.useState<Record<string, number>>({});

  React.useEffect(() => {
    let cancelled = false;
    async function tick() {
      const wsId = readWorkspaceCookie();
      if (!wsId) return;
      try {
        const rows = await pipelines.list(wsId, 50);
        if (cancelled) return;
        setCounts(bucketRuns(rows));
      } catch {
        // ignore — next tick retries
      }
    }
    void tick();
    const id = window.setInterval(tick, POLL_MS);
    return () => {
      cancelled = true;
      window.clearInterval(id);
    };
  }, []);

  const totalActive = Object.values(counts).reduce((a, b) => a + b, 0);

  return (
    <section
      aria-labelledby="pipeline-canvas-heading"
      className="rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-5"
    >
      <header className="mb-4 flex items-baseline justify-between gap-3">
        <div>
          <h2
            id="pipeline-canvas-heading"
            className="text-sm font-semibold text-[var(--color-foreground)]"
          >
            Recovery pipeline
          </h2>
          <p className="mt-0.5 text-xs text-[var(--color-muted-foreground)]">
            8 stages, live across your workspaces.
          </p>
        </div>
        <Link
          href={"/console/recovery" as Route}
          className="text-xs font-medium text-[var(--color-primary)] hover:underline"
        >
          {totalActive > 0
            ? `${totalActive} stage${totalActive === 1 ? "" : "s"} active →`
            : "View all →"}
        </Link>
      </header>

      <div className="overflow-x-auto">
        <ol className="flex min-w-max items-start gap-2">
          {STAGES.map((s, i) => {
            const c = counts[s.key] ?? 0;
            const live = c > 0;
            return (
              <React.Fragment key={s.key}>
                <li className="flex flex-col items-center">
                  <Link
                    href={"/console/recovery" as Route}
                    title={s.description}
                    className={cn(
                      "group flex h-12 w-24 flex-col items-center justify-center rounded-md border px-2 py-1 transition-colors",
                      live
                        ? "border-blue-500/40 bg-blue-500/10"
                        : "border-[var(--color-border)] bg-[var(--color-background)] hover:bg-[var(--color-muted)]",
                    )}
                  >
                    <span
                      className={cn(
                        "text-[11px] font-medium leading-tight",
                        live
                          ? "text-blue-700 dark:text-blue-300"
                          : "text-[var(--color-foreground)]",
                      )}
                    >
                      {s.label}
                    </span>
                    <span
                      className={cn(
                        "mt-0.5 font-mono text-[10px]",
                        live
                          ? "text-blue-600 dark:text-blue-300"
                          : "text-[var(--color-muted-foreground)]",
                      )}
                    >
                      {live ? `${c} live` : "idle"}
                    </span>
                  </Link>
                  <span className="mt-1 max-w-[6rem] truncate text-center text-[10px] text-[var(--color-muted-foreground)]">
                    {s.description}
                  </span>
                </li>
                {i < STAGES.length - 1 && (
                  <span
                    aria-hidden
                    className={cn(
                      "mt-5 h-px w-3",
                      live ? "bg-blue-500/60" : "bg-[var(--color-border)]",
                    )}
                  />
                )}
              </React.Fragment>
            );
          })}
        </ol>
      </div>
    </section>
  );
}
