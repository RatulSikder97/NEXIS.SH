// /careers — Open roles + why-work-here. Each role has a mailto: apply link
// so we don't need a recruiting backend yet.
import type { Metadata } from "next";
import {
  Brain,
  Briefcase,
  Building2,
  Globe,
  Heart,
  Megaphone,
  Sparkles,
  Star,
  Users,
} from "lucide-react";

import { MarketingShell } from "@/components/marketing/MarketingShell";
import { PageHero } from "@/components/marketing/PageHero";
import { Section, SectionInner } from "@/components/marketing/SectionContainer";
import { SectionHeading } from "@/components/marketing/SectionHeading";
import { Button } from "@/components/ui/Button";

export const metadata: Metadata = {
  title: "Careers — Open roles",
  description:
    "Senior, distributed, deliberately small. Five roles open right now at NEXIS.",
};

type Role = {
  title: string;
  team: string;
  location: string;
  blurb: string;
  Icon: typeof Briefcase;
};

const ROLES: Role[] = [
  {
    title: "Senior Backend Engineer",
    team: "Control Plane",
    location: "Remote (Americas / EU)",
    blurb:
      "Build the Go services that orchestrate agent fleets — REST, WebSockets, queues, idempotent recovery, distributed locks.",
    Icon: Briefcase,
  },
  {
    title: "ML Engineer",
    team: "Agent Quality",
    location: "Remote (Americas / EU)",
    blurb:
      "Own evaluator harnesses, prompt regression suites, and retrieval quality across our nine agents. You like graphs and you like numbers.",
    Icon: Brain,
  },
  {
    title: "Developer Relations Engineer",
    team: "Community",
    location: "Remote (any)",
    blurb:
      "Build the integration cookbooks, run the office hours, and tell our story to SRE teams in print, code, and talks.",
    Icon: Megaphone,
  },
  {
    title: "Account Executive (Sales)",
    team: "Go-to-Market",
    location: "NYC or Remote",
    blurb:
      "Own the mid-market motion: from technical evaluation through procurement. You&apos;ve sold infrastructure to engineering teams before.",
    Icon: Building2,
  },
  {
    title: "Customer Success Engineer",
    team: "Customer Success",
    location: "Remote (Americas)",
    blurb:
      "Onboard new workspaces, tune severity routing for each customer, and translate field signal back into the product backlog.",
    Icon: Heart,
  },
];

const WHY = [
  {
    title: "Built for operators",
    body: "Every product decision goes through 'will this make an on-call shift better?' If the answer is no, it doesn&apos;t ship.",
    Icon: Star,
  },
  {
    title: "Senior team",
    body: "We hire deliberately. You&apos;ll work alongside engineers who&apos;ve carried real pagers at real scale.",
    Icon: Users,
  },
  {
    title: "Real autonomy",
    body: "Async-first. Distributed. You own outcomes, not hours. Strong opinions on shipping, weak opinions on where the chair is.",
    Icon: Globe,
  },
  {
    title: "Real upside",
    body: "Top-of-market base, meaningful equity, generous PTO. Read the comp doc on day one — no surprises.",
    Icon: Sparkles,
  },
];

export default function CareersPage() {
  return (
    <MarketingShell>
      <PageHero
        eyebrow="CAREERS"
        title="Help us put the 3am page out of business."
        lead="We&apos;re small, senior, and distributed. Five roles open. If you&apos;ve carried a pager and want to take that experience out of other engineers&apos; lives — read on."
      />

      <Section tone="background">
        <SectionInner>
          <SectionHeading
            eyebrow="OPEN ROLES"
            title="Five roles right now."
          />
          <div className="mt-10 grid grid-cols-1 gap-5 md:grid-cols-2">
            {ROLES.map((role) => (
              <article
                key={role.title}
                className="group flex h-full flex-col gap-3 rounded-[14px] border border-[var(--color-border)] bg-[var(--color-card)] p-6 shadow-sm transition-all hover:-translate-y-[2px] hover:shadow-md"
              >
                <div className="flex items-start justify-between gap-4">
                  <div className="flex items-center gap-3">
                    <span className="flex h-10 w-10 items-center justify-center rounded-lg bg-[color-mix(in_srgb,var(--color-primary)_12%,var(--color-muted))] text-[var(--color-primary)]">
                      <role.Icon className="h-5 w-5" aria-hidden />
                    </span>
                    <div>
                      <h3 className="text-[16px] font-medium text-[var(--color-foreground)]">
                        {role.title}
                      </h3>
                      <p className="text-[12px] text-[var(--color-muted-foreground)]">
                        {role.team} · {role.location}
                      </p>
                    </div>
                  </div>
                </div>
                <p className="text-[13px] leading-[1.65] text-[var(--color-muted-foreground)]">
                  {role.blurb}
                </p>
                <div className="mt-2">
                  <Button asChild>
                    <a
                      href={`mailto:careers@nexis.dev?subject=Application%20%E2%80%94%20${encodeURIComponent(role.title)}`}
                    >
                      Apply
                    </a>
                  </Button>
                </div>
              </article>
            ))}
          </div>
          <p className="mt-8 text-[13px] text-[var(--color-muted-foreground)]">
            Don&apos;t see your role? Send us a note at{" "}
            <a className="font-medium text-[var(--color-primary)] hover:underline" href="mailto:careers@nexis.dev">
              careers@nexis.dev
            </a>
            . We&apos;re always interested in talking to operators who get it.
          </p>
        </SectionInner>
      </Section>

      <Section tone="muted">
        <SectionInner>
          <SectionHeading
            eyebrow="WHY NEXIS"
            title="Four reasons to come build with us."
          />
          <div className="mt-10 grid grid-cols-1 gap-5 md:grid-cols-2 lg:grid-cols-4">
            {WHY.map((w) => (
              <article
                key={w.title}
                className="flex h-full flex-col gap-3 rounded-[14px] border border-[var(--color-border)] bg-[var(--color-card)] p-6 shadow-sm"
              >
                <span className="flex h-10 w-10 items-center justify-center rounded-lg bg-[color-mix(in_srgb,var(--color-primary)_12%,var(--color-muted))] text-[var(--color-primary)]">
                  <w.Icon className="h-5 w-5" aria-hidden />
                </span>
                <h3 className="text-[15px] font-medium text-[var(--color-foreground)]">
                  {w.title}
                </h3>
                <p className="text-[12px] leading-[1.65] text-[var(--color-muted-foreground)]">
                  {w.body}
                </p>
              </article>
            ))}
          </div>
        </SectionInner>
      </Section>
    </MarketingShell>
  );
}
