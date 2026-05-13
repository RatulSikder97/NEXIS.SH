"use client";

import Link from "next/link";
import type { Route } from "next";
import { ArrowRight } from "lucide-react";

import { content } from "@/lib/content";

import { Button } from "@/components/ui/Button";
import { SectionLabel } from "@/components/ui/SectionLabel";
import { StaggerGroup } from "@/components/animations/StaggerGroup";
import { FadeUp } from "@/components/animations/FadeUp";
import { TerminalBlink } from "@/components/ui/TerminalBlink";
import { SystemVector } from "@/components/ui/SystemVector";

export default function Hero({ signedIn = false }: { signedIn?: boolean }) {
  return (
    <section
      id="features"
      className="relative w-full min-h-[100vh] flex items-center py-[120px] scroll-mt-[88px] bg-[var(--color-background)] overflow-hidden"
    >
      {/* Soft accent wash — gives light-mode hero depth that's missing on a
          flat white background. Two stacked radial gradients (top + bottom-
          right) plus a subtle dot grid. */}
      <div
        aria-hidden
        className="pointer-events-none absolute inset-0 bg-[radial-gradient(ellipse_at_top,_color-mix(in_srgb,var(--color-primary)_12%,transparent)_0%,_transparent_55%)]"
      />
      <div
        aria-hidden
        className="pointer-events-none absolute -right-32 -bottom-32 h-[460px] w-[460px] rounded-full bg-[radial-gradient(circle,_color-mix(in_srgb,var(--color-primary)_18%,transparent)_0%,_transparent_70%)] blur-2xl"
      />
      <div
        aria-hidden
        className="pointer-events-none absolute inset-0 opacity-[0.025] [background-image:radial-gradient(var(--color-foreground)_1px,transparent_1px)] [background-size:24px_24px]"
      />

      <div className="relative z-10 mx-auto flex max-w-[1200px] flex-col gap-16 px-6 md:flex-row md:items-start md:justify-between md:gap-20">
        <div className="flex-1">
          <div className="min-h-[calc(100vh-240px)] flex flex-col justify-center">
            <StaggerGroup preset="hero" mode="immediate">
              {/* Status pill — small kicker above the eyebrow. Links to /status
                  so visitors who want uptime evidence land there directly. */}
              <FadeUp useWhileInView={false}>
                <Link
                  href={content.hero.statusPill.href as Route}
                  className="inline-flex items-center gap-2 rounded-full border border-[var(--color-border)] bg-[var(--color-card)] px-3 py-1 text-[12px] font-medium text-[var(--color-muted-foreground)] shadow-sm transition-colors hover:bg-[var(--color-muted)] hover:text-[var(--color-foreground)]"
                >
                  <span className="relative flex h-2 w-2">
                    <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-[var(--color-success)] opacity-75" />
                    <span className="relative inline-flex h-2 w-2 rounded-full bg-[var(--color-success)]" />
                  </span>
                  {content.hero.statusPill.label}
                  <ArrowRight className="h-3 w-3" />
                </Link>
              </FadeUp>

              <FadeUp useWhileInView={false} className="mt-5">
                <SectionLabel>{content.hero.preHeading}</SectionLabel>
              </FadeUp>

              <FadeUp useWhileInView={false} className="mt-4">
                <h1 className="text-[40px] leading-[1.05] font-medium tracking-tight text-[var(--color-foreground)] md:text-[64px]">
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
                    <Link href={"/sign-up" as Route}>{content.hero.ctaPrimary}</Link>
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

      {/* Skip to dashboard — only shown to signed-in visitors so existing
          users have a quick path back to /console without scrolling. */}
      {signedIn ? (
        <Link
          href={"/console/dashboard" as Route}
          className="absolute bottom-6 right-6 z-10 hidden md:inline-flex items-center gap-2 rounded-full border border-[var(--color-border)] bg-[var(--color-card)]/90 px-4 py-2 text-[13px] font-medium text-[var(--color-foreground)] shadow-sm backdrop-blur-sm transition-colors hover:bg-[var(--color-card)]"
        >
          Skip to dashboard
          <ArrowRight className="h-3.5 w-3.5" />
        </Link>
      ) : null}
    </section>
  );
}
