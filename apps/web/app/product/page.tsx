// /product — full product overview surface. Three deep feature blocks
// (Detection / Recovery / Approval) tied directly to the agent fleet, plus a
// 12-capability grid and a final CTA strip. Server component; no
// client-side data fetching needed here.
import Link from "next/link";
import type { Route } from "next";
import type { Metadata } from "next";
import {
  ActivitySquare,
  AlertTriangle,
  CheckCircle2,
  CircleAlert,
  Cog,
  Database,
  FileSearch2,
  FlaskConical,
  GitMerge,
  GitPullRequestArrow,
  Layers,
  ShieldCheck,
  Sparkles,
  Telescope,
  Users,
  Workflow,
} from "lucide-react";

import { MarketingShell } from "@/components/marketing/MarketingShell";
import { PageHero } from "@/components/marketing/PageHero";
import { PrettyCard } from "@/components/marketing/PrettyCard";
import { Section, SectionInner } from "@/components/marketing/SectionContainer";
import { SectionHeading } from "@/components/marketing/SectionHeading";
import { Button } from "@/components/ui/Button";
import { TerminalBlink } from "@/components/ui/TerminalBlink";

export const metadata: Metadata = {
  title: "Product — Detect, recover, approve",
  description:
    "NEXIS is a closed-loop incident-response platform. Nine specialised agents detect anomalies, trace root cause, generate validated patches, and route them through engineer approval.",
};

type Feature = {
  eyebrow: string;
  title: string;
  body: string;
  bullets: string[];
  Icon: typeof Telescope;
};

const FEATURES: Feature[] = [
  {
    eyebrow: "DETECTION",
    title: "Sentinel — anomalies caught before customers feel them.",
    body: "Sentinel ingests signals from Sentry, Datadog, OTel collectors, GitHub Actions runs, and your own metrics endpoints. It correlates them into a single anomaly stream and routes high-confidence events directly into the recovery pipeline.",
    bullets: [
      "Streaming anomaly model — flags events within 90 seconds of occurrence.",
      "Severity routing: P0 → on-call DM, P1 → channel, P2 → ticket queue.",
      "False-positive suppression learned per workspace, never global.",
      "Replays the last 24h on every new integration so you start with context.",
    ],
    Icon: Telescope,
  },
  {
    eyebrow: "RECOVERY",
    title: "Pathfinder + Synthesiser — root cause and a working patch.",
    body: "Pathfinder builds a causal graph across services, deploys, schemas, and recent migrations. Synthesiser uses that graph plus your contract repo to generate candidate patches — then QA writes property tests to validate them in a Docker-isolated shadow pipeline.",
    bullets: [
      "Neo4j-backed causal traversal — explains why, not just what.",
      "Patch candidates ranked by blast radius, test coverage, and rollback cost.",
      "Shadow pipeline mirrors prod topology with zero customer traffic.",
      "Every candidate carries a plain-English summary for the approver.",
    ],
    Icon: FlaskConical,
  },
  {
    eyebrow: "APPROVAL",
    title: "Approval Gate — engineers stay in the loop on what matters.",
    body: "Severity-routed approvals reach the right human via Slack, PagerDuty, or email. The approver sees the diff, the failing-then-passing test, and a one-paragraph summary. Approve and the fix ships through your existing deploy tooling (ArgoCD, GitHub Actions, custom).",
    bullets: [
      "One-click approval with cryptographic audit trail.",
      "Approval policies per project — auto-merge for cosmetic, human for schema.",
      "Auto-rollback if post-deploy health checks fail within 10 minutes.",
      "Mobile-friendly approval surface; median engineer time 60 seconds.",
    ],
    Icon: ShieldCheck,
  },
];

type Capability = { title: string; description: string; Icon: typeof Sparkles };

const CAPABILITIES: Capability[] = [
  {
    title: "Causal RCA",
    description: "Graph-backed traversal across services, deploys, schemas.",
    Icon: FileSearch2,
  },
  {
    title: "Patch synthesis",
    description: "LLM patches constrained to your contracts and schemas.",
    Icon: GitMerge,
  },
  {
    title: "Shadow validation",
    description: "Docker-isolated pipeline mirrors prod topology.",
    Icon: FlaskConical,
  },
  {
    title: "Property testing",
    description: "Auto-generated unit + property suites per candidate.",
    Icon: CheckCircle2,
  },
  {
    title: "Severity routing",
    description: "P0/P1/P2/P3 policies per project, no global defaults.",
    Icon: CircleAlert,
  },
  {
    title: "Audit trail",
    description: "Typed agent hand-offs persisted forever, immutable.",
    Icon: Layers,
  },
  {
    title: "Live pipeline canvas",
    description: "Watch every stage of recovery in real time.",
    Icon: Workflow,
  },
  {
    title: "Energy wallet",
    description: "Per-token + per-recovery metering, transparent costs.",
    Icon: ActivitySquare,
  },
  {
    title: "Schema migrations",
    description: "Data Engineer agent owns backfill + cutover plans.",
    Icon: Database,
  },
  {
    title: "Pull-request emission",
    description: "Patches land as PRs on your repo, signed and verified.",
    Icon: GitPullRequestArrow,
  },
  {
    title: "Role-based access",
    description: "Approval policies and budgets per team and per project.",
    Icon: Users,
  },
  {
    title: "Compliance-ready",
    description: "SOC 2 Type I in progress; air-gapped option on Enterprise.",
    Icon: Cog,
  },
];

export default function ProductPage() {
  return (
    <MarketingShell>
      <PageHero
        eyebrow="THE PLATFORM"
        title="Autonomous engineering, supervised by you."
        lead="NEXIS is a closed-loop incident-response platform built for SRE and platform teams. It detects anomalies, traces root cause, generates and validates patches, and routes them through engineer approval — so your team only sees the diffs that need a human."
        actions={
          <>
            <Button size="lg" asChild>
              <Link href={"/sign-up" as Route}>Start free</Link>
            </Button>
            <Button variant="outline" size="lg" asChild>
              <Link href={"/agents" as Route}>Meet the fleet →</Link>
            </Button>
          </>
        }
      />

      {/* Console preview — a screenshot would live here, but we have a real
          live terminal that gives a much better sense of the product. */}
      <Section tone="muted" compact>
        <SectionInner>
          <div className="mx-auto grid max-w-[1100px] grid-cols-1 gap-10 md:grid-cols-[1.1fr_1fr] md:items-center">
            <div>
              <h2 className="text-[24px] font-medium leading-[1.3] tracking-tight text-[var(--color-foreground)] md:text-[28px]">
                A live recovery, mid-flight.
              </h2>
              <p className="mt-4 text-[15px] leading-[1.7] text-[var(--color-muted-foreground)]">
                When Sentinel flags an anomaly, every agent hand-off lands in
                the audit log within seconds. This is what you see on the live
                pipeline canvas in <code className="rounded bg-[var(--color-muted)] px-1 py-0.5 font-mono text-[12px]">/console/incidents</code>.
              </p>
              <div className="mt-6 flex gap-3">
                <Button asChild>
                  <Link href={"/sign-up" as Route}>Open the console</Link>
                </Button>
              </div>
            </div>
            <div>
              <TerminalBlink
                title="incident-2847.log"
                lines={[
                  "[00:02:14] sentinel › anomaly detected — pipeline: etl_orders",
                  "[00:02:15] pathfinder › traversing dependency graph...",
                  "[00:02:17] pathfinder › root cause: schema drift on orders.total_amount",
                  "[00:02:18] synthesiser › generating candidate patches...",
                  "[00:02:21] synthesiser › patch_001 ready — cast coercion + downstream migration",
                  "[00:02:22] validator › deploying to shadow pipeline...",
                  "[00:02:29] validator › 2,847 property tests passed. 0 failures.",
                  "[00:02:30] nexis › patch awaiting approval ✓",
                ]}
              />
            </div>
          </div>
        </SectionInner>
      </Section>

      {/* Three deep feature blocks — alternate the image-left/image-right
          positions so the page reads with rhythm instead of as a column. */}
      {FEATURES.map((feature, idx) => {
        const reverse = idx % 2 === 1;
        const tone = idx % 2 === 0 ? "background" : "muted";
        return (
          <Section key={feature.title} tone={tone}>
            <SectionInner>
              <div
                className={`grid grid-cols-1 gap-12 md:grid-cols-2 md:items-center ${
                  reverse ? "md:[&>:first-child]:order-2" : ""
                }`}
              >
                <div>
                  <SectionHeading
                    eyebrow={feature.eyebrow}
                    title={feature.title}
                    lead={feature.body}
                  />
                  <ul className="mt-8 space-y-3">
                    {feature.bullets.map((b) => (
                      <li
                        key={b}
                        className="flex items-start gap-3 text-[15px] leading-[1.6] text-[var(--color-foreground)]"
                      >
                        <CheckCircle2
                          className="h-4 w-4 mt-1 shrink-0 text-[var(--color-primary)]"
                          aria-hidden
                        />
                        <span>{b}</span>
                      </li>
                    ))}
                  </ul>
                </div>
                <div className="relative">
                  <div className="relative overflow-hidden rounded-[18px] border border-[var(--color-border)] bg-[var(--color-card)] p-8 shadow-sm">
                    <span
                      aria-hidden
                      className="pointer-events-none absolute -inset-1 bg-[radial-gradient(circle_at_top_right,_color-mix(in_srgb,var(--color-primary)_10%,transparent)_0%,_transparent_60%)]"
                    />
                    <div className="relative flex flex-col items-start gap-6">
                      <span className="flex h-14 w-14 items-center justify-center rounded-2xl bg-[color-mix(in_srgb,var(--color-primary)_12%,var(--color-background))] text-[var(--color-primary)]">
                        <feature.Icon className="h-7 w-7" aria-hidden />
                      </span>
                      <div className="grid w-full grid-cols-2 gap-3">
                        {Array.from({ length: 4 }).map((_, i) => (
                          <div
                            key={i}
                            className="rounded-md border border-[var(--color-border)] bg-[var(--color-muted)] px-3 py-3"
                          >
                            <div className="font-mono text-[10px] uppercase tracking-[0.12em] text-[var(--color-muted-foreground)]">
                              metric {i + 1}
                            </div>
                            <div className="mt-1 text-[18px] font-medium text-[var(--color-foreground)]">
                              {idx === 0
                                ? ["94%", "1.2s", "0.3%", "12k"][i]
                                : idx === 1
                                  ? ["2,847", "0.18s", "0.4%", "12"][i]
                                  : ["60s", "1-click", "100%", "0"][i]}
                            </div>
                          </div>
                        ))}
                      </div>
                      <div className="flex w-full items-center gap-2 text-[12px] text-[var(--color-muted-foreground)]">
                        <AlertTriangle className="h-3.5 w-3.5" aria-hidden />
                        Live preview is illustrative — real telemetry varies per workspace.
                      </div>
                    </div>
                  </div>
                </div>
              </div>
            </SectionInner>
          </Section>
        );
      })}

      {/* 12-capability grid */}
      <Section tone="background">
        <SectionInner>
          <SectionHeading
            eyebrow="CAPABILITIES"
            title="Everything you'd expect from an incident-response platform."
            lead="And a few things you wouldn't — like a metered energy wallet and signed pull requests."
          />
          <div className="mt-12 grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {CAPABILITIES.map((c) => (
              <PrettyCard key={c.title}>
                <div className="flex items-start gap-4">
                  <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-[color-mix(in_srgb,var(--color-primary)_12%,var(--color-muted))] text-[var(--color-primary)]">
                    <c.Icon className="h-5 w-5" aria-hidden />
                  </span>
                  <div>
                    <h3 className="text-[15px] font-medium text-[var(--color-foreground)]">
                      {c.title}
                    </h3>
                    <p className="mt-1 text-[13px] leading-[1.6] text-[var(--color-muted-foreground)]">
                      {c.description}
                    </p>
                  </div>
                </div>
              </PrettyCard>
            ))}
          </div>
        </SectionInner>
      </Section>

      {/* CTA strip */}
      <Section tone="accent" compact>
        <SectionInner>
          <div className="flex flex-col items-center text-center">
            <h2 className="text-[28px] font-medium leading-[1.2] tracking-tight text-[var(--color-foreground)] md:text-[36px]">
              Ready to put the fleet to work?
            </h2>
            <p className="mt-3 max-w-[600px] text-[15px] leading-[1.65] text-[var(--color-muted-foreground)]">
              Free tier ships with a synthetic incident generator so you can
              try the full pipeline without wiring up production.
            </p>
            <div className="mt-7 flex flex-wrap justify-center gap-3">
              <Button size="lg" asChild>
                <Link href={"/sign-up" as Route}>Get started</Link>
              </Button>
              <Button size="lg" variant="outline" asChild>
                <Link href={"/pricing" as Route}>View pricing</Link>
              </Button>
            </div>
          </div>
        </SectionInner>
      </Section>
    </MarketingShell>
  );
}
