"use client";

// Phase 3.5 Stage 6 — Onboarding wizard client.
// Phase 8 — Sample-repo step after provisioning.
//
// Three-phase machine:
//   A) form    — name input + RegionPicker → POST /v1/workspaces
//   B) running — heading + ProvisioningAnimation → on ready, transition to C
//   C) sample  — SampleRepoStep: try the fixture | connect GitHub | skip
//
// Errors during create stay in phase A. Errors during provisioning surface
// inside the animation card with a Retry button that drops us back to A.
// The Step 2 surface owns its own navigation; we hand it the workspaceId.

import * as React from "react";
import { Loader2 } from "lucide-react";

import { Button } from "@/components/ui/Button";
import { RegionPicker } from "@/components/onboarding/RegionPicker";
import { ProvisioningAnimation } from "@/components/onboarding/ProvisioningAnimation";
import { SampleRepoStep } from "@/components/onboarding/SampleRepoStep";
import { workspaces, type Region, type Workspace } from "@/lib/workspaces";

type Phase =
  | { kind: "form"; error: string | null }
  | { kind: "running"; ws: Workspace }
  | { kind: "sample"; ws: Workspace };

export function OnboardingClient({ regions }: { regions: Region[] }) {
  const [name, setName] = React.useState("");
  const [region, setRegion] = React.useState<string | null>(
    regions[0]?.id ?? null,
  );
  const [submitting, setSubmitting] = React.useState(false);
  const [phase, setPhase] = React.useState<Phase>({ kind: "form", error: null });

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    if (!region) return;
    setSubmitting(true);
    try {
      const ws = await workspaces.create(name.trim(), region);
      setPhase({ kind: "running", ws });
    } catch (err) {
      setPhase({
        kind: "form",
        error: err instanceof Error ? err.message : "Failed to create workspace",
      });
    } finally {
      setSubmitting(false);
    }
  }

  function onReady() {
    // Phase 8: flip to the sample-repo step instead of routing straight to
    // /console. The Step 2 surface owns its own navigation away from
    // onboarding so the user lands on whichever follow-on path they pick.
    setPhase((prev) => {
      if (prev.kind !== "running") return prev;
      return { kind: "sample", ws: prev.ws };
    });
  }

  function onError() {
    // Drop the animation and let the user retry from the form. The
    // suspend isn't fired here — backend gives us a workspace row in
    // status=error; the user can retry with a fresh name.
    setPhase({
      kind: "form",
      error: "Provisioning failed. Try again with a different name or region.",
    });
  }

  if (phase.kind === "sample") {
    return (
      <SampleRepoStep
        workspaceId={phase.ws.id}
        workspaceName={phase.ws.name}
      />
    );
  }

  if (phase.kind === "running") {
    const regionInfo = regions.find((r) => r.id === phase.ws.region);
    return (
      <div className="space-y-6">
        <div>
          <p className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]">
            Setting up
          </p>
          <h1 className="mt-1 text-2xl font-semibold text-[var(--color-foreground)]">
            Provisioning {phase.ws.name}
          </h1>
          <p className="mt-2 inline-flex items-center gap-2 text-xs text-[var(--color-muted-foreground)]">
            <span className="inline-block h-2 w-2 rounded-full bg-amber-500" />
            <span className="font-mono">{phase.ws.region}</span>
            {regionInfo && <span>· {regionInfo.name}</span>}
          </p>
        </div>
        <ProvisioningAnimation
          workspaceId={phase.ws.id}
          name={phase.ws.name}
          region={phase.ws.region}
          onReady={onReady}
          onError={onError}
        />
      </div>
    );
  }

  return (
    <form className="space-y-6" onSubmit={submit}>
      <div>
        <h1 className="text-2xl font-semibold text-[var(--color-foreground)]">
          Create your workspace
        </h1>
        <p className="mt-1 text-sm text-[var(--color-muted-foreground)]">
          Pick a region close to your team. You can add more workspaces later.
        </p>
      </div>

      <div className="space-y-1">
        <label
          htmlFor="ws-name"
          className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]"
        >
          Workspace name
        </label>
        <input
          id="ws-name"
          type="text"
          required
          minLength={2}
          maxLength={64}
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="Production"
          className="w-full rounded-md border border-[var(--color-border)] bg-[var(--color-background)] px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-ring)]"
        />
      </div>

      <div className="space-y-2">
        <p className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]">
          Region
        </p>
        <RegionPicker regions={regions} selected={region} onSelect={setRegion} />
      </div>

      {phase.error && (
        <div
          role="alert"
          className="rounded-md border border-red-500/30 bg-red-500/10 px-4 py-3 text-sm text-red-700 dark:text-red-300"
        >
          {phase.error}
        </div>
      )}

      <div className="flex justify-end">
        <Button type="submit" disabled={submitting || !name.trim() || !region}>
          {submitting && <Loader2 className="h-4 w-4 animate-spin" />}
          Create workspace
        </Button>
      </div>
    </form>
  );
}
