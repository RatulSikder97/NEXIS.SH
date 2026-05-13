"use client";

import Link from "next/link";
import type { Route } from "next";

import { FadeUp } from "@/components/animations/FadeUp";
import { ProviderLogo, type ProviderID } from "@/components/integrations/ProviderLogo";
import { Button } from "@/components/ui/Button";
import { SectionLabel } from "@/components/ui/SectionLabel";

// 4x7 grid = 28 providers. Order matches the categories on /integrations so
// users see Source control → Observability → Incident → Deploy → Chat → Data
// → Security / flags as they scan left-to-right, top-to-bottom.
const PROVIDERS: ProviderID[] = [
  // Source control
  "github",
  "gitlab",
  "bitbucket",
  "argocd",
  // Observability
  "sentry",
  "datadog",
  "newrelic",
  "grafana_cloud",
  // Observability cont.
  "prometheus",
  "honeycomb",
  "splunk",
  "pagerduty",
  // Incident + chat
  "opsgenie",
  "incident_io",
  "slack",
  "ms_teams",
  // Chat + infra
  "discord",
  "kubernetes",
  "flux_cd",
  "aws_cloudwatch",
  // Infra + data
  "gcp_monitoring",
  "azure_monitor",
  "kafka",
  "airflow",
  // Data + flags
  "spark",
  "databricks",
  "snowflake",
  "dbt",
];

export default function IntegrationsTeaser() {
  return (
    <section
      id="integrations-teaser"
      className="w-full bg-[var(--color-background)] py-[120px] scroll-mt-[88px]"
      aria-label="Integrations preview"
    >
      <div className="mx-auto max-w-[1200px] px-6">
        <div className="flex flex-col gap-3 md:flex-row md:items-end md:justify-between">
          <FadeUp>
            <SectionLabel>WORKS WITH YOUR STACK</SectionLabel>
            <h2 className="mt-4 text-[32px] font-medium leading-[1.15] tracking-tight text-[var(--color-foreground)] md:text-[40px]">
              Plug into every tool your team already uses.
            </h2>
          </FadeUp>
          <FadeUp>
            <Button asChild variant="outline" size="sm">
              <Link href={"/integrations" as Route}>Browse all integrations →</Link>
            </Button>
          </FadeUp>
        </div>

        <div className="mt-12 overflow-hidden rounded-[14px] border border-[var(--color-border)] bg-[var(--color-card)] shadow-sm">
          <div className="grid grid-cols-4 md:grid-cols-7">
            {PROVIDERS.map((p) => (
              <div
                key={p}
                className="group relative flex h-[88px] items-center justify-center border-b border-r border-[var(--color-border)] bg-[var(--color-card)] last:border-b-0 hover:bg-[var(--color-muted)] transition-colors"
                title={p.replace(/_/g, " ")}
              >
                <span className="h-7 w-7 text-[var(--color-muted-foreground)] grayscale opacity-80 group-hover:grayscale-0 group-hover:opacity-100 transition-all">
                  <ProviderLogo provider={p} className="h-7 w-7" />
                </span>
              </div>
            ))}
          </div>
        </div>

        <p className="mt-4 text-center text-[13px] text-[var(--color-muted-foreground)]">
          28 integrations across source control, observability, incident
          management, deploy & infra, chat, data, and security.
        </p>
      </div>
    </section>
  );
}
