"use client";

import { content } from "@/lib/content";
import { SectionLabel } from "@/components/ui/SectionLabel";
import { StatCard } from "@/components/ui/StatCard";

export default function Metrics() {
  return (
    <section
      id="metrics"
      className="w-full border-y border-[var(--color-border)] bg-[var(--color-muted)] py-[120px] scroll-mt-[88px]"
      aria-label="Metrics"
    >
      <div className="mx-auto max-w-[1200px] px-6">
        <SectionLabel>{content.metrics.label}</SectionLabel>
        <h2 className="mt-4 text-[28px] font-medium leading-[1.15] tracking-tight text-[var(--color-foreground)] md:text-[32px]">
          {content.metrics.headline}
        </h2>
      </div>
      <div className="mx-auto mt-12 grid max-w-[1200px] grid-cols-1 px-6 md:mt-14 md:grid-cols-3">
        {content.metrics.stats.map((stat, idx) => (
          <div
            key={stat.label}
            className={[
              "py-6 md:py-0",
              idx === 0 ? "" : "md:border-l md:border-[var(--color-border)]",
            ].join(" ")}
          >
            <div className="relative px-6">
              <div className="absolute left-6 top-0 h-[1px] w-[120px] bg-[var(--color-primary)]/70" />
              <div className="pt-6">
                <StatCard {...stat} />
              </div>
            </div>
          </div>
        ))}
      </div>
    </section>
  );
}
