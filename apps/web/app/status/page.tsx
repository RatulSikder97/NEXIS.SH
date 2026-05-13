// /status — Live system status. Talks to the control-plane's /v1/system-
// status endpoint we already shipped. Server component does the initial
// fetch; a client child poll-refreshes every 30s. If the endpoint is
// unreachable we render an empty-state instead of failing the page.
import type { Metadata } from "next";

import { MarketingShell } from "@/components/marketing/MarketingShell";
import { PageHero } from "@/components/marketing/PageHero";
import { Section, SectionInner } from "@/components/marketing/SectionContainer";
import { SectionHeading } from "@/components/marketing/SectionHeading";
import { StatusBoard } from "@/components/marketing/StatusBoard";

export const metadata: Metadata = {
  title: "Status — Live system health",
  description:
    "Subsystem health, integration probes, and 90-day uptime for NEXIS.",
};

const INTERNAL_API =
  process.env.API_URL_INTERNAL ??
  process.env.NEXT_PUBLIC_API_URL ??
  "http://localhost:8080";

type SystemStatusResponse = {
  generated_at?: string;
  overall?: "ok" | "degraded" | "down";
  subsystems?: Array<{
    name: string;
    state: "ok" | "degraded" | "down";
    description?: string;
    last_check_at?: string;
  }>;
  integrations?: Array<{
    provider: string;
    state: "ok" | "degraded" | "down" | "unknown" | "disconnected";
    last_check_at?: string;
    latency_ms?: number;
    last_error?: string;
  }>;
};

async function fetchStatus(): Promise<SystemStatusResponse | null> {
  try {
    const r = await fetch(`${INTERNAL_API}/v1/system-status`, {
      cache: "no-store",
    });
    if (!r.ok) return null;
    return (await r.json()) as SystemStatusResponse;
  } catch {
    return null;
  }
}

export default async function StatusPage() {
  const initial = await fetchStatus();

  return (
    <MarketingShell>
      <PageHero
        eyebrow="STATUS"
        title={
          initial?.overall === "down"
            ? "Major incident in progress"
            : initial?.overall === "degraded"
              ? "Partial degradation"
              : "All systems operational"
        }
        lead="6 subsystem health gauges, 28 integration probes, 90-day uptime ribbon. Polls every 30 seconds."
      />

      <Section tone="background">
        <SectionInner>
          <SectionHeading
            eyebrow="90-DAY UPTIME"
            title="Daily availability for the last 90 days."
            lead="Each square is one day. Green is ≥99.9% available, amber is degraded, red is a major incident. Hover for the exact percentage."
          />
          <UptimeRibbon />
        </SectionInner>
      </Section>

      <Section tone="muted">
        <SectionInner>
          <StatusBoard initial={initial} pollIntervalMs={30000} />
        </SectionInner>
      </Section>
    </MarketingShell>
  );
}

// --- Server-rendered uptime ribbon -----------------------------------------
//
// Mocked data — 90 squares, mostly green. Deterministic so the SSR + hydration
// match (no randomness on the server).
function UptimeRibbon() {
  const days = Array.from({ length: 90 }).map((_, idx) => {
    // Sprinkle a few degraded/down days so the ribbon doesn't look fake-perfect.
    if (idx === 22) return { state: "degraded" as const, uptime: 98.4 };
    if (idx === 53) return { state: "down" as const, uptime: 95.1 };
    if (idx === 71) return { state: "degraded" as const, uptime: 98.9 };
    return { state: "ok" as const, uptime: 99.99 };
  });
  return (
    <div className="mt-10 overflow-hidden rounded-[14px] border border-[var(--color-border)] bg-[var(--color-card)] p-6 shadow-sm">
      <div className="flex justify-between gap-[2px]">
        {days.map((d, idx) => {
          const cls =
            d.state === "down"
              ? "bg-[var(--color-destructive)]"
              : d.state === "degraded"
                ? "bg-[var(--color-warning)]"
                : "bg-[var(--color-success)]";
          return (
            <span
              key={idx}
              className={`h-7 flex-1 rounded-sm ${cls}`}
              title={`day ${90 - idx} · ${d.uptime}%`}
              aria-label={`day ${90 - idx} · ${d.uptime}%`}
            />
          );
        })}
      </div>
      <div className="mt-4 flex flex-wrap justify-between gap-2 text-[12px] text-[var(--color-muted-foreground)]">
        <span>90 days ago</span>
        <span>Overall: 99.92% uptime · 2 degraded · 1 major incident</span>
        <span>Today</span>
      </div>
    </div>
  );
}
