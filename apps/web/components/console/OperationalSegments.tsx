"use client";

// OperationalSegments — the reusable "Detailed logs / operational segments"
// panel mounted on every operations surface. Renders a vertical stage list
// where each stage carries:
//   * status pill (running / succeeded / failed / pending / skipped)
//   * relative duration ("3m 12s") + absolute timestamps in a tooltip
//   * an optional `detail` line of free text
//   * an expandable block of `sub_steps`, each with its own status + an
//     optional JSON `payload` viewer.
//
// The pattern mirrors AgentRunTimeline but is decoupled from any specific
// fetch — callers pass shaped data in. The bottom-of-page convention is:
//   <OperationalSegments title="Detailed logs" segments={...} />
// so the same component renders on Workflows, Activity, Validator, Recovery,
// Performance, Cost, Health, etc. without each page reinventing the layout.

import * as React from "react";
import {
  ChevronDown,
  ChevronRight,
  CircleDashed,
  CircleSlash,
  Loader2,
  XCircle,
  CheckCircle2,
  type LucideIcon,
} from "lucide-react";

import { cn } from "@/lib/utils";
import { formatDurationMs, formatTimeMs } from "@/lib/agents-format";

export type SegmentStatus =
  | "running"
  | "succeeded"
  | "failed"
  | "pending"
  | "skipped";

export type OperationalSubStep = {
  label: string;
  status: string;
  payload?: Record<string, unknown>;
};

export type OperationalSegment = {
  label: string;
  started_at: string;
  finished_at?: string;
  status: SegmentStatus;
  detail?: string;
  duration_ms?: number;
  sub_steps?: OperationalSubStep[];
};

const STATUS_ICON: Record<SegmentStatus, LucideIcon> = {
  running: Loader2,
  succeeded: CheckCircle2,
  failed: XCircle,
  pending: CircleDashed,
  skipped: CircleSlash,
};

const STATUS_DOT: Record<SegmentStatus, string> = {
  running: "bg-blue-500",
  succeeded: "bg-emerald-500",
  failed: "bg-red-500",
  pending: "bg-zinc-400",
  skipped: "bg-zinc-300",
};

const STATUS_PILL: Record<SegmentStatus, string> = {
  running:
    "bg-blue-500/15 text-blue-700 ring-blue-500/30 dark:text-blue-300",
  succeeded:
    "bg-emerald-500/15 text-emerald-700 ring-emerald-500/30 dark:text-emerald-300",
  failed:
    "bg-red-500/15 text-red-700 ring-red-500/30 dark:text-red-300",
  pending:
    "bg-zinc-500/15 text-zinc-700 ring-zinc-500/30 dark:text-zinc-300",
  skipped:
    "bg-zinc-500/10 text-zinc-600 ring-zinc-500/20 dark:text-zinc-400",
};

function PayloadBlock({ payload }: { payload: Record<string, unknown> }) {
  const [open, setOpen] = React.useState(false);
  const empty = !payload || Object.keys(payload).length === 0;
  if (empty) return null;
  return (
    <div className="mt-1">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        className="inline-flex items-center gap-1 text-[11px] text-[var(--color-muted-foreground)] hover:text-[var(--color-foreground)]"
      >
        {open ? (
          <ChevronDown className="h-3 w-3" />
        ) : (
          <ChevronRight className="h-3 w-3" />
        )}
        Show payload
      </button>
      {open && (
        <pre className="mt-1 max-h-80 overflow-auto rounded-md bg-[var(--color-muted)]/40 p-3 text-[11px] font-mono leading-snug text-[var(--color-foreground)]">
          {JSON.stringify(payload, null, 2)}
        </pre>
      )}
    </div>
  );
}

function SubStepRow({ step }: { step: OperationalSubStep }) {
  return (
    <div className="rounded-md border border-[var(--color-border)] bg-[var(--color-background)] px-3 py-2">
      <div className="flex items-center justify-between gap-2">
        <span className="text-xs font-medium text-[var(--color-foreground)]">
          {step.label}
        </span>
        <span className="text-[10px] uppercase tracking-widest text-[var(--color-muted-foreground)]">
          {step.status}
        </span>
      </div>
      {step.payload && <PayloadBlock payload={step.payload} />}
    </div>
  );
}

function Segment({ seg, last }: { seg: OperationalSegment; last: boolean }) {
  const [open, setOpen] = React.useState(false);
  const Icon = STATUS_ICON[seg.status];
  const hasSubsteps = (seg.sub_steps?.length ?? 0) > 0;
  const isRunning = seg.status === "running";
  const startedTitle = `Started ${formatTimeMs(seg.started_at)}`;
  const finishedTitle = seg.finished_at
    ? ` · Finished ${formatTimeMs(seg.finished_at)}`
    : "";
  return (
    <li className="relative flex gap-3">
      <div className="flex flex-col items-center">
        <span
          className={cn(
            "inline-flex h-8 w-8 items-center justify-center rounded-full ring-1",
            seg.status === "running"
              ? "ring-blue-500/40 bg-blue-500/10"
              : seg.status === "succeeded"
                ? "ring-emerald-500/40 bg-emerald-500/10"
                : seg.status === "failed"
                  ? "ring-red-500/40 bg-red-500/10"
                  : "ring-[var(--color-border)] bg-[var(--color-card)]",
          )}
        >
          <Icon
            className={cn(
              "h-4 w-4",
              isRunning && "animate-spin",
              seg.status === "running" && "text-blue-600",
              seg.status === "succeeded" && "text-emerald-600",
              seg.status === "failed" && "text-red-600",
              seg.status === "pending" && "text-[var(--color-muted-foreground)]",
              seg.status === "skipped" && "text-[var(--color-muted-foreground)]",
            )}
          />
        </span>
        {!last && (
          <span
            aria-hidden
            className="mt-1 h-full w-px flex-1 bg-[var(--color-border)]"
          />
        )}
      </div>
      <div className="flex-1 pb-5">
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-sm font-medium text-[var(--color-foreground)]">
            {seg.label}
          </span>
          <span
            className={cn(
              "inline-flex items-center rounded-full px-2 py-0.5 text-[10px] font-medium uppercase tracking-widest ring-1",
              STATUS_PILL[seg.status],
            )}
          >
            <span
              aria-hidden
              className={cn(
                "mr-1 inline-block h-1.5 w-1.5 rounded-full",
                STATUS_DOT[seg.status],
                isRunning && "animate-pulse",
              )}
            />
            {seg.status}
          </span>
          {typeof seg.duration_ms === "number" && (
            <span
              className="font-mono text-[11px] text-[var(--color-muted-foreground)]"
              title={`${startedTitle}${finishedTitle}`}
            >
              {formatDurationMs(seg.duration_ms)}
            </span>
          )}
        </div>
        {seg.detail && (
          <p className="mt-1 text-xs text-[var(--color-muted-foreground)]">
            {seg.detail}
          </p>
        )}
        {hasSubsteps && (
          <div className="mt-2">
            <button
              type="button"
              onClick={() => setOpen((v) => !v)}
              aria-expanded={open}
              className="inline-flex items-center gap-1 text-[11px] text-[var(--color-muted-foreground)] hover:text-[var(--color-foreground)]"
            >
              {open ? (
                <ChevronDown className="h-3 w-3" />
              ) : (
                <ChevronRight className="h-3 w-3" />
              )}
              {seg.sub_steps?.length} sub-step{seg.sub_steps?.length === 1 ? "" : "s"}
            </button>
            {open && (
              <div className="mt-2 space-y-1.5">
                {seg.sub_steps?.map((s, i) => (
                  <SubStepRow key={`${s.label}-${i}`} step={s} />
                ))}
              </div>
            )}
          </div>
        )}
      </div>
    </li>
  );
}

export function OperationalSegments({
  segments,
  title = "Detailed logs",
  description,
  emptyMessage = "No operational segments recorded yet.",
}: {
  segments: OperationalSegment[];
  title?: string;
  description?: string;
  emptyMessage?: string;
}) {
  return (
    <section
      aria-labelledby="op-segments-heading"
      className="rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-5"
    >
      <header className="mb-4">
        <h2
          id="op-segments-heading"
          className="text-sm font-semibold text-[var(--color-foreground)]"
        >
          {title}
        </h2>
        {description && (
          <p className="mt-0.5 text-xs text-[var(--color-muted-foreground)]">
            {description}
          </p>
        )}
      </header>
      {segments.length === 0 ? (
        <p className="text-xs text-[var(--color-muted-foreground)]">
          {emptyMessage}
        </p>
      ) : (
        <ol className="relative space-y-0">
          {segments.map((s, i) => (
            <Segment
              key={`${s.label}-${i}`}
              seg={s}
              last={i === segments.length - 1}
            />
          ))}
        </ol>
      )}
    </section>
  );
}
