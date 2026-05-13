"use client";

// ProjectIntegrationIcons — one-row brand-logo strip used by every project
// card. Renders the 6 integration providers in a fixed order using the same
// inline SVG brand marks as the /console/integrations grid. Connected
// providers render at full opacity; unconnected ones drop to 25% so the user
// can scan wiring at a glance.

import * as React from "react";

import { ProviderLogo } from "@/components/integrations/ProviderLogo";
import { cn } from "@/lib/utils";
import type { ConnectedIntegrationProvider } from "@/lib/projects";

export type ProjectIntegrationDescriptor = {
  provider: ConnectedIntegrationProvider;
  label: string;
};

export const PROJECT_INTEGRATION_PROVIDERS: ProjectIntegrationDescriptor[] = [
  { provider: "github", label: "GitHub" },
  { provider: "sentry", label: "Sentry" },
  { provider: "argocd", label: "ArgoCD" },
  { provider: "slack", label: "Slack" },
  { provider: "datadog", label: "Datadog" },
  { provider: "pagerduty", label: "PagerDuty" },
];

export function ProjectIntegrationIcons({
  connected,
  size = "sm",
}: {
  connected: Set<ConnectedIntegrationProvider>;
  size?: "sm" | "md";
}) {
  const boxSize = size === "md" ? "h-6 w-6" : "h-5 w-5";
  const iconSize = size === "md" ? "h-4 w-4" : "h-3.5 w-3.5";
  return (
    <div className="flex items-center gap-1.5" aria-label="Connected integrations">
      {PROJECT_INTEGRATION_PROVIDERS.map((p) => {
        const isOn = connected.has(p.provider);
        return (
          <span
            key={p.provider}
            title={`${p.label}${isOn ? " (connected)" : " (not configured)"}`}
            className={cn(
              "inline-flex items-center justify-center rounded transition-opacity",
              boxSize,
              isOn ? "opacity-100" : "opacity-25 grayscale",
            )}
            aria-label={`${p.label}: ${isOn ? "connected" : "not configured"}`}
          >
            <ProviderLogo
              provider={p.provider}
              className={cn("flex items-center justify-center", iconSize)}
            />
          </span>
        );
      })}
    </div>
  );
}
