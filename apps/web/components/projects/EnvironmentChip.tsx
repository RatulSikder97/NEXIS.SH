"use client";

// EnvironmentChip — small pill used wherever a project's environment is
// surfaced (list cards, detail header, dashboard health grid). The three
// environments map to a deliberate colour ramp:
//
//   dev      → zinc   (neutral, low signal)
//   staging  → amber  (caution, pre-prod)
//   prod     → blue   (production, attention without alarm)
//
// Kept here rather than inlined so the three call sites can't drift apart.

import * as React from "react";

import { cn } from "@/lib/utils";
import type { ProjectEnvironment } from "@/lib/projects";

const ENV_STYLES: Record<ProjectEnvironment, string> = {
  dev: "bg-zinc-500/10 text-zinc-700 ring-zinc-500/30 dark:text-zinc-300",
  staging: "bg-amber-500/15 text-amber-700 ring-amber-500/30 dark:text-amber-300",
  prod: "bg-blue-500/15 text-blue-700 ring-blue-500/30 dark:text-blue-300",
};

const ENV_LABELS: Record<ProjectEnvironment, string> = {
  dev: "Dev",
  staging: "Staging",
  prod: "Prod",
};

export function EnvironmentChip({
  env,
  size = "sm",
}: {
  env: ProjectEnvironment;
  size?: "sm" | "xs";
}) {
  const padding = size === "xs" ? "px-1.5 py-0 text-[9px]" : "px-2 py-0.5 text-[10px]";
  return (
    <span
      className={cn(
        "inline-flex items-center rounded-full font-medium uppercase tracking-widest ring-1",
        padding,
        ENV_STYLES[env],
      )}
    >
      {ENV_LABELS[env]}
    </span>
  );
}
