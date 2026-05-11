"use client";

import { content } from "@/lib/content";

import { Button } from "@/components/ui/Button";
import { SectionLabel } from "@/components/ui/SectionLabel";
import { StaggerGroup } from "@/components/animations/StaggerGroup";
import { FadeUp } from "@/components/animations/FadeUp";
import { TerminalBlink } from "@/components/ui/TerminalBlink";
import { SystemVector } from "@/components/ui/SystemVector";

export default function Hero() {
  return (
    <section
      id="features"
      className="relative w-full min-h-[100vh] flex items-center py-[120px] scroll-mt-[88px] bg-[var(--color-background)]"
    >
      <div
        aria-hidden
        className="pointer-events-none absolute inset-0 bg-[radial-gradient(ellipse_at_top,_color-mix(in_srgb,var(--color-primary)_8%,transparent)_0%,_transparent_55%)]"
      />
      <div className="relative z-10 mx-auto flex max-w-[1200px] flex-col gap-16 px-6 md:flex-row md:items-start md:justify-between md:gap-20">
        <div className="flex-1">
          <div className="min-h-[calc(100vh-240px)] flex flex-col justify-center">
            <StaggerGroup preset="hero" mode="immediate">
              <FadeUp useWhileInView={false}>
                <SectionLabel>{content.hero.preHeading}</SectionLabel>
              </FadeUp>

              <FadeUp useWhileInView={false} className="mt-4">
                <h1 className="text-[40px] leading-[1.1] font-medium tracking-tight text-[var(--color-foreground)] md:text-[64px]">
                  {content.hero.heading}
                </h1>
              </FadeUp>

              <FadeUp useWhileInView={false} className="mt-5">
                <p className="text-[18px] leading-[1.7] text-[var(--color-muted-foreground)] max-w-[560px]">
                  {content.hero.subheading}
                </p>
              </FadeUp>

              <FadeUp useWhileInView={false} className="mt-8">
                <div className="flex flex-wrap justify-start gap-3">
                  <Button size="lg" asChild>
                    <a href="#waitlist">{content.hero.ctaPrimary}</a>
                  </Button>
                  <Button variant="outline" size="lg" asChild>
                    <a href="#how-it-works">{content.hero.ctaSecondary}</a>
                  </Button>
                </div>
              </FadeUp>

              <FadeUp useWhileInView={false} className="mt-4">
                <div className="text-[13px] text-[var(--color-muted-foreground)]">
                  {content.hero.socialProof}
                </div>
              </FadeUp>
            </StaggerGroup>
          </div>
        </div>

        <div className="hidden w-[440px] space-y-4 md:block">
          <TerminalBlink
            title={content.hero.terminalTitle}
            lines={content.hero.terminalLines}
          />
          <SystemVector />
        </div>
      </div>
    </section>
  );
}
