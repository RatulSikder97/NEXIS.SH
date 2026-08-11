"use client";

// Phase 4 Stage 7 — ActivityTimeline.
//
// A vertical "left-rail" timeline of the 9-agent recovery fleet plus the
// terminal Pipeline.Complete row. The component is stateless w.r.t. SSE —
// the parent owns the events array (keyed by seq) and re-renders this when
// new frames arrive.
//
// Per-agent row state is derived from the most-recent event with a matching
// agent_role:
//   no events     → pending  (grey, hollow circle, dim label)
//   started       → running  (blue, spinning ring, label normal)
//   succeeded     → done     (green check, duration suffix)
//   failed        → error    (red X, message in subtext)
//   timed_out     → error    (red X, "timed out")
//   retrying      → warn     (amber, "attempt N")
//
// Connector lines between rows light up once the previous row has a
// terminal-state event (succeeded/failed/timed_out). Pulsing animations
// gate on usePrefersReducedMotion.
//
// Tests-passed badge: the Backend.Codegen finish frame carries the
// payload {tests_passed, test_count, coverage, patch_key, report_key}.
// We surface "12/12 tests passed" as a small badge on that row.

import * as React from "react";
import {
  ChevronDown,
  ChevronRight,
  CircleSlash,
  Loader2,
  MinusCircle,
  X,
} from "lucide-react";

import { AgentIcon } from "@/components/pipelines/AgentIcon";
import {
  AGENTS_IN_ORDER,
  AGENT_LABELS,
  type ActivityEvent,
} from "@/lib/pipelines";
import { formatTokens } from "@/lib/eval";
import { cn } from "@/lib/utils";
import { usePrefersReducedMotion } from "@/lib/usePrefersReducedMotion";

// SQA Phase 4 — incident timeline must surface per-agent token + cost
// telemetry inline so an operator inspecting a recovery run can spot the
// expensive step without expanding each payload.
//
// Adaptive USD precision matches components/billing/UsageSummary.tsx
// formatUSD (≥$0.01 → 2 decimals, sub-cent → 4) so the timeline + the
// billing surface read the same number for the same spend.
function formatUSDFromCents(cents: number): string {
  const dollars = cents / 100;
  const decimals = Math.abs(dollars) >= 0.01 || dollars === 0 ? 2 : 4;
  return new Intl.NumberFormat(undefined, {
    style: "currency",
    currency: "USD",
    minimumFractionDigits: decimals,
    maximumFractionDigits: decimals,
  }).format(dollars);
}

// extractCostPillData mines tokens_in / tokens_out / cost_cents out of an
// arbitrary payload. Any field can be absent — we render whichever subset
// is present so a payload that only ships token counts (no cost) still
// gets a pill. Returns null when the payload carries none of them, which
// keeps the row layout unchanged for the pure-status agents (sentinel,
// approval gate, pipeline complete).
function extractCostPillData(payload: Record<string, unknown>): {
  tokensIn?: number;
  tokensOut?: number;
  costCents?: number;
} | null {
  const ti = payload.tokens_in;
  const to = payload.tokens_out;
  const cc = payload.cost_cents;
  const tokensIn =
    typeof ti === "number" && Number.isFinite(ti) ? ti : undefined;
  const tokensOut =
    typeof to === "number" && Number.isFinite(to) ? to : undefined;
  const costCents =
    typeof cc === "number" && Number.isFinite(cc) ? cc : undefined;
  if (
    tokensIn === undefined &&
    tokensOut === undefined &&
    costCents === undefined
  ) {
    return null;
  }
  return { tokensIn, tokensOut, costCents };
}

function CostPills({ payload }: { payload: Record<string, unknown> }) {
  const data = extractCostPillData(payload);
  if (!data) return null;
  return (
    <span
      className="inline-flex shrink-0 items-center gap-1 rounded-full bg-[var(--color-muted)]/60 px-2 py-0.5 font-mono text-[10px] text-[var(--color-foreground)] ring-1 ring-[var(--color-border)]"
      title="Tokens in · tokens out · cost"
    >
      {data.tokensIn !== undefined && (
        <span data-pill="tokens-in">{formatTokens(data.tokensIn)} in</span>
      )}
      {data.tokensIn !== undefined && data.tokensOut !== undefined && (
        <span aria-hidden className="text-[var(--color-muted-foreground)]">
          ·
        </span>
      )}
      {data.tokensOut !== undefined && (
        <span data-pill="tokens-out">{formatTokens(data.tokensOut)} out</span>
      )}
      {(data.tokensIn !== undefined || data.tokensOut !== undefined) &&
        data.costCents !== undefined && (
          <span aria-hidden className="text-[var(--color-muted-foreground)]">
            ·
          </span>
        )}
      {data.costCents !== undefined && (
        <span data-pill="cost">{formatUSDFromCents(data.costCents)}</span>
      )}
    </span>
  );
}

type RowState =
  | "pending"
  | "running"
  | "succeeded"
  | "failed"
  | "retrying"
  | "skipped";

type Row = {
  role: string;
  label: string;
  state: RowState;
  startedAt?: number;
  finishedAt?: number;
  message?: string;
  payload?: Record<string, unknown>;
  attempt?: number;
};

// pickLatest returns the highest-seq event we've seen for a given role.
// Multiple "started" + "succeeded" rows can land for one agent; we always
// reflect the last one.
function pickLatest(
  events: ActivityEvent[],
  role: string,
): ActivityEvent | undefined {
  let best: ActivityEvent | undefined;
  for (const e of events) {
    if (e.agent_role !== role) continue;
    if (!best || e.seq > best.seq) best = e;
  }
  return best;
}

// pickFinish prefers a terminal-state event (succeeded/failed/timed_out) so
// the rendered duration matches the actual completion frame even when a
// later "retrying" event comes in for a follow-up attempt.
function pickFinish(
  events: ActivityEvent[],
  role: string,
): ActivityEvent | undefined {
  for (let i = events.length - 1; i >= 0; i--) {
    const e = events[i];
    if (e.agent_role !== role) continue;
    if (
      e.status === "succeeded" ||
      e.status === "failed" ||
      e.status === "timed_out"
    ) {
      return e;
    }
  }
  return undefined;
}

function pickStart(
  events: ActivityEvent[],
  role: string,
): ActivityEvent | undefined {
  for (const e of events) {
    if (e.agent_role !== role) continue;
    if (e.status === "started") return e;
  }
  return undefined;
}

function buildRow(events: ActivityEvent[], role: string): Row {
  const latest = pickLatest(events, role);
  const label = AGENT_LABELS[role as keyof typeof AGENT_LABELS] ?? role;
  if (!latest) {
    return { role, label, state: "pending" };
  }
  const start = pickStart(events, role);
  const finish = pickFinish(events, role);
  const startedAt = start ? Date.parse(start.ts) : undefined;
  const finishedAt = finish ? Date.parse(finish.ts) : undefined;
  let state: RowState = "pending";
  switch (latest.status) {
    case "succeeded":
      state = "succeeded";
      break;
    case "failed":
    case "timed_out":
      state = "failed";
      break;
    case "retrying":
      state = "retrying";
      break;
    case "started":
      state = "running";
      break;
    case "skipped":
      state = "skipped";
      break;
  }
  return {
    role,
    label,
    state,
    startedAt,
    finishedAt,
    message: latest.message,
    payload: finish?.payload ?? latest.payload,
    attempt: latest.attempt,
  };
}

function formatDuration(ms: number): string {
  if (!Number.isFinite(ms) || ms < 0) return "—";
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)}s`;
  const m = Math.floor(ms / 60_000);
  const s = Math.round((ms % 60_000) / 1000);
  return s === 0 ? `${m}m` : `${m}m ${s}s`;
}

// renderCodegenBadge surfaces the Backend.Codegen finish frame's
// tests_passed / test_count fields when present. Defensive parsing: the
// payload is unknown-shaped JSON.
function renderCodegenBadge(payload: Record<string, unknown>): React.ReactNode {
  const total = payload.test_count;
  const passed = payload.tests_passed;
  if (typeof total !== "number" || typeof passed !== "number") return null;
  if (total === 0) return null;
  const ok = passed === total;
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[10px] font-medium ring-1",
        ok
          ? "bg-emerald-500/15 text-emerald-700 ring-emerald-500/30 dark:text-emerald-300"
          : "bg-amber-500/15 text-amber-700 ring-amber-500/30 dark:text-amber-300",
      )}
    >
      {passed}/{total} tests passed
    </span>
  );
}

function StateIcon({
  state,
  reduced,
  // `live` brightens the running pulse for the Phase 6 guided-demo flow.
  // Default false keeps the standard timeline visually unchanged.
  live = false,
}: {
  state: RowState;
  reduced: boolean;
  live?: boolean;
}) {
  switch (state) {
    case "pending":
      return (
        <span className="grid h-6 w-6 place-items-center rounded-full border border-[var(--color-border)] bg-[var(--color-card)]">
          <CircleSlash className="h-3.5 w-3.5 text-[var(--color-muted-foreground)]" />
        </span>
      );
    case "running":
      return (
        <span
          className={cn(
            "grid h-6 w-6 place-items-center rounded-full ring-1",
            live
              ? "bg-blue-500/30 ring-blue-500/70 shadow-[0_0_0_4px_rgba(59,130,246,0.18)]"
              : "bg-blue-500/15 ring-blue-500/40",
            live && !reduced && "animate-pulse",
          )}
        >
          <Loader2
            className={cn(
              "h-3.5 w-3.5 text-blue-600 dark:text-blue-300",
              !reduced && "animate-spin",
            )}
          />
        </span>
      );
    case "succeeded":
      return (
        <span className="grid h-6 w-6 place-items-center rounded-full bg-emerald-500 text-white">
          <svg
            xmlns="http://www.w3.org/2000/svg"
            viewBox="0 0 20 20"
            fill="currentColor"
            className="h-3.5 w-3.5"
            aria-hidden
          >
            <path
              fillRule="evenodd"
              d="M16.704 5.295a1 1 0 0 1 0 1.41l-7.5 7.5a1 1 0 0 1-1.412 0l-3.5-3.5a1 1 0 1 1 1.416-1.41l2.79 2.79 6.79-6.79a1 1 0 0 1 1.416 0Z"
              clipRule="evenodd"
            />
          </svg>
        </span>
      );
    case "failed":
      return (
        <span className="grid h-6 w-6 place-items-center rounded-full bg-red-500 text-white">
          <X className="h-3.5 w-3.5" />
        </span>
      );
    case "retrying":
      return (
        <span className="grid h-6 w-6 place-items-center rounded-full bg-amber-500/20 ring-1 ring-amber-500/40">
          <Loader2
            className={cn(
              "h-3.5 w-3.5 text-amber-700 dark:text-amber-300",
              !reduced && "animate-spin",
            )}
          />
        </span>
      );
    case "skipped":
      return (
        <span className="grid h-6 w-6 place-items-center rounded-full border border-dashed border-[var(--color-border)] bg-[var(--color-card)]">
          <MinusCircle className="h-3.5 w-3.5 text-[var(--color-muted-foreground)]" />
        </span>
      );
  }
}

function PayloadDetails({ payload }: { payload: Record<string, unknown> }) {
  const [open, setOpen] = React.useState(false);
  if (!payload || Object.keys(payload).length === 0) return null;
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
        Payload
      </button>
      {open && (
        <pre className="mt-1 max-h-60 overflow-auto rounded-md bg-[var(--color-muted)]/40 p-2 text-[11px] font-mono leading-snug text-[var(--color-foreground)]">
          {JSON.stringify(payload, null, 2)}
        </pre>
      )}
    </div>
  );
}

export function ActivityTimeline({
  events,
  // The detail-page heartbeat (1s) lets running rows show a live elapsed
  // duration without each row owning a ticker.
  now,
  // `liveMode` is true when the user got here from /console/live-demo via
  // ?live=1. Each row carries `data-role` + `data-state` so the parent can
  // scroll the most-recent running row into view. The active-row pulse
  // is also brighter when this flag is on.
  liveMode = false,
}: {
  events: ActivityEvent[];
  now: number;
  liveMode?: boolean;
}) {
  const reduced = usePrefersReducedMotion();
  const order = React.useMemo(
    () => [...AGENTS_IN_ORDER, "pipeline" as const],
    [],
  );
  const rows = React.useMemo(
    () => order.map((role) => buildRow(events, role)),
    [order, events],
  );

  return (
    <ol className="relative space-y-0">
      {rows.map((row, i) => {
        const prev = i > 0 ? rows[i - 1] : undefined;
        const connectorLit =
          prev?.state === "succeeded" ||
          prev?.state === "failed" ||
          prev?.state === "running" ||
          prev?.state === "retrying";
        const isLast = i === rows.length - 1;
        const elapsed =
          row.state === "running" && row.startedAt
            ? Math.max(0, now - row.startedAt)
            : row.startedAt && row.finishedAt
              ? row.finishedAt - row.startedAt
              : null;
        return (
          <li
            key={row.role}
            data-role={row.role}
            data-state={row.state}
            className="relative pb-4 pl-9"
          >
            {/* connector line into this row */}
            {i > 0 && (
              <span
                aria-hidden
                className={cn(
                  "absolute left-[11px] -top-1 h-3 w-px",
                  connectorLit
                    ? "bg-[var(--color-border)]"
                    : "bg-[var(--color-border)]/60",
                )}
              />
            )}
            {/* trailing connector */}
            {!isLast && (
              <span
                aria-hidden
                className={cn(
                  "absolute left-[11px] top-7 bottom-0 w-px",
                  row.state === "succeeded" || row.state === "failed"
                    ? "bg-[var(--color-border)]"
                    : "bg-[var(--color-border)]/60",
                )}
              />
            )}
            <span className="absolute left-0 top-0">
              <StateIcon state={row.state} reduced={reduced} live={liveMode} />
            </span>
            <div className="flex items-start justify-between gap-3">
              <div className="min-w-0">
                <div className="flex items-center gap-2">
                  <AgentIcon
                    role={row.role}
                    className={cn(
                      "h-4 w-4",
                      row.state === "pending" || row.state === "skipped"
                        ? "text-[var(--color-muted-foreground)]"
                        : "text-[var(--color-foreground)]",
                    )}
                  />
                  <span
                    className={cn(
                      "text-sm font-medium",
                      row.state === "pending" || row.state === "skipped"
                        ? "text-[var(--color-muted-foreground)]"
                        : "text-[var(--color-foreground)]",
                    )}
                  >
                    {row.label}
                  </span>
                  {row.state === "skipped" && (
                    <span className="inline-flex items-center rounded-full bg-[var(--color-muted)] px-2 py-0.5 text-[10px] font-medium text-[var(--color-muted-foreground)] ring-1 ring-[var(--color-border)]">
                      not selected
                    </span>
                  )}
                  {row.role === "backend" &&
                    row.state === "succeeded" &&
                    row.payload &&
                    renderCodegenBadge(row.payload)}
                  {row.state === "retrying" && row.attempt && (
                    <span className="inline-flex items-center rounded-full bg-amber-500/15 px-2 py-0.5 text-[10px] font-medium text-amber-700 ring-1 ring-amber-500/30 dark:text-amber-300">
                      attempt {row.attempt}
                    </span>
                  )}
                </div>
                {row.message && (
                  <p
                    className={cn(
                      "mt-1 text-xs",
                      row.state === "failed"
                        ? "text-red-700 dark:text-red-300"
                        : "text-[var(--color-muted-foreground)]",
                    )}
                  >
                    {row.message}
                  </p>
                )}
                {row.payload && <PayloadDetails payload={row.payload} />}
              </div>
              <div className="flex shrink-0 items-center gap-2 text-right text-xs text-[var(--color-muted-foreground)]">
                {row.payload && <CostPills payload={row.payload} />}
                {elapsed !== null && (
                  <span className="font-mono">{formatDuration(elapsed)}</span>
                )}
              </div>
            </div>
          </li>
        );
      })}
    </ol>
  );
}
