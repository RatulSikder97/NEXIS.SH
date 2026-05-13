// /changelog — Reverse-chronological release log. Mock entries spanning
// v0.1.0–v0.9.0 with feature/fix/breaking labels and multi-line bullets.
// Server component; no client state needed.
import type { Metadata } from "next";

import { MarketingShell } from "@/components/marketing/MarketingShell";
import { PageHero } from "@/components/marketing/PageHero";
import { Section, SectionInner } from "@/components/marketing/SectionContainer";

export const metadata: Metadata = {
  title: "Changelog — Releases",
  description:
    "Reverse-chronological release log for NEXIS. Features, fixes, breaking changes.",
};

type Tag = "feature" | "fix" | "breaking" | "perf" | "security";

type Entry = {
  version: string;
  date: string;
  title: string;
  tags: Tag[];
  bullets: string[];
};

const ENTRIES: Entry[] = [
  {
    version: "v0.9.0",
    date: "2026-05-12",
    title: "Live preview proxy + status page hardening",
    tags: ["feature", "perf"],
    bullets: [
      "New /v1/system-status endpoint exposes 28 integration probes and 6 subsystem health gauges.",
      "Approval Gate latency dropped 40% — median 0.4s, p95 1.1s.",
      "ProviderLogo grid hover state is now grayscale → colour to give the integrations grid more life.",
    ],
  },
  {
    version: "v0.8.2",
    date: "2026-05-06",
    title: "Theme persistence + light-mode hardening",
    tags: ["feature", "fix"],
    bullets: [
      "next-themes preference now hydrates from server cookie so first paint never flashes the wrong theme.",
      "Fix: live demo terminal contrast in light mode (was using dark-only stroke colour).",
      "Fix: HealthPill component no longer crashes when the integration response omits the new health block.",
    ],
  },
  {
    version: "v0.8.0",
    date: "2026-04-22",
    title: "Stripe checkout for Pro tier",
    tags: ["feature"],
    bullets: [
      "Wallet → Pro tier upgrade now flows through Stripe Checkout with idempotent webhook handling.",
      "Monthly invoices are line-itemed by agent (Synthesiser tokens vs QA tokens vs Approval Gate runs).",
      "New /console/billing page surfaces upcoming charges and proration calculations live.",
    ],
  },
  {
    version: "v0.7.5",
    date: "2026-04-10",
    title: "Eval-driven incident replay",
    tags: ["feature", "perf"],
    bullets: [
      "Replay an incident from /console/incidents/[id] with one click — useful for tuning agent prompts.",
      "Eval framework now reads NASA-TLX cognitive-load survey responses to tune approval routing.",
      "30% reduction in QA agent token usage via prompt-cache reuse across siblings.",
    ],
  },
  {
    version: "v0.7.0",
    date: "2026-03-28",
    title: "Real-time pipeline canvas",
    tags: ["feature"],
    bullets: [
      "/console/incidents/[id]/pipeline now renders agent hand-offs live via WebSocket.",
      "Hover a node to see its input + output JSON.",
      "Filter pipeline events by severity + agent in the sidebar.",
    ],
  },
  {
    version: "v0.6.0",
    date: "2026-03-12",
    title: "Wave 1 integrations: Datadog + PagerDuty",
    tags: ["feature"],
    bullets: [
      "Datadog adapter ships with metric + monitor APIs wired into Sentinel.",
      "PagerDuty escalation policies surface in /console/integrations.",
      "Slack adapter now supports threaded incident channels (one per incident_id).",
    ],
  },
  {
    version: "v0.5.4",
    date: "2026-02-26",
    title: "ArgoCD adapter v2 + auto-rollback",
    tags: ["feature", "fix"],
    bullets: [
      "ArgoCD adapter now syncs application health every 30s during recovery.",
      "Auto-rollback fires when post-deploy health checks fail within 10 minutes.",
      "Fix: project deletion no longer leaves orphaned integration rows.",
    ],
  },
  {
    version: "v0.5.0",
    date: "2026-02-08",
    title: "Audit log + RBAC GA",
    tags: ["feature", "breaking"],
    bullets: [
      "Append-only audit log lands in /console/audit with full agent hand-off history.",
      "RBAC: per-project + per-workspace roles (Owner, Approver, Member, Viewer).",
      "BREAKING: /v1/integrations response shape now includes a `health` block (Wave 1).",
    ],
  },
  {
    version: "v0.4.0",
    date: "2026-01-19",
    title: "Auth hardening — MFA + magic-link",
    tags: ["security", "feature"],
    bullets: [
      "TOTP-backed MFA via otplib; QR-code enrolment on /auth/mfa.",
      "Magic-link sign-in flow for email-only orgs.",
      "Session cookie now httpOnly + SameSite=Lax + Secure in production.",
    ],
  },
  {
    version: "v0.3.0",
    date: "2026-01-04",
    title: "Console redesign + light-mode default",
    tags: ["feature", "breaking"],
    bullets: [
      "Light mode is now the default theme for new visitors (matches docs/PROJECT_PLAN §4.1).",
      "/console layout split into (app), (auth), (console) route groups for cleaner middleware.",
      "BREAKING: removed deprecated /dashboard route — use /console/dashboard.",
    ],
  },
  {
    version: "v0.2.0",
    date: "2025-12-12",
    title: "Synthesiser + QA agents",
    tags: ["feature"],
    bullets: [
      "Synthesiser ships with retrieval over recent diffs + design docs.",
      "QA agent generates unit + property test suites per candidate.",
      "Shadow pipeline runs in a Docker-in-Docker sandbox with no network egress.",
    ],
  },
  {
    version: "v0.1.0",
    date: "2025-11-08",
    title: "Initial public beta",
    tags: ["feature"],
    bullets: [
      "Sentinel + Pathfinder agents ship with Sentry + GitHub integrations.",
      "Synthetic incidents only — production ingestion gated behind workspace owner approval.",
      "Free tier with 1 workspace + 3 projects.",
    ],
  },
];

const TAG_STYLE: Record<Tag, string> = {
  feature: "bg-[color-mix(in_srgb,var(--color-primary)_15%,transparent)] text-[var(--color-primary)]",
  fix: "bg-[color-mix(in_srgb,var(--color-success)_18%,transparent)] text-[var(--color-success)]",
  breaking: "bg-[color-mix(in_srgb,var(--color-destructive)_15%,transparent)] text-[var(--color-destructive)]",
  perf: "bg-[color-mix(in_srgb,var(--color-warning)_18%,transparent)] text-[var(--color-warning)]",
  security: "bg-[var(--color-muted)] text-[var(--color-foreground)]",
};

export default function ChangelogPage() {
  return (
    <MarketingShell>
      <PageHero
        eyebrow="CHANGELOG"
        title="Every release, every change, every fix."
        lead="A reverse-chronological log of what we shipped. Subscribe via the GitHub releases feed to stay current."
      />

      <Section tone="background">
        <SectionInner>
          <ol className="relative space-y-12 pl-6 md:pl-10 border-l border-[var(--color-border)]">
            {ENTRIES.map((entry) => (
              <li key={entry.version} className="relative">
                <span
                  className="absolute -left-[33px] md:-left-[45px] top-1 flex h-4 w-4 items-center justify-center rounded-full border border-[var(--color-border)] bg-[var(--color-card)]"
                  aria-hidden
                >
                  <span className="h-2 w-2 rounded-full bg-[var(--color-primary)]" />
                </span>
                <header className="flex flex-wrap items-baseline gap-x-4 gap-y-1">
                  <span className="font-mono text-[14px] font-medium text-[var(--color-primary)]">
                    {entry.version}
                  </span>
                  <time className="text-[12px] text-[var(--color-muted-foreground)]">
                    {entry.date}
                  </time>
                  <div className="flex flex-wrap gap-1.5">
                    {entry.tags.map((t) => (
                      <span
                        key={t}
                        className={`inline-flex items-center rounded-full px-2 py-0.5 text-[10px] font-medium uppercase tracking-[0.1em] ${TAG_STYLE[t]}`}
                      >
                        {t}
                      </span>
                    ))}
                  </div>
                </header>
                <h2 className="mt-2 text-[20px] font-medium leading-[1.3] tracking-tight text-[var(--color-foreground)]">
                  {entry.title}
                </h2>
                <ul className="mt-4 space-y-2 text-[14px] leading-[1.7] text-[var(--color-muted-foreground)] list-disc pl-5">
                  {entry.bullets.map((b) => (
                    <li key={b}>{b}</li>
                  ))}
                </ul>
              </li>
            ))}
          </ol>
        </SectionInner>
      </Section>
    </MarketingShell>
  );
}
