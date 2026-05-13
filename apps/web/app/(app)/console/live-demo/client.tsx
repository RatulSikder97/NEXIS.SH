"use client";

// Live Demo — sector-grouped scenario catalog.
//
// Replaces the original 3-card client. The page is now organised as the
// full roadmap of fault scenarios grouped by sector (application, data,
// deploy, infrastructure, observability, security). Each card:
//
//   * shows a lucide icon, title, severity pill, and source-provider chip
//   * lists the L1/L2 agents that will fire during the run
//   * surfaces an "Run scenario" button (or a "Coming soon" badge for
//     scenarios whose backend fixture isn't wired yet)
//
// Filtering:
//   * a sector chip strip + search box reduces the catalog inline
//   * the empty state for a search miss is handled with a quiet inline
//     message instead of a card grid
//
// Running flow:
//   * clicking "Run scenario" opens a centered modal with a small ticker
//     ("Booting Sentinel…" then "Routing to incident timeline…") then
//     pushes the user to /console/incidents/{run_id}?live=1.
//   * "Coming soon" scenarios open a different modal explaining how the
//     scenario will become available (fixture + allowlist) plus a no-op
//     "Vote for this scenario" affordance.

import * as React from "react";
import { useRouter } from "next/navigation";
import type { Route } from "next";
import {
  AlertOctagon,
  Bug,
  Box,
  Container,
  Cpu,
  Database,
  Divide,
  DollarSign,
  Gauge,
  Image as ImageIcon,
  KeyRound,
  Layers,
  ListOrdered,
  Loader2,
  Lock,
  MemoryStick,
  PlayCircle,
  Plug,
  Radio,
  Rocket,
  Search,
  Shield,
  ShieldAlert,
  ShieldCheck,
  Sparkles,
  Timer,
  TrendingUp,
  Workflow,
  X,
  Zap,
  type LucideIcon,
} from "lucide-react";
import * as Dialog from "@radix-ui/react-dialog";

import { Button } from "@/components/ui/Button";
import { ProviderLogo } from "@/components/integrations/ProviderLogo";
import { pipelines } from "@/lib/pipelines";
import { cn } from "@/lib/utils";
import {
  SCENARIOS,
  scenariosBySector,
  type DemoScenario,
  type DemoScenarioSector,
  type DemoScenarioSeverity,
  type DemoScenarioSourceProvider,
} from "@/components/live-demo/SCENARIOS";

// ICON_MAP resolves a scenario.icon_hint string to a LucideIcon. Keeping
// this co-located with the page (not in SCENARIOS.ts) avoids pulling
// every icon into the catalog module — the page imports just the ones
// it actually renders.
const ICON_MAP: Record<string, LucideIcon> = {
  Bug,
  Zap,
  AlertOctagon,
  Divide,
  ListOrdered,
  Plug,
  Timer,
  Database,
  Layers,
  Lock,
  Rocket,
  MemoryStick,
  Image: ImageIcon,
  Shield,
  ShieldCheck,
  ShieldAlert,
  TrendingUp,
  Workflow,
  Cpu,
  Gauge,
  Box,
  Sparkles,
  Container,
  Radio,
  DollarSign,
  KeyRound,
};

const SECTOR_FILTERS: { id: DemoScenarioSector | "all"; label: string }[] = [
  { id: "all", label: "All" },
  { id: "application", label: "Application" },
  { id: "data", label: "Database & data" },
  { id: "deploy", label: "Deploy" },
  { id: "observability", label: "Observability" },
  { id: "security", label: "Security" },
];

const SEVERITY_PILL: Record<
  DemoScenarioSeverity,
  { bg: string; fg: string; ring: string; dot: string; label: string }
> = {
  low: {
    bg: "bg-emerald-500/15",
    fg: "text-emerald-700 dark:text-emerald-300",
    ring: "ring-emerald-500/30",
    dot: "bg-emerald-500",
    label: "Low",
  },
  medium: {
    bg: "bg-amber-500/15",
    fg: "text-amber-700 dark:text-amber-300",
    ring: "ring-amber-500/30",
    dot: "bg-amber-500",
    label: "Medium",
  },
  high: {
    bg: "bg-red-500/15",
    fg: "text-red-700 dark:text-red-300",
    ring: "ring-red-500/30",
    dot: "bg-red-500",
    label: "High",
  },
  critical: {
    bg: "bg-fuchsia-500/15",
    fg: "text-fuchsia-700 dark:text-fuchsia-300",
    ring: "ring-fuchsia-500/30",
    dot: "bg-fuchsia-500",
    label: "Critical",
  },
};

const AGENT_LABEL: Record<string, string> = {
  sentinel: "Sentinel",
  pathfinder: "Pathfinder",
  synthesiser: "Synthesiser",
  architect: "Architect",
  backend: "Backend",
  qa: "QA",
  devops: "DevOps",
  data_engineer: "Data Engineer",
  approval_gate: "Approval Gate",
};

function SeverityPill({ severity }: { severity: DemoScenarioSeverity }) {
  const p = SEVERITY_PILL[severity];
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-[10px] font-medium uppercase tracking-widest ring-1",
        p.bg,
        p.fg,
        p.ring,
      )}
      aria-label={`Severity ${p.label}`}
    >
      <span className={cn("inline-block h-1.5 w-1.5 rounded-full", p.dot)} aria-hidden />
      {p.label}
    </span>
  );
}

function SourceChip({ source }: { source: DemoScenarioSourceProvider }) {
  if (source === "synthetic") {
    return (
      <span className="inline-flex items-center gap-1 rounded-full bg-[var(--color-muted)]/60 px-2 py-0.5 text-[10px] font-medium uppercase tracking-widest text-[var(--color-muted-foreground)] ring-1 ring-[var(--color-border)]">
        Synthetic
      </span>
    );
  }
  return (
    <span className="inline-flex items-center gap-1.5 rounded-full bg-[var(--color-muted)]/40 px-2 py-0.5 text-[10px] font-medium uppercase tracking-widest text-[var(--color-foreground)] ring-1 ring-[var(--color-border)]">
      <ProviderLogo provider={source} className="inline-flex h-3 w-3" />
      {source}
    </span>
  );
}

function AgentChip({ role }: { role: string }) {
  return (
    <span className="inline-flex items-center rounded-md bg-[var(--color-muted)]/50 px-1.5 py-0.5 text-[10px] font-medium text-[var(--color-muted-foreground)] ring-1 ring-[var(--color-border)]">
      {AGENT_LABEL[role] ?? role}
    </span>
  );
}

function formatDurationEstimate(ms: number): string {
  const s = Math.round(ms / 1000);
  if (s < 60) return `~${s}s`;
  const m = Math.floor(s / 60);
  const r = s % 60;
  return r === 0 ? `~${m}m` : `~${m}m ${r}s`;
}

function ScenarioCard({
  scenario,
  onRun,
  onPreview,
  isPending,
  disabled,
}: {
  scenario: DemoScenario;
  onRun: (s: DemoScenario) => void;
  onPreview: (s: DemoScenario) => void;
  isPending: boolean;
  disabled: boolean;
}) {
  const Icon = ICON_MAP[scenario.icon_hint] ?? Bug;
  const ready = scenario.ready;
  return (
    <div
      className={cn(
        "flex flex-col rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-4 transition-colors",
        "hover:border-[var(--color-primary)]/60",
      )}
    >
      <div className="flex items-start gap-2.5">
        <span
          className={cn(
            "grid h-9 w-9 shrink-0 place-items-center rounded-md text-[var(--color-foreground)]",
            ready
              ? "bg-[var(--color-primary)]/10 ring-1 ring-[var(--color-primary)]/30"
              : "bg-[var(--color-muted)]/60",
          )}
        >
          <Icon className="h-4 w-4" />
        </span>
        <div className="min-w-0 flex-1">
          <h3 className="truncate text-sm font-semibold text-[var(--color-foreground)]">
            {scenario.title}
          </h3>
          <p className="mt-0.5 line-clamp-2 text-xs text-[var(--color-muted-foreground)]">
            {scenario.description}
          </p>
        </div>
      </div>

      <div className="mt-3 flex flex-wrap items-center gap-1.5">
        <SeverityPill severity={scenario.severity} />
        <SourceChip source={scenario.source_provider} />
        {!ready && (
          <span className="inline-flex items-center rounded-full bg-zinc-500/10 px-2 py-0.5 text-[10px] font-medium uppercase tracking-widest text-zinc-600 ring-1 ring-zinc-500/20 dark:text-zinc-400">
            Coming soon
          </span>
        )}
      </div>

      <div className="mt-3 flex flex-wrap gap-1">
        {scenario.expected_agents.map((role) => (
          <AgentChip key={role} role={role} />
        ))}
      </div>

      <div className="mt-3 flex items-center justify-between gap-2 border-t border-[var(--color-border)] pt-3">
        <span className="text-[11px] text-[var(--color-muted-foreground)]">
          {formatDurationEstimate(scenario.expected_duration_ms)} ·{" "}
          {scenario.expected_agents.length} agents
        </span>
        {ready ? (
          <Button
            size="sm"
            onClick={() => onRun(scenario)}
            disabled={disabled}
            aria-label={`Run ${scenario.title}`}
          >
            {isPending ? (
              <Loader2 className="h-4 w-4 animate-spin" />
            ) : (
              <PlayCircle className="h-4 w-4" />
            )}
            Run scenario
          </Button>
        ) : (
          <Button
            size="sm"
            variant="outline"
            onClick={() => onPreview(scenario)}
            aria-label={`Preview ${scenario.title}`}
          >
            Preview
          </Button>
        )}
      </div>
    </div>
  );
}

const TICKER_STEPS = [
  "Booting Sentinel + the L2 fleet…",
  "Injecting synthetic fixture…",
  "Routing to incident timeline…",
] as const;

function RunningModal({
  scenario,
  open,
  onCancel,
}: {
  scenario: DemoScenario | null;
  open: boolean;
  onCancel: () => void;
}) {
  // The "ticker" is a pseudo-progress affordance while the demo POST is in
  // flight. It rotates every ~800ms; the redirect happens as soon as the
  // POST resolves, so the ticker rarely reaches the third step. The point
  // is to confirm the click landed — full real-time feedback is the
  // incident timeline the user is about to be redirected to.
  const [step, setStep] = React.useState(0);
  React.useEffect(() => {
    if (!open) {
      setStep(0);
      return;
    }
    const id = window.setInterval(() => {
      setStep((s) => Math.min(s + 1, TICKER_STEPS.length - 1));
    }, 800);
    return () => window.clearInterval(id);
  }, [open]);

  return (
    <Dialog.Root open={open} onOpenChange={(v) => !v && onCancel()}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-40 bg-black/40 backdrop-blur-sm data-[state=open]:animate-in data-[state=open]:fade-in" />
        <Dialog.Content className="fixed left-1/2 top-1/2 z-50 w-[min(440px,calc(100vw-2rem))] -translate-x-1/2 -translate-y-1/2 rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-6 shadow-xl outline-none data-[state=open]:animate-in data-[state=open]:fade-in data-[state=open]:zoom-in-95">
          <Dialog.Title className="flex items-center gap-2 text-base font-semibold text-[var(--color-foreground)]">
            <Loader2 className="h-4 w-4 animate-spin text-[var(--color-primary)]" />
            Running scenario
          </Dialog.Title>
          <Dialog.Description className="mt-1 text-xs text-[var(--color-muted-foreground)]">
            {scenario?.title ?? "Scenario"}
          </Dialog.Description>

          <ol className="mt-5 space-y-2">
            {TICKER_STEPS.map((label, i) => {
              const active = i === step;
              const done = i < step;
              return (
                <li
                  key={label}
                  className="flex items-center gap-2 text-xs text-[var(--color-muted-foreground)]"
                >
                  <span
                    aria-hidden
                    className={cn(
                      "inline-block h-1.5 w-1.5 rounded-full",
                      done
                        ? "bg-emerald-500"
                        : active
                          ? "animate-pulse bg-[var(--color-primary)]"
                          : "bg-zinc-400/50",
                    )}
                  />
                  <span
                    className={cn(
                      active || done
                        ? "text-[var(--color-foreground)]"
                        : "text-[var(--color-muted-foreground)]",
                    )}
                  >
                    {label}
                  </span>
                </li>
              );
            })}
          </ol>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

function ComingSoonModal({
  scenario,
  open,
  onClose,
}: {
  scenario: DemoScenario | null;
  open: boolean;
  onClose: () => void;
}) {
  if (!scenario) return null;
  const fixturePath = `services/control-plane/fixtures/scenarios/${scenario.id}.json`;
  return (
    <Dialog.Root open={open} onOpenChange={(v) => !v && onClose()}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-40 bg-black/40 backdrop-blur-sm data-[state=open]:animate-in data-[state=open]:fade-in" />
        <Dialog.Content className="fixed left-1/2 top-1/2 z-50 w-[min(520px,calc(100vw-2rem))] -translate-x-1/2 -translate-y-1/2 rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-6 shadow-xl outline-none data-[state=open]:animate-in data-[state=open]:fade-in data-[state=open]:zoom-in-95">
          <div className="flex items-start justify-between gap-2">
            <div>
              <Dialog.Title className="text-base font-semibold text-[var(--color-foreground)]">
                {scenario.title}
              </Dialog.Title>
              <Dialog.Description className="mt-1 text-xs text-[var(--color-muted-foreground)]">
                Backend support coming soon.
              </Dialog.Description>
            </div>
            <Dialog.Close asChild>
              <button
                type="button"
                aria-label="Close"
                className="inline-flex h-7 w-7 items-center justify-center rounded-md text-[var(--color-muted-foreground)] hover:bg-[var(--color-muted)] hover:text-[var(--color-foreground)]"
              >
                <X className="h-4 w-4" />
              </button>
            </Dialog.Close>
          </div>

          <div className="mt-4 space-y-3 text-sm text-[var(--color-foreground)]">
            <p className="text-[var(--color-muted-foreground)]">{scenario.details}</p>
            <div className="rounded-md border border-[var(--color-border)] bg-[var(--color-muted)]/30 p-3 text-xs text-[var(--color-muted-foreground)]">
              <p className="font-medium text-[var(--color-foreground)]">
                What needs to happen for this scenario to go live:
              </p>
              <ol className="mt-2 list-decimal space-y-1 pl-4">
                <li>
                  Drop a fixture at{" "}
                  <code className="font-mono text-[10px]">{fixturePath}</code>.
                </li>
                <li>
                  Add <code className="font-mono text-[10px]">{scenario.id}</code> to the{" "}
                  <code className="font-mono text-[10px]">demoScenarios</code> allowlist in{" "}
                  <code className="font-mono text-[10px]">pipelines.go</code>.
                </li>
                <li>
                  Flip <code className="font-mono text-[10px]">ready</code> to{" "}
                  <code className="font-mono text-[10px]">true</code> in the catalog.
                </li>
              </ol>
            </div>
          </div>

          <div className="mt-5 flex items-center justify-end gap-2">
            <Button variant="outline" size="sm" onClick={onClose}>
              Close
            </Button>
            <Button
              size="sm"
              onClick={onClose}
              aria-label={`Vote for ${scenario.title}`}
            >
              Vote for this scenario
            </Button>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

export function LiveDemoClient({ workspaceId }: { workspaceId: string }) {
  const router = useRouter();
  const [filter, setFilter] = React.useState<DemoScenarioSector | "all">("all");
  const [query, setQuery] = React.useState("");
  const [running, setRunning] = React.useState<DemoScenario | null>(null);
  const [preview, setPreview] = React.useState<DemoScenario | null>(null);
  const [error, setError] = React.useState<string | null>(null);

  const totalCount = SCENARIOS.length;
  const readyCount = SCENARIOS.filter((s) => s.ready).length;

  // groups is the sector-ordered list, then sector-filter + search-filter
  // applied on top. We drop empty sectors after filtering so the page
  // doesn't render orphan section headers.
  const groups = React.useMemo(() => {
    const all = scenariosBySector();
    const q = query.trim().toLowerCase();
    return all
      .map((g) => ({
        ...g,
        items: g.items.filter((s) => {
          if (filter !== "all" && s.sector !== filter) return false;
          if (!q) return true;
          return (
            s.title.toLowerCase().includes(q) ||
            s.description.toLowerCase().includes(q) ||
            s.id.toLowerCase().includes(q)
          );
        }),
      }))
      .filter((g) => g.items.length > 0);
  }, [filter, query]);

  async function runScenario(scenario: DemoScenario) {
    if (!workspaceId || !scenario.ready) return;
    setRunning(scenario);
    setError(null);
    try {
      const run = await pipelines.createScenario(workspaceId, {
        scenario: scenario.id,
        severity: scenario.severity,
        source: scenario.source_provider,
      });
      router.push(`/console/incidents/${run.id}?live=1` as Route);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to run scenario");
      setRunning(null);
    }
  }

  return (
    <div className="space-y-6">
      <header>
        <h1 className="text-2xl font-semibold text-[var(--color-foreground)]">
          Live Demo
        </h1>
        <p className="mt-1 max-w-2xl text-sm text-[var(--color-muted-foreground)]">
          Pick any scenario to fire it against the synthetic fixture. The full L1+L2 fleet
          runs end-to-end — Sentinel detects, Pathfinder diagnoses, the L1 agents code +
          test, and the approval gate routes by severity.
        </p>
        <p className="mt-2 text-xs text-[var(--color-muted-foreground)]">
          <span className="font-medium text-[var(--color-foreground)]">{readyCount}</span> of{" "}
          {totalCount} scenarios wired today. The rest ship as fixtures land in the control
          plane.
        </p>
      </header>

      {error && (
        <div
          role="alert"
          className="rounded-md border border-red-500/30 bg-red-500/10 px-4 py-3 text-sm text-red-700 dark:text-red-300"
        >
          {error}
        </div>
      )}

      {!workspaceId && (
        <div
          role="status"
          className="rounded-md border border-amber-500/30 bg-amber-500/10 px-4 py-3 text-sm text-amber-700 dark:text-amber-300"
        >
          No workspace selected. Pick one in the sidebar to run a demo.
        </div>
      )}

      {/* Filter row + search */}
      <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
        <div
          role="tablist"
          aria-label="Filter scenarios by sector"
          className="flex flex-wrap gap-1.5"
        >
          {SECTOR_FILTERS.map((f) => {
            const active = filter === f.id;
            return (
              <button
                key={f.id}
                type="button"
                role="tab"
                aria-selected={active}
                onClick={() => setFilter(f.id)}
                className={cn(
                  "inline-flex items-center rounded-full px-3 py-1 text-xs font-medium ring-1 transition-colors",
                  active
                    ? "bg-[var(--color-primary)]/15 text-[var(--color-primary)] ring-[var(--color-primary)]/30"
                    : "bg-[var(--color-card)] text-[var(--color-muted-foreground)] ring-[var(--color-border)] hover:text-[var(--color-foreground)]",
                )}
              >
                {f.label}
              </button>
            );
          })}
        </div>

        <label className="relative inline-flex items-center lg:w-64">
          <Search
            className="pointer-events-none absolute left-3 h-4 w-4 text-[var(--color-muted-foreground)]"
            aria-hidden
          />
          <input
            type="search"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search scenarios"
            aria-label="Search scenarios"
            className="w-full rounded-md border border-[var(--color-border)] bg-[var(--color-background)] py-2 pl-9 pr-3 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-ring)]"
          />
        </label>
      </div>

      {/* Sections */}
      {groups.length === 0 ? (
        <div className="rounded-lg border border-dashed border-[var(--color-border)] bg-[var(--color-card)] p-8 text-center text-sm text-[var(--color-muted-foreground)]">
          No scenarios match your filters. Try a different sector or clear the search.
        </div>
      ) : (
        <div className="space-y-8">
          {groups.map((g) => (
            <section key={g.sector} aria-label={g.meta.label}>
              <header className="mb-3">
                <h2 className="text-[10px] font-semibold uppercase tracking-widest text-[var(--color-muted-foreground)]">
                  {g.meta.label}
                </h2>
                <p className="mt-0.5 text-xs text-[var(--color-muted-foreground)]">
                  {g.meta.intro}
                </p>
              </header>
              <div className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3">
                {g.items.map((s) => (
                  <ScenarioCard
                    key={s.id}
                    scenario={s}
                    onRun={runScenario}
                    onPreview={setPreview}
                    isPending={running?.id === s.id}
                    disabled={running !== null || !workspaceId}
                  />
                ))}
              </div>
            </section>
          ))}
        </div>
      )}

      <RunningModal
        scenario={running}
        open={running !== null}
        onCancel={() => setRunning(null)}
      />
      <ComingSoonModal
        scenario={preview}
        open={preview !== null}
        onClose={() => setPreview(null)}
      />
    </div>
  );
}
