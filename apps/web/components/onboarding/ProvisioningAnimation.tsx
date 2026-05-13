"use client";

// Phase 3.5 Stage 6 — Provisioning step animation.
//
// Subscribes to /v1/workspaces/{id}/events (SSE) and renders a five-row
// checklist that reflects the provisioning_step from each frame. The
// control-plane emits one frame per step transition plus a final
// status=ready or status=error frame; backend closes the stream after.
//
// Visual contract:
//   - pending  → gray Circle
//   - active   → spinning Loader2
//   - done     → green CheckCircle2
//   - error    → red XCircle
//
// On ready: pause 1s for a beat of polish, then fire onReady() so the
// parent can route to /console. On error: surface the message + a Retry
// button via onError() — parent decides whether to refresh or reset state.

import * as React from "react";
import {
  CheckCircle2,
  Circle,
  Loader2,
  XCircle,
  type LucideIcon,
} from "lucide-react";

import { cn } from "@/lib/utils";
import { workspaces, type ProvisioningStep } from "@/lib/workspaces";

// Canonical order of provisioning steps. Must match the strings the
// control-plane emits in ProvisioningStep.step. The 5th row (`ready`) is
// the terminal "Ready" marker — when it lights up green, provisioning is
// done.
const STEPS: { step: string; label: string }[] = [
  { step: "creating_organization", label: "Creating organization" },
  { step: "allocating_host", label: "Allocating host" },
  { step: "provisioning_datacenter", label: "Provisioning datacenter" },
  { step: "deploying", label: "Deploying services" },
  { step: "ready", label: "Workspace ready" },
];

type RowState = "pending" | "active" | "done" | "error";

function StatusIcon({ state }: { state: RowState }) {
  let Icon: LucideIcon = Circle;
  let cls = "text-[var(--color-muted-foreground)]/40";
  if (state === "active") {
    Icon = Loader2;
    cls = "text-[var(--color-primary)] animate-spin";
  } else if (state === "done") {
    Icon = CheckCircle2;
    cls = "text-emerald-500";
  } else if (state === "error") {
    Icon = XCircle;
    cls = "text-red-500";
  }
  return <Icon className={cn("h-5 w-5 shrink-0", cls)} aria-hidden />;
}

export function ProvisioningAnimation({
  workspaceId,
  onReady,
  onError,
}: {
  workspaceId: string;
  // `name` and `region` aren't rendered here — the parent owns the heading
  // and region pill — but accepting them keeps the public prop shape from
  // the spec so callers don't need to know the implementation detail.
  name?: string;
  region?: string;
  onReady: () => void;
  onError: (message: string) => void;
}) {
  // Use refs intentionally absent — we want state updates to drive the
  // re-render and `useEffect` owns the EventSource lifetime.
  const [currentStep, setCurrentStep] = React.useState<string | null>(null);
  const [completed, setCompleted] = React.useState<Set<string>>(new Set());
  const [errored, setErrored] = React.useState<{
    step: string;
    message: string;
  } | null>(null);
  const [lastLabel, setLastLabel] = React.useState<Record<string, string>>({});

  React.useEffect(() => {
    let cancelled = false;
    const es = workspaces.events(workspaceId, (ev: ProvisioningStep) => {
      if (cancelled) return;
      setLastLabel((prev) =>
        ev.label && prev[ev.step] !== ev.label
          ? { ...prev, [ev.step]: ev.label }
          : prev,
      );
      if (ev.status === "error") {
        setErrored({ step: ev.step, message: ev.message ?? "Provisioning failed" });
        return;
      }
      if (ev.status === "ready") {
        // Mark every step done; the final synthesized "ready" event also
        // implicitly completes any earlier ones we may have missed if the
        // SSE client picked up mid-stream.
        setCompleted(new Set(STEPS.map((s) => s.step)));
        setCurrentStep(null);
        // Pause 1s so the green check actually registers visually before
        // we whip the user off to /console.
        setTimeout(() => {
          if (!cancelled) onReady();
        }, 1000);
        return;
      }
      // in_progress
      setCurrentStep(ev.step);
      // Everything *before* this step is implicitly done.
      const idx = STEPS.findIndex((s) => s.step === ev.step);
      if (idx > 0) {
        setCompleted((prev) => {
          const next = new Set(prev);
          for (let i = 0; i < idx; i++) next.add(STEPS[i].step);
          return next;
        });
      }
    });
    return () => {
      cancelled = true;
      es.close();
    };
    // workspaceId is the only meaningful dep; onReady / onError are kept
    // out of the dep array to avoid resubscribing if the parent passes
    // fresh closures each render.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [workspaceId]);

  function rowState(step: string): RowState {
    if (errored?.step === step) return "error";
    if (completed.has(step)) return "done";
    if (currentStep === step) return "active";
    return "pending";
  }

  return (
    <div className="space-y-3">
      <ul className="space-y-3">
        {STEPS.map((s) => {
          const state = rowState(s.step);
          const label = lastLabel[s.step] ?? s.label;
          return (
            <li
              key={s.step}
              className={cn(
                "flex items-center gap-3 rounded-md border px-4 py-3 text-sm transition-colors",
                state === "active"
                  ? "border-[var(--color-primary)]/40 bg-[var(--color-primary)]/5"
                  : "border-[var(--color-border)] bg-[var(--color-card)]",
              )}
            >
              <StatusIcon state={state} />
              <span
                className={cn(
                  state === "pending"
                    ? "text-[var(--color-muted-foreground)]"
                    : "text-[var(--color-foreground)]",
                )}
              >
                {label}
              </span>
            </li>
          );
        })}
      </ul>
      {errored && (
        <div
          role="alert"
          className="space-y-3 rounded-md border border-red-500/30 bg-red-500/10 px-4 py-3 text-sm text-red-700 dark:text-red-300"
        >
          <p>{errored.message}</p>
          <button
            type="button"
            onClick={() => onError(errored.message)}
            className="rounded-md border border-red-500/50 px-3 py-1.5 text-xs font-medium hover:bg-red-500/10"
          >
            Retry
          </button>
        </div>
      )}
    </div>
  );
}
