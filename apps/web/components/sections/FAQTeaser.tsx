"use client";

import { useState } from "react";
import Link from "next/link";
import type { Route } from "next";
import { ChevronDown } from "lucide-react";

import { FadeUp } from "@/components/animations/FadeUp";
import { SectionLabel } from "@/components/ui/SectionLabel";

type QA = { q: string; a: string };

const FAQS: QA[] = [
  {
    q: "How does NEXIS know which patches are safe to apply?",
    a: "Every patch is validated in a Docker-isolated shadow pipeline. Synthesiser generates candidates, QA agents author unit + property tests against your contracts, and the patch only surfaces for approval after the test suite passes with zero regressions.",
  },
  {
    q: "What happens if the patch is wrong?",
    a: "Approval Gate routes every diff to a human owner. You see the patch, the failing test it now passes, and a plain-English summary before clicking approve. Nothing ships without you. If something does break post-deploy, NEXIS auto-rolls back via the same deployment surface (ArgoCD / GitHub Actions).",
  },
  {
    q: "How do you keep our secrets safe?",
    a: "Every integration credential is encrypted at rest with KeyVault and never logged. Agents run inside ephemeral containers with no network egress except to whitelisted provider APIs. We are SOC 2 Type I in progress and will publish a public security page when audit completes.",
  },
  {
    q: "Which models does NEXIS use?",
    a: "We default to a tuned mix of Claude Sonnet 4.5 for codegen and a smaller routing model for classification, with optional self-host adapters. Token usage is metered and exposed on /console/ai-energy so you can see exactly what each fix costs.",
  },
  {
    q: "Can we run NEXIS on-prem?",
    a: "Enterprise tier supports a self-hosted control plane behind your VPC, with all agent inference proxied through your existing model gateway. Talk to sales for deployment patterns.",
  },
  {
    q: "What does an approval look like in practice?",
    a: "You receive a Slack DM (or PagerDuty page if it's high severity). One link opens a diff view with the failing test → passing test transition, the data lineage Pathfinder traced, and an Approve / Reject button. Median engineer time: 60 seconds.",
  },
];

export default function FAQTeaser() {
  const [openIdx, setOpenIdx] = useState<number | null>(0);

  return (
    <section
      id="faq-teaser"
      className="w-full bg-[var(--color-background)] py-[120px] scroll-mt-[88px]"
      aria-label="FAQ"
    >
      <div className="mx-auto max-w-[960px] px-6">
        <FadeUp>
          <SectionLabel>FREQUENTLY ASKED</SectionLabel>
        </FadeUp>
        <FadeUp className="mt-4">
          <h2 className="text-[32px] font-medium leading-[1.15] tracking-tight text-[var(--color-foreground)] md:text-[40px]">
            The first questions every engineering team asks.
          </h2>
        </FadeUp>

        <ul className="mt-12 divide-y divide-[var(--color-border)] overflow-hidden rounded-[14px] border border-[var(--color-border)] bg-[var(--color-card)] shadow-sm">
          {FAQS.map((item, idx) => {
            const open = openIdx === idx;
            return (
              <li key={item.q}>
                <button
                  type="button"
                  onClick={() => setOpenIdx(open ? null : idx)}
                  aria-expanded={open}
                  className="flex w-full items-center justify-between gap-6 px-6 py-5 text-left transition-colors hover:bg-[var(--color-muted)]/40"
                >
                  <span className="text-[16px] font-medium text-[var(--color-foreground)]">
                    {item.q}
                  </span>
                  <ChevronDown
                    aria-hidden
                    className={`h-4 w-4 shrink-0 text-[var(--color-muted-foreground)] transition-transform ${
                      open ? "rotate-180" : ""
                    }`}
                  />
                </button>
                <div
                  className={`grid overflow-hidden transition-all duration-200 ease-out ${
                    open
                      ? "grid-rows-[1fr] opacity-100"
                      : "grid-rows-[0fr] opacity-0"
                  }`}
                >
                  <div className="overflow-hidden">
                    <p className="px-6 pb-6 text-[15px] leading-[1.7] text-[var(--color-muted-foreground)]">
                      {item.a}
                    </p>
                  </div>
                </div>
              </li>
            );
          })}
        </ul>

        <p className="mt-6 text-center text-[14px] text-[var(--color-muted-foreground)]">
          More questions?{" "}
          <Link
            href={"/docs" as Route}
            className="font-medium text-[var(--color-primary)] hover:underline"
          >
            Read the docs →
          </Link>
        </p>
      </div>
    </section>
  );
}
