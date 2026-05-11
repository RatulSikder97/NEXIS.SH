"use client";

import { useEffect, useRef, useState } from "react";
import { motion, useInView } from "framer-motion";

import { content } from "@/lib/content";
import { FadeUp } from "@/components/animations/FadeUp";
import { SectionLabel } from "@/components/ui/SectionLabel";
import { StepItem } from "@/components/ui/StepItem";
import { usePrefersReducedMotion } from "@/lib/usePrefersReducedMotion";

export default function HowItWorks() {
  const steps = content.howItWorks.steps;
  const containerRef = useRef<HTMLDivElement | null>(null);
  const inView = useInView(containerRef, { once: true, margin: "-100px" });
  const reduced = usePrefersReducedMotion();

  const stepRefs = useRef<Array<HTMLDivElement | null>>([]);
  const [activeIndex, setActiveIndex] = useState(0);

  useEffect(() => {
    if (typeof IntersectionObserver === "undefined") return;
    const observer = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) {
          if (!entry.isIntersecting) continue;
          const idx = Number((entry.target as HTMLElement).dataset.index);
          if (!Number.isNaN(idx)) setActiveIndex(idx);
        }
      },
      { threshold: 0.55 }
    );
    const current = stepRefs.current.filter(Boolean);
    current.forEach((el) => observer.observe(el as Element));
    return () => observer.disconnect();
  }, []);

  return (
    <section
      id="how-it-works"
      className="w-full bg-[var(--color-background)] py-[120px] scroll-mt-[88px]"
      aria-label="How Nexis Works"
    >
      <div className="mx-auto max-w-[1200px] px-6" ref={containerRef}>
        <FadeUp>
          <SectionLabel>{content.howItWorks.label}</SectionLabel>
        </FadeUp>
        <FadeUp className="mt-6">
          <div className="flex flex-col gap-4 md:flex-row md:items-end md:justify-between">
            <h2 className="text-[32px] font-medium leading-[1.15] tracking-tight text-[var(--color-foreground)] md:text-[40px]">
              {content.howItWorks.headline}
            </h2>
            <p className="max-w-[480px] text-[15px] leading-[1.75] text-[var(--color-muted-foreground)]">
              {content.howItWorks.lead}
            </p>
          </div>
        </FadeUp>

        {reduced ? (
          <div className="mt-6 h-[1px] w-full bg-[var(--color-border)]" />
        ) : (
          <motion.div
            className="mt-6 h-[1px] w-full origin-left bg-[var(--color-primary)]/70"
            initial={{ scaleX: 0 }}
            whileInView={{ scaleX: 1 }}
            viewport={{ margin: "-100px", once: true }}
            transition={{ duration: 0.52, ease: [0.22, 1, 0.36, 1] }}
          />
        )}

        <div className="mt-14">
          <div className="relative space-y-6">
            <div className="absolute bottom-0 left-[14px] top-0 w-[1px] bg-[var(--color-border)]" />
            {reduced ? (
              <div className="absolute bottom-0 left-[14px] top-0 w-[1px] bg-[var(--color-primary)]/70" />
            ) : (
              <motion.div
                className="absolute bottom-0 left-[14px] top-0 w-[1px] origin-top bg-[var(--color-primary)]/70"
                initial={{ scaleY: 0 }}
                animate={inView ? { scaleY: 1 } : { scaleY: 0 }}
                transition={{ duration: 0.52, ease: [0.22, 1, 0.36, 1] }}
              />
            )}

            {steps.map((step, idx) => {
              const stepCard = (
                <div className="grid grid-cols-[30px_1fr] gap-4">
                  <div className="relative flex justify-center pt-8">
                    <span
                      className={[
                        "h-[10px] w-[10px] rounded-full border",
                        idx === activeIndex
                          ? "border-[var(--color-primary)] bg-[var(--color-primary)]"
                          : "border-[var(--color-border)] bg-[var(--color-card)]",
                      ].join(" ")}
                    />
                  </div>
                  <div className="rounded-[12px] border border-[var(--color-border)] bg-[var(--color-card)] transition-shadow duration-150 hover:shadow-sm">
                    <StepItem
                      number={step.number}
                      title={step.title}
                      description={step.description}
                      active={idx === activeIndex}
                      className="bg-transparent"
                    />
                  </div>
                </div>
              );

              return (
                <div
                  key={step.number}
                  ref={(el) => {
                    stepRefs.current[idx] = el;
                  }}
                  data-index={idx}
                >
                  {reduced ? (
                    stepCard
                  ) : (
                    <motion.div
                      initial={{ opacity: 0, y: 18 }}
                      whileInView={{ opacity: 1, y: 0 }}
                      viewport={{ margin: "-100px", once: true }}
                      transition={{ duration: 0.28, delay: idx * 0.06 }}
                      animate={
                        idx === activeIndex
                          ? { scale: 1.005, x: 2 }
                          : { scale: 1, x: 0 }
                      }
                    >
                      {stepCard}
                    </motion.div>
                  )}
                </div>
              );
            })}
          </div>
        </div>
      </div>
    </section>
  );
}
