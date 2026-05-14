// Per-agent drill-down — server component.
//
// Owner/admin enters from /console/agents (the fleet grid) and lands here on
// the agent they want to inspect. We show:
//   • Header: agent name + label + layer chip + status dot (colours mirror
//     the fleet grid so the eye doesn't have to retrain).
//   • Description paragraph from the catalog — pulled live off the agents
//     list so the source of truth stays on the backend.
//   • Stat strip: total runs (7d), total tokens, total cost, p95 duration.
//   • Runs table: paginated 50-per-page; each row expands inline to the
//     full per-event payload viewer so an operator can see tokens, cost,
//     tool calls, decisions, and model name verbatim.
//
// Notes:
//   • Next 16 hands route params + searchParams as Promises; we await both.
//   • notFound() when the agent name doesn't appear in the fleet — the
//     backend is the catalogue, we don't carry our own mirror.

import { cookies } from "next/headers";
import { notFound, redirect } from "next/navigation";
import Link from "next/link";
import type { Route } from "next";
import { ChevronLeft, PlayCircle, Wrench } from "lucide-react";

import { agents, type AgentInfo } from "@/lib/agents";
import { formatRelative, formatTokens, formatUSD } from "@/lib/agents-format";
import { AgentRunsTable } from "@/components/agents/AgentRunsTable";
import { EmptyState } from "@/components/empty-state/EmptyState";
import { Button } from "@/components/ui/Button";

const API =
  process.env.API_URL_INTERNAL ??
  process.env.NEXT_PUBLIC_API_URL ??
  "http://localhost:8080";

const LAYER_LABEL: Record<AgentInfo["layer"], string> = {
  l1: "L1",
  l2: "L2",
  router: "Router",
  detector: "Detector",
};

const LAYER_COLOR: Record<AgentInfo["layer"], string> = {
  l1: "bg-[var(--color-primary)]/15 text-[var(--color-primary)]",
  l2: "bg-purple-500/15 text-purple-600",
  router: "bg-amber-500/15 text-amber-700",
  detector: "bg-emerald-500/15 text-emerald-700",
};

const STATUS_COLOR: Record<AgentInfo["status"], string> = {
  available: "bg-emerald-500",
  degraded: "bg-amber-500",
  disabled: "bg-[var(--color-muted-foreground)]/40",
};

export default async function AgentDetailPage({
  params,
}: {
  params: Promise<{ name: string }>;
}) {
  const { name } = await params;

  const c = await cookies();
  const session = c.get("nexis_session");
  if (!session) redirect("/sign-in");
  const cookieHeader = `nexis_session=${session.value}`;

  const wsCookie = c.get("nexis_workspace");
  let wsID = wsCookie?.value ?? "";
  if (!wsID) {
    const wsR = await fetch(`${API}/v1/workspaces`, {
      headers: { cookie: cookieHeader },
      cache: "no-store",
    });
    if (wsR.ok) {
      const list = await wsR.json();
      wsID = Array.isArray(list) && list.length > 0 ? list[0].id : "";
    }
  }
  if (!wsID) redirect("/onboarding/workspace");

  // Pull the fleet once so the header + description + stats all read from
  // the same row the fleet grid showed the user.
  const fleet = await agents.list(wsID, cookieHeader);
  const agent = fleet.find((a) => a.name === name);
  if (!agent) notFound();

  const initial = await agents.runs(wsID, name, {
    limit: 50,
    offset: 0,
    cookie: cookieHeader,
  });

  const hasRuns = initial.runs.length > 0;

  return (
    <div className="space-y-6">
      <div>
        <Link
          href={"/console/agents" as Route}
          className="inline-flex items-center gap-1 text-xs text-[var(--color-muted-foreground)] hover:text-[var(--color-foreground)]"
        >
          <ChevronLeft className="h-3.5 w-3.5" />
          Back to agents
        </Link>
      </div>

      <header className="space-y-2">
        <div className="flex flex-wrap items-center gap-2">
          <h1 className="text-2xl font-semibold text-[var(--color-foreground)]">
            {agent.label}
          </h1>
          <span
            className={
              "rounded px-1.5 py-0.5 text-[10px] font-medium " +
              LAYER_COLOR[agent.layer]
            }
          >
            {LAYER_LABEL[agent.layer]}
          </span>
          <span
            className="inline-flex items-center gap-1.5 text-[11px] text-[var(--color-muted-foreground)]"
            aria-label={`status ${agent.status}`}
          >
            <span
              aria-hidden
              className={
                "inline-block h-2 w-2 rounded-full " +
                STATUS_COLOR[agent.status] +
                (agent.status === "available" ? " animate-pulse" : "")
              }
            />
            {agent.status}
          </span>
        </div>
        <p className="font-mono text-[10px] text-[var(--color-muted-foreground)]">
          {agent.name}
        </p>
        <p className="text-sm text-[var(--color-muted-foreground)] max-w-2xl leading-relaxed">
          {agent.description}
        </p>
      </header>

      <section
        aria-label="Agent statistics"
        className="grid grid-cols-2 gap-3 md:grid-cols-4"
      >
        <Stat label="Total runs (7d)" value={String(agent.recent_runs)} />
        <Stat
          label="Total tokens"
          value={
            agent.total_tokens_in === 0 && agent.total_tokens_out === 0
              ? "—"
              : `${formatTokens(agent.total_tokens_in)} / ${formatTokens(agent.total_tokens_out)}`
          }
          hint="in / out"
        />
        <Stat
          label="Total cost"
          value={
            agent.total_cost_cents_exact > 0
              ? formatUSD(agent.total_cost_cents_exact)
              : "—"
          }
        />
        <Stat
          label="p95 duration"
          value={agent.p95_duration_ms > 0 ? `${agent.p95_duration_ms}ms` : "—"}
          hint={`p50 ${agent.p50_duration_ms > 0 ? `${agent.p50_duration_ms}ms` : "—"}`}
        />
        <Stat label="Last seen" value={formatRelative(agent.last_seen_at)} />
      </section>

      <section aria-label="Run log" className="space-y-3">
        <div className="flex items-end justify-between gap-3">
          <div>
            <h2 className="text-base font-semibold text-[var(--color-foreground)]">
              Run log
            </h2>
            <p className="text-xs text-[var(--color-muted-foreground)]">
              Every workflow run that invoked this agent — click a row to see
              the full event timeline and per-event payload.
            </p>
          </div>
          <span className="text-xs text-[var(--color-muted-foreground)]">
            {initial.total} total
          </span>
        </div>

        {hasRuns ? (
          <AgentRunsTable
            workspaceId={wsID}
            agentName={name}
            initialRuns={initial.runs}
            initialTotal={initial.total}
          />
        ) : (
          <EmptyState
            iconNode={<Wrench className="h-5 w-5" aria-hidden />}
            title="No runs yet"
            description="This agent hasn't executed in the last 7 days. Trigger a synthetic demo to walk it through the recovery loop."
            cta={
              <Button asChild size="sm">
                <Link href={"/console/live-demo" as Route}>
                  <PlayCircle className="h-4 w-4" />
                  Run synthetic demo
                </Link>
              </Button>
            }
          />
        )}
      </section>
    </div>
  );
}

function Stat({
  label,
  value,
  hint,
}: {
  label: string;
  value: string;
  hint?: string;
}) {
  return (
    <div className="rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] px-4 py-3">
      <p className="text-[10px] uppercase tracking-widest text-[var(--color-muted-foreground)]">
        {label}
      </p>
      <p className="mt-1 font-medium text-[var(--color-foreground)]">{value}</p>
      {hint && (
        <p className="mt-0.5 text-[10px] text-[var(--color-muted-foreground)]">
          {hint}
        </p>
      )}
    </div>
  );
}
