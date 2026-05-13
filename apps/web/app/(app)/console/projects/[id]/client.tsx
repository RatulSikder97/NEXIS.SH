"use client";

// Phase 3.5 — Project detail client.
//
// Four tabs driven by local state (no nested routes):
//
//   * Overview      — KPI strip + recent incidents + recovery pipeline mini
//   * Integrations  — per-provider mapping rows (icon + connected/missing)
//   * Recovery Policy — read-only summary of the 7-field policy + edit CTA
//   * Activity      — project-scoped operational segments
//
// When the BE projects endpoint is offline (`backendMissing` prop), we still
// render the surface but each tab shows an EmptyState "Endpoint coming soon"
// rather than crashing. This keeps the FE→BE rollout decoupled.
//
// The tab pattern is a simple `useState<TabId>` + pill bar at the top; we
// don't use the URL hash because the four-tab surface fits comfortably in
// a single React tree and SSR doesn't need to deep-link tabs.

import * as React from "react";
import Link from "next/link";
import type { Route } from "next";
import {
  Activity,
  AlertTriangle,
  CheckCircle2,
  Edit3,
  ExternalLink,
  Gauge,
  Inbox,
  Pencil,
  Plug,
  ShieldCheck,
  Sparkles,
  TimerReset,
  Unplug,
  Wallet,
  XCircle,
  Zap,
} from "lucide-react";

import { Button } from "@/components/ui/Button";
import { EmptyState } from "@/components/empty-state/EmptyState";
import { EnvironmentChip } from "@/components/projects/EnvironmentChip";
import {
  PROJECT_INTEGRATION_PROVIDERS,
  ProjectIntegrationIcons,
} from "@/components/projects/ProjectIntegrationIcons";
import { RecoveryPipelineMini } from "@/components/console/RecoveryPipelineMini";
import {
  OperationalSegments,
  type OperationalSegment,
} from "@/components/console/OperationalSegments";
import { cn } from "@/lib/utils";
import {
  connectedProviders,
  type ConnectedIntegrationProvider,
  type Project,
} from "@/lib/projects";
import type { WorkflowRun } from "@/lib/pipelines";

type TabId = "overview" | "integrations" | "policy" | "activity";

const TABS: Array<{ id: TabId; label: string }> = [
  { id: "overview", label: "Overview" },
  { id: "integrations", label: "Integrations" },
  { id: "policy", label: "Recovery Policy" },
  { id: "activity", label: "Activity" },
];

function formatRelative(iso: string, now: number): string {
  const t = Date.parse(iso);
  if (!Number.isFinite(t)) return iso;
  const diff = Math.max(0, now - t);
  const s = Math.round(diff / 1000);
  if (s < 60) return `${s}s ago`;
  const m = Math.round(s / 60);
  if (m < 60) return `${m}m ago`;
  const h = Math.round(m / 60);
  if (h < 24) return `${h}h ago`;
  const d = Math.round(h / 24);
  return `${d}d ago`;
}

function statusPill(status: WorkflowRun["status"]) {
  switch (status) {
    case "running":
    case "queued":
      return {
        className:
          "bg-blue-500/15 text-blue-700 ring-blue-500/30 dark:text-blue-300",
        label: status,
      };
    case "succeeded":
      return {
        className:
          "bg-emerald-500/15 text-emerald-700 ring-emerald-500/30 dark:text-emerald-300",
        label: status,
      };
    case "failed":
    case "timed_out":
    case "cancelled":
      return {
        className:
          "bg-red-500/15 text-red-700 ring-red-500/30 dark:text-red-300",
        label: status,
      };
  }
}

// SelectorValue — shows the human-readable value for the connected piece of
// a selector. Returns null when the provider has no selector configured so
// the row can render "Not configured".
function selectorValue(
  project: Project,
  provider: ConnectedIntegrationProvider,
): string | null {
  const sel = project.selectors;
  switch (provider) {
    case "github":
      return sel.github_repo
        ? sel.github_default_branch
          ? `${sel.github_repo} · ${sel.github_default_branch}`
          : sel.github_repo
        : null;
    case "sentry":
      if (sel.sentry_organization_slug && sel.sentry_project_slug) {
        return `${sel.sentry_organization_slug}/${sel.sentry_project_slug}`;
      }
      return sel.sentry_project_slug ?? sel.sentry_organization_slug ?? null;
    case "argocd":
      if (sel.argocd_app_name && sel.argocd_project) {
        return `${sel.argocd_project}/${sel.argocd_app_name}`;
      }
      return sel.argocd_app_name ?? sel.argocd_server_url ?? null;
    case "slack":
      return sel.slack_channel_id ?? null;
    case "datadog":
      if (sel.datadog_service_tag && sel.datadog_env_tag) {
        return `${sel.datadog_service_tag} · ${sel.datadog_env_tag}`;
      }
      return sel.datadog_service_tag ?? sel.datadog_env_tag ?? null;
    case "pagerduty":
      return (
        sel.pagerduty_service_id ?? sel.pagerduty_escalation_policy_id ?? null
      );
  }
}

function KpiBlock({
  label,
  value,
  hint,
  icon: Icon,
}: {
  label: string;
  value: string;
  hint?: string;
  icon: React.ComponentType<{ className?: string }>;
}) {
  return (
    <div className="rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-4">
      <div className="flex items-center justify-between">
        <p className="text-[10px] font-semibold uppercase tracking-widest text-[var(--color-muted-foreground)]">
          {label}
        </p>
        <Icon className="h-4 w-4 text-[var(--color-muted-foreground)]" />
      </div>
      <p className="mt-2 text-2xl font-semibold text-[var(--color-foreground)]">
        {value}
      </p>
      {hint && (
        <p className="mt-1 text-xs text-[var(--color-muted-foreground)]">
          {hint}
        </p>
      )}
    </div>
  );
}

function OverviewTab({
  project,
  incidents,
  backendMissing,
}: {
  project: Project;
  incidents: WorkflowRun[];
  backendMissing: boolean;
}) {
  // project is currently rendered by the parent header; we keep the prop on
  // the signature so future detail rows (per-project SLO panels, owner card)
  // can read it without changing the call site.
  void project;
  const [now, setNow] = React.useState<number>(() => Date.now());
  React.useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), 5_000);
    return () => window.clearInterval(id);
  }, []);

  const open = incidents.filter(
    (r) => r.status === "running" || r.status === "queued",
  ).length;
  const succeeded = incidents.filter((r) => r.status === "succeeded").length;
  const successRate = incidents.length
    ? Math.round((succeeded / incidents.length) * 100)
    : -1;
  const active = open > 0;

  return (
    <div className="space-y-6">
      <section aria-labelledby="project-kpis">
        <h2 id="project-kpis" className="sr-only">
          Project KPIs
        </h2>
        <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
          <KpiBlock
            label="7d MTTR"
            value="—"
            hint="No data yet"
            icon={TimerReset}
          />
          <KpiBlock
            label="Recovery success"
            value={successRate >= 0 ? `${successRate}%` : "—"}
            hint={
              incidents.length
                ? `${succeeded} of ${incidents.length} runs`
                : "No runs yet"
            }
            icon={ShieldCheck}
          />
          <KpiBlock
            label="Open incidents"
            value={String(open)}
            hint={open > 0 ? "Active runs" : "All clear"}
            icon={AlertTriangle}
          />
          <KpiBlock
            label="Mean tokens / run"
            value="—"
            hint="No data yet"
            icon={Wallet}
          />
        </div>
      </section>

      {active && (
        <section>
          <h2 className="mb-3 text-sm font-semibold text-[var(--color-foreground)]">
            Active recovery
          </h2>
          <RecoveryPipelineMini />
        </section>
      )}

      <section
        aria-labelledby="recent-incidents"
        className="rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-5"
      >
        <header className="mb-4 flex items-center justify-between">
          <h2
            id="recent-incidents"
            className="text-sm font-semibold text-[var(--color-foreground)]"
          >
            Recent incidents
          </h2>
          <Button asChild variant="ghost" size="sm">
            <Link href={"/console/incidents" as Route}>
              View all
              <ExternalLink className="h-3.5 w-3.5" />
            </Link>
          </Button>
        </header>
        {backendMissing ? (
          <EmptyState
            icon={Inbox}
            title="Endpoint coming soon"
            description="The control-plane projects API is rolling out. Incidents will appear here once the backend ships."
          />
        ) : incidents.length === 0 ? (
          <EmptyState
            icon={Inbox}
            title="No incidents yet"
            description="When this project receives an alert, the recovery pipeline run will show up here."
          />
        ) : (
          <div className="overflow-hidden rounded-md border border-[var(--color-border)]">
            <table className="w-full text-sm">
              <thead className="bg-[var(--color-muted)]/40 text-left text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]">
                <tr>
                  <th className="px-4 py-2 font-medium">When</th>
                  <th className="px-4 py-2 font-medium">Type</th>
                  <th className="px-4 py-2 font-medium">Status</th>
                  <th className="px-4 py-2 font-medium">Step</th>
                  <th className="px-4 py-2 font-medium" aria-hidden />
                </tr>
              </thead>
              <tbody>
                {incidents.slice(0, 10).map((r) => {
                  const pill = statusPill(r.status);
                  return (
                    <tr
                      key={r.id}
                      className="border-t border-[var(--color-border)]"
                    >
                      <td className="px-4 py-3 text-[var(--color-muted-foreground)]">
                        {formatRelative(r.started_at, now)}
                      </td>
                      <td className="px-4 py-3 font-mono text-xs">
                        {r.workflow_type}
                      </td>
                      <td className="px-4 py-3">
                        <span
                          className={cn(
                            "inline-flex items-center rounded-full px-2 py-0.5 text-[10px] font-medium uppercase tracking-widest ring-1",
                            pill.className,
                          )}
                        >
                          {pill.label}
                        </span>
                      </td>
                      <td className="px-4 py-3 text-[var(--color-muted-foreground)]">
                        {r.current_step ?? "—"}
                      </td>
                      <td className="px-4 py-3 text-right">
                        <Button asChild variant="outline" size="sm">
                          <Link
                            href={
                              `/console/incidents/${r.id}` as Route
                            }
                          >
                            Open
                          </Link>
                        </Button>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </div>
  );
}

function IntegrationsTab({
  project,
  backendMissing,
}: {
  project: Project;
  backendMissing: boolean;
}) {
  const connected = connectedProviders(project);
  if (backendMissing) {
    return (
      <EmptyState
        icon={Plug}
        title="Endpoint coming soon"
        description="Integration mappings will land here once the control-plane projects API ships."
      />
    );
  }
  return (
    <section
      aria-labelledby="integrations-heading"
      className="rounded-lg border border-[var(--color-border)] bg-[var(--color-card)]"
    >
      <header className="border-b border-[var(--color-border)] p-5">
        <h2
          id="integrations-heading"
          className="text-sm font-semibold text-[var(--color-foreground)]"
        >
          Provider mappings
        </h2>
        <p className="mt-0.5 text-xs text-[var(--color-muted-foreground)]">
          Each provider can be wired or disconnected independently. Use the
          Connect project wizard to add a missing mapping.
        </p>
      </header>
      <ul className="divide-y divide-[var(--color-border)]">
        {PROJECT_INTEGRATION_PROVIDERS.map((p) => {
          const Icon = p.icon;
          const isConnected = connected.has(p.provider);
          const value = selectorValue(project, p.provider);
          return (
            <li
              key={p.provider}
              className="flex items-center justify-between gap-3 p-4"
            >
              <div className="flex items-center gap-3">
                <span
                  className={cn(
                    "inline-flex h-9 w-9 items-center justify-center rounded-md",
                    isConnected
                      ? "bg-[var(--color-muted)]/60 text-[var(--color-foreground)]"
                      : "bg-[var(--color-muted)]/40 text-[var(--color-muted-foreground)]/60",
                  )}
                >
                  <Icon className="h-4 w-4" aria-hidden />
                </span>
                <div>
                  <p className="text-sm font-medium text-[var(--color-foreground)]">
                    {p.label}
                  </p>
                  <p className="font-mono text-xs text-[var(--color-muted-foreground)]">
                    {value ?? "Not configured"}
                  </p>
                </div>
              </div>
              <div className="flex items-center gap-2">
                <span
                  className={cn(
                    "inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[10px] font-medium uppercase tracking-widest ring-1",
                    isConnected
                      ? "bg-emerald-500/10 text-emerald-700 ring-emerald-500/30 dark:text-emerald-300"
                      : "bg-[var(--color-muted)] text-[var(--color-muted-foreground)] ring-[var(--color-border)]",
                  )}
                >
                  {isConnected ? (
                    <>
                      <CheckCircle2 className="h-3 w-3" aria-hidden />
                      Connected
                    </>
                  ) : (
                    <>
                      <XCircle className="h-3 w-3" aria-hidden />
                      Not configured
                    </>
                  )}
                </span>
                {isConnected ? (
                  <Button asChild variant="outline" size="sm">
                    <Link
                      href={
                        `/console/projects/${project.id}/settings` as Route
                      }
                    >
                      <Pencil className="h-3.5 w-3.5" />
                      Edit
                    </Link>
                  </Button>
                ) : (
                  <Button asChild variant="outline" size="sm">
                    <Link
                      href={
                        `/console/projects/${project.id}/settings` as Route
                      }
                    >
                      <Plug className="h-3.5 w-3.5" />
                      Connect
                    </Link>
                  </Button>
                )}
                {isConnected && (
                  <Button asChild variant="ghost" size="sm">
                    <Link
                      href={
                        `/console/projects/${project.id}/settings` as Route
                      }
                    >
                      <Unplug className="h-3.5 w-3.5" />
                      Disconnect
                    </Link>
                  </Button>
                )}
              </div>
            </li>
          );
        })}
      </ul>
    </section>
  );
}

function PolicySummaryCard({
  label,
  value,
  description,
}: {
  label: string;
  value: string;
  description?: string;
}) {
  return (
    <div className="rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-4">
      <p className="text-[10px] font-semibold uppercase tracking-widest text-[var(--color-muted-foreground)]">
        {label}
      </p>
      <p className="mt-2 text-base font-semibold text-[var(--color-foreground)]">
        {value}
      </p>
      {description && (
        <p className="mt-1 text-xs text-[var(--color-muted-foreground)]">
          {description}
        </p>
      )}
    </div>
  );
}

function PolicyTab({
  project,
  backendMissing,
}: {
  project: Project;
  backendMissing: boolean;
}) {
  if (backendMissing) {
    return (
      <EmptyState
        icon={ShieldCheck}
        title="Endpoint coming soon"
        description="The recovery-policy API is rolling out. Once the backend ships, the current policy will render here."
      />
    );
  }
  const p = project.recovery_policy;
  return (
    <div className="space-y-6">
      {p.kill_switch_enabled && (
        <div
          role="alert"
          className="flex items-start gap-3 rounded-md border border-red-500/30 bg-red-500/10 p-4 text-sm text-red-700 dark:text-red-300"
        >
          <Zap className="mt-0.5 h-4 w-4 shrink-0" />
          <div>
            <p className="font-semibold">Kill switch is ENGAGED</p>
            <p className="text-xs">
              Auto-recovery is disabled for this project. New incidents will
              still be detected and surfaced, but no PRs or deploys will run
              until the kill switch is cleared.
            </p>
          </div>
        </div>
      )}

      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-sm font-semibold text-[var(--color-foreground)]">
            Current policy
          </h2>
          <p className="mt-0.5 text-xs text-[var(--color-muted-foreground)]">
            Read-only summary. Use the editor for changes.
          </p>
        </div>
        <Button asChild>
          <Link href={`/console/projects/${project.id}/settings` as Route}>
            <Edit3 className="h-4 w-4" />
            Edit policy
          </Link>
        </Button>
      </div>

      <div className="grid grid-cols-1 gap-3 md:grid-cols-2 lg:grid-cols-3">
        <PolicySummaryCard
          label="Auto-merge low severity"
          value={p.auto_merge_low_severity ? "Enabled" : "Disabled"}
          description="Low-risk recoveries land without operator review."
        />
        <PolicySummaryCard
          label="Auto-merge medium severity"
          value={p.auto_merge_medium_severity ? "Enabled" : "Disabled"}
          description={
            p.auto_merge_medium_severity
              ? `After ${p.medium_countdown_seconds}s countdown`
              : "Medium incidents wait for explicit approval."
          }
        />
        <PolicySummaryCard
          label="Medium countdown"
          value={`${p.medium_countdown_seconds}s`}
          description="Cooldown before auto-merge fires on medium severity."
        />
        <PolicySummaryCard
          label="Max concurrent recoveries"
          value={String(p.max_concurrent_recoveries)}
          description="Upper bound for in-flight pipelines on this project."
        />
        <PolicySummaryCard
          label="Rollback on SLO breach"
          value={p.rollback_on_slo_breach ? "Enabled" : "Disabled"}
          description="Auto-revert deploys when the post-merge SLO probe fails."
        />
        <PolicySummaryCard
          label="Approvers"
          value={
            p.approver_user_ids.length === 0
              ? "Org default"
              : `${p.approver_user_ids.length} configured`
          }
          description={
            p.approver_user_ids.length > 0
              ? p.approver_user_ids
                  .map((id) => id.slice(0, 8))
                  .join(", ")
              : "Falls back to the org-level approval routing."
          }
        />
      </div>
    </div>
  );
}

function ActivityTab({
  project,
  incidents,
  backendMissing,
}: {
  project: Project;
  incidents: WorkflowRun[];
  backendMissing: boolean;
}) {
  // Project-scoped activity stream is sourced from the recent incidents
  // list — we surface each run as one segment so the same OperationalSegments
  // pattern used elsewhere in the console renders without a project-specific
  // events SDK.
  const segments: OperationalSegment[] = React.useMemo(() => {
    return incidents.slice(0, 20).map((r) => {
      const status: OperationalSegment["status"] =
        r.status === "running" || r.status === "queued"
          ? "running"
          : r.status === "succeeded"
            ? "succeeded"
            : r.status === "failed" ||
                r.status === "timed_out" ||
                r.status === "cancelled"
              ? "failed"
              : "pending";
      const finished = r.completed_at;
      return {
        label: `${r.workflow_type} — ${r.id.slice(0, 8)}`,
        started_at: r.started_at,
        finished_at: finished,
        status,
        detail: r.current_step
          ? `Current step: ${r.current_step}`
          : r.error,
        duration_ms: r.duration_ms,
      };
    });
  }, [incidents]);
  void project;
  if (backendMissing) {
    return (
      <EmptyState
        icon={Activity}
        title="Endpoint coming soon"
        description="Activity stream will be wired up once the projects API ships."
      />
    );
  }
  if (segments.length === 0) {
    return (
      <EmptyState
        icon={Sparkles}
        title="Nothing has happened yet"
        description="Activity for this project will appear here as agents fire."
      />
    );
  }
  return (
    <OperationalSegments
      segments={segments}
      title="Project activity"
      description="One segment per pipeline run, newest first."
    />
  );
}

export function ProjectDetailClient({
  project,
  backendMissing,
  workspaceId,
  initialIncidents,
}: {
  project: Project;
  backendMissing: boolean;
  workspaceId: string;
  initialIncidents: WorkflowRun[];
}) {
  const [tab, setTab] = React.useState<TabId>("overview");
  void workspaceId;
  const connected = connectedProviders(project);

  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-3">
        <div className="flex items-start justify-between gap-4">
          <div>
            <p className="text-[10px] font-semibold uppercase tracking-widest text-[var(--color-muted-foreground)]">
              Project
            </p>
            <div className="mt-1 flex items-center gap-3">
              <h1 className="text-2xl font-semibold text-[var(--color-foreground)]">
                {project.name}
              </h1>
              <EnvironmentChip env={project.environment} size="sm" />
            </div>
            {project.description && (
              <p className="mt-1 max-w-2xl text-sm text-[var(--color-muted-foreground)]">
                {project.description}
              </p>
            )}
          </div>
          <div className="flex items-center gap-2">
            <ProjectIntegrationIcons connected={connected} size="md" />
            <Button asChild variant="outline" size="sm">
              <Link href={`/console/projects/${project.id}/settings` as Route}>
                <Gauge className="h-4 w-4" />
                Settings
              </Link>
            </Button>
          </div>
        </div>
      </div>

      {backendMissing && (
        <div
          role="status"
          className="rounded-md border border-amber-500/30 bg-amber-500/10 px-4 py-3 text-sm text-amber-700 dark:text-amber-300"
        >
          The projects API is still rolling out. Tabs may show placeholder
          content until the backend lands.
        </div>
      )}

      <div className="flex items-center gap-1 rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-1">
        {TABS.map((t) => (
          <button
            key={t.id}
            type="button"
            onClick={() => setTab(t.id)}
            aria-current={tab === t.id ? "page" : undefined}
            className={cn(
              "flex-1 rounded-md px-3 py-2 text-sm font-medium transition-colors",
              tab === t.id
                ? "bg-[var(--color-muted)] text-[var(--color-foreground)]"
                : "text-[var(--color-muted-foreground)] hover:bg-[var(--color-muted)]/60 hover:text-[var(--color-foreground)]",
            )}
          >
            {t.label}
          </button>
        ))}
      </div>

      {tab === "overview" && (
        <OverviewTab
          project={project}
          incidents={initialIncidents}
          backendMissing={backendMissing}
        />
      )}
      {tab === "integrations" && (
        <IntegrationsTab project={project} backendMissing={backendMissing} />
      )}
      {tab === "policy" && (
        <PolicyTab project={project} backendMissing={backendMissing} />
      )}
      {tab === "activity" && (
        <ActivityTab
          project={project}
          incidents={initialIncidents}
          backendMissing={backendMissing}
        />
      )}
    </div>
  );
}
