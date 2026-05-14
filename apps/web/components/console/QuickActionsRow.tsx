"use client";

// QuickActionsRow — single-row affordance bar rendered on the dashboard.
// Each action is a button that fires its specific verb without leaving the
// page:
//   * "Trigger sample incident" → POST /v1/workspaces/{ws}/pipelines/demo
//     then navigate to /console/incidents/{id} (DEV only — 404s in prod).
//   * "Invite member" → link to /console/settings/members.
//   * "Connect GitHub" → triggers integrations.mockInstallGithub() (dev-only
//     install path) — the link below routes to /console/integrations for
//     the manual config flow.
//   * "View docs" → link to /docs.
//
// On error (e.g. demo route returns 404 in prod), the action degrades to a
// muted inline message — no toast wiring needed.

import * as React from "react";
import Link from "next/link";
import type { Route } from "next";
import { useRouter } from "next/navigation";
import {
  BookOpen,
  Github,
  PlayCircle,
  UserPlus,
  type LucideIcon,
} from "lucide-react";

import { cn } from "@/lib/utils";
import { pipelines } from "@/lib/pipelines";
import { integrations } from "@/lib/integrations";

const COOKIE_WORKSPACE = "nexis_workspace";

function readWorkspaceCookie(): string | null {
  if (typeof document === "undefined") return null;
  const m = document.cookie.match(
    new RegExp(
      "(?:^|; )" +
        COOKIE_WORKSPACE.replace(/[.$?*|{}()[\]\\/+^]/g, "\\$&") +
        "=([^;]*)",
    ),
  );
  return m ? decodeURIComponent(m[1]) : null;
}

function ActionButton({
  icon: Icon,
  label,
  description,
  onClick,
  href,
  externalHref,
  disabled,
  pending,
}: {
  icon: LucideIcon;
  label: string;
  description: string;
  onClick?: () => void;
  href?: string;
  externalHref?: string;
  disabled?: boolean;
  pending?: boolean;
}) {
  const inner = (
    <div
      className={cn(
        "flex items-start gap-3 rounded-md border border-[var(--color-border)] bg-[var(--color-card)] px-4 py-3 text-left transition-colors",
        disabled
          ? "cursor-not-allowed opacity-60"
          : "hover:border-[var(--color-primary)]/40 hover:bg-[var(--color-muted)]/40",
      )}
    >
      <span className="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-[var(--color-muted)] text-[var(--color-muted-foreground)]">
        <Icon className="h-4 w-4" />
      </span>
      <div className="min-w-0 flex-1">
        <p className="truncate text-sm font-medium text-[var(--color-foreground)]">
          {label}
        </p>
        <p className="mt-0.5 truncate text-[11px] text-[var(--color-muted-foreground)]">
          {pending ? "Working…" : description}
        </p>
      </div>
    </div>
  );
  if (externalHref && !disabled) {
    return (
      <a
        href={externalHref}
        target="_blank"
        rel="noopener noreferrer"
        className="block"
      >
        {inner}
      </a>
    );
  }
  if (href && !disabled) {
    return (
      <Link href={href as Route} className="block">
        {inner}
      </Link>
    );
  }
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled || pending}
      className="block w-full text-left"
    >
      {inner}
    </button>
  );
}

export function QuickActionsRow() {
  const router = useRouter();
  const [demoPending, setDemoPending] = React.useState(false);
  const [demoError, setDemoError] = React.useState<string | null>(null);

  async function runDemo() {
    const wsId = readWorkspaceCookie();
    if (!wsId) {
      setDemoError("No workspace selected.");
      return;
    }
    setDemoPending(true);
    setDemoError(null);
    try {
      const run = await pipelines.createDemo(wsId, {
        scenario: "schema-drift",
        source: "dashboard",
      });
      router.push(`/console/incidents/${run.id}` as Route);
    } catch (err) {
      setDemoError(err instanceof Error ? err.message : "Demo unavailable");
    } finally {
      setDemoPending(false);
    }
  }

  function connectGithub() {
    integrations.mockInstallGithub();
  }

  return (
    <section aria-labelledby="quick-actions-heading" className="space-y-2">
      <h2
        id="quick-actions-heading"
        className="text-[10px] font-semibold uppercase tracking-widest text-[var(--color-muted-foreground)]"
      >
        Quick actions
      </h2>
      <div className="grid grid-cols-1 gap-2 md:grid-cols-2 xl:grid-cols-4">
        <ActionButton
          icon={PlayCircle}
          label="Trigger sample incident"
          description={
            demoError ?? "Run a synthetic recovery flow end-to-end"
          }
          onClick={runDemo}
          pending={demoPending}
        />
        <ActionButton
          icon={UserPlus}
          label="Invite member"
          description="Add a teammate to this organisation"
          href="/console/settings/members"
        />
        <ActionButton
          icon={Github}
          label="Connect GitHub"
          description="Wire PR creation + repo metadata"
          onClick={connectGithub}
        />
        <ActionButton
          icon={BookOpen}
          label="View docs"
          description="Operator guide + API reference"
          externalHref="https://github.com/nexis-eco"
        />
      </div>
    </section>
  );
}
