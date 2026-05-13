"use client";

import Link from "next/link";
import type { Route } from "next";

import { content } from "@/lib/content";
import { Button } from "@/components/ui/Button";
import { FadeUp } from "@/components/animations/FadeUp";
import { StaggerGroup } from "@/components/animations/StaggerGroup";

export default function CTA() {
  return (
    <section
      id="cta"
      className="w-full bg-[var(--color-background)] py-[120px]"
      aria-label="Request access"
    >
      <div className="mx-auto max-w-[1200px] px-6">
        <div className="rounded-[12px] border border-[var(--color-border)] bg-[var(--color-card)] px-6 py-14 shadow-sm md:px-10">
          <div className="mb-8 flex justify-center">
            <div className="h-[1px] w-[180px] bg-[var(--color-primary)]/70" />
          </div>
          <div className="flex flex-col items-center text-center">
            <StaggerGroup preset="section">
              <FadeUp useWhileInView={false}>
                <h2 className="text-[32px] font-semibold leading-[1.15] tracking-tight text-[var(--color-foreground)] md:text-[40px]">
                  {content.cta.headline}
                </h2>
              </FadeUp>

              <FadeUp useWhileInView={false} className="mt-4">
                <p className="max-w-[640px] text-[16px] leading-[1.7] text-[var(--color-muted-foreground)]">
                  {content.cta.sub}
                </p>
              </FadeUp>

              <FadeUp useWhileInView={false} className="mt-10">
                <Button size="lg" asChild>
                  <Link href={"/sign-up" as Route}>{content.cta.button}</Link>
                </Button>
              </FadeUp>
            </StaggerGroup>
          </div>
        </div>
      </div>
    </section>
  );
}
