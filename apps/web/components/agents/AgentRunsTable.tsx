"use client";

// AgentRunsTable — paginated run log for one agent.
//
// Seeded with the first page (server-fetched) and the total count so the
// "Load more" button knows when to stop. Each row is clickable and expands
// inline to reveal the AgentRunTimeline for that run — admins can drill
// from a per-agent overview into the full per-event payload (tokens, cost,
// tool calls, decisions, model name) without leaving the page.
//
// Columns: started_at (relative), scenario, severity, status, duration,
// tokens in/out, cost, summary message (truncated to 80 chars + tooltip).

import * as React from "react";
import { ChevronDown, ChevronRight, Loader2 } from "lucide-react";

import { Button } from "@/components/ui/Button";
import {
  agents,
  type AgentRun,
  type AgentRunSeverity,
  type AgentRunStatus,
} from "@/lib/agents";
import {
  formatDurationMs,
  formatRelative,
  formatTokens,
  formatUSD,
} from "@/lib/agents-format";
import { cn } from "@/lib/utils";
import { AgentRunTimeline } from "@/components/agents/AgentRunTimeline";

const PAGE = 50;
const SUMMARY_CHARS = 80;

const SEVERITY_PALETTE: Record<AgentRunSeverity, { bg: string; fg: string; ring: string; label: string }> = {
  low: {
    bg: "bg-emerald-500/15",
    fg: "text-emerald-700 dark:text-emerald-300",
    ring: "ring-emerald-500/30",
    label: "low",
  },
  medium: {
    bg: "bg-amber-500/15",
    fg: "text-amber-700 dark:text-amber-300",
    ring: "ring-amber-500/30",
    label: "medium",
  },
  high: {
    bg: "bg-red-500/15",
    fg: "text-red-700 dark:text-red-300",
    ring: "ring-red-500/30",
    label: "high",
  },
  unknown: {
    bg: "bg-[var(--color-muted)]",
    fg: "text-[var(--color-muted-foreground)]",
    ring: "ring-[var(--color-border)]",
    label: "—",
  },
};

function SeverityBadge({ severity }: { severity: AgentRunSeverity }) {
  const p = SEVERITY_PALETTE[severity] ?? SEVERITY_PALETTE.unknown;
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

const STATUS_PALETTE: Record<AgentRunStatus, { dot: string; fg: string; label: string }> = {
  succeeded: {
    dot: "bg-emerald-500",
    fg: "text-emerald-700 dark:text-emerald-300",
    label: "Succeeded",
  },
  failed: {
    dot: "bg-red-500",
    fg: "text-red-700 dark:text-red-300",
    label: "Failed",
  },
  running: {
    dot: "bg-blue-500",
    fg: "text-blue-700 dark:text-blue-300",
    label: "Running",
  },
  degraded: {
    dot: "bg-amber-500",
    fg: "text-amber-700 dark:text-amber-300",
    label: "Degraded",
  },
};

function StatusCell({ status }: { status: AgentRunStatus }) {
  const p = STATUS_PALETTE[status] ?? STATUS_PALETTE.running;
  return (
    <span className={cn("inline-flex items-center gap-1.5 text-xs font-medium", p.fg)}>
      <span
        className={cn("inline-block h-1.5 w-1.5 rounded-full", p.dot, status === "running" && "animate-pulse")}
        aria-hidden
      />
      {p.label}
    </span>
  );
}

function truncate(s: string | undefined, n: number): string {
  if (!s) return "—";
  if (s.length <= n) return s;
  return s.slice(0, n - 1) + "…";
}

export function AgentRunsTable({
  workspaceId,
  agentName,
  initialRuns,
  initialTotal,
}: {
  workspaceId: string;
  agentName: string;
  initialRuns: AgentRun[];
  initialTotal: number;
}) {
  const [rows, setRows] = React.useState<AgentRun[]>(initialRuns);
  const [total, setTotal] = React.useState<number>(initialTotal);
  const [loading, setLoading] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);
  const [openId, setOpenId] = React.useState<string | null>(null);

  const hasMore = rows.length < total;

  async function loadMore() {
    if (loading || !hasMore) return;
    setLoading(true);
    setError(null);
    try {
      const next = await agents.runs(workspaceId, agentName, {
        limit: PAGE,
        offset: rows.length,
      });
      setRows((cur) => [...cur, ...next.runs]);
      setTotal(next.total);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load more");
    } finally {
      setLoading(false);
    }
  }

  function toggle(runId: string) {
    setOpenId((cur) => (cur === runId ? null : runId));
  }

  return (
    <div className="space-y-3">
      <div className="overflow-hidden rounded-lg border border-[var(--color-border)] bg-[var(--color-card)]">
        <table className="w-full text-sm">
          <thead className="bg-[var(--color-muted)]/40 text-left text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]">
            <tr>
              <th className="px-3 py-2 font-medium" aria-label="Toggle" />
              <th className="px-3 py-2 font-medium">Started</th>
              <th className="px-3 py-2 font-medium">Scenario</th>
              <th className="px-3 py-2 font-medium">Severity</th>
              <th className="px-3 py-2 font-medium">Status</th>
              <th className="px-3 py-2 font-medium">Duration</th>
              <th className="px-3 py-2 font-medium">Tokens in / out</th>
              <th className="px-3 py-2 font-medium">Cost</th>
              <th className="px-3 py-2 font-medium">Summary</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => {
              const open = openId === r.run_id;
              return (
                <React.Fragment key={r.run_id}>
                  <tr
                    className={cn(
                      "border-t border-[var(--color-border)] cursor-pointer hover:bg-[var(--color-muted)]/30 focus-visible:bg-[var(--color-muted)]/40 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-[var(--color-ring)]",
                      open && "bg-[var(--color-muted)]/40",
                    )}
                    onClick={() => toggle(r.run_id)}
                    onKeyDown={(e) => {
                      // Keyboard activation for the row's role="button"
                      // contract: Enter or Space should toggle the
                      // disclosure. Without this the row is reachable via
                      // Tab (tabIndex={0}) but cannot actually be activated
                      // from the keyboard.
                      if (e.key === "Enter" || e.key === " ") {
                        e.preventDefault();
                        toggle(r.run_id);
                      }
                    }}
                    role="button"
                    tabIndex={0}
                    aria-expanded={open}
                    aria-controls={`run-events-${r.run_id}`}
                  >
                    <td className="px-3 py-3 align-top">
                      {open ? (
                        <ChevronDown className="h-4 w-4 text-[var(--color-muted-foreground)]" />
                      ) : (
                        <ChevronRight className="h-4 w-4 text-[var(--color-muted-foreground)]" />
                      )}
                    </td>
                    <td className="whitespace-nowrap px-3 py-3 align-top text-[var(--color-muted-foreground)]">
                      {formatRelative(r.started_at)}
                    </td>
                    <td className="px-3 py-3 align-top">
                      <span className="font-mono text-xs text-[var(--color-foreground)]">
                        {r.scenario || "—"}
                      </span>
                    </td>
                    <td className="px-3 py-3 align-top">
                      <SeverityBadge severity={r.severity} />
                    </td>
                    <td className="px-3 py-3 align-top">
                      <StatusCell status={r.status} />
                    </td>
                    <td className="whitespace-nowrap px-3 py-3 align-top text-[var(--color-muted-foreground)] font-mono text-xs">
                      {formatDurationMs(r.duration_ms)}
                    </td>
                    <td className="whitespace-nowrap px-3 py-3 align-top text-[var(--color-foreground)] font-mono text-xs">
                      {r.tokens_in === 0 && r.tokens_out === 0
                        ? "—"
                        : `${formatTokens(r.tokens_in)} / ${formatTokens(r.tokens_out)}`}
                    </td>
                    <td className="whitespace-nowrap px-3 py-3 align-top text-[var(--color-foreground)] font-mono text-xs">
                      {r.cost_cents_exact > 0 ? formatUSD(r.cost_cents_exact) : "—"}
                    </td>
                    <td
                      className="max-w-[24rem] px-3 py-3 align-top text-xs text-[var(--color-muted-foreground)]"
                      title={r.summary_message || undefined}
                    >
                      {truncate(r.summary_message, SUMMARY_CHARS)}
                    </td>
                  </tr>
                  {open && (
                    <tr id={`run-events-${r.run_id}`} className="bg-[var(--color-muted)]/20">
                      <td colSpan={9} className="px-4 py-4">
                        <div className="space-y-3">
                          <div className="flex flex-wrap items-center justify-between gap-3">
                            <div className="flex flex-wrap items-center gap-2 text-xs">
                              <span className="font-mono text-[10px] text-[var(--color-muted-foreground)]">
                                run_id
                              </span>
                              <span className="font-mono text-xs text-[var(--color-foreground)]">
                                {r.run_id}
                              </span>
                              {r.degraded && (
                                <span className="inline-flex items-center rounded-full bg-amber-500/15 px-2 py-0.5 text-[10px] font-medium text-amber-700 ring-1 ring-amber-500/30 dark:text-amber-300">
                                  degraded
                                </span>
                              )}
                              <span className="text-[var(--color-muted-foreground)]">
                                {r.event_count} events
                              </span>
                            </div>
                          </div>
                          <AgentRunTimeline
                            wsId={workspaceId}
                            agentName={agentName}
                            runId={r.run_id}
                          />
                        </div>
                      </td>
                    </tr>
                  )}
                </React.Fragment>
              );
            })}
          </tbody>
        </table>
      </div>

      {error && (
        <div
          role="alert"
          className="rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-xs text-red-700 dark:text-red-300"
        >
          {error}
        </div>
      )}

      <div className="flex items-center justify-between text-xs text-[var(--color-muted-foreground)]">
        <span>
          Showing {rows.length} of {total}
        </span>
        {hasMore && (
          <Button
            variant="outline"
            size="sm"
            onClick={loadMore}
            disabled={loading}
          >
            {loading ? (
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
            ) : null}
            Load more
          </Button>
        )}
      </div>
    </div>
  );
}
