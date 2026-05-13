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
import { CheckCircle2, X } from "lucide-react";
import { useRouter, useSearchParams } from "next/navigation";

import { IntegrationCard } from "@/components/integrations/IntegrationCard";
import { ConfigureDialog } from "@/components/integrations/ConfigureDialog";
import { HealthPill } from "@/components/integrations/HealthPill";
import {
  INTEGRATION_MANIFESTS,
  type IntegrationProvider,
} from "@/lib/integrations-config";
import type { Integration, IntegrationHealth } from "@/lib/integrations";
import { cn } from "@/lib/utils";

type CardSpec = {
  provider: IntegrationProvider;
  name: string;
  description: string;
  // Wave 1: Slack is now live; Datadog + PagerDuty stay disabled until their
  // backend adapters land in Wave 2.
  available: boolean;
};

const CARDS: CardSpec[] = [
  {
    provider: "github",
    name: "GitHub",
    description: "PR creation + repo metadata for code changes.",
    available: true,
  },
  {
    provider: "sentry",
    name: "Sentry",
    description: "Incident ingestion via webhook.",
    available: true,
  },
  {
    provider: "argocd",
    name: "ArgoCD",
    description: "Deployment + rollback orchestration.",
    available: true,
  },
  {
    provider: "slack",
    name: "Slack",
    description: "Notify channels + DM approvers.",
    available: true,
  },
  {
    provider: "datadog",
    name: "Datadog",
    description: "Metric-driven anomaly detection.",
    available: true,
  },
  {
    provider: "pagerduty",
    name: "PagerDuty",
    description: "On-call routing + paging.",
    available: true,
  },
];

// Derives a HealthPill-ready health DTO from whatever the backend returned.
// The Wave 1 backend (Task 1) will start populating `health`; until then we
// fall back to "unknown" when connected, "disconnected" otherwise — matching
// the contract assumption in the task spec.
function healthFromIntegration(
  row: Integration | undefined,
  raw: unknown,
): IntegrationHealth {
  const r = (raw ?? {}) as { health?: IntegrationHealth; connected?: boolean };
  if (r.health) return r.health;
  const connected = row?.status === "connected" || r.connected === true;
  return { state: connected ? "unknown" : "disconnected" };
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

  const [activeProvider, setActiveProvider] = React.useState<IntegrationProvider | null>(
    null,
  );

  // Lazy useState initialiser reads searchParams once on first render and
  // seeds the banner; subsequent renders preserve the value through normal
  // setState flow. This keeps the "show on first paint" behaviour without
  // calling setState inside useEffect (which would trigger React 19's
  // set-state-in-effect rule and an extra cascading render).
  const [showInstalled, setShowInstalled] = React.useState<boolean>(
    () => searchParams?.get("installed") === "github",
  );

  // Side-effect for the same signal: scrub the query string and revalidate.
  // We only mutate the router (an external system), never component state.
  React.useEffect(() => {
    if (searchParams?.get("installed") === "github") {
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

      <div className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3">
        {CARDS.map((c) => {
          const row = byProvider.get(c.provider);
          const health = healthFromIntegration(row, row);
          return (
            <IntegrationCard
              key={c.provider}
              provider={c.provider}
              name={c.name}
              description={c.description}
              status={row?.status}
              comingSoon={!c.available}
              onConfigure={c.available ? () => setActiveProvider(c.provider) : undefined}
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
