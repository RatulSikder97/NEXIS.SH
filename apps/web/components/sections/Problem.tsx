"use client";

import { motion } from "framer-motion";
import { content } from "@/lib/content";
import { FadeUp } from "@/components/animations/FadeUp";
import { StaggerGroup } from "@/components/animations/StaggerGroup";
import { SectionLabel } from "@/components/ui/SectionLabel";
import { usePrefersReducedMotion } from "@/lib/usePrefersReducedMotion";

function Icon({ kind }: { kind: "silent" | "manual" | "closed-loop" }) {
  const common = "h-6 w-6 stroke-current";
  switch (kind) {
    case "silent":
      return (
        <svg
          className={common}
          viewBox="0 0 24 24"
          fill="none"
          strokeWidth="1.8"
          strokeLinecap="round"
          strokeLinejoin="round"
        >
          <path d="M12 9v4" />
          <path d="M12 17h.01" />
          <path d="M4.5 7.5l15 0" />
          <path d="M6.5 7.5l-1.2 11h14.4l-1.2-11" />
        </svg>
      );
    case "manual":
      return (
        <svg
          className={common}
          viewBox="0 0 24 24"
          fill="none"
          strokeWidth="1.8"
          strokeLinecap="round"
          strokeLinejoin="round"
        >
          <path d="M10 4h10" />
          <path d="M10 10h10" />
          <path d="M10 16h10" />
          <path d="M4 7h.01" />
          <path d="M4 13h.01" />
          <path d="M4 19h.01" />
        </svg>
      );
    case "closed-loop":
      return (
        <svg
          className={common}
          viewBox="0 0 24 24"
          fill="none"
          strokeWidth="1.8"
          strokeLinecap="round"
          strokeLinejoin="round"
        >
          <path d="M20 7h-8" />
          <path d="M20 7v6" />
          <path d="M4 17h8" />
          <path d="M4 17v-6" />
          <path d="M12 7c-4 0-7 3-7 7" />
          <path d="M12 17c4 0 7-3 7-7" />
        </svg>
      );
  }
}

export default function Problem() {
  const reduced = usePrefersReducedMotion();

  return (
    <section
      id="problem"
      className="w-full border-y border-border bg-surface/40 py-[120px] scroll-mt-[88px]"
      aria-label="Problem"
    >
      <div className="mx-auto max-w-[1100px] px-6">
        <div>
          <SectionLabel>{content.problem.label}</SectionLabel>
          <h2 className="mt-6 text-[32px] font-medium leading-[1.15] text-accent md:text-[40px]">
            {content.problem.headline}
          </h2>
          <p className="mt-4 max-w-[700px] text-[16px] leading-[1.7] text-text-secondary">
            {content.problem.intro}
          </p>
        </div>

        <StaggerGroup className="mt-14 grid grid-cols-1 gap-6 md:grid-cols-3">
          {content.problem.points.map((point, idx) => {
            const tone =
              idx === 0
                ? {
                    ring: "border-primary/70",
                    top: "bg-primary/80",
                    icon: "text-primary",
                    chip: "text-primary border-primary/40 bg-primary-soft",
                  }
                : idx === 1
                  ? {
                      ring: "border-secondary/70",
                      top: "bg-secondary/80",
                      icon: "text-secondary",
                      chip: "text-secondary border-secondary/40 bg-secondary-soft",
                    }
                  : {
                      ring: "border-border-hover",
                      top: "bg-text-secondary/60",
                      icon: "text-accent",
                      chip: "text-text-secondary border-border-hover bg-surface",
                    };

            const card = (
              <div
                className={[
                  "group relative min-h-[240px] rounded-[12px] border border-border bg-surface p-6",
                  "transition-transform duration-150 ease-out hover:-translate-y-[2px]",
                  tone.ring,
                ].join(" ")}
              >
                <div
                  className={[
                    "absolute inset-x-0 top-0 h-[1px] transition-opacity duration-150",
                    tone.top,
                  ].join(" ")}
                />

                <div className="flex items-center justify-between">
                  <span
                    className={[
                      "inline-flex items-center rounded-[999px] border px-2 py-1 font-mono text-[11px]",
                      tone.chip,
                    ].join(" ")}
                  >
                    0{idx + 1}
                  </span>
                  <div className={tone.icon}>
                    <Icon
                      kind={
                        idx === 0
                          ? "silent"
                          : idx === 1
                            ? "manual"
                            : "closed-loop"
                      }
                    />
                  </div>
                </div>

                <div className="mt-6">
                  <h3 className="text-[18px] font-medium tracking-[-0.01em] text-accent">
                    {point.title}
                  </h3>
                  <p className="mt-3 text-[15px] leading-[1.75] text-text-secondary">
                    {point.description}
                  </p>
                </div>
              </div>
            );

            return (
              <FadeUp key={point.title} useWhileInView={false}>
                {reduced ? (
                  card
                ) : (
                  <motion.div
                    initial={{ opacity: 0, y: 18, scale: 0.985 }}
                    whileInView={{ opacity: 1, y: 0, scale: 1 }}
                    viewport={{ margin: "-100px", once: true }}
                    transition={{ duration: 0.28, delay: idx * 0.06 }}
                  >
                    {card}
                  </motion.div>
                )}
              </FadeUp>
            );
          })}
        </StaggerGroup>
      </div>
    </section>
  );
}

