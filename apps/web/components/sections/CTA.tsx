"use client";

import { content } from "@/lib/content";
import { Button } from "@/components/ui/Button";
import { FadeUp } from "@/components/animations/FadeUp";
import { StaggerGroup } from "@/components/animations/StaggerGroup";

export default function CTA() {
  return (
    <section
      id="early-access"
      className="w-full py-[120px]"
      aria-label="Request Early Access"
    >
      <div className="mx-auto max-w-[1100px] px-6">
        <div className="rounded-[12px] border border-border bg-surface px-6 py-14 md:px-10">
          <div className="mb-8 flex justify-center">
            <div className="h-[1px] w-[180px] bg-primary/70" />
          </div>
          <div className="flex flex-col items-center text-center">
          <StaggerGroup preset="section">
            <FadeUp useWhileInView={false}>
              <h2 className="text-[32px] font-medium leading-[1.15] text-accent md:text-[40px]">
                {content.cta.headline}
              </h2>
            </FadeUp>

            <FadeUp useWhileInView={false} className="mt-4">
              <p className="text-[16px] leading-[1.7] text-text-secondary">
                {content.cta.sub}
              </p>
            </FadeUp>

            <FadeUp useWhileInView={false} className="mt-10">
              <Button href="#early-access" variant="primary" size="lg">
                {content.cta.button}
              </Button>
            </FadeUp>
          </StaggerGroup>
          </div>
        </div>
      </div>
    </section>
  );
}

