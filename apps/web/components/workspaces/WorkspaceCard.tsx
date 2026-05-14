"use client";

// Phase 3.5 Stage 7 — Home page workspace summary card.
//
// Mirrors the existing org card on /console: shows name, region, a
// status dot, and a relative timestamp. Owners get an inline Suspend
// button. We compute relative-time client-side so the rendered string
// updates if the user leaves the tab open.

import * as React from "react";
import { useRouter } from "next/navigation";
import { Loader2 } from "lucide-react";

import { Button } from "@/components/ui/Button";
import { workspaces, type Workspace, type WorkspaceStatus } from "@/lib/workspaces";
import { cn } from "@/lib/utils";

const DOT_COLOR: Record<WorkspaceStatus, string> = {
  ready: "bg-emerald-500",
  provisioning: "bg-amber-500",
  error: "bg-red-500",
  suspended: "bg-[var(--color-muted-foreground)]/40",
};

const STATUS_LABEL: Record<WorkspaceStatus, string> = {
  ready: "Ready",
  provisioning: "Provisioning",
  error: "Error",
  suspended: "Suspended",
};

function relativeTime(iso: string): string {
  const then = new Date(iso).getTime();
  if (Number.isNaN(then)) return iso;
  const diff = Math.max(0, Date.now() - then);
  const m = Math.floor(diff / 60_000);
  if (m < 1) return "just now";
  if (m < 60) return `${m} minute${m === 1 ? "" : "s"} ago`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h} hour${h === 1 ? "" : "s"} ago`;
  const d = Math.floor(h / 24);
  return `${d} day${d === 1 ? "" : "s"} ago`;
}

export function WorkspaceCard({
  workspace,
  isOwner,
}: {
  workspace: Workspace;
  isOwner: boolean;
}) {
  const router = useRouter();
  const [busy, setBusy] = React.useState(false);

  async function suspend() {
    if (!confirm(`Suspend workspace "${workspace.name}"? You can recreate later.`)) {
      return;
    }
    setBusy(true);
    try {
      await workspaces.suspend(workspace.id);
      router.refresh();
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-5">
      <div className="flex items-start justify-between gap-4">
        <div className="min-w-0">
          <p className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]">
            Workspace
          </p>
          <h2 className="mt-1 truncate text-lg font-semibold text-[var(--color-foreground)]">
            {workspace.name}
          </h2>
          <p className="mt-1 inline-flex items-center gap-2 text-xs text-[var(--color-muted-foreground)]">
            <span
              aria-hidden
              className={cn("inline-block h-2 w-2 rounded-full", DOT_COLOR[workspace.status])}
            />
            <span>{STATUS_LABEL[workspace.status]}</span>
            <span>·</span>
            <span className="font-mono">{workspace.region}</span>
          </p>
          <p className="mt-2 text-xs text-[var(--color-muted-foreground)]">
            Created {relativeTime(workspace.created_at)}
          </p>
        </div>
        {isOwner && workspace.status !== "suspended" && (
          <Button
            type="button"
            size="sm"
            variant="outline"
            disabled={busy}
            onClick={suspend}
          >
            {busy && <Loader2 className="h-4 w-4 animate-spin" />}
            Suspend
          </Button>
        )}
      </div>
    </div>
  );
}
