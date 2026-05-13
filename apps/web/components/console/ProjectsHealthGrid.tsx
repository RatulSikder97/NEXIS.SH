"use client";

// ProjectsHealthGrid — operations dashboard widget rendered below the KPI
// strip on /console.
//
// Polls the projects + pipelines endpoints for the current workspace at 30s
// and surfaces one tile per project (max 8) with:
//   * Project name + environment chip
//   * 7d MTTR (or "—" when no data)
//   * Open-incidents badge
//   * Current recovery status (idle / running / failed) derived from the
//     in-flight workflow_runs for the workspace.
//
// Each tile links to /console/projects/{id}. The header carries a "View all"
// link to /console/projects. When the workspace has zero projects we render
// an inline empty-state card with a "Connect first project" CTA — the same
// surface a fresh workspace lands on.

import * as React from "react";
import Link from "next/link";
import type { Route } from "next";
import { ArrowRight, Boxes, Plus } from "lucide-react";

import { Button } from "@/components/ui/Button";
import { cn } from "@/lib/utils";
import { EnvironmentChip } from "@/components/projects/EnvironmentChip";
import { projects as projectsSdk, type Project } from "@/lib/projects";
import { pipelines, type WorkflowRun } from "@/lib/pipelines";

const COOKIE_WORKSPACE = "nexis_workspace";
const POLL_MS = 30_000;
const MAX_TILES = 8;

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

type RecoveryStatus = "idle" | "running" | "failed";

function statusForProject(
  project: Project,
  runs: WorkflowRun[],
): { status: RecoveryStatus; open: number } {
  // We don't have a project_id on WorkflowRun today, so the dashboard tile
  // shows workspace-wide running + recent-failed signals against every
  // project tile in the same workspace. When the backend projects the
  // project_id onto the run row, this filter can tighten without a UI
  // change. The void below silences the unused-var lint until then.
  void project;
  let open = 0;
  let anyFailed = false;
  for (const r of runs) {
    if (r.status === "running" || r.status === "queued") open++;
    if (
      r.status === "failed" ||
      r.status === "timed_out" ||
      r.status === "cancelled"
    ) {
      // Only count failed runs from the last hour so the tile recovers
      // visually after the operator addresses the breakage.
      const t = Date.parse(r.completed_at ?? r.started_at);
      if (Number.isFinite(t) && Date.now() - t < 60 * 60 * 1000) {
        anyFailed = true;
      }
    }
  }
  if (open > 0) return { status: "running", open };
  if (anyFailed) return { status: "failed", open: 0 };
  return { status: "idle", open: 0 };
}

function StatusBadge({ status }: { status: RecoveryStatus }) {
  const map: Record<RecoveryStatus, { className: string; label: string }> = {
    idle: {
      className:
        "bg-emerald-500/10 text-emerald-700 ring-emerald-500/30 dark:text-emerald-300",
      label: "Idle",
    },
    running: {
      className:
        "bg-blue-500/15 text-blue-700 ring-blue-500/30 dark:text-blue-300",
      label: "Running",
    },
    failed: {
      className:
        "bg-red-500/10 text-red-700 ring-red-500/30 dark:text-red-300",
      label: "Failed",
    },
  };
  const s = map[status];
  return (
    <span
      className={cn(
        "inline-flex items-center rounded-full px-2 py-0.5 text-[10px] font-medium uppercase tracking-widest ring-1",
        s.className,
      )}
    >
      {s.label}
    </span>
  );
}

function ProjectTile({
  project,
  runs,
}: {
  project: Project;
  runs: WorkflowRun[];
}) {
  const { status, open } = statusForProject(project, runs);
  return (
    <Link
      href={`/console/projects/${project.id}` as Route}
      className="group flex flex-col gap-2 rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-4 transition-colors hover:border-[var(--color-primary)]"
    >
      <div className="flex items-start justify-between gap-2">
        <p className="truncate text-sm font-semibold text-[var(--color-foreground)]">
          {project.name}
        </p>
        <EnvironmentChip env={project.environment} size="xs" />
      </div>
      <div className="flex items-center justify-between gap-2 text-xs text-[var(--color-muted-foreground)]">
        <span>
          7d MTTR <span className="font-mono text-[var(--color-foreground)]">—</span>
        </span>
        <span>
          {open > 0 ? (
            <span className="font-mono text-red-600 dark:text-red-400">
              {open} open
            </span>
          ) : (
            <span className="text-[var(--color-muted-foreground)]">
              No open
            </span>
          )}
        </span>
      </div>
      <div className="mt-auto pt-1">
        <StatusBadge status={status} />
      </div>
    </Link>
  );
}

function EmptyTile() {
  return (
    <div className="flex flex-col items-start gap-2 rounded-lg border border-dashed border-[var(--color-border)] bg-[var(--color-card)] p-4">
      <div className="inline-flex h-8 w-8 items-center justify-center rounded-md bg-[var(--color-muted)]/60 text-[var(--color-muted-foreground)]">
        <Boxes className="h-4 w-4" />
      </div>
      <p className="text-sm font-medium text-[var(--color-foreground)]">
        No projects yet
      </p>
      <p className="text-xs text-[var(--color-muted-foreground)]">
        Connect a service to start routing incidents.
      </p>
      <Button asChild size="sm" className="mt-1">
        <Link href={"/console/projects/new" as Route}>
          <Plus className="h-4 w-4" />
          Connect first project
        </Link>
      </Button>
    </div>
  );
}

export function ProjectsHealthGrid() {
  const [rows, setRows] = React.useState<Project[]>([]);
  const [runs, setRuns] = React.useState<WorkflowRun[]>([]);
  const [loaded, setLoaded] = React.useState(false);

  React.useEffect(() => {
    let cancelled = false;
    async function tick() {
      const wsId = readWorkspaceCookie();
      if (!wsId) {
        if (!cancelled) {
          setRows([]);
          setRuns([]);
          setLoaded(true);
        }
        return;
      }
      try {
        const [list, runRows] = await Promise.all([
          projectsSdk.list(wsId),
          pipelines.list(wsId, 50).catch(() => [] as WorkflowRun[]),
        ]);
        if (cancelled) return;
        setRows(list);
        setRuns(runRows);
      } finally {
        if (!cancelled) setLoaded(true);
      }
    }
    void tick();
    const id = window.setInterval(tick, POLL_MS);
    return () => {
      cancelled = true;
      window.clearInterval(id);
    };
  }, []);

  const visible = rows.slice(0, MAX_TILES);
  const overflow = rows.length > MAX_TILES;

  return (
    <section
      aria-labelledby="projects-health-heading"
      className="rounded-lg border border-[var(--color-border)] bg-[var(--color-card)]/40 p-5"
    >
      <header className="mb-4 flex items-center justify-between">
        <div>
          <h2
            id="projects-health-heading"
            className="text-sm font-semibold text-[var(--color-foreground)]"
          >
            Projects health
          </h2>
          <p className="mt-0.5 text-xs text-[var(--color-muted-foreground)]">
            Auto-healing targets in this workspace.
          </p>
        </div>
        <Button asChild variant="ghost" size="sm">
          <Link href={"/console/projects" as Route}>
            View all
            <ArrowRight className="h-4 w-4" />
          </Link>
        </Button>
      </header>

      {loaded && rows.length === 0 ? (
        <EmptyTile />
      ) : (
        <div className="grid grid-cols-1 gap-3 md:grid-cols-2 lg:grid-cols-4">
          {visible.map((p) => (
            <ProjectTile key={p.id} project={p} runs={runs} />
          ))}
        </div>
      )}

      {overflow && (
        <p className="mt-3 text-xs text-[var(--color-muted-foreground)]">
          Showing the first {MAX_TILES} of {rows.length} projects. Open{" "}
          <Link
            href={"/console/projects" as Route}
            className="underline-offset-2 hover:underline"
          >
            Projects
          </Link>{" "}
          to see the rest.
        </p>
      )}
    </section>
  );
}
