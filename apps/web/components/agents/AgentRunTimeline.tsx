"use client";

// AgentRunTimeline — per-event timeline + payload viewer for one (agent, run)
// pair. Fetches activity_events on mount via the agents SDK and renders a
// vertical timeline: one row per event with kind chip, ms-precision ts,
// message, and a collapsible "Show payload" section that pretty-prints the
// full JSONB blob so admins can see tokens, cost, tool calls, decisions,
// model_name, etc verbatim.
//
// Synthetic / stub-fallback rows get an amber left border + "stub fallback"
// pill — these are the rows where `payload.degraded === true`, surfaced by
// the control plane when an L1/L2 agent had to fall back to a stub instead
// of running the real model.

import * as React from "react";
import { AlertTriangle, ChevronDown, ChevronRight, Loader2 } from "lucide-react";

import { agents, type ActivityEventResp } from "@/lib/agents";
import { formatTimeMs } from "@/lib/agents-format";
import { cn } from "@/lib/utils";

type Kind = ActivityEventResp["kind"];

const KIND_PALETTE: Record<Kind, { bg: string; fg: string; ring: string; label: string }> = {
  start: {
    bg: "bg-blue-500/15",
    fg: "text-blue-700 dark:text-blue-300",
    ring: "ring-blue-500/30",
    label: "start",
  },
  log: {
    bg: "bg-[var(--color-muted)]",
    fg: "text-[var(--color-muted-foreground)]",
    ring: "ring-[var(--color-border)]",
    label: "log",
  },
  finish: {
    bg: "bg-emerald-500/15",
    fg: "text-emerald-700 dark:text-emerald-300",
    ring: "ring-emerald-500/30",
    label: "finish",
  },
  error: {
    bg: "bg-red-500/15",
    fg: "text-red-700 dark:text-red-300",
    ring: "ring-red-500/30",
    label: "error",
  },
};

function KindChip({ kind }: { kind: Kind }) {
  const p = KIND_PALETTE[kind] ?? KIND_PALETTE.log;
  return (
    <span
      className={cn(
        "inline-flex items-center rounded-full px-2 py-0.5 text-[10px] font-medium uppercase tracking-widest ring-1",
        p.bg,
        p.fg,
        p.ring,
      )}
    >
      {p.label}
    </span>
  );
}

function PayloadBlock({ payload }: { payload: Record<string, unknown> }) {
  const [open, setOpen] = React.useState(false);
  const empty = !payload || Object.keys(payload).length === 0;
  if (empty) return null;
  return (
    <div className="mt-2">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        className="inline-flex items-center gap-1 text-xs text-[var(--color-muted-foreground)] hover:text-[var(--color-foreground)]"
      >
        {open ? (
          <ChevronDown className="h-3 w-3" />
        ) : (
          <ChevronRight className="h-3 w-3" />
        )}
        Show payload
      </button>
      {open && (
        <pre className="mt-1 max-h-96 overflow-auto rounded-md bg-[var(--color-muted)]/40 p-3 text-[11px] font-mono leading-snug text-[var(--color-foreground)]">
          {JSON.stringify(payload, null, 2)}
        </pre>
      )}
    </div>
  );
}

export function AgentRunTimeline({
  wsId,
  agentName,
  runId,
}: {
  wsId: string;
  agentName: string;
  runId: string;
}) {
  const [events, setEvents] = React.useState<ActivityEventResp[] | null>(null);
  const [error, setError] = React.useState<string | null>(null);

  React.useEffect(() => {
    // Each timeline instance mounts once for a specific (wsId, agentName,
    // runId) — the parent table unmounts the row body when collapsed and
    // mounts a fresh instance when re-expanded. So we don't need to reset
    // state synchronously here (which trips react-hooks/set-state-in-effect);
    // the cancelled flag protects against a late promise resolution if the
    // owning component does ever swap props on us.
    let cancelled = false;
    agents
      .events(wsId, agentName, runId)
      .then((rows) => {
        if (cancelled) return;
        setEvents(rows);
      })
      .catch((err) => {
        if (cancelled) return;
        setError(err instanceof Error ? err.message : "Failed to load events");
        setEvents([]);
      });
    return () => {
      cancelled = true;
    };
  }, [wsId, agentName, runId]);

  if (events === null) {
    return (
      <div className="flex items-center gap-2 text-xs text-[var(--color-muted-foreground)]">
        <Loader2 className="h-3.5 w-3.5 animate-spin" />
        Loading events…
      </div>
    );
  }
  if (error) {
    return (
      <div
        role="alert"
        className="rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-xs text-red-700 dark:text-red-300"
      >
        {error}
      </div>
    );
  }
  if (events.length === 0) {
    return (
      <p className="text-xs text-[var(--color-muted-foreground)]">
        No events recorded for this run.
      </p>
    );
  }

  return (
    <ol className="relative space-y-3">
      {events.map((e) => {
        const degraded = e.payload?.degraded === true;
        return (
          <li
            key={e.id}
            className={cn(
              "relative rounded-md border bg-[var(--color-card)] p-3 pl-4",
              degraded
                ? "border-amber-500/30 border-l-4 border-l-amber-500"
                : "border-[var(--color-border)]",
            )}
          >
            <div className="flex flex-wrap items-center gap-2">
              <KindChip kind={e.kind} />
              <span className="font-mono text-[11px] text-[var(--color-muted-foreground)]">
                {formatTimeMs(e.ts)}
              </span>
              <span className="font-mono text-[11px] text-[var(--color-muted-foreground)]">
                {e.agent_role}
              </span>
              {degraded && (
                <span className="inline-flex items-center gap-1 rounded-full bg-amber-500/15 px-2 py-0.5 text-[10px] font-medium text-amber-700 ring-1 ring-amber-500/30 dark:text-amber-300">
                  <AlertTriangle className="h-3 w-3" aria-hidden />
                  stub fallback
                </span>
              )}
            </div>
            {e.message && (
              <p
                className={cn(
                  "mt-1.5 text-sm",
                  e.kind === "error"
                    ? "text-red-700 dark:text-red-300"
                    : "text-[var(--color-foreground)]",
                )}
              >
                {e.message}
              </p>
            )}
            <PayloadBlock payload={e.payload ?? {}} />
          </li>
        );
      })}
    </ol>
  );
}
