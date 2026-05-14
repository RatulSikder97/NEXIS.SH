"use client";

// Phase 3.5 Stage 8 — Workspaces settings client.
//
// Table of every workspace in the org + a primary "Create workspace"
// button that opens a Radix Dialog. The dialog reuses the onboarding
// form (RegionPicker + name input) and switches to the
// ProvisioningAnimation once create returns. On ready, the dialog
// closes itself and refreshes the route so the table picks up the new
// row. Suspend lives inline on each row; owners only.

import * as React from "react";
import * as Dialog from "@radix-ui/react-dialog";
import { Boxes, Loader2, X } from "lucide-react";
import { useRouter } from "next/navigation";

import { Button } from "@/components/ui/Button";
import { EmptyState } from "@/components/empty-state/EmptyState";
import { RegionPicker } from "@/components/onboarding/RegionPicker";
import { ProvisioningAnimation } from "@/components/onboarding/ProvisioningAnimation";
import {
  workspaces,
  type Region,
  type Workspace,
  type WorkspaceStatus,
} from "@/lib/workspaces";
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

function formatDate(iso: string): string {
  try {
    return new Date(iso).toLocaleString(undefined, {
      month: "short",
      day: "numeric",
      year: "numeric",
    });
  } catch {
    return iso;
  }
}

type DialogPhase =
  | { kind: "form"; name: string; region: string | null; error: string | null }
  | { kind: "running"; ws: Workspace };

export function WorkspacesSettingsClient({
  initial,
  regions,
  role,
}: {
  initial: Workspace[];
  regions: Region[];
  role: "owner" | "admin" | "member";
}) {
  const router = useRouter();
  const [open, setOpen] = React.useState(false);
  const [phase, setPhase] = React.useState<DialogPhase>({
    kind: "form",
    name: "",
    region: regions[0]?.id ?? null,
    error: null,
  });
  const [submitting, setSubmitting] = React.useState(false);
  const [suspending, setSuspending] = React.useState<string | null>(null);

  const canCreate = role === "owner" || role === "admin";
  const canSuspend = role === "owner";

  function resetDialog() {
    setPhase({
      kind: "form",
      name: "",
      region: regions[0]?.id ?? null,
      error: null,
    });
  }

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    if (phase.kind !== "form" || !phase.region) return;
    setSubmitting(true);
    try {
      const ws = await workspaces.create(phase.name.trim(), phase.region);
      setPhase({ kind: "running", ws });
    } catch (err) {
      setPhase((prev) =>
        prev.kind === "form"
          ? { ...prev, error: err instanceof Error ? err.message : "Failed" }
          : prev,
      );
    } finally {
      setSubmitting(false);
    }
  }

  function onReady() {
    setOpen(false);
    resetDialog();
    router.refresh();
  }

  function onError(message: string) {
    setPhase({
      kind: "form",
      name: "",
      region: regions[0]?.id ?? null,
      error: message,
    });
  }

  async function suspend(id: string, name: string) {
    if (!confirm(`Suspend workspace "${name}"? This is reversible by an admin.`)) {
      return;
    }
    setSuspending(id);
    try {
      await workspaces.suspend(id);
      router.refresh();
    } finally {
      setSuspending(null);
    }
  }

  return (
    <div className="space-y-8">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold">Workspaces</h1>
          <p className="mt-1 text-sm text-[var(--color-muted-foreground)]">
            Provision isolated environments for each team or stage. Each
            workspace is regionally pinned.
          </p>
        </div>
        {canCreate && (
          <Button
            type="button"
            onClick={() => {
              resetDialog();
              setOpen(true);
            }}
          >
            Create workspace
          </Button>
        )}
      </div>

      {initial.length === 0 ? (
        <EmptyState
          icon={Boxes}
          title="No workspaces yet"
          description="Provision an isolated environment for each team or stage. Each workspace is regionally pinned."
          cta={
            canCreate && (
              <Button
                size="sm"
                type="button"
                onClick={() => {
                  resetDialog();
                  setOpen(true);
                }}
              >
                Create workspace
              </Button>
            )
          }
        />
      ) : (
        <div className="overflow-hidden rounded-lg border border-[var(--color-border)] bg-[var(--color-card)]">
          <table className="w-full text-sm">
            <thead className="bg-[var(--color-muted)]/40 text-left text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]">
              <tr>
                <th className="px-4 py-2 font-medium">Name</th>
                <th className="px-4 py-2 font-medium">Region</th>
                <th className="px-4 py-2 font-medium">Status</th>
                <th className="px-4 py-2 font-medium">Created</th>
                <th className="px-4 py-2 font-medium" aria-hidden></th>
              </tr>
            </thead>
            <tbody>
              {initial.map((w) => (
                <tr key={w.id} className="border-t border-[var(--color-border)]">
                  <td className="px-4 py-3 font-medium">{w.name}</td>
                  <td className="px-4 py-3 font-mono text-xs text-[var(--color-muted-foreground)]">
                    {w.region}
                  </td>
                  <td className="px-4 py-3">
                    <span className="inline-flex items-center gap-2">
                      <span
                        aria-hidden
                        className={cn(
                          "inline-block h-2 w-2 rounded-full",
                          DOT_COLOR[w.status],
                        )}
                      />
                      {STATUS_LABEL[w.status]}
                    </span>
                  </td>
                  <td className="px-4 py-3 text-[var(--color-muted-foreground)]">
                    {formatDate(w.created_at)}
                  </td>
                  <td className="px-4 py-3 text-right">
                    {canSuspend && w.status !== "suspended" && (
                      <Button
                        size="sm"
                        variant="outline"
                        type="button"
                        disabled={suspending === w.id}
                        onClick={() => suspend(w.id, w.name)}
                      >
                        {suspending === w.id && (
                          <Loader2 className="h-4 w-4 animate-spin" />
                        )}
                        Suspend
                      </Button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <Dialog.Root
        open={open}
        onOpenChange={(o) => {
          // While provisioning is running, ignore outside-click dismissals;
          // the user gets to see the animation through to ready/error.
          if (!o && phase.kind === "running") return;
          setOpen(o);
          if (!o) resetDialog();
        }}
      >
        <Dialog.Portal>
          <Dialog.Overlay className="fixed inset-0 z-40 bg-black/40 backdrop-blur-sm" />
          <Dialog.Content className="fixed left-1/2 top-1/2 z-50 w-[min(640px,calc(100vw-2rem))] -translate-x-1/2 -translate-y-1/2 rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-6 shadow-xl outline-none">
            <div className="flex items-start justify-between gap-4 pb-4">
              <div>
                <Dialog.Title className="text-lg font-semibold">
                  {phase.kind === "form"
                    ? "Create workspace"
                    : `Provisioning ${phase.ws.name}`}
                </Dialog.Title>
                <Dialog.Description className="mt-1 text-xs text-[var(--color-muted-foreground)]">
                  {phase.kind === "form"
                    ? "Pick a name and a region. You can suspend or recreate later."
                    : `Region: ${phase.ws.region}. This usually takes under a minute.`}
                </Dialog.Description>
              </div>
              {phase.kind === "form" && (
                <Dialog.Close asChild>
                  <button
                    type="button"
                    aria-label="Close"
                    className="inline-flex h-8 w-8 items-center justify-center rounded-md text-[var(--color-muted-foreground)] hover:bg-[var(--color-muted)] hover:text-[var(--color-foreground)]"
                  >
                    <X className="h-4 w-4" />
                  </button>
                </Dialog.Close>
              )}
            </div>

            {phase.kind === "form" ? (
              <form onSubmit={submit} className="space-y-5">
                <div className="space-y-1">
                  <label
                    htmlFor="settings-ws-name"
                    className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]"
                  >
                    Workspace name
                  </label>
                  <input
                    id="settings-ws-name"
                    type="text"
                    required
                    minLength={2}
                    maxLength={64}
                    value={phase.name}
                    onChange={(e) =>
                      setPhase({ ...phase, name: e.target.value, error: null })
                    }
                    placeholder="Staging"
                    className="w-full rounded-md border border-[var(--color-border)] bg-[var(--color-background)] px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-ring)]"
                  />
                </div>
                <div className="space-y-2">
                  <p className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]">
                    Region
                  </p>
                  <RegionPicker
                    regions={regions}
                    selected={phase.region}
                    onSelect={(id) =>
                      setPhase({ ...phase, region: id, error: null })
                    }
                  />
                </div>
                {phase.error && (
                  <div
                    role="alert"
                    className="rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-sm text-red-700 dark:text-red-300"
                  >
                    {phase.error}
                  </div>
                )}
                <div className="flex justify-end gap-2 pt-2">
                  <Button
                    type="button"
                    variant="ghost"
                    disabled={submitting}
                    onClick={() => setOpen(false)}
                  >
                    Cancel
                  </Button>
                  <Button
                    type="submit"
                    disabled={submitting || !phase.name.trim() || !phase.region}
                  >
                    {submitting && <Loader2 className="h-4 w-4 animate-spin" />}
                    Create workspace
                  </Button>
                </div>
              </form>
            ) : (
              <ProvisioningAnimation
                workspaceId={phase.ws.id}
                name={phase.ws.name}
                region={phase.ws.region}
                onReady={onReady}
                onError={onError}
              />
            )}
          </Dialog.Content>
        </Dialog.Portal>
      </Dialog.Root>
    </div>
  );
}
