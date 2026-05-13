// /integrations — Public marketing integration catalog. Mirrors the same
// seven categories as /console/integrations but with marketing-friendly
// descriptions and a Live / Roadmap badge so visitors can see what works
// today vs what is coming.
import Link from "next/link";
import type { Route } from "next";
import type { Metadata } from "next";
import { Check, Clock } from "lucide-react";

import { MarketingShell } from "@/components/marketing/MarketingShell";
import { PageHero } from "@/components/marketing/PageHero";
import { Section, SectionInner } from "@/components/marketing/SectionContainer";
import { SectionHeading } from "@/components/marketing/SectionHeading";
import { ProviderLogo, type ProviderID } from "@/components/integrations/ProviderLogo";
import { Button } from "@/components/ui/Button";

export const metadata: Metadata = {
  title: "Integrations — Works with your stack",
  description:
    "28 integrations across source control, observability, incident management, deploy & infra, chat, data, and security & flags.",
};

type Entry = {
  id: ProviderID;
  name: string;
  blurb: string;
  status: "Live" | "Roadmap";
};

type Category = { title: string; description: string; entries: Entry[] };

const LIVE: Set<ProviderID> = new Set([
  "github",
  "sentry",
  "argocd",
  "slack",
  "datadog",
  "pagerduty",
]);

const CATEGORIES: Category[] = [
  {
    title: "Source control",
    description: "Where patches land. PRs are signed, labelled, and traceable.",
    entries: [
      { id: "github", name: "GitHub", blurb: "Open PRs from approved patches; read repo state for context.", status: "Live" },
      { id: "gitlab", name: "GitLab", blurb: "Merge requests with the same audit metadata as GitHub.", status: "Roadmap" },
      { id: "bitbucket", name: "Bitbucket", blurb: "Pull-request emission against Bitbucket Cloud + Server.", status: "Roadmap" },
    ],
  },
  {
    title: "Observability",
    description: "Where Sentinel hears the world. Metrics, traces, logs, events.",
    entries: [
      { id: "sentry", name: "Sentry", blurb: "Issue webhook ingest + REST API for crash context.", status: "Live" },
      { id: "datadog", name: "Datadog", blurb: "Monitor + metric query for anomaly seed data.", status: "Live" },
      { id: "newrelic", name: "New Relic", blurb: "APM + NRQL queries feed Sentinel correlation.", status: "Roadmap" },
      { id: "grafana_cloud", name: "Grafana Cloud", blurb: "Dashboards as input. Alerts as triggers.", status: "Roadmap" },
      { id: "prometheus", name: "Prometheus", blurb: "Direct PromQL queries against your scrape targets.", status: "Roadmap" },
      { id: "honeycomb", name: "Honeycomb", blurb: "BubbleUp queries pulled into Pathfinder traversal.", status: "Roadmap" },
      { id: "splunk", name: "Splunk", blurb: "Search head queries for log-based RCA.", status: "Roadmap" },
    ],
  },
  {
    title: "Incident management",
    description: "Where on-call lives. Severity routes here when humans are needed.",
    entries: [
      { id: "pagerduty", name: "PagerDuty", blurb: "On-call schedule lookup + escalation for P0/P1.", status: "Live" },
      { id: "opsgenie", name: "Opsgenie", blurb: "Same escalation surface as PagerDuty, native API.", status: "Roadmap" },
      { id: "incident_io", name: "incident.io", blurb: "Sync NEXIS incidents with your IR runbook tool.", status: "Roadmap" },
    ],
  },
  {
    title: "Deploy & infra",
    description: "Where validated patches ship to. Approval-gated by default.",
    entries: [
      { id: "argocd", name: "ArgoCD", blurb: "Application sync + rollback through standard ArgoCD primitives.", status: "Live" },
      { id: "kubernetes", name: "Kubernetes", blurb: "Direct cluster API for diagnostics + remediation.", status: "Roadmap" },
      { id: "flux_cd", name: "FluxCD", blurb: "GitOps reconciliation via Flux Kustomization resources.", status: "Roadmap" },
      { id: "aws_cloudwatch", name: "AWS CloudWatch", blurb: "Logs + metrics ingestion across AWS accounts.", status: "Roadmap" },
      { id: "gcp_monitoring", name: "GCP Monitoring", blurb: "Stackdriver-era APIs for GCP-hosted workloads.", status: "Roadmap" },
      { id: "azure_monitor", name: "Azure Monitor", blurb: "App Insights queries surfaced to Sentinel.", status: "Roadmap" },
    ],
  },
  {
    title: "Chat",
    description: "Where engineers actually look. Approvals come to you.",
    entries: [
      { id: "slack", name: "Slack", blurb: "DM approvers; post incident channels with diff previews.", status: "Live" },
      { id: "ms_teams", name: "Microsoft Teams", blurb: "Adaptive Cards with diff + approve / reject buttons.", status: "Roadmap" },
      { id: "discord", name: "Discord", blurb: "Useful for indie + open-source workspaces.", status: "Roadmap" },
    ],
  },
  {
    title: "Data & pipelines",
    description: "Where data integrity matters. Migrations + backfill plans live here.",
    entries: [
      { id: "airflow", name: "Airflow", blurb: "DAG-level integration for ETL incident routing.", status: "Roadmap" },
      { id: "spark", name: "Spark", blurb: "Job-level error + lineage ingestion.", status: "Roadmap" },
      { id: "databricks", name: "Databricks", blurb: "Notebook + job orchestration for data-engineer agent.", status: "Roadmap" },
      { id: "snowflake", name: "Snowflake", blurb: "Schema introspection for migration planning.", status: "Roadmap" },
      { id: "dbt", name: "dbt", blurb: "Model graph + test results consumed by Pathfinder.", status: "Roadmap" },
      { id: "kafka", name: "Kafka", blurb: "Topic lag + schema-registry checks for streaming pipelines.", status: "Roadmap" },
    ],
  },
  {
    title: "Security & flags",
    description: "Where blast-radius gets contained. Flags off in seconds.",
    entries: [
      { id: "launchdarkly", name: "LaunchDarkly", blurb: "Auto-flag-off a release on confirmed regression.", status: "Roadmap" },
      { id: "vault", name: "HashiCorp Vault", blurb: "Short-lived credentials for agent runs.", status: "Roadmap" },
    ],
  },
];

function StatusBadge({ status }: { status: Entry["status"] }) {
  if (status === "Live") {
    return (
      <span className="inline-flex items-center gap-1 rounded-full bg-[color-mix(in_srgb,var(--color-success)_15%,transparent)] px-2 py-0.5 text-[10px] font-medium uppercase tracking-[0.1em] text-[var(--color-success)]">
        <Check className="h-3 w-3" /> Live
      </span>
    );
  }
  return (
    <span className="inline-flex items-center gap-1 rounded-full bg-[var(--color-muted)] px-2 py-0.5 text-[10px] font-medium uppercase tracking-[0.1em] text-[var(--color-muted-foreground)]">
      <Clock className="h-3 w-3" /> Roadmap
    </span>
  );
}

export default function IntegrationsPage() {
  const live = CATEGORIES.flatMap((c) => c.entries).filter((e) => e.status === "Live").length;
  const total = CATEGORIES.reduce((n, c) => n + c.entries.length, 0);

  return (
    <MarketingShell>
      <PageHero
        eyebrow="INTEGRATIONS"
        title="Plug into every tool your team already uses."
        lead={`Six providers are wired up today (${live} live), ${total - live} more on the roadmap. Same auth flow, same audit log, same recovery pipeline.`}
        actions={
          <>
            <Button size="lg" asChild>
              <Link href={"/sign-up" as Route}>Get started</Link>
            </Button>
            <Button size="lg" variant="outline" asChild>
              <Link href={"/docs" as Route}>Integration docs →</Link>
            </Button>
          </>
        }
      />

      {CATEGORIES.map((category, idx) => (
        <Section key={category.title} tone={idx % 2 === 0 ? "background" : "muted"}>
          <SectionInner>
            <SectionHeading
              eyebrow={`CATEGORY ${String(idx + 1).padStart(2, "0")}`}
              title={category.title}
              lead={category.description}
            />
            <div className="mt-10 grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
              {category.entries.map((entry) => (
                <article
                  key={entry.id}
                  className="group relative flex h-full flex-col rounded-[14px] border border-[var(--color-border)] bg-[var(--color-card)] p-5 shadow-sm transition-all hover:-translate-y-[2px] hover:shadow-md"
                >
                  <header className="flex items-start justify-between gap-3">
                    <span className="flex h-11 w-11 items-center justify-center rounded-xl border border-[var(--color-border)] bg-[var(--color-muted)] text-[var(--color-foreground)]">
                      <ProviderLogo provider={entry.id} className="h-6 w-6" />
                    </span>
                    <StatusBadge status={entry.status} />
                  </header>
                  <h3 className="mt-4 text-[16px] font-medium text-[var(--color-foreground)]">
                    {entry.name}
                  </h3>
                  <p className="mt-1 text-[13px] leading-[1.6] text-[var(--color-muted-foreground)]">
                    {entry.blurb}
                  </p>
                </article>
              ))}
            </div>
          </SectionInner>
        </Section>
      ))}

      <Section tone="accent" compact>
        <SectionInner>
          <div className="flex flex-col items-center text-center">
            <h2 className="text-[24px] font-medium tracking-tight text-[var(--color-foreground)] md:text-[30px]">
              Don&apos;t see your tool?
            </h2>
            <p className="mt-2 max-w-[640px] text-[14px] text-[var(--color-muted-foreground)]">
              Roadmap items are scoped — request priority via the contact form
              and we&apos;ll tell you where it sits in our queue.
            </p>
            <Button asChild className="mt-6">
              <Link href={"/contact" as Route}>Request an integration</Link>
            </Button>
          </div>
        </SectionInner>
      </Section>
    </MarketingShell>
  );
}

// Avoid `unused-vars` flag for `LIVE` constant — it documents which providers
// are live and is consumed by the typed Set comparison if we want to drive it
// from the category list later. Suppressing here is cleaner than removing it.
void LIVE;
