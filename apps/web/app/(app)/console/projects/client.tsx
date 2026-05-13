"use client";

// Phase 3.5 — Projects list client.
//
// Card-grid surface with one tile per project. Each card carries:
//   * Project name + environment chip
//   * Connected-integrations icon row (6 lucide glyphs, dimmed when missing)
//   * 7-day MTTR + open-incidents badge (best-effort — fall back to "—")
//
// Cards link to /console/projects/{id}. Hover state pulls the border to
// var(--color-primary) so the click affordance matches the rest of the
// console grid pattern.
//
// Empty state ("No projects yet") renders the existing EmptyState with a
// "Connect a project" CTA → /console/projects/new. A "Connect project"
// button also sits in the top-right header for the populated case.

import * as React from "react";
import Link from "next/link";
import type { Route } from "next";
import { Boxes, Plus } from "lucide-react";

import { Button } from "@/components/ui/Button";
import { EmptyState } from "@/components/empty-state/EmptyState";
import { EnvironmentChip } from "@/components/projects/EnvironmentChip";
import { ProjectIntegrationIcons } from "@/components/projects/ProjectIntegrationIcons";
import { connectedProviders, type Project } from "@/lib/projects";
import { cn } from "@/lib/utils";

// formatMttr renders a number of seconds as "Xm Ys" / "Xh Ym". -1 sentinel
// means "no data yet" — we show an em-dash in that case so the surface
// never lies about MTTR until the backend stat lands.
function formatMttr(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds < 0) return "—";
  if (seconds < 60) return `${Math.round(seconds)}s`;
  const m = Math.floor(seconds / 60);
  if (m < 60) return `${m}m ${Math.round(seconds % 60)}s`;
  const h = Math.floor(m / 60);
  return `${h}h ${m % 60}m`;
}

// ProjectCard — a single tile in the responsive grid. The MTTR + open-
// incident counts are sourced from optional stats we may project onto
// the project row in a later wave — for now they degrade to "—" / 0
// so the cards stay informative without lying.
type ProjectStats = {
  mttr_7d_seconds: number;
  open_incidents: number;
};

function ProjectCard({
  project,
  stats,
}: {
  project: Project;
  stats?: ProjectStats;
}) {
  const connected = connectedProviders(project);
  const mttr = formatMttr(stats?.mttr_7d_seconds ?? -1);
  const open = stats?.open_incidents ?? 0;
  return (
    <Link
      href={`/console/projects/${project.id}` as Route}
      className={cn(
        "group flex flex-col gap-3 rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-5",
        "transition-colors hover:border-[var(--color-primary)] hover:cursor-pointer",
      )}
    >
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0 flex-1">
          <p className="truncate text-base font-semibold text-[var(--color-foreground)]">
            {project.name}
          </p>
          {project.description && (
            <p className="mt-0.5 line-clamp-2 text-xs text-[var(--color-muted-foreground)]">
              {project.description}
            </p>
          )}
        </div>
        <EnvironmentChip env={project.environment} />
      </div>

      <div className="flex items-center gap-2">
        <ProjectIntegrationIcons connected={connected} />
      </div>

      <div className="mt-auto flex items-center justify-between border-t border-[var(--color-border)]/60 pt-3">
        <div className="space-y-0.5">
          <p className="text-[10px] font-semibold uppercase tracking-widest text-[var(--color-muted-foreground)]">
            7d MTTR
          </p>
          <p className="font-mono text-sm text-[var(--color-foreground)]">
            {mttr}
          </p>
        </div>
        <span
          className={cn(
            "inline-flex items-center rounded-full px-2 py-0.5 text-[10px] font-medium uppercase tracking-widest ring-1",
            open > 0
              ? "bg-red-500/10 text-red-700 ring-red-500/30 dark:text-red-300"
              : "bg-emerald-500/10 text-emerald-700 ring-emerald-500/30 dark:text-emerald-300",
          )}
        >
          {open > 0 ? `${open} open` : "Healthy"}
        </span>
      </div>
    </Link>
  );
}

export function ProjectsListClient({
  workspaceId,
  initial,
}: {
  workspaceId: string;
  initial: Project[];
}) {
  // We don't have a stats endpoint yet — pass undefined so cards show "—".
  // When BE projects 7d-MTTR + open-incident counts onto the project row
  // (or via a sidecar endpoint) we can swap this to a real lookup table.
  const statsByProject: Map<string, ProjectStats> = React.useMemo(
    () => new Map(),
    [],
  );

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold">Projects</h1>
          <p className="mt-1 text-sm text-[var(--color-muted-foreground)]">
            Auto-healing targets for this workspace. Connect a project to
            start routing incidents into a recovery pipeline.
          </p>
        </div>
        {initial.length > 0 && (
          <Button asChild size="sm">
            <Link href={"/console/projects/new" as Route}>
              <Plus className="h-4 w-4" />
              Connect project
            </Link>
          </Button>
        )}
      </div>

      {!workspaceId && (
        <div
          role="status"
          className="rounded-md border border-amber-500/30 bg-amber-500/10 px-4 py-3 text-sm text-amber-700 dark:text-amber-300"
        >
          No workspace selected. Pick one in the sidebar to view projects.
        </div>
      )}

      {initial.length === 0 ? (
        <EmptyState
          icon={Boxes}
          title="No projects yet"
          description="Connect your first service to start auto-healing"
          cta={
            <Button asChild>
              <Link href={"/console/projects/new" as Route}>
                <Plus className="h-4 w-4" />
                Connect a project
              </Link>
            </Button>
          }
        />
      ) : (
        <div className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3">
          {initial.map((p) => (
            <ProjectCard
              key={p.id}
              project={p}
              stats={statsByProject.get(p.id)}
            />
          ))}
        </div>
      )}
    </div>
  );
}
