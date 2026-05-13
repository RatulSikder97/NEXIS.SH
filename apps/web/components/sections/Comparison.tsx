"use client";

import { motion } from "framer-motion";
import { Check, X } from "lucide-react";

import { FadeUp } from "@/components/animations/FadeUp";
import { SectionLabel } from "@/components/ui/SectionLabel";
import { usePrefersReducedMotion } from "@/lib/usePrefersReducedMotion";

type Row = {
  dimension: string;
  without: string;
  with: string;
};

const ROWS: Row[] = [
  {
    dimension: "MTTR for pipeline incidents",
    without: "47 minutes — engineer pages, opens dashboards, hunts the bug.",
    with: "Under 5 minutes — Sentinel flags, Pathfinder roots, Synthesiser patches.",
  },
  {
    dimension: "Root-cause analysis",
    without: "Manual triage across Sentry, Datadog, GitHub, Slack.",
    with: "Single causal graph: services × deploys × schemas × tests.",
  },
  {
    dimension: "Fix correctness",
    without: "Best-guess patch. Hope your tests cover the edge case.",
    with: "2,000+ property tests in a shadow pipeline before the diff lands.",
  },
  {
    dimension: "Approval latency",
    without: "Async review in a stale Slack thread.",
    with: "One-click approval with a plain-English summary of the diff.",
  },
  {
    dimension: "Audit trail",
    without: "Whoever was on-call wrote a Notion post-mortem two weeks late.",
    with: "Typed agent hand-offs persisted to the audit log automatically.",
  },
  {
    dimension: "On-call quality of life",
    without: "3am pages. Burnout. Engineers leaving.",
    with: "Approve from your phone in 60 seconds, back to sleep.",
  },
];

export default function Comparison() {
  const reduced = usePrefersReducedMotion();

  return (
    <section
      id="comparison"
      className="w-full bg-[var(--color-background)] py-[120px] scroll-mt-[88px]"
      aria-label="With NEXIS vs without NEXIS"
    >
      <div className="mx-auto max-w-[1200px] px-6">
        <FadeUp>
          <SectionLabel>BEFORE / AFTER</SectionLabel>
        </FadeUp>
        <FadeUp className="mt-4">
          <h2 className="text-[32px] font-medium leading-[1.15] tracking-tight text-[var(--color-foreground)] md:text-[40px]">
            What changes when an autonomous fleet handles the unhappy path.
          </h2>
        </FadeUp>

        <div className="mt-14 overflow-hidden rounded-[14px] border border-[var(--color-border)] bg-[var(--color-card)] shadow-sm">
          <div className="grid grid-cols-[1.2fr_1fr_1fr] border-b border-[var(--color-border)] bg-[var(--color-muted)]">
            <div className="px-6 py-4 text-[11px] font-medium uppercase tracking-[0.15em] text-[var(--color-muted-foreground)]">
              Dimension
            </div>
            <div className="px-6 py-4 text-[11px] font-medium uppercase tracking-[0.15em] text-[var(--color-muted-foreground)]">
              Without NEXIS
            </div>
            <div className="px-6 py-4 text-[11px] font-medium uppercase tracking-[0.15em] text-[var(--color-primary)]">
              With NEXIS
            </div>
          </div>

          {ROWS.map((row, idx) => {
            const body = (
              <div
                className={[
                  "grid grid-cols-1 md:grid-cols-[1.2fr_1fr_1fr] gap-2 md:gap-0",
                  idx % 2 === 0 ? "bg-[var(--color-background)]" : "bg-[var(--color-muted)]/40",
                  "border-b border-[var(--color-border)] last:border-b-0",
                ].join(" ")}
              >
                <div className="px-6 py-5 text-[15px] font-medium text-[var(--color-foreground)]">
                  {row.dimension}
                </div>
                <div className="px-6 py-5 text-[14px] leading-[1.6] text-[var(--color-muted-foreground)]">
                  <span className="mr-2 inline-flex items-center justify-center align-text-bottom h-4 w-4 rounded-full bg-[color-mix(in_srgb,var(--color-destructive)_15%,transparent)] text-[var(--color-destructive)]">
                    <X className="h-3 w-3" />
                  </span>
                  {row.without}
                </div>
                <div className="px-6 py-5 text-[14px] leading-[1.6] text-[var(--color-foreground)]">
                  <span className="mr-2 inline-flex items-center justify-center align-text-bottom h-4 w-4 rounded-full bg-[color-mix(in_srgb,var(--color-success)_18%,transparent)] text-[var(--color-success)]">
                    <Check className="h-3 w-3" />
                  </span>
                  {row.with}
                </div>
              </div>
            );

            if (reduced) return <div key={row.dimension}>{body}</div>;
            return (
              <motion.div
                key={row.dimension}
                initial={{ opacity: 0, y: 14 }}
                whileInView={{ opacity: 1, y: 0 }}
                viewport={{ margin: "-100px", once: true }}
                transition={{ duration: 0.28, delay: idx * 0.04 }}
              >
                {body}
              </motion.div>
            );
          })}
        </div>
      </div>
    </section>
  );
}
