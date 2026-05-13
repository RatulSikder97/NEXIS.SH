// Phase 5+6 — Agents fleet surface.
//
// Server-renders the 9-agent fleet (5 L1 + 3 L2 + Sentinel detector + Approval
// router) with per-org observability rolled up from activity_events over the
// last 7 days: recent runs, last-seen, token totals, p50/p95 duration, and a
// degraded-status badge when any recent finish carried `payload.degraded`.

import { cookies } from "next/headers";
import { redirect } from "next/navigation";

import { agents, type AgentInfo } from "@/lib/agents";

const API =
  process.env.API_URL_INTERNAL ??
  process.env.NEXT_PUBLIC_API_URL ??
  "http://localhost:8080";

function formatUSD(cents: number): string {
  const dollars = cents / 100;
  const decimals = Math.abs(dollars) >= 0.01 || dollars === 0 ? 2 : 4;
  return new Intl.NumberFormat(undefined, {
    style: "currency",
    currency: "USD",
    minimumFractionDigits: decimals,
    maximumFractionDigits: decimals,
  }).format(dollars);
}

function formatTokens(n: number): string {
  if (n >= 1_000_000) return (n / 1_000_000).toFixed(1) + "M";
  if (n >= 1_000) return (n / 1_000).toFixed(1) + "k";
  return String(n);
}

function formatRelative(iso?: string): string {
  if (!iso) return "—";
  try {
    const diff = Date.now() - new Date(iso).getTime();
    const m = Math.floor(diff / 60_000);
    if (m < 1) return "just now";
    if (m < 60) return `${m}m ago`;
    const h = Math.floor(m / 60);
    if (h < 24) return `${h}h ago`;
    const d = Math.floor(h / 24);
    return `${d}d ago`;
  } catch {
    return iso;
  }
}

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
  disabled: "bg-zinc-400",
};

export default async function AgentsPage() {
  const c = await cookies();
  const session = c.get("nexis_session");
  if (!session) redirect("/sign-in");
  const wsCookie = c.get("nexis_workspace");

  // The layout already validated the session + current workspace exists.
  // Fall back to the workspaces list if no cookie is present (e.g. user
  // landed here without picking a workspace).
  let wsID = wsCookie?.value ?? "";
  if (!wsID) {
    const wsR = await fetch(`${API}/v1/workspaces`, {
      headers: { cookie: `nexis_session=${session.value}` },
      cache: "no-store",
    });
    if (wsR.ok) {
      const list = await wsR.json();
      wsID = Array.isArray(list) && list.length > 0 ? list[0].id : "";
    }
  }
  if (!wsID) redirect("/onboarding/workspace");

  const fleet = await agents.list(wsID, `nexis_session=${session.value}`);

  return (
    <div className="space-y-6">
      <div className="space-y-1">
        <p className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]">
          Fleet
        </p>
        <h1 className="text-2xl font-semibold">Agents</h1>
        <p className="text-sm text-[var(--color-muted-foreground)] max-w-2xl">
          9 specialised agents + the approval router. Five L1 specialists synthesise the patch + tests; three L2 agents detect, diagnose, and validate; the Sentinel detector lives outside the workflow but on the same fleet.
        </p>
      </div>

      <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
        {fleet.map((a) => (
          <article
            key={a.name}
            className="rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-5"
          >
            <header className="flex items-start justify-between gap-3">
              <div>
                <div className="flex items-center gap-2">
                  <h2 className="text-base font-semibold text-[var(--color-foreground)]">
                    {a.label}
                  </h2>
                  <span
                    className={
                      "rounded px-1.5 py-0.5 text-[10px] font-medium " +
                      LAYER_COLOR[a.layer]
                    }
                  >
                    {LAYER_LABEL[a.layer]}
                  </span>
                </div>
                <p className="font-mono text-[10px] text-[var(--color-muted-foreground)] mt-0.5">
                  {a.name}
                </p>
              </div>
              <span
                className="inline-flex items-center gap-1.5 text-[11px] text-[var(--color-muted-foreground)]"
                aria-label={`status ${a.status}`}
              >
                <span
                  aria-hidden
                  className={
                    "inline-block h-2 w-2 rounded-full " +
                    STATUS_COLOR[a.status] +
                    (a.status === "available" ? " animate-pulse" : "")
                  }
                />
                {a.status}
              </span>
            </header>

            <p className="mt-3 text-xs text-[var(--color-muted-foreground)] leading-relaxed">
              {a.description}
            </p>

            <dl className="mt-4 grid grid-cols-2 gap-3 text-xs">
              <Stat label="Recent runs (7d)" value={String(a.recent_runs)} />
              <Stat label="Last seen" value={formatRelative(a.last_seen_at)} />
              <Stat
                label="Tokens (in / out)"
                value={
                  a.total_tokens_in === 0 && a.total_tokens_out === 0
                    ? "—"
                    : `${formatTokens(a.total_tokens_in)} / ${formatTokens(a.total_tokens_out)}`
                }
              />
              <Stat
                label="Total cost"
                value={a.total_cost_cents_exact > 0 ? formatUSD(a.total_cost_cents_exact) : "—"}
              />
              <Stat
                label="p50 duration"
                value={a.p50_duration_ms > 0 ? `${a.p50_duration_ms}ms` : "—"}
              />
              <Stat
                label="p95 duration"
                value={a.p95_duration_ms > 0 ? `${a.p95_duration_ms}ms` : "—"}
              />
            </dl>
          </article>
        ))}
      </div>

      {fleet.length === 0 && (
        <div className="rounded-lg border border-dashed border-[var(--color-border)] bg-[var(--color-card)] p-8 text-center">
          <p className="text-sm text-[var(--color-muted-foreground)]">
            Agent fleet unavailable. The control-plane couldn&apos;t list
            agents for this workspace.
          </p>
        </div>
      )}
    </div>
  );
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <dt className="text-[var(--color-muted-foreground)]">{label}</dt>
      <dd className="mt-0.5 font-medium text-[var(--color-foreground)]">{value}</dd>
    </div>
  );
}
