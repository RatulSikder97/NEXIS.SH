"use client";

// Phase 6 Stage 9 — PendingApprovalsTable.
//
// Six columns:
//   Severity      (SeverityBadge — low/medium/high pill)
//   Run           (mono, first 8 chars, click → /console/incidents/{run})
//   Scenario      (schema_drift / null_deref / oom / unknown)
//   Awaiting      (relative "Xm" since awaiting_decision_since)
//   Synth conf.   (the Synthesiser confidence from agent_summaries)
//   Action        (Approve / Reject buttons that open DecisionDialog)
//
// Empty state delegates to the parent — we just render an empty <tbody>.
// The parent owns the polling loop and the row-removal optimistic update;
// `onApprove`/`onReject` here are just the click-router callbacks.

import * as React from "react";
import Link from "next/link";
import type { Route } from "next";

import { SeverityBadge } from "@/components/approvals/SeverityBadge";
import { Button } from "@/components/ui/Button";
import type { PendingApproval } from "@/lib/approvals";

// formatRelative returns "Xs ago" / "Xm ago" / "Xh ago" with a sensible
// fallback for inputs that don't parse. Keeps the table updating against
// the parent's `now` heartbeat.
function formatRelative(iso: string, now: number): string {
  if (!iso) return "—";
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return iso;
  const delta = Math.max(0, now - t);
  if (delta < 5_000) return "just now";
  if (delta < 60_000) return `${Math.floor(delta / 1000)}s ago`;
  if (delta < 3_600_000) return `${Math.floor(delta / 60_000)}m ago`;
  if (delta < 86_400_000) return `${Math.floor(delta / 3_600_000)}h ago`;
  try {
    return new Date(t).toLocaleString(undefined, {
      month: "short",
      day: "numeric",
      hour: "2-digit",
      minute: "2-digit",
    });
  } catch {
    return iso;
  }
}

// formatScenario maps the snake_case enum to a short title for the column.
function formatScenario(scenario: string | null | undefined): string {
  if (!scenario) return "Unknown";
  switch (scenario) {
    case "schema_drift":
      return "Schema drift";
    case "null_deref":
      return "Null dereference";
    case "oom":
      return "Memory pressure";
    default:
      return scenario;
  }
}

// formatConfidence renders a 0..1 number as a percentage, falling back to —
// for unknown values. Synthesiser confidence is a float in the agent summary.
function formatConfidence(value: unknown): string {
  if (typeof value !== "number" || !Number.isFinite(value)) return "—";
  const pct = Math.max(0, Math.min(1, value)) * 100;
  return `${pct.toFixed(0)}%`;
}

export function PendingApprovalsTable({
  rows,
  now,
  onApprove,
  onReject,
}: {
  rows: PendingApproval[];
  now: number;
  onApprove: (row: PendingApproval) => void;
  onReject: (row: PendingApproval) => void;
}) {
  return (
    <div className="overflow-hidden rounded-lg border border-[var(--color-border)] bg-[var(--color-card)]">
      <table className="w-full text-sm">
        <thead className="bg-[var(--color-muted)]/40 text-left text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]">
          <tr>
            <th className="px-4 py-2 font-medium">Severity</th>
            <th className="px-4 py-2 font-medium">Run</th>
            <th className="px-4 py-2 font-medium">Scenario</th>
            <th className="px-4 py-2 font-medium">Awaiting</th>
            <th className="px-4 py-2 font-medium">Synthesiser</th>
            <th className="px-4 py-2 font-medium text-right">Decision</th>
          </tr>
        </thead>
        <tbody>
          {rows.length === 0 ? (
            <tr>
              <td
                colSpan={6}
                className="px-4 py-8 text-center text-sm text-[var(--color-muted-foreground)]"
              >
                No pending approvals.
              </td>
            </tr>
          ) : (
            rows.map((row) => {
              const short = row.run_id.slice(0, 8);
              const synthConf = row.agent_summaries?.Synthesiser?.confidence;
              return (
                <tr
                  key={row.run_id}
                  className="border-t border-[var(--color-border)]"
                >
                  <td className="px-4 py-3">
                    <SeverityBadge severity={row.severity} />
                  </td>
                  <td className="px-4 py-3">
                    <Link
                      href={
                        `/console/incidents/${row.run_id}` as Route
                      }
                      className="font-mono text-xs text-[var(--color-foreground)] hover:underline"
                    >
                      {short}
                    </Link>
                  </td>
                  <td className="px-4 py-3 text-[var(--color-foreground)]">
                    {formatScenario(row.scenario)}
                  </td>
                  <td className="whitespace-nowrap px-4 py-3 text-[var(--color-muted-foreground)]">
                    {formatRelative(row.awaiting_decision_since, now)}
                  </td>
                  <td className="whitespace-nowrap px-4 py-3 text-[var(--color-muted-foreground)]">
                    <span className="font-mono text-xs">
                      {formatConfidence(synthConf)}
                    </span>
                  </td>
                  <td className="px-4 py-3 text-right">
                    <div className="inline-flex items-center gap-2">
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() => onReject(row)}
                      >
                        Reject
                      </Button>
                      <Button size="sm" onClick={() => onApprove(row)}>
                        Approve
                      </Button>
                    </div>
                  </td>
                </tr>
              );
            })
          )}
        </tbody>
      </table>
    </div>
  );
}
