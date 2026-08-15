"use client";

// Phase 3 Stage 7 + Real-integrations Wave 1 — Integrations surface (client).
//
// Renders the 3×2 card grid and dispatches the generic ConfigureDialog when
// a card's Configure CTA fires. Each card now shows a live HealthPill (Task
// 1 of the real-integrations plan) instead of the legacy status badge.
//
// Wave 1 layout decisions:
//   * The Configure dialog is now manifest-driven (ConfigureDialog +
//     INTEGRATION_MANIFESTS) — the three legacy per-provider forms
//     (GitHubConfigureForm/SentryConfigureForm/ArgoCDConfigureForm) are no
//     longer mounted from here, but kept on disk pending Wave 2 cleanup.
//   * Slack flips from `comingSoon: true` to live. Datadog and PagerDuty
//     stay disabled until their backend adapters land in Wave 2.
//   * Health is derived per-row with a fallback when the backend hasn't
//     started returning the new `health` block yet (Wave 1 ships FE-first).
//
// The `?installed=github` query (set by the mock-install 302) is detected
// once on mount and shown as a dismissible banner. We strip the query so a
// reload doesn't replay the notice.

import * as React from "react";
import { AlertTriangle, CheckCircle2, X } from "lucide-react";
import { useRouter, useSearchParams } from "next/navigation";

import { IntegrationCard } from "@/components/integrations/IntegrationCard";
import { ConfigureDialog } from "@/components/integrations/ConfigureDialog";
import { HealthPill } from "@/components/integrations/HealthPill";
import { type ProviderID } from "@/components/integrations/ProviderLogo";
import { INTEGRATION_MANIFESTS } from "@/lib/integrations-config";
import type {
  Integration,
  IntegrationHealth,
  IntegrationProvider,
} from "@/lib/integrations";
import { cn } from "@/lib/utils";

// Failure codes emitted by the control-plane's OAuth install callbacks
// (handler/integration_oauth.go). Each one gets an actionable sentence — the
// operator has just been bounced back from a provider and needs to know
// whether to retry, re-check the App config, or fix credentials.
function installErrorMessage(provider: string, code: string): string {
  const name = provider.charAt(0).toUpperCase() + provider.slice(1);
  switch (code) {
    case "unauthorized":
      return `${name} sent you back but your session had expired. Sign in and start the install again from this page.`;
    case "state_missing":
      return `${name} install could not be verified because it did not start here. Click Configure on the ${name} card and follow the flow from the console.`;
    case "state_mismatch":
      return `${name} install was refused: the security token did not match. Start the install again from this page, in this browser.`;
    case "missing_installation_id":
      return `${name} did not return an installation id — the install was cancelled or the App's Setup URL is misconfigured.`;
    case "provider_unavailable":
      return `The ${name} provider is not configured on this deployment. Check the server's ${provider.toUpperCase()} settings.`;
    case "connect_failed":
      return `${name} returned the install, but validating it failed. Check that the App credentials on the server match this installation.`;
    default:
      return `${name} install failed (${code}).`;
  }
}

type CardSpec = {
  // Provider widened to the full ProviderID set (includes roadmap providers
  // like databricks/spark/vault that don't have backend rows yet).
  provider: ProviderID;
  name: string;
  description: string;
  available: boolean;
  category:
    | "source"
    | "observability"
    | "incident"
    | "deploy"
    | "chat"
    | "data"
    | "security";
};

const CARDS: CardSpec[] = [
  // ---- Incident intake ----
  // First card on the page on purpose: it is the only incident source that
  // connects without a vendor account, so on a fresh deployment it is the
  // one that makes Sentinel start detecting.
  {
    provider: "webhook",
    name: "Incident intake",
    description:
      "Signed webhook any service can post its own faults to. No vendor account needed.",
    available: true,
    category: "observability",
  },
  // ---- Source control ----
  {
    provider: "github",
    name: "GitHub",
    description: "PR creation + repo metadata for code changes.",
    available: true,
    category: "source",
  },
  {
    provider: "gitlab",
    name: "GitLab",
    description: "Merge requests + repo metadata.",
    available: false,
    category: "source",
  },
  {
    provider: "bitbucket",
    name: "Bitbucket",
    description: "Pull requests + branch operations.",
    available: false,
    category: "source",
  },

  // ---- Observability ----
  {
    provider: "sentry",
    name: "Sentry",
    description: "Error tracking + issue webhooks.",
    available: true,
    category: "observability",
  },
  {
    provider: "datadog",
    name: "Datadog",
    description: "Metrics, APM, and anomaly detection.",
    available: true,
    category: "observability",
  },
  {
    provider: "newrelic",
    name: "New Relic",
    description: "Full-stack observability and APM.",
    available: false,
    category: "observability",
  },
  {
    provider: "grafana_cloud",
    name: "Grafana Cloud",
    description: "Hosted Grafana + Mimir/Loki/Tempo.",
    available: false,
    category: "observability",
  },
  {
    provider: "prometheus",
    name: "Prometheus",
    description: "Metrics + Alertmanager webhook ingestion.",
    available: false,
    category: "observability",
  },
  {
    provider: "honeycomb",
    name: "Honeycomb",
    description: "Structured-event observability + BubbleUp.",
    available: false,
    category: "observability",
  },
  {
    provider: "splunk",
    name: "Splunk",
    description: "Log search + Observability Cloud alerts.",
    available: false,
    category: "observability",
  },

  // ---- Incident management ----
  {
    provider: "pagerduty",
    name: "PagerDuty",
    description: "On-call routing + paging.",
    available: true,
    category: "incident",
  },
  {
    provider: "opsgenie",
    name: "Opsgenie",
    description: "Atlassian on-call + escalation.",
    available: false,
    category: "incident",
  },
  {
    provider: "incident_io",
    name: "incident.io",
    description: "Modern incident response platform.",
    available: false,
    category: "incident",
  },

  // ---- Deploy / infra ----
  {
    provider: "argocd",
    name: "ArgoCD",
    description: "Deployment + rollback orchestration.",
    available: true,
    category: "deploy",
  },
  {
    provider: "flux_cd",
    name: "Flux CD",
    description: "GitOps deploys + Helm controller.",
    available: false,
    category: "deploy",
  },
  {
    provider: "kubernetes",
    name: "Kubernetes",
    description: "Direct cluster API: scale, restart, drain.",
    available: false,
    category: "deploy",
  },
  {
    provider: "aws_cloudwatch",
    name: "AWS CloudWatch",
    description: "AWS metrics, alarms, and Lambda recovery.",
    available: false,
    category: "deploy",
  },
  {
    provider: "gcp_monitoring",
    name: "GCP Monitoring",
    description: "GCP alerts + Cloud Run/GKE recovery.",
    available: false,
    category: "deploy",
  },
  {
    provider: "azure_monitor",
    name: "Azure Monitor",
    description: "Azure alerts + AKS recovery.",
    available: false,
    category: "deploy",
  },

  // ---- Chat / notifications ----
  {
    provider: "slack",
    name: "Slack",
    description: "Notify channels + DM approvers.",
    available: true,
    category: "chat",
  },
  {
    provider: "ms_teams",
    name: "Microsoft Teams",
    description: "Channel posts + approval actions.",
    available: false,
    category: "chat",
  },
  {
    provider: "discord",
    name: "Discord",
    description: "Webhook notifications.",
    available: false,
    category: "chat",
  },

  // ---- Data / pipelines ----
  {
    provider: "spark",
    name: "Apache Spark",
    description: "Job failure detection + retry orchestration.",
    available: false,
    category: "data",
  },
  {
    provider: "databricks",
    name: "Databricks",
    description: "Cluster + workflow recovery.",
    available: false,
    category: "data",
  },
  {
    provider: "airflow",
    name: "Apache Airflow",
    description: "DAG failure recovery + backfill triggers.",
    available: false,
    category: "data",
  },
  {
    provider: "snowflake",
    name: "Snowflake",
    description: "Query failures + warehouse scaling.",
    available: false,
    category: "data",
  },
  {
    provider: "dbt",
    name: "dbt",
    description: "Model failure recovery + lineage-aware retries.",
    available: false,
    category: "data",
  },
  {
    provider: "kafka",
    name: "Kafka",
    description: "Consumer lag + dead-letter routing.",
    available: false,
    category: "data",
  },

  // ---- Security / flags ----
  {
    provider: "launchdarkly",
    name: "LaunchDarkly",
    description: "Auto-kill flags on incident detection.",
    available: false,
    category: "security",
  },
  {
    provider: "vault",
    name: "HashiCorp Vault",
    description: "Secret rotation on credential compromise.",
    available: false,
    category: "security",
  },
];

const CATEGORY_ORDER: CardSpec["category"][] = [
  "source",
  "observability",
  "incident",
  "deploy",
  "chat",
  "data",
  "security",
];
const CATEGORY_LABELS: Record<
  CardSpec["category"],
  { title: string; description: string }
> = {
  source: {
    title: "Source control",
    description: "Where your code + PRs live.",
  },
  observability: {
    title: "Observability",
    description: "Where incidents are detected.",
  },
  incident: {
    title: "Incident management",
    description: "On-call routing + paging.",
  },
  deploy: {
    title: "Deploy & infrastructure",
    description: "Where rollbacks land.",
  },
  chat: {
    title: "Chat & notifications",
    description: "Where approvers get pinged.",
  },
  data: {
    title: "Data & pipelines",
    description: "Job and pipeline recovery.",
  },
  security: {
    title: "Security & flags",
    description: "Feature flags + secret rotation.",
  },
};

// Derives a HealthPill-ready health DTO from whatever the backend returned.
// Preference order:
//   1. Explicit `.health` block (Wave 1 BE will populate this).
//   2. Derive from `row.status` + `row.last_error` + metadata.latency_ms.
//   3. Fall back to disconnected.
function healthFromIntegration(
  row: Integration | undefined,
  raw: unknown,
): IntegrationHealth {
  const r = (raw ?? {}) as {
    health?: IntegrationHealth;
    connected?: boolean;
    last_error?: string;
    metadata?: { latency_ms?: number };
    updated_at?: string;
  };
  if (r.health) return r.health;
  const connected = row?.status === "connected" || r.connected === true;
  if (!connected) return { state: "disconnected" };
  const lastError = row?.last_error ?? r.last_error ?? "";
  // Connected + last_error set → degraded (works but with errors)
  if (lastError) {
    return {
      state: "degraded",
      latency_ms: r.metadata?.latency_ms,
      last_check_at: row?.updated_at ?? r.updated_at,
      last_error: lastError,
    };
  }
  // Connected + no errors → healthy. If the BE has recorded a latency on
  // the row metadata, surface it.
  return {
    state: "healthy",
    latency_ms: r.metadata?.latency_ms,
    last_check_at: row?.updated_at ?? r.updated_at,
  };
}

// The page.tsx server component still passes `orgId` + `apiUrl` because the
// legacy per-provider forms consumed them. Wave 1's manifest-driven dialog
// doesn't need either — the orgId is implied by the session cookie on the
// server, and the API URL is read directly from NEXT_PUBLIC_API_URL inside
// the SDK. We keep the props on the type so page.tsx compiles unchanged but
// don't destructure them here.
type IntegrationsClientProps = {
  initial: Integration[];
  orgId: string;
  apiUrl: string;
};

export function IntegrationsClient({ initial }: IntegrationsClientProps) {
  const router = useRouter();
  const searchParams = useSearchParams();

  const [activeProvider, setActiveProvider] =
    React.useState<IntegrationProvider | null>(null);

  // Lazy useState initialiser reads searchParams once on first render and
  // seeds the banner; subsequent renders preserve the value through normal
  // setState flow. This keeps the "show on first paint" behaviour without
  // calling setState inside useEffect (which would trigger React 19's
  // set-state-in-effect rule and an extra cascading render).
  const [showInstalled, setShowInstalled] = React.useState<boolean>(
    () => searchParams?.get("installed") === "github",
  );

  // The OAuth callbacks redirect here with `?install_error=<code>&provider=`
  // on every failure branch. Until now nothing rendered it, so a failed
  // install looked identical to never having clicked Connect.
  const [installError, setInstallError] = React.useState<{
    provider: string;
    code: string;
  } | null>(() => {
    const code = searchParams?.get("install_error");
    if (!code) return null;
    return { provider: searchParams?.get("provider") ?? "provider", code };
  });

  // Side-effect for the same signal: scrub the query string and revalidate.
  // We only mutate the router (an external system), never component state.
  React.useEffect(() => {
    if (
      searchParams?.get("installed") === "github" ||
      searchParams?.get("install_error")
    ) {
      router.replace("/console/integrations");
      router.refresh();
    }
    // We intentionally only react to the initial mount; subsequent query
    // changes are driven by user actions which don't need the banner.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Index rows by provider so each card can hand a live row to its HealthPill
  // + ConfigureDialog. We index against the raw response (cast to a loose
  // record) so the health-derivation path can read the new `health` block if
  // the backend has started returning it, while still letting the legacy
  // `Integration` shape compile.
  const byProvider = React.useMemo(() => {
    const m = new Map<IntegrationProvider, Integration>();
    for (const i of initial) {
      m.set(i.provider as IntegrationProvider, i);
    }
    return m;
  }, [initial]);

  function closeDialog() {
    setActiveProvider(null);
  }

  function onConnectSuccess() {
    // Re-fetch the integrations list so the HealthPill reflects the new
    // connection state on the next render.
    router.refresh();
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold">Integrations</h1>
        <p className="mt-1 text-sm text-[var(--color-muted-foreground)]">
          Connect the third-party tools NEXIS uses to detect incidents, ship
          fixes, and roll back deployments.
        </p>
      </div>

      {showInstalled && (
        <div
          className={cn(
            "flex items-center justify-between gap-3 rounded-md border border-emerald-500/30 bg-emerald-500/10 px-4 py-3 text-sm text-emerald-700 dark:text-emerald-300",
          )}
          role="status"
        >
          <div className="flex items-center gap-2">
            <CheckCircle2 className="h-4 w-4" />
            <span>GitHub connected successfully.</span>
          </div>
          <button
            type="button"
            aria-label="Dismiss"
            onClick={() => setShowInstalled(false)}
            className="rounded p-1 hover:bg-emerald-500/10"
          >
            <X className="h-4 w-4" />
          </button>
        </div>
      )}

      {installError && (
        <div
          className="flex items-start justify-between gap-3 rounded-md border border-red-500/30 bg-red-500/10 px-4 py-3 text-sm text-red-700 dark:text-red-300"
          role="alert"
        >
          <div className="flex items-start gap-2">
            <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
            <span>
              {installErrorMessage(installError.provider, installError.code)}
            </span>
          </div>
          <button
            type="button"
            aria-label="Dismiss"
            onClick={() => setInstallError(null)}
            className="rounded p-1 hover:bg-red-500/10"
          >
            <X className="h-4 w-4" />
          </button>
        </div>
      )}

      {CATEGORY_ORDER.map((cat) => {
        const cards = CARDS.filter((c) => c.category === cat);
        if (cards.length === 0) return null;
        const liveCount = cards.filter((c) => c.available).length;
        return (
          <section key={cat} className="space-y-3">
            <header className="flex items-end justify-between gap-3">
              <div>
                <h2 className="text-sm font-semibold uppercase tracking-widest text-[var(--color-foreground)]">
                  {CATEGORY_LABELS[cat].title}
                </h2>
                <p className="text-xs text-[var(--color-muted-foreground)]">
                  {CATEGORY_LABELS[cat].description}
                </p>
              </div>
              <span className="text-[11px] text-[var(--color-muted-foreground)]">
                {liveCount} live · {cards.length - liveCount} roadmap
              </span>
            </header>
            <div className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3">
              {cards.map((c) => {
                const row = byProvider.get(c.provider as IntegrationProvider);
                const health = healthFromIntegration(row, row);
                return (
                  <IntegrationCard
                    key={c.provider}
                    provider={c.provider}
                    name={c.name}
                    description={c.description}
                    status={row?.status}
                    comingSoon={!c.available}
                    onConfigure={
                      c.available
                        ? () =>
                            setActiveProvider(c.provider as IntegrationProvider)
                        : undefined
                    }
                    statusSlot={
                      c.available ? (
                        <HealthPill
                          state={health.state}
                          latency_ms={health.latency_ms}
                          last_check_at={health.last_check_at}
                          last_error={health.last_error}
                        />
                      ) : undefined
                    }
                  />
                );
              })}
            </div>
          </section>
        );
      })}

      {activeProvider !== null && (
        <ConfigureDialog
          manifest={INTEGRATION_MANIFESTS[activeProvider]}
          open={activeProvider !== null}
          onOpenChange={(open) => {
            if (!open) closeDialog();
          }}
          onSuccess={onConnectSuccess}
        />
      )}
    </div>
  );
}
