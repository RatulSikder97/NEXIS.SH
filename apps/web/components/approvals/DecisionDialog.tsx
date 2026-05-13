"use client";

// Phase 6 Stage 9 — DecisionDialog.
//
// Small Radix Dialog wrapping a single optional `notes` textarea + confirm /
// cancel buttons. The caller decides whether the dialog is an approve or a
// reject by passing `kind`; the button colour + label flip to match.
//
// The form submits to the parent via `onConfirm(notes)`. The parent is
// responsible for calling approvals.approve / approvals.reject and removing
// the row from the table on success. We expose a `pending` flag so the parent
// can disable the confirm button while the POST is in flight.

import * as React from "react";
import * as Dialog from "@radix-ui/react-dialog";
import { Loader2, X } from "lucide-react";

import { Button } from "@/components/ui/Button";
import { cn } from "@/lib/utils";

export type DecisionKind = "approve" | "reject";

export function DecisionDialog({
  open,
  onOpenChange,
  kind,
  runIdShort,
  scenario,
  pending,
  error,
  onConfirm,
}: {
  open: boolean;
  onOpenChange: (next: boolean) => void;
  kind: DecisionKind;
  runIdShort: string;
  scenario: string | null;
  pending: boolean;
  error: string | null;
  onConfirm: (notes: string) => void | Promise<void>;
}) {
  const [notes, setNotes] = React.useState("");

  // Reset notes whenever the dialog re-opens so a previous reject's
  // explanation doesn't leak into a fresh approve.
  React.useEffect(() => {
    if (open) setNotes("");
  }, [open]);

  const title =
    kind === "approve"
      ? `Approve run ${runIdShort}`
      : `Reject run ${runIdShort}`;
  const description =
    kind === "approve"
      ? "Signal the workflow to proceed to GitOps. The patch will be opened as a PR."
      : "Signal the workflow to abort. The pipeline will exit with status=failed and no PR will be opened.";
  const confirmLabel = kind === "approve" ? "Approve" : "Reject";

  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-40 bg-black/40 backdrop-blur-sm" />
        <Dialog.Content className="fixed left-1/2 top-1/2 z-50 w-[min(480px,calc(100vw-2rem))] -translate-x-1/2 -translate-y-1/2 rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-6 shadow-xl outline-none">
          <div className="flex items-start justify-between gap-3">
            <div className="min-w-0">
              <Dialog.Title className="text-lg font-semibold">
                {title}
              </Dialog.Title>
              <Dialog.Description className="mt-1 text-xs text-[var(--color-muted-foreground)]">
                {description}
              </Dialog.Description>
              {scenario && (
                <p className="mt-1 text-xs text-[var(--color-muted-foreground)]">
                  Scenario:{" "}
                  <span className="font-mono text-[var(--color-foreground)]">
                    {scenario}
                  </span>
                </p>
              )}
            </div>
            <Dialog.Close asChild>
              <button
                type="button"
                className="rounded-md p-1 text-[var(--color-muted-foreground)] hover:bg-[var(--color-muted)] hover:text-[var(--color-foreground)]"
                aria-label="Close"
              >
                <X className="h-4 w-4" />
              </button>
            </Dialog.Close>
          </div>

          <form
            className="mt-4 space-y-3"
            onSubmit={(e) => {
              e.preventDefault();
              if (pending) return;
              void onConfirm(notes.trim());
            }}
          >
            <label className="block text-xs font-medium text-[var(--color-muted-foreground)]">
              Notes <span className="font-normal">(optional)</span>
              <textarea
                value={notes}
                onChange={(e) => setNotes(e.target.value)}
                rows={3}
                placeholder={
                  kind === "approve"
                    ? "e.g. patch reviewed, tests green"
                    : "e.g. rolling back, plan rerun"
                }
                className="mt-1 block w-full resize-none rounded-md border border-[var(--color-border)] bg-[var(--color-background)] px-3 py-2 text-sm text-[var(--color-foreground)] placeholder:text-[var(--color-muted-foreground)] focus:outline-none focus:ring-2 focus:ring-[var(--color-ring)]"
                disabled={pending}
              />
            </label>
            {error && (
              <p
                role="alert"
                className="rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-xs text-red-700 dark:text-red-300"
              >
                {error}
              </p>
            )}
            <div className="flex items-center justify-end gap-2 pt-2">
              <Dialog.Close asChild>
                <Button type="button" variant="ghost" size="sm" disabled={pending}>
                  Cancel
                </Button>
              </Dialog.Close>
              <Button
                type="submit"
                size="sm"
                disabled={pending}
                className={cn(
                  kind === "reject" &&
                    "!bg-red-600 !text-white hover:!opacity-90",
                )}
              >
                {pending && <Loader2 className="h-4 w-4 animate-spin" />}
                {confirmLabel}
              </Button>
            </div>
          </form>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
