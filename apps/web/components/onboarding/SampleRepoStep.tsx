"use client";

// Phase 8 — Step 2 of onboarding wizard.
//
// After the workspace lands in status=ready, the user picks between three
// onramps:
//
//   A. Try the synthetic fixture (left)
//      → POST /v1/workspaces/{ws}/seed-sample
//      → navigate to /console/incidents/{run_id}?live=1
//      The Phase 6 timeline UI takes over from there.
//
//   B. Connect your own GitHub repo (middle)
//      → /console/integrations
//      User completes the OAuth dance and comes back to the console at
//      their leisure; we don't try to round-trip them through the wizard.
//
//   C. Skip (right)
//      → /console
//      For users who want to poke around before doing anything.
//
// Selection is local state only — the control-plane infers the path from
// whether seed-sample is ever called.

import * as React from "react";
import { useRouter } from "next/navigation";
import type { Route } from "next";
import { GitBranch, Loader2, PlayCircle, SkipForward } from "lucide-react";

import { Button } from "@/components/ui/Button";
import { seed } from "@/lib/seed";

export function SampleRepoStep({
  workspaceId,
  workspaceName,
}: {
  workspaceId: string;
  workspaceName: string;
}) {
  const router = useRouter();
  const [submitting, setSubmitting] = React.useState<
    null | "sample" | "github" | "skip"
  >(null);
  const [error, setError] = React.useState<string | null>(null);

  async function runSample() {
    setSubmitting("sample");
    setError(null);
    try {
      const { run_id } = await seed.sample(workspaceId);
      router.replace(
        `/console/incidents/${run_id}?live=1` as Route,
      );
    } catch (err) {
      setError(
        err instanceof Error
          ? err.message
          : "Failed to start the synthetic fixture.",
      );
      setSubmitting(null);
    }
  }

  function connectGithub() {
    setSubmitting("github");
    router.replace("/console/integrations" as Route);
  }

  function skip() {
    setSubmitting("skip");
    router.replace("/console" as Route);
  }

  return (
    <div className="space-y-6" data-tour="onboarding-sample-step">
      <div>
        <p className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]">
          Step 2 of 2
        </p>
        <h1 className="mt-1 text-2xl font-semibold text-[var(--color-foreground)]">
          Try {workspaceName} on a real incident
        </h1>
        <p className="mt-2 text-sm text-[var(--color-muted-foreground)]">
          Pick how you want to see the recovery loop run. You can always
          come back to either option from the sidebar.
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

      <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
        <Card
          icon={<PlayCircle className="h-5 w-5 text-[var(--color-primary)]" />}
          title="Try the synthetic fixture"
          body="Spins up a deterministic incident bundled with NEXIS. You'll watch every agent fire end-to-end in under a minute."
          cta={
            <Button
              type="button"
              onClick={runSample}
              disabled={submitting !== null}
              className="w-full"
            >
              {submitting === "sample" ? (
                <Loader2 className="h-4 w-4 animate-spin" />
              ) : (
                <PlayCircle className="h-4 w-4" />
              )}
              Run synthetic incident
            </Button>
          }
          dataTour="onboarding-sample-card"
        />

        <Card
          icon={<GitBranch className="h-5 w-5 text-[var(--color-foreground)]" />}
          title="Connect your own GitHub repo"
          body="Wire up the GitHub integration. Future incidents from Sentry will fan out into PRs against your repo."
          cta={
            <Button
              type="button"
              variant="outline"
              onClick={connectGithub}
              disabled={submitting !== null}
              className="w-full"
            >
              {submitting === "github" ? (
                <Loader2 className="h-4 w-4 animate-spin" />
              ) : (
                <GitBranch className="h-4 w-4" />
              )}
              Connect GitHub
            </Button>
          }
          dataTour="onboarding-byo-card"
        />
      </div>

      <div className="flex justify-end pt-2">
        <Button
          type="button"
          variant="ghost"
          size="sm"
          onClick={skip}
          disabled={submitting !== null}
        >
          {submitting === "skip" ? (
            <Loader2 className="h-4 w-4 animate-spin" />
          ) : (
            <SkipForward className="h-4 w-4" />
          )}
          I&apos;ll connect later
        </Button>
      </div>
    </div>
  );
}

function Card({
  icon,
  title,
  body,
  cta,
  dataTour,
}: {
  icon: React.ReactNode;
  title: string;
  body: string;
  cta: React.ReactNode;
  dataTour: string;
}) {
  return (
    <div
      className="flex flex-col gap-4 rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-5"
      data-tour={dataTour}
    >
      <div className="space-y-2">
        <div className="inline-flex h-9 w-9 items-center justify-center rounded-md bg-[var(--color-muted)]/60">
          {icon}
        </div>
        <h2 className="text-sm font-semibold text-[var(--color-foreground)]">
          {title}
        </h2>
        <p className="text-xs text-[var(--color-muted-foreground)]">{body}</p>
      </div>
      <div className="mt-auto">{cta}</div>
    </div>
  );
}
