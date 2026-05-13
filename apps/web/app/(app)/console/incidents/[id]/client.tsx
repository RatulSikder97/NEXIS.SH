"use client";

// Phase 4 Stage 7 — Pipeline run detail client.
//
// SSE consumer + header. The server-component shell seeds us with the run
// snapshot + every persisted activity_event, so first paint is the full
// timeline as it exists in the DB. We then open the SSE stream — which the
// server replays from the DB before tailing the live broker, so duplicate
// frames are expected — and dedup by `seq`.
//
// Lifecycle:
//   • mount       → set state from initial, open EventSource via SDK
//   • onmessage   → merge frame by seq, update run status if terminal
//   • named close → SDK auto-closes the EventSource, we set runFinished
//   • unmount     → close the EventSource defensively
//
// The 1s heartbeat drives live-elapsed durations for in-flight rows.
// The run-status header tracks the latest known status — when the SSE
// closes we re-fetch the run row once to pick up the final status/duration
// (the activity events alone don't carry it).

import * as React from "react";
import Link from "next/link";
import type { Route } from "next";
import { ChevronLeft, Loader2 } from "lucide-react";

import { ActivityTimeline } from "@/components/pipelines/ActivityTimeline";
import { StatusPill } from "@/components/pipelines/StatusPill";
import {
  pipelines,
  type ActivityEvent,
  type PipelineDetail,
  type WorkflowRun,
} from "@/lib/pipelines";
import { cn } from "@/lib/utils";

const TICK_MS = 1000;

function formatTime(iso: string | undefined): string {
  if (!iso) return "—";
  try {
    return new Date(iso).toLocaleString(undefined, {
      month: "short",
      day: "numeric",
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit",
    });
  } catch {
    return iso;
  }
}

function formatDuration(ms: number | undefined): string {
  if (typeof ms !== "number" || !Number.isFinite(ms) || ms < 0) return "—";
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)}s`;
  const m = Math.floor(ms / 60_000);
  const s = Math.round((ms % 60_000) / 1000);
  return s === 0 ? `${m}m` : `${m}m ${s}s`;
}

export function TimelineClient({
  workspaceId,
  runId,
  initial,
}: {
  workspaceId: string;
  runId: string;
  initial: PipelineDetail | null;
}) {
  const [run, setRun] = React.useState<WorkflowRun | null>(
    initial?.run ?? null,
  );
  const [eventsBySeq, setEventsBySeq] = React.useState<
    Record<number, ActivityEvent>
  >(() => {
    const m: Record<number, ActivityEvent> = {};
    for (const e of initial?.events ?? []) m[e.seq] = e;
    return m;
  });
  const [now, setNow] = React.useState<number>(() => Date.now());
  const [finished, setFinished] = React.useState<boolean>(() => {
    const s = initial?.run?.status;
    return s === "succeeded" || s === "failed" || s === "timed_out" || s === "cancelled";
  });
  const [streamError, setStreamError] = React.useState<string | null>(null);

  // 1s heartbeat for live durations.
  React.useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), TICK_MS);
    return () => window.clearInterval(id);
  }, []);

  // Open SSE on mount unless we're already terminal. Re-open if runId
  // changes (would only happen via deep-link navigation).
  React.useEffect(() => {
    if (!workspaceId || !runId) return;
    if (finished) return;
    let cancelled = false;
    const es = pipelines.events(
      workspaceId,
      runId,
      (ev) => {
        if (cancelled) return;
        setEventsBySeq((prev) => {
          if (prev[ev.seq]) return prev;
          return { ...prev, [ev.seq]: ev };
        });
      },
      () => {
        if (cancelled) return;
        setFinished(true);
        // Refresh the run row to pick up status/duration after the close.
        pipelines
          .get(workspaceId, runId)
          .then((d) => {
            if (cancelled) return;
            setRun(d.run);
            setEventsBySeq((prev) => {
              const next = { ...prev };
              for (const e of d.events) next[e.seq] = e;
              return next;
            });
          })
          .catch(() => {
            // ignore — the user can refresh manually.
          });
      },
    );
    es.onerror = () => {
      // The browser will auto-reconnect; we surface a soft hint that the
      // stream paused, then clear it once the next frame arrives.
      if (cancelled) return;
      setStreamError("Reconnecting…");
    };
    es.onopen = () => {
      if (cancelled) return;
      setStreamError(null);
    };
    return () => {
      cancelled = true;
      es.close();
    };
    // We deliberately only re-run when the addressable run changes, not
    // when `finished` flips — flipping `finished` is the signal to stop
    // listening, and re-running the effect would just re-evaluate and
    // tear down (which is what we want, hence including it).
  }, [workspaceId, runId, finished]);

  // Sort events by seq for the timeline.
  const events = React.useMemo(() => {
    const arr = Object.values(eventsBySeq);
    arr.sort((a, b) => a.seq - b.seq);
    return arr;
  }, [eventsBySeq]);

  const liveStatus: WorkflowRun["status"] = run?.status ?? "queued";
  const liveDuration =
    run?.duration_ms ??
    (run && (liveStatus === "running" || liveStatus === "queued")
      ? Math.max(0, now - Date.parse(run.started_at))
      : undefined);

  return (
    <div className="space-y-6">
      <div>
        <Link
          href={"/console/incidents" as Route}
          className="inline-flex items-center gap-1 text-xs text-[var(--color-muted-foreground)] hover:text-[var(--color-foreground)]"
        >
          <ChevronLeft className="h-3.5 w-3.5" />
          Back to incidents
        </Link>
      </div>

      <header className="flex flex-wrap items-start justify-between gap-4">
        <div className="min-w-0 space-y-1">
          <h1 className="text-2xl font-semibold">
            <span className="font-mono text-base text-[var(--color-muted-foreground)]">
              {runId.slice(0, 8)}
            </span>
            <span className="ml-2 text-[var(--color-foreground)]">
              {run?.workflow_type ?? "RecoveryPipeline"}
            </span>
          </h1>
          <div className="flex flex-wrap items-center gap-2 text-xs text-[var(--color-muted-foreground)]">
            <span>Started {formatTime(run?.started_at)}</span>
            {run?.completed_at && (
              <>
                <span aria-hidden>·</span>
                <span>Finished {formatTime(run.completed_at)}</span>
              </>
            )}
            <span aria-hidden>·</span>
            <span className="font-mono">{formatDuration(liveDuration)}</span>
          </div>
          {run?.error && (
            <p className="text-xs text-red-700 dark:text-red-300">
              {run.error}
            </p>
          )}
        </div>
        <div className="flex items-center gap-2">
          <StatusPill status={liveStatus} />
          {!finished && (
            <span
              className={cn(
                "inline-flex items-center gap-1 text-xs text-[var(--color-muted-foreground)]",
              )}
            >
              <Loader2 className="h-3 w-3 animate-spin" />
              {streamError ?? "Live"}
            </span>
          )}
        </div>
      </header>

      <section
        aria-label="Activity timeline"
        className="rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-5"
      >
        <ActivityTimeline events={events} now={now} />
      </section>
    </div>
  );
}
