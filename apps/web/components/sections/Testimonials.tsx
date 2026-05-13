"use client";

import { motion } from "framer-motion";
import { Quote } from "lucide-react";

import { FadeUp } from "@/components/animations/FadeUp";
import { SectionLabel } from "@/components/ui/SectionLabel";
import { usePrefersReducedMotion } from "@/lib/usePrefersReducedMotion";

type Testimonial = {
  quote: string;
  name: string;
  role: string;
  company: string;
  initials: string;
};

const TESTIMONIALS: Testimonial[] = [
  {
    quote:
      "We saw our 3am pages drop 70% in the first month. The team is finally shipping again instead of triaging.",
    name: "Priya Khanna",
    role: "Staff SRE",
    company: "Northpoint",
    initials: "PK",
  },
  {
    quote:
      "NEXIS is the first incident tool we trust to actually propose the fix. The shadow-pipeline validation is the unlock.",
    name: "Marcus Rivera",
    role: "Platform Engineer",
    company: "Veritas",
    initials: "MR",
  },
  {
    quote:
      "I get a Slack approval with a plain-English diff. I tap a button. The fix ships. That's it. It feels like magic.",
    name: "Sara Tanaka",
    role: "VP Engineering",
    company: "Halcyon",
    initials: "ST",
  },
];

export default function Testimonials() {
  const reduced = usePrefersReducedMotion();

  return (
    <section
      id="testimonials"
      className="w-full bg-[var(--color-muted)] border-y border-[var(--color-border)] py-[120px] scroll-mt-[88px]"
      aria-label="Customer voices"
    >
      <div className="mx-auto max-w-[1200px] px-6">
        <FadeUp>
          <SectionLabel>WHAT TEAMS SAY</SectionLabel>
        </FadeUp>
        <FadeUp className="mt-4">
          <h2 className="text-[32px] font-medium leading-[1.15] tracking-tight text-[var(--color-foreground)] md:text-[40px]">
            On-call rotations, but quiet.
          </h2>
        </FadeUp>

        <div className="mt-14 grid grid-cols-1 gap-6 md:grid-cols-3">
          {TESTIMONIALS.map((t, idx) => {
            const card = (
              <figure className="relative h-full rounded-[14px] border border-[var(--color-border)] bg-[var(--color-card)] p-6 shadow-sm">
                <Quote
                  aria-hidden
                  className="absolute right-5 top-5 h-7 w-7 text-[var(--color-primary)]/25"
                />
                <blockquote className="relative text-[16px] leading-[1.65] text-[var(--color-foreground)]">
                  &ldquo;{t.quote}&rdquo;
                </blockquote>
                <figcaption className="mt-6 flex items-center gap-3">
                  <span className="flex h-10 w-10 items-center justify-center rounded-full bg-[color-mix(in_srgb,var(--color-primary)_15%,var(--color-muted))] text-[13px] font-medium text-[var(--color-primary)]">
                    {t.initials}
                  </span>
                  <span>
                    <span className="block text-[14px] font-medium text-[var(--color-foreground)]">
                      {t.name}
                    </span>
                    <span className="block text-[12px] text-[var(--color-muted-foreground)]">
                      {t.role} · {t.company}
                    </span>
                  </span>
                </figcaption>
              </figure>
            );

            if (reduced) return <div key={t.name}>{card}</div>;
            return (
              <motion.div
                key={t.name}
                initial={{ opacity: 0, y: 18 }}
                whileInView={{ opacity: 1, y: 0 }}
                viewport={{ margin: "-100px", once: true }}
                transition={{ duration: 0.28, delay: idx * 0.06 }}
              >
                {card}
              </motion.div>
            );
          })}
        </div>
      </div>
    </section>
  );
}
