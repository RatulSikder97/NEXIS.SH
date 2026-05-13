// /docs — Top-level docs landing. Hosts a "60 seconds" intro plus a grid of
// links into the Nextra-hosted documentation. Until /docs is wired locally
// the outbound links go to docs.nexis.dev, which is a placeholder host name.
import type { Route } from "next";
import Link from "next/link";
import type { Metadata } from "next";
import {
  BookOpen,
  CheckCircle2,
  Code2,
  Compass,
  GitBranch,
  Hammer,
  Layers,
  Rocket,
  Workflow,
} from "lucide-react";

import { MarketingShell } from "@/components/marketing/MarketingShell";
import { PageHero } from "@/components/marketing/PageHero";
import { Section, SectionInner } from "@/components/marketing/SectionContainer";
import { SectionHeading } from "@/components/marketing/SectionHeading";
import { Button } from "@/components/ui/Button";

export const metadata: Metadata = {
  title: "Docs — Get NEXIS running in 60 seconds",
  description:
    "Install, quickstart, integration setup, agent reference, runbook, architecture, and API reference.",
};

type Card = {
  title: string;
  description: string;
  href: string;
  Icon: typeof BookOpen;
  external?: boolean;
};

const CARDS: Card[] = [
  {
    title: "Install",
    description: "Spin up NEXIS locally with one Docker command or install the CLI.",
    href: "https://docs.nexis.dev/install",
    Icon: Hammer,
    external: true,
  },
  {
    title: "Quickstart",
    description: "Connect Sentry + GitHub and walk through your first synthetic incident.",
    href: "https://docs.nexis.dev/quickstart",
    Icon: Rocket,
    external: true,
  },
  {
    title: "Integrations",
    description: "Wire up the six live providers and configure severity routing.",
    href: "https://docs.nexis.dev/integrations",
    Icon: GitBranch,
    external: true,
  },
  {
    title: "Agent reference",
    description: "Models, hand-off contracts, retry policies — one page per agent.",
    href: "https://docs.nexis.dev/agents",
    Icon: Workflow,
    external: true,
  },
  {
    title: "Runbook",
    description: "What to do during an incident; how to override agent decisions safely.",
    href: "https://docs.nexis.dev/runbook",
    Icon: BookOpen,
    external: true,
  },
  {
    title: "Architecture",
    description: "Control plane, agent fleet, data plane — diagrams and DDL.",
    href: "https://docs.nexis.dev/architecture",
    Icon: Layers,
    external: true,
  },
  {
    title: "API reference",
    description: "REST + WebSocket endpoints, OAuth, idempotency keys, rate limits.",
    href: "https://docs.nexis.dev/api",
    Icon: Code2,
    external: true,
  },
  {
    title: "Help & support",
    description: "Slack community, status page, support email and SLAs.",
    href: "/contact",
    Icon: Compass,
  },
];

function CardLink({ card }: { card: Card }) {
  const inner = (
    <article className="group relative h-full rounded-[14px] border border-[var(--color-border)] bg-[var(--color-card)] p-6 shadow-sm transition-all hover:-translate-y-[2px] hover:shadow-md">
      <span className="flex h-10 w-10 items-center justify-center rounded-lg bg-[color-mix(in_srgb,var(--color-primary)_12%,var(--color-muted))] text-[var(--color-primary)]">
        <card.Icon className="h-5 w-5" aria-hidden />
      </span>
      <h3 className="mt-4 text-[16px] font-medium text-[var(--color-foreground)]">
        {card.title}
      </h3>
      <p className="mt-1 text-[13px] leading-[1.6] text-[var(--color-muted-foreground)]">
        {card.description}
      </p>
      <span className="mt-4 inline-flex items-center text-[12px] font-medium text-[var(--color-primary)]">
        Read →
      </span>
    </article>
  );

  if (card.external) {
    return (
      <a key={card.title} href={card.href} target="_blank" rel="noreferrer" className="block h-full">
        {inner}
      </a>
    );
  }
  return (
    <Link key={card.title} href={card.href as Route} className="block h-full">
      {inner}
    </Link>
  );
}

export default function DocsPage() {
  return (
    <MarketingShell>
      <PageHero
        eyebrow="DOCUMENTATION"
        title="Everything you need to run NEXIS in production."
        lead="Install, quickstart, integration setup, agent reference, runbook, architecture, and the API spec — eight sections, all built around real recovery flows."
        actions={
          <>
            <Button asChild size="lg">
              <a href="https://docs.nexis.dev/quickstart" target="_blank" rel="noreferrer">
                Start the quickstart →
              </a>
            </Button>
            <Button asChild size="lg" variant="outline">
              <a href="https://github.com/nexis-eco" target="_blank" rel="noreferrer">
                Browse on GitHub
              </a>
            </Button>
          </>
        }
      />

      <Section tone="muted">
        <SectionInner>
          <SectionHeading
            eyebrow="60 SECONDS"
            title="What's NEXIS, in a paragraph."
          />
          <div className="mt-8 grid grid-cols-1 gap-8 md:grid-cols-2 md:items-start">
            <p className="text-[16px] leading-[1.75] text-[var(--color-foreground)]">
              NEXIS is a closed-loop incident-response platform for SRE and
              platform teams. It runs nine specialised AI agents that detect
              anomalies, trace root cause through a causal graph, generate
              candidate patches against your contract repo, validate them in
              a Docker-isolated shadow pipeline, and route the chosen fix
              through engineer approval. The result: median time-to-fix drops
              from ~47 minutes to under 5, and the engineer&apos;s job moves
              from triage to verdict.
            </p>
            <ul className="space-y-3 text-[14px] leading-[1.6]">
              {[
                "Detection — Sentinel ingests Sentry, OTel, Datadog signals in real time.",
                "RCA — Pathfinder traverses a service × deploy × schema causal graph.",
                "Synthesis — Synthesiser writes patches constrained to your contracts.",
                "Validation — QA runs property tests in a shadow pipeline.",
                "Approval — Engineer sees diff + summary, approves in 60 seconds.",
              ].map((line) => (
                <li key={line} className="flex items-start gap-3">
                  <CheckCircle2 className="h-4 w-4 mt-1 text-[var(--color-primary)]" aria-hidden />
                  <span className="text-[var(--color-foreground)]">{line}</span>
                </li>
              ))}
            </ul>
          </div>
        </SectionInner>
      </Section>

      <Section tone="background">
        <SectionInner>
          <SectionHeading
            eyebrow="JUMP INTO A TOPIC"
            title="Eight starting points."
            lead="Each card links to a self-contained section of the docs. External links open the Nextra-hosted documentation; in-product links stay inside this app."
          />
          <div className="mt-12 grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
            {CARDS.map((card) => (
              <CardLink key={card.title} card={card} />
            ))}
          </div>
        </SectionInner>
      </Section>

      <Section tone="accent" compact id="faq">
        <SectionInner>
          <div className="flex flex-col items-center text-center">
            <h2 className="text-[26px] font-medium leading-[1.2] tracking-tight text-[var(--color-foreground)] md:text-[32px]">
              Still got questions?
            </h2>
            <p className="mt-3 max-w-[640px] text-[14px] text-[var(--color-muted-foreground)]">
              Drop into the community Slack or open an issue on GitHub. We
              read everything.
            </p>
            <div className="mt-6 flex flex-wrap justify-center gap-3">
              <Button asChild>
                <a href="https://github.com/nexis-eco" target="_blank" rel="noreferrer">
                  GitHub issues
                </a>
              </Button>
              <Button asChild variant="outline">
                <Link href={"/contact" as Route}>Email support</Link>
              </Button>
            </div>
          </div>
        </SectionInner>
      </Section>
    </MarketingShell>
  );
}
