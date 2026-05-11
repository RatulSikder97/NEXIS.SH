"use client";

import { content } from "@/lib/content";
import { SectionLabel } from "@/components/ui/SectionLabel";
import { StatCard } from "@/components/ui/StatCard";

export default function Metrics() {
  return (
    <section
      className="w-full border-y border-border bg-surface py-[120px]"
      aria-label="Metrics"
    >
      <div className="mx-auto max-w-[1100px] px-6">
        <SectionLabel>{content.metrics.label}</SectionLabel>
        <h2 className="mt-4 text-[28px] font-medium leading-[1.15] text-accent md:text-[32px]">
          {content.metrics.headline}
        </h2>
      </div>
      <div className="mx-auto mt-12 flex max-w-[1100px] flex-col px-6 md:mt-14 md:flex-row">
        {content.metrics.stats.map((stat, idx) => (
          <div
            key={stat.label}
            className={[
              "flex-1",
              "py-6",
              idx === 0 ? "" : "border-l border-border",
              "md:py-0",
            ].join(" ")}
          >
            <div className="relative px-6">
              <div
                className={[
                  "absolute left-6 top-0 h-[1px] w-[120px]",
                  idx === 1 ? "bg-secondary/70" : "bg-primary/70",
                ].join(" ")}
              />
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

