"use client";

// ProjectIntegrationIcons — one-row icon strip used by every project card.
//
// Renders the 6 integration providers in a fixed order. Connected providers
// (computed from the project's selectors via connectedProviders()) render
// in `text-foreground`; unconnected ones drop to `text-muted-foreground/40`
// so the user can scan which projects are fully wired at a glance.
//
// Lucide does not ship vendor logos, so each provider maps to the closest
// semantic-fit icon from lucide-react:
//
//   github     → GitBranch       (PRs are how recovery surfaces here)
//   sentry     → Bug             (Sentry is the error-tracker side)
//   argocd     → Rocket          (deploy / rollback)
//   slack      → MessageSquare   (chat notifications)
//   datadog    → Activity        (metrics + APM)
//   pagerduty  → BellRing        (paging / on-call)
//
// The icon set + order is exposed as PROJECT_INTEGRATION_PROVIDERS so the
// detail page's Integrations tab can share the same registry.

import * as React from "react";
import {
  Activity,
  BellRing,
  Bug,
  GitBranch,
  MessageSquare,
  Rocket,
  type LucideIcon,
} from "lucide-react";

import { cn } from "@/lib/utils";
import type { ConnectedIntegrationProvider } from "@/lib/projects";

export type ProjectIntegrationDescriptor = {
  provider: ConnectedIntegrationProvider;
  label: string;
  icon: LucideIcon;
};

export const PROJECT_INTEGRATION_PROVIDERS: ProjectIntegrationDescriptor[] = [
  { provider: "github", label: "GitHub", icon: GitBranch },
  { provider: "sentry", label: "Sentry", icon: Bug },
  { provider: "argocd", label: "ArgoCD", icon: Rocket },
  { provider: "slack", label: "Slack", icon: MessageSquare },
  { provider: "datadog", label: "Datadog", icon: Activity },
  { provider: "pagerduty", label: "PagerDuty", icon: BellRing },
];

export function ProjectIntegrationIcons({
  connected,
  size = "sm",
}: {
  connected: Set<ConnectedIntegrationProvider>;
  size?: "sm" | "md";
}) {
  const iconSize = size === "md" ? "h-4 w-4" : "h-3.5 w-3.5";
  return (
    <div className="flex items-center gap-1.5" aria-label="Connected integrations">
      {PROJECT_INTEGRATION_PROVIDERS.map((p) => {
        const Icon = p.icon;
        const isOn = connected.has(p.provider);
        return (
          <span
            key={p.provider}
            title={`${p.label}${isOn ? " (connected)" : " (not configured)"}`}
            className={cn(
              "inline-flex items-center justify-center rounded p-0.5 transition-colors",
              isOn
                ? "text-[var(--color-foreground)]"
                : "text-[var(--color-muted-foreground)]/40",
            )}
            aria-label={`${p.label}: ${isOn ? "connected" : "not configured"}`}
          >
            <Icon className={iconSize} aria-hidden />
          </span>
        );
      })}
    </div>
  );
}
