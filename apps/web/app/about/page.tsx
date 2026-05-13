// /about — Company page. Mission, founding story, team grid, values list.
import type { Metadata } from "next";
import {
  Compass,
  Eye,
  HeartHandshake,
  ShieldCheck,
  Sparkles,
  Workflow,
} from "lucide-react";

import { MarketingShell } from "@/components/marketing/MarketingShell";
import { PageHero } from "@/components/marketing/PageHero";
import { Section, SectionInner } from "@/components/marketing/SectionContainer";
import { SectionHeading } from "@/components/marketing/SectionHeading";

export const metadata: Metadata = {
  title: "About — Our mission",
  description:
    "NEXIS was founded in 2026 to take the worst part of being on call off engineers' shoulders.",
};

const TEAM = [
  { name: "Ratul Sikder", role: "Founder & CEO", initials: "RS" },
  { name: "Maya Patel", role: "Co-founder & CTO", initials: "MP" },
  { name: "Daniel Okafor", role: "Head of Research", initials: "DO" },
  { name: "Lina Almeida", role: "Head of Design", initials: "LA" },
  { name: "Jonas Weber", role: "Founding Engineer", initials: "JW" },
];

const VALUES = [
  {
    title: "Operators first",
    description:
      "We build for the engineer at 3am, not the executive at 9am. If the on-call rotation isn't quieter, we haven't shipped anything.",
    Icon: ShieldCheck,
  },
  {
    title: "Closed loops only",
    description:
      "We don't ship 'suggestion' tools that hand a long PDF to an engineer. Every recovery either ships a verified fix or escalates with a clear reason.",
    Icon: Workflow,
  },
  {
    title: "Show your work",
    description:
      "Every agent hand-off lands in an audit log. Every patch carries a plain-English summary. If we can't explain it, we don't ship it.",
    Icon: Eye,
  },
  {
    title: "Engineer in the loop",
    description:
      "Autonomy is not the goal. Reducing toil is the goal. A human approves anything that touches production by default.",
    Icon: HeartHandshake,
  },
  {
    title: "Compounding correctness",
    description:
      "Every successful recovery makes the next one cheaper, faster, and more accurate. We earn our place in the loop by getting better, not by getting louder.",
    Icon: Sparkles,
  },
];

export default function AboutPage() {
  return (
    <MarketingShell>
      <PageHero
        eyebrow="ABOUT NEXIS"
        title="We're building the autonomous on-call your team deserves."
        lead="Founded in 2026 by engineers who got tired of 3am pages, NEXIS exists to make the unhappy path of running production systems quieter, faster, and more humane."
      />

      <Section tone="muted">
        <SectionInner>
          <div className="grid grid-cols-1 gap-12 md:grid-cols-2 md:items-start">
            <div>
              <SectionHeading
                eyebrow="OUR MISSION"
                title="Give engineering teams their weekends back."
              />
              <div className="mt-6 space-y-4 text-[15px] leading-[1.75] text-[var(--color-muted-foreground)]">
                <p>
                  Every engineering team we&apos;ve worked with — startups,
                  scale-ups, public companies — has the same hidden cost: a
                  brittle layer of human attention spent watching dashboards,
                  triaging incidents, and writing post-mortems. It&apos;s the
                  reason engineers burn out. It&apos;s the reason teams keep
                  hiring SREs but can&apos;t move faster.
                </p>
                <p>
                  We built NEXIS because the existing observability stack is
                  excellent at detecting problems and miserable at solving
                  them. Sentry tells you something broke. Datadog tells you
                  what changed. PagerDuty wakes someone up. A human still has
                  to do all the actual work of figuring out what went wrong
                  and writing the fix. That last mile is where the cost lives.
                </p>
                <p>
                  Our wager is that a fleet of specialised agents, each
                  expert at one stage of recovery and constrained to your own
                  contracts and tests, can close that loop — with the
                  engineer staying in the loop on anything that matters.
                </p>
              </div>
            </div>
            <div>
              <SectionHeading
                eyebrow="OUR STORY"
                title="A 3am page that should never have woken anyone up."
              />
              <div className="mt-6 space-y-4 text-[15px] leading-[1.75] text-[var(--color-muted-foreground)]">
                <p>
                  In early 2026, Ratul and Maya were on-call together at a
                  data infra company. A 3am page fired because a Sentry rule
                  had matched a noisy log line in a deprecated service. They
                  spent forty minutes acknowledging the alert, paging through
                  dashboards, and finally writing a one-line YAML change to
                  silence it.
                </p>
                <p>
                  The next morning, they sketched out a system where each
                  step of that forty minutes — detection, correlation, fix,
                  validation, approval — was its own specialised agent. They
                  wrote the first prototype that weekend.
                </p>
                <p>
                  NEXIS is what that prototype turned into after eight months
                  of building, talking to SRE teams at twelve companies, and
                  shipping the first six integrations. We&apos;re a small
                  team building deliberately — operators first, closed loops
                  only.
                </p>
              </div>
            </div>
          </div>
        </SectionInner>
      </Section>

      <Section tone="background">
        <SectionInner>
          <SectionHeading
            eyebrow="THE TEAM"
            title="Small. Senior. Building deliberately."
            lead="We hire engineers who&apos;ve carried a pager and know exactly what we&apos;re trying to remove."
          />
          <div className="mt-12 grid grid-cols-1 gap-5 sm:grid-cols-2 lg:grid-cols-3">
            {TEAM.map((p) => (
              <article
                key={p.name}
                className="flex items-center gap-4 rounded-[14px] border border-[var(--color-border)] bg-[var(--color-card)] p-5 shadow-sm transition-all hover:-translate-y-[2px] hover:shadow-md"
              >
                <span
                  aria-hidden
                  className="flex h-14 w-14 shrink-0 items-center justify-center rounded-full bg-[color-mix(in_srgb,var(--color-primary)_15%,var(--color-muted))] text-[16px] font-medium text-[var(--color-primary)]"
                >
                  {p.initials}
                </span>
                <div>
                  <h3 className="text-[15px] font-medium text-[var(--color-foreground)]">
                    {p.name}
                  </h3>
                  <p className="text-[12px] text-[var(--color-muted-foreground)]">
                    {p.role}
                  </p>
                </div>
              </article>
            ))}
          </div>
        </SectionInner>
      </Section>

      <Section tone="muted">
        <SectionInner>
          <SectionHeading
            eyebrow="OUR VALUES"
            title="Five things we won&apos;t bend on."
          />
          <div className="mt-10 grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3">
            {VALUES.map((v) => (
              <article
                key={v.title}
                className="flex h-full flex-col gap-3 rounded-[14px] border border-[var(--color-border)] bg-[var(--color-card)] p-6 shadow-sm"
              >
                <span className="flex h-10 w-10 items-center justify-center rounded-lg bg-[color-mix(in_srgb,var(--color-primary)_12%,var(--color-muted))] text-[var(--color-primary)]">
                  <v.Icon className="h-5 w-5" aria-hidden />
                </span>
                <h3 className="text-[16px] font-medium text-[var(--color-foreground)]">
                  {v.title}
                </h3>
                <p className="text-[13px] leading-[1.65] text-[var(--color-muted-foreground)]">
                  {v.description}
                </p>
              </article>
            ))}
          </div>
          <div className="mt-12 flex items-center gap-3 text-[13px] text-[var(--color-muted-foreground)]">
            <Compass className="h-4 w-4" aria-hidden />
            Backed by founders, operators, and SRE leaders from companies
            you&apos;d recognise.
          </div>
        </SectionInner>
      </Section>
    </MarketingShell>
  );
}
