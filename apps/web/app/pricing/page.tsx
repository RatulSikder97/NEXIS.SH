// /pricing — 3-tier plan comparison + feature matrix + FAQ. Server
// component; the FAQ accordion is delegated to a small client child.
import Link from "next/link";
import type { Route } from "next";
import type { Metadata } from "next";
import { Check, Minus, Sparkles } from "lucide-react";

import { MarketingShell } from "@/components/marketing/MarketingShell";
import { PageHero } from "@/components/marketing/PageHero";
import { Section, SectionInner } from "@/components/marketing/SectionContainer";
import { SectionHeading } from "@/components/marketing/SectionHeading";
import { Button } from "@/components/ui/Button";
import { PricingFAQ } from "@/components/marketing/PricingFAQ";

export const metadata: Metadata = {
  title: "Pricing — Free, Pro, Enterprise",
  description:
    "Free for synthetic incidents. Pro at $49/mo with metered usage. Enterprise on contact. No hidden surprises.",
};

type Tier = {
  name: string;
  price: string;
  cadence: string;
  blurb: string;
  features: string[];
  cta: { label: string; href: string };
  featured?: boolean;
};

const TIERS: Tier[] = [
  {
    name: "Free",
    price: "$0",
    cadence: "forever",
    blurb: "Everything you need to evaluate NEXIS with synthetic incidents.",
    features: [
      "1 workspace",
      "Up to 3 projects",
      "Synthetic incidents only",
      "Sentinel + Pathfinder + Synthesiser agents",
      "Community Slack support",
      "All 28 integrations browseable (Live ones connectable)",
    ],
    cta: { label: "Start free", href: "/sign-up" },
  },
  {
    name: "Pro",
    price: "$49",
    cadence: "per workspace / month",
    blurb: "Production-grade for SRE and platform teams.",
    features: [
      "Unlimited projects",
      "Production incident ingestion",
      "All 9 agents (Architect, Backend, QA, DevOps, Data Eng., Approval Gate)",
      "$0.05 per 1k LLM tokens (metered)",
      "$2 per successful recovery (only if patch ships)",
      "Slack + email support, 24h response",
      "90-day audit retention",
    ],
    cta: { label: "Get Pro", href: "/sign-up" },
    featured: true,
  },
  {
    name: "Enterprise",
    price: "Custom",
    cadence: "contact us",
    blurb: "On-prem control plane, dedicated SSO, your own model gateway.",
    features: [
      "Everything in Pro",
      "Self-hosted control plane (in your VPC)",
      "SSO (SAML, Okta, Azure AD)",
      "Bring-your-own model gateway",
      "Air-gapped deployment option",
      "Volume token + recovery pricing",
      "24/7 support with named contact",
      "Custom audit retention + DPA",
    ],
    cta: { label: "Talk to sales", href: "/contact" },
  },
];

type FeatureRow = {
  feature: string;
  free: string | true | false;
  pro: string | true | false;
  enterprise: string | true | false;
};

const MATRIX: FeatureRow[] = [
  { feature: "Workspaces", free: "1", pro: "Unlimited", enterprise: "Unlimited" },
  { feature: "Projects per workspace", free: "3", pro: "Unlimited", enterprise: "Unlimited" },
  { feature: "Synthetic incidents", free: true, pro: true, enterprise: true },
  { feature: "Production incidents", free: false, pro: true, enterprise: true },
  { feature: "Detection + RCA agents (Sentinel, Pathfinder)", free: true, pro: true, enterprise: true },
  { feature: "Full L1 fleet (Synthesiser, Backend, QA, DevOps, Data Eng.)", free: false, pro: true, enterprise: true },
  { feature: "Architect (L2) + Approval Gate", free: false, pro: true, enterprise: true },
  { feature: "Audit retention", free: "7 days", pro: "90 days", enterprise: "Custom" },
  { feature: "SSO (SAML)", free: false, pro: false, enterprise: true },
  { feature: "Self-hosted control plane", free: false, pro: false, enterprise: true },
  { feature: "BYOK / model gateway", free: false, pro: false, enterprise: true },
  { feature: "Support response SLA", free: "Community", pro: "24h", enterprise: "24/7 named" },
];

function MatrixCell({ value }: { value: string | true | false }) {
  if (value === true) {
    return (
      <span className="inline-flex h-6 w-6 items-center justify-center rounded-full bg-[color-mix(in_srgb,var(--color-success)_18%,transparent)] text-[var(--color-success)]">
        <Check className="h-3.5 w-3.5" />
      </span>
    );
  }
  if (value === false) {
    return (
      <span className="inline-flex h-6 w-6 items-center justify-center rounded-full bg-[var(--color-muted)] text-[var(--color-muted-foreground)]">
        <Minus className="h-3.5 w-3.5" />
      </span>
    );
  }
  return (
    <span className="text-[13px] font-medium text-[var(--color-foreground)]">
      {value}
    </span>
  );
}

export default function PricingPage() {
  return (
    <MarketingShell>
      <PageHero
        eyebrow="PRICING"
        title="Three plans. No surprises. Pay only for what ships."
        lead="Free for evaluation. Pro at $49/mo with metered LLM tokens and successful recoveries. Enterprise for self-hosted + SSO."
        align="center"
      />

      <Section tone="background" compact>
        <SectionInner>
          <div className="grid grid-cols-1 gap-6 md:grid-cols-3 md:items-stretch">
            {TIERS.map((tier) => (
              <article
                key={tier.name}
                className={[
                  "relative flex h-full flex-col rounded-[18px] border bg-[var(--color-card)] p-7 shadow-sm transition-all hover:shadow-md",
                  tier.featured
                    ? "border-[var(--color-primary)] ring-1 ring-[var(--color-primary)]/30"
                    : "border-[var(--color-border)]",
                ].join(" ")}
              >
                {tier.featured ? (
                  <span className="absolute -top-3 left-7 inline-flex items-center gap-1 rounded-full bg-[var(--color-primary)] px-3 py-1 text-[11px] font-medium uppercase tracking-[0.12em] text-[var(--color-primary-foreground)] shadow-sm">
                    <Sparkles className="h-3 w-3" /> Most popular
                  </span>
                ) : null}
                <h3 className="text-[14px] font-medium uppercase tracking-[0.12em] text-[var(--color-muted-foreground)]">
                  {tier.name}
                </h3>
                <div className="mt-4 flex items-baseline gap-2">
                  <span className="text-[40px] font-medium tracking-tight text-[var(--color-foreground)]">
                    {tier.price}
                  </span>
                  <span className="text-[13px] text-[var(--color-muted-foreground)]">
                    {tier.cadence}
                  </span>
                </div>
                <p className="mt-3 text-[14px] leading-[1.6] text-[var(--color-muted-foreground)]">
                  {tier.blurb}
                </p>
                <ul className="mt-6 flex-1 space-y-2">
                  {tier.features.map((f) => (
                    <li key={f} className="flex items-start gap-2 text-[13px] text-[var(--color-foreground)]">
                      <Check className="h-4 w-4 shrink-0 mt-0.5 text-[var(--color-primary)]" aria-hidden />
                      <span>{f}</span>
                    </li>
                  ))}
                </ul>
                <Button
                  asChild
                  size="lg"
                  variant={tier.featured ? "default" : "outline"}
                  className="mt-7 w-full"
                >
                  <Link href={tier.cta.href as Route}>{tier.cta.label}</Link>
                </Button>
              </article>
            ))}
          </div>
          <p className="mt-6 text-center text-[12px] text-[var(--color-muted-foreground)]">
            All plans include the 28-provider integration catalog. Metered
            usage is billed monthly in arrears with line-item invoices.
          </p>
        </SectionInner>
      </Section>

      <Section tone="muted">
        <SectionInner>
          <SectionHeading
            eyebrow="COMPARE PLANS"
            title="Full feature matrix."
            lead="Every capability NEXIS exposes — and which tier you need for it."
          />
          <div className="mt-10 overflow-hidden rounded-[14px] border border-[var(--color-border)] bg-[var(--color-card)] shadow-sm">
            <div className="grid grid-cols-[1.4fr_1fr_1fr_1fr] border-b border-[var(--color-border)] bg-[var(--color-muted)]/60">
              <div className="px-5 py-3 text-[11px] font-medium uppercase tracking-[0.15em] text-[var(--color-muted-foreground)]">
                Feature
              </div>
              {(["Free", "Pro", "Enterprise"] as const).map((h) => (
                <div
                  key={h}
                  className="px-5 py-3 text-center text-[11px] font-medium uppercase tracking-[0.15em] text-[var(--color-muted-foreground)]"
                >
                  {h}
                </div>
              ))}
            </div>
            {MATRIX.map((row, idx) => (
              <div
                key={row.feature}
                className={[
                  "grid grid-cols-[1.4fr_1fr_1fr_1fr] items-center",
                  idx % 2 === 0 ? "bg-[var(--color-card)]" : "bg-[var(--color-muted)]/40",
                  "border-b border-[var(--color-border)] last:border-b-0",
                ].join(" ")}
              >
                <div className="px-5 py-4 text-[14px] font-medium text-[var(--color-foreground)]">
                  {row.feature}
                </div>
                <div className="flex justify-center px-5 py-4"><MatrixCell value={row.free} /></div>
                <div className="flex justify-center px-5 py-4"><MatrixCell value={row.pro} /></div>
                <div className="flex justify-center px-5 py-4"><MatrixCell value={row.enterprise} /></div>
              </div>
            ))}
          </div>
        </SectionInner>
      </Section>

      <Section tone="background">
        <SectionInner>
          <SectionHeading
            eyebrow="PRICING QUESTIONS"
            title="The fine print, surfaced."
          />
          <div className="mt-10">
            <PricingFAQ />
          </div>
        </SectionInner>
      </Section>
    </MarketingShell>
  );
}
