"use client";

// CostClient — MTD spend, tokens, per-agent attribution, daily stacked bar.
//
// Data lineage:
//   * MTD spend = sum of agent.total_cost_cents_exact + this-month eval-run
//     cost (openai + ollama legs).
//   * Tokens consumed = sum of agents.total_tokens_in + total_tokens_out.
//   * Prompt-cache hit % — we don't have a stable cache field on the agents
//     list today; "—" until /v1/orgs/{org}/cost lands.
//   * Average cost / incident — total_cost / max(recent_runs, 1) across L1.
//   * Daily stacked bar — group eval runs by day-of-month and sum cost.

import * as React from "react";
import { DollarSign } from "lucide-react";

import { cn } from "@/lib/utils";
import { EmptyState } from "@/components/empty-state/EmptyState";
import {
  OperationalSegments,
  type OperationalSegment,
} from "@/components/console/OperationalSegments";
import {
  formatRelative,
  formatTokens,
  formatUSD,
} from "@/lib/agents-format";
import { formatCentsExact, type EvalRunSummary } from "@/lib/eval";

export type CostAgent = {
  name: string;
  label: string;
  layer: "l1" | "l2" | "router" | "detector";
  recent_runs: number;
  tokens_in: number;
  tokens_out: number;
  cost_cents_exact: number;
};

export type CostEvalRun = EvalRunSummary & { workspace_id: string };

const LAYER_BAR_BG: Record<CostAgent["layer"], string> = {
  l1: "bg-blue-500",
  l2: "bg-purple-500",
  router: "bg-amber-500",
  detector: "bg-emerald-500",
};

const LAYER_LABEL: Record<CostAgent["layer"], string> = {
  l1: "L1",
  l2: "L2",
  router: "Router",
  detector: "Detector",
};

function startOfMonthMs(): number {
  const d = new Date();
  return new Date(d.getFullYear(), d.getMonth(), 1).getTime();
}

function dailyBars(
  runs: CostEvalRun[],
): Array<{ day: string; openai_cents: number; ollama_cents: number; total_cents: number }> {
  const since = Date.now() - 30 * 86_400_000;
  const buckets = new Map<
    string,
    { openai_cents: number; ollama_cents: number }
  >();
  for (const r of runs) {
    const t = Date.parse(r.started_at);
    if (!Number.isFinite(t) || t < since) continue;
    const day = new Date(t).toISOString().slice(0, 10);
    const ex = buckets.get(day) ?? { openai_cents: 0, ollama_cents: 0 };
    ex.openai_cents += r.openai_cost_cents_exact ?? 0;
    ex.ollama_cents += r.ollama_cost_cents_exact ?? 0;
    buckets.set(day, ex);
  }
  return Array.from(buckets.entries())
    .sort((a, b) => (a[0] < b[0] ? -1 : 1))
    .map(([day, v]) => ({
      day,
      openai_cents: v.openai_cents,
      ollama_cents: v.ollama_cents,
      total_cents: v.openai_cents + v.ollama_cents,
    }));
}

function KPICard({
  label,
  value,
  hint,
}: {
  label: string;
  value: string;
  hint?: string;
}) {
  return (
    <div className="rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-4">
      <p className="text-[10px] font-semibold uppercase tracking-widest text-[var(--color-muted-foreground)]">
        {label}
      </p>
      <p className="mt-2 text-2xl font-semibold text-[var(--color-foreground)]">
        {value}
      </p>
      {hint && (
        <p className="mt-1 text-[11px] text-[var(--color-muted-foreground)]">
          {hint}
        </p>
      )}
    </div>
  );
}

function segmentsFor(
  evalRuns: CostEvalRun[],
  agents: CostAgent[],
): OperationalSegment[] {
  // Combine: spend events for each agent + a few highest-cost eval runs.
  const agentSegs: OperationalSegment[] = agents
    .slice()
    .sort((a, b) => b.cost_cents_exact - a.cost_cents_exact)
    .slice(0, 8)
    .map((a) => ({
      label: `${a.label} · ${LAYER_LABEL[a.layer]}`,
      started_at: new Date().toISOString(),
      finished_at: new Date().toISOString(),
      duration_ms: 0,
      status: a.cost_cents_exact > 0 ? "succeeded" : "pending",
      detail: `${a.recent_runs} runs · ${formatTokens(
        a.tokens_in + a.tokens_out,
      )} tokens · ${a.cost_cents_exact > 0 ? formatUSD(a.cost_cents_exact) : "—"}`,
    }));

  const topRuns: OperationalSegment[] = evalRuns
    .slice()
    .sort(
      (a, b) =>
        b.openai_cost_cents_exact +
        b.ollama_cost_cents_exact -
        (a.openai_cost_cents_exact + a.ollama_cost_cents_exact),
    )
    .slice(0, 6)
    .map((r) => ({
      label: `Eval · ${r.scenario}`,
      started_at: r.started_at,
      finished_at: r.finished_at,
      duration_ms:
        r.duration_openai_ms + r.duration_ollama_ms || undefined,
      status:
        r.openai_status === "succeeded" && r.ollama_status === "succeeded"
          ? "succeeded"
          : r.openai_status === "failed" || r.ollama_status === "failed"
            ? "failed"
            : "running",
      detail: `OpenAI ${formatCentsExact(r.openai_cost_cents_exact)} · Ollama ${formatCentsExact(
        r.ollama_cost_cents_exact,
      )} · ${formatRelative(r.started_at)}`,
    }));

  return [...agentSegs, ...topRuns];
}

export function CostClient({
  evalRuns,
  agents,
}: {
  evalRuns: CostEvalRun[];
  agents: CostAgent[];
}) {
  const monthStart = startOfMonthMs();

  let mtdAgentCents = 0;
  let totalTokensIn = 0;
  let totalTokensOut = 0;
  let totalRuns = 0;
  for (const a of agents) {
    mtdAgentCents += a.cost_cents_exact;
    totalTokensIn += a.tokens_in;
    totalTokensOut += a.tokens_out;
    totalRuns += a.recent_runs;
  }

  let mtdEvalCents = 0;
  for (const r of evalRuns) {
    const t = Date.parse(r.started_at);
    if (!Number.isFinite(t) || t < monthStart) continue;
    mtdEvalCents += r.openai_cost_cents_exact + r.ollama_cost_cents_exact;
  }
  // Eval costs come back in fractional-cents (cents × 10000) — convert to
  // plain cents before summing with agent costs which are plain cents.
  const mtdEvalCentsPlain = mtdEvalCents / 10000;
  const totalMtdCents = mtdAgentCents + mtdEvalCentsPlain;
  const avgPerIncident =
    totalRuns > 0 ? mtdAgentCents / Math.max(totalRuns, 1) : 0;

  const dailyData = dailyBars(evalRuns);

  if (agents.length === 0 && evalRuns.length === 0) {
    return (
      <div className="space-y-6">
        <div className="space-y-1">
          <p className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]">
            Observability
          </p>
          <h1 className="text-2xl font-semibold">Cost tracker</h1>
        </div>
        <EmptyState
          icon={DollarSign}
          title="No cost data yet"
          description="Agent runs and eval runs both surface cost. Once either fires, MTD spend will populate here."
        />
      </div>
    );
  }

  const maxDay = Math.max(1, ...dailyData.map((d) => d.total_cents));

  return (
    <div className="space-y-6">
      <div className="space-y-1">
        <p className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]">
          Observability
        </p>
        <h1 className="text-2xl font-semibold">Cost tracker</h1>
        <p className="max-w-2xl text-sm text-[var(--color-muted-foreground)]">
          Month-to-date spend across agent traffic and eval runs.
          Prompt-cache hit % and per-incident attribution will populate when
          the org-cost endpoint lands.
        </p>
      </div>

      <section
        aria-labelledby="cost-kpi-heading"
        className="grid grid-cols-2 gap-3 lg:grid-cols-4"
      >
        <h2 id="cost-kpi-heading" className="sr-only">
          Cost KPIs
        </h2>
        <KPICard
          label="MTD total spend"
          value={totalMtdCents > 0 ? formatUSD(totalMtdCents) : "—"}
          hint="Agent + eval runs"
        />
        <KPICard
          label="Tokens consumed"
          value={formatTokens(totalTokensIn + totalTokensOut)}
          hint="In + out"
        />
        <KPICard
          label="Cache hit %"
          value="—"
          hint="Available once endpoint lands"
        />
        <KPICard
          label="Avg cost / incident"
          value={avgPerIncident > 0 ? formatUSD(avgPerIncident) : "—"}
          hint="Across L1+L2 agent runs"
        />
      </section>

      <section
        aria-labelledby="cost-daily-heading"
        className="rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-5"
      >
        <header className="mb-3 flex items-baseline justify-between">
          <div>
            <h2
              id="cost-daily-heading"
              className="text-sm font-semibold text-[var(--color-foreground)]"
            >
              Daily spend
            </h2>
            <p className="mt-0.5 text-xs text-[var(--color-muted-foreground)]">
              Eval runs grouped by day (last 30d). Real-time recovery spend
              will append once the cost endpoint ships.
            </p>
          </div>
        </header>
        {dailyData.length === 0 ? (
          <p className="text-xs text-[var(--color-muted-foreground)]">
            No daily spend data yet.
          </p>
        ) : (
          <div className="flex items-end gap-1.5">
            {dailyData.map((d) => {
              const hOpenai = (d.openai_cents / maxDay) * 120;
              const hOllama = (d.ollama_cents / maxDay) * 120;
              return (
                <div
                  key={d.day}
                  className="flex flex-1 flex-col items-center gap-1"
                  title={`${d.day} · OpenAI ${formatCentsExact(d.openai_cents)} · Ollama ${formatCentsExact(d.ollama_cents)}`}
                >
                  <div className="relative flex h-32 w-full flex-col-reverse">
                    <div
                      className="w-full rounded-t bg-blue-500"
                      style={{ height: `${hOpenai}px` }}
                      aria-hidden
                    />
                    <div
                      className="w-full bg-purple-500"
                      style={{ height: `${hOllama}px` }}
                      aria-hidden
                    />
                  </div>
                  <span className="text-[9px] font-mono text-[var(--color-muted-foreground)]">
                    {d.day.slice(5)}
                  </span>
                </div>
              );
            })}
          </div>
        )}
        <div className="mt-3 flex items-center gap-4 text-[11px] text-[var(--color-muted-foreground)]">
          <span className="inline-flex items-center gap-1">
            <span aria-hidden className="h-2 w-2 rounded bg-blue-500" />
            OpenAI
          </span>
          <span className="inline-flex items-center gap-1">
            <span aria-hidden className="h-2 w-2 rounded bg-purple-500" />
            Ollama
          </span>
        </div>
      </section>

      <section
        aria-labelledby="cost-agents-heading"
        className="overflow-hidden rounded-lg border border-[var(--color-border)] bg-[var(--color-card)]"
      >
        <header className="border-b border-[var(--color-border)] px-5 py-3">
          <h2
            id="cost-agents-heading"
            className="text-sm font-semibold text-[var(--color-foreground)]"
          >
            Per-agent spend
          </h2>
        </header>
        <div className="overflow-x-auto">
        <table className="w-full min-w-[720px] text-sm">
          <thead className="bg-[var(--color-muted)]/40 text-left text-[10px] uppercase tracking-widest text-[var(--color-muted-foreground)]">
            <tr>
              <th className="px-3 py-2 font-medium">Agent</th>
              <th className="px-3 py-2 font-medium">Layer</th>
              <th className="px-3 py-2 font-medium">Runs</th>
              <th className="px-3 py-2 font-medium">Tokens</th>
              <th className="px-3 py-2 font-medium">MTD cost</th>
              <th className="px-3 py-2 font-medium">Share</th>
            </tr>
          </thead>
          <tbody>
            {agents
              .slice()
              .sort((a, b) => b.cost_cents_exact - a.cost_cents_exact)
              .map((a) => {
                const share =
                  mtdAgentCents > 0
                    ? (a.cost_cents_exact / mtdAgentCents) * 100
                    : 0;
                return (
                  <tr
                    key={a.name}
                    className="border-t border-[var(--color-border)] hover:bg-[var(--color-muted)]/20"
                  >
                    <td className="px-3 py-2 text-xs">
                      <p className="font-medium text-[var(--color-foreground)]">
                        {a.label}
                      </p>
                      <p className="font-mono text-[10px] text-[var(--color-muted-foreground)]">
                        {a.name}
                      </p>
                    </td>
                    <td className="px-3 py-2">
                      <span className="inline-flex items-center rounded px-1.5 py-0.5 text-[10px] font-medium ring-1 ring-[var(--color-border)] bg-[var(--color-muted)] text-[var(--color-muted-foreground)]">
                        {LAYER_LABEL[a.layer]}
                      </span>
                    </td>
                    <td className="px-3 py-2 font-mono text-xs text-[var(--color-muted-foreground)]">
                      {a.recent_runs}
                    </td>
                    <td className="px-3 py-2 font-mono text-xs text-[var(--color-muted-foreground)]">
                      {formatTokens(a.tokens_in + a.tokens_out)}
                    </td>
                    <td className="px-3 py-2 font-mono text-xs text-[var(--color-muted-foreground)]">
                      {a.cost_cents_exact > 0
                        ? formatUSD(a.cost_cents_exact)
                        : "—"}
                    </td>
                    <td className="px-3 py-2">
                      <div className="flex items-center gap-2">
                        <div className="h-1.5 w-24 overflow-hidden rounded-full bg-[var(--color-muted)]">
                          <div
                            className={cn(
                              "h-full transition-all",
                              LAYER_BAR_BG[a.layer],
                            )}
                            style={{ width: `${Math.min(share, 100)}%` }}
                            aria-hidden
                          />
                        </div>
                        <span className="font-mono text-[10px] text-[var(--color-muted-foreground)]">
                          {share.toFixed(1)}%
                        </span>
                      </div>
                    </td>
                  </tr>
                );
              })}
          </tbody>
        </table>
        </div>
      </section>

      <OperationalSegments
        title="Operational segments"
        description="Highest-cost agents + recent expensive eval runs."
        segments={segmentsFor(evalRuns, agents)}
      />
    </div>
  );
}
