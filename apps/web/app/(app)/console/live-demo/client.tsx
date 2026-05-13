"use client";

// Phase 4 Stage 7 — Live Demo client.
//
// One CTA: "Inject fault → watch the loop." Phase 4 ships three scenario
// buttons (schema-drift, null-deref, OOM). All three call
// /v1/workspaces/{ws}/pipelines/demo today — the `scenario` field is
// forwarded in the input payload so Phase 6 can branch the underlying
// workflow per scenario without changing the surface.
//
// On success: router.push to /console/incidents/{run.id} so the operator
// lands on the live timeline.

import * as React from "react";
import { useRouter } from "next/navigation";
import type { Route } from "next";
import {
  Bug,
  Database,
  Loader2,
  MemoryStick,
  PlayCircle,
} from "lucide-react";

import { Button } from "@/components/ui/Button";
import { pipelines } from "@/lib/pipelines";

type Scenario = {
  id: "schema-drift" | "null-deref" | "oom";
  title: string;
  description: string;
  icon: React.ComponentType<{ className?: string }>;
};

const SCENARIOS: Scenario[] = [
  {
    id: "schema-drift",
    title: "Schema drift",
    description:
      "A migration lands in prod that breaks a read query. Sentinel catches the regression, Architect plans a backfill, Backend ships the fix.",
    icon: Database,
  },
  {
    id: "null-deref",
    title: "Null dereference",
    description:
      "A new API path forgets to guard an optional field. The recovery loop reproduces the trace, generates a patch, and gates merge on QA.",
    icon: Bug,
  },
  {
    id: "oom",
    title: "Memory pressure",
    description:
      "A new feature pushes a request handler past its memory budget. DevOps autoscales while Backend trims the allocation.",
    icon: MemoryStick,
  },
];

export function LiveDemoClient({ workspaceId }: { workspaceId: string }) {
  const router = useRouter();
  const [pending, setPending] = React.useState<Scenario["id"] | null>(null);
  const [error, setError] = React.useState<string | null>(null);

  async function inject(scenario: Scenario["id"]) {
    if (!workspaceId) return;
    setPending(scenario);
    setError(null);
    try {
      const run = await pipelines.createDemo(workspaceId, {
        scenario,
      });
      router.push(`/console/incidents/${run.id}` as Route);
    } catch (err) {
      setError(
        err instanceof Error ? err.message : "Failed to inject scenario",
      );
      setPending(null);
    }
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold">Live Demo</h1>
        <p className="mt-1 text-sm text-[var(--color-muted-foreground)]">
          Inject a synthetic fault and watch the recovery loop end-to-end —
          Sentinel detects, the fleet diagnoses, plans, codes, tests, ships.
        </p>
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
          No workspace selected. Pick one in the sidebar to run a demo.
        </div>
      )}

      <section
        aria-label="Demo scenarios"
        className="grid grid-cols-1 gap-4 lg:grid-cols-3"
      >
        {SCENARIOS.map((s) => {
          const Icon = s.icon;
          const busy = pending === s.id;
          return (
            <div
              key={s.id}
              className="flex flex-col rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-5"
            >
              <div className="flex items-center gap-2">
                <span className="grid h-8 w-8 place-items-center rounded-md bg-[var(--color-muted)]/60 text-[var(--color-foreground)]">
                  <Icon className="h-4 w-4" />
                </span>
                <h2 className="text-base font-semibold text-[var(--color-foreground)]">
                  {s.title}
                </h2>
              </div>
              <p className="mt-3 flex-1 text-sm text-[var(--color-muted-foreground)]">
                {s.description}
              </p>
              <div className="mt-4">
                <Button
                  size="sm"
                  onClick={() => inject(s.id)}
                  disabled={pending !== null || !workspaceId}
                >
                  {busy ? (
                    <Loader2 className="h-4 w-4 animate-spin" />
                  ) : (
                    <PlayCircle className="h-4 w-4" />
                  )}
                  Inject {s.title.toLowerCase()}
                </Button>
              </div>
            </div>
          );
        })}
      </section>
    </div>
  );
}
