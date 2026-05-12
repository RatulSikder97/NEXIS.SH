"use client";

// Phase 3 Stage 7 — Integrations surface (client).
//
// Renders the 3×2 card grid + a Radix Dialog whose contents swap on
// `activeProvider`. After a successful connect/disconnect the child form
// calls router.refresh() so the parent server component re-fetches the
// integrations list with fresh status.
//
// The `?installed=github` query (set by the mock-install 302) is detected
// once on mount and shown as a dismissible banner. We strip the query so a
// reload doesn't replay the notice.

import * as React from "react";
import * as Dialog from "@radix-ui/react-dialog";
import { CheckCircle2, X } from "lucide-react";
import { useRouter, useSearchParams } from "next/navigation";

import { IntegrationCard } from "@/components/integrations/IntegrationCard";
import { GitHubConfigureForm } from "@/components/integrations/GitHubConfigureForm";
import { SentryConfigureForm } from "@/components/integrations/SentryConfigureForm";
import { ArgoCDConfigureForm } from "@/components/integrations/ArgoCDConfigureForm";
import { cn } from "@/lib/utils";
import type { Integration } from "@/lib/integrations";

type RealProvider = "github" | "sentry" | "argocd";

type CardSpec = {
  provider: RealProvider | "datadog" | "pagerduty" | "slack";
  name: string;
  description: string;
  comingSoon?: boolean;
};

const CARDS: CardSpec[] = [
  {
    provider: "github",
    name: "GitHub",
    description: "PR creation + repo metadata for code changes.",
  },
  {
    provider: "sentry",
    name: "Sentry",
    description: "Incident ingestion via webhook.",
  },
  {
    provider: "argocd",
    name: "ArgoCD",
    description: "Deployment + rollback orchestration.",
  },
  {
    provider: "datadog",
    name: "Datadog",
    description: "Metric-driven anomaly detection.",
    comingSoon: true,
  },
  {
    provider: "pagerduty",
    name: "PagerDuty",
    description: "On-call routing + paging.",
    comingSoon: true,
  },
  {
    provider: "slack",
    name: "Slack",
    description: "Notify channels + DM approvers.",
    comingSoon: true,
  },
];

function titleFor(provider: RealProvider): string {
  switch (provider) {
    case "github":
      return "Configure GitHub";
    case "sentry":
      return "Configure Sentry";
    case "argocd":
      return "Configure ArgoCD";
  }
}

export function IntegrationsClient({
  initial,
  orgId,
  apiUrl: _apiUrl,
}: {
  initial: Integration[];
  orgId: string;
  apiUrl: string;
}) {
  const router = useRouter();
  const searchParams = useSearchParams();
  const [activeProvider, setActiveProvider] = React.useState<RealProvider | null>(
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

  // Build a quick lookup by provider so the cards can hand the live row to
  // the Configure form.
  const byProvider = React.useMemo(() => {
    const m = new Map<RealProvider, Integration>();
    for (const i of initial) m.set(i.provider, i);
    return m;
  }, [initial]);

  const active = activeProvider ? byProvider.get(activeProvider) : undefined;

  function closeDialog() {
    setActiveProvider(null);
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
          const row = c.comingSoon
            ? undefined
            : byProvider.get(c.provider as RealProvider);
          return (
            <IntegrationCard
              key={c.provider}
              provider={c.provider}
              name={c.name}
              description={c.description}
              status={row?.status}
              comingSoon={c.comingSoon}
              onConfigure={
                c.comingSoon
                  ? undefined
                  : () => setActiveProvider(c.provider as RealProvider)
              }
            />
          );
        })}
      </div>

      <Dialog.Root
        open={activeProvider !== null}
        onOpenChange={(open) => {
          if (!open) closeDialog();
        }}
      >
        <Dialog.Portal>
          <Dialog.Overlay className="fixed inset-0 z-40 bg-black/40 backdrop-blur-sm data-[state=open]:animate-in data-[state=open]:fade-in data-[state=closed]:animate-out data-[state=closed]:fade-out" />
          <Dialog.Content className="fixed left-1/2 top-1/2 z-50 w-[min(560px,calc(100vw-2rem))] -translate-x-1/2 -translate-y-1/2 rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-6 shadow-xl outline-none data-[state=open]:animate-in data-[state=open]:fade-in data-[state=open]:zoom-in-95 data-[state=closed]:animate-out data-[state=closed]:fade-out">
            <div className="flex items-start justify-between gap-4 pb-4">
              <div>
                <Dialog.Title className="text-lg font-semibold">
                  {activeProvider ? titleFor(activeProvider) : ""}
                </Dialog.Title>
                <Dialog.Description className="mt-1 text-xs text-[var(--color-muted-foreground)]">
                  Credentials are stored encrypted (AES-GCM).
                </Dialog.Description>
              </div>
              <Dialog.Close asChild>
                <button
                  type="button"
                  aria-label="Close"
                  className="inline-flex h-8 w-8 items-center justify-center rounded-md text-[var(--color-muted-foreground)] hover:bg-[var(--color-muted)] hover:text-[var(--color-foreground)]"
                >
                  <X className="h-4 w-4" />
                </button>
              </Dialog.Close>
            </div>

            {activeProvider === "github" && (
              <GitHubConfigureForm integration={active} onClose={closeDialog} />
            )}
            {activeProvider === "sentry" && (
              <SentryConfigureForm
                integration={active}
                orgId={orgId}
                onClose={closeDialog}
              />
            )}
            {activeProvider === "argocd" && (
              <ArgoCDConfigureForm integration={active} onClose={closeDialog} />
            )}
          </Dialog.Content>
        </Dialog.Portal>
      </Dialog.Root>
    </div>
  );
}
