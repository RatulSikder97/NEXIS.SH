"use client";

// Phase 6 Stage 9 — DecisionDialog.
//
// Small Radix Dialog wrapping a single optional `notes` textarea + confirm /
// cancel buttons. The caller decides whether the dialog is an approve, a
// reject, or a modify by passing `kind`; the button colour + label flip to
// match. The RLHF `modify` kind adds a required unified-diff textarea
// (prefilled from `initialDiff` when the parent could resolve the pending
// patch) — the submitted diff replaces the agent's patch in the deploy and
// the (original, edited) pair is recorded as a training example.
//
// The form submits to the parent via `onConfirm(notes, modifiedDiff)` —
// `modifiedDiff` is only non-empty for kind="modify". The parent is
// responsible for calling approvals.approve / reject / modify and removing
// the row from the table on success. We expose a `pending` flag so the parent
// can disable the confirm button while the POST is in flight.

import * as React from "react";
import * as Dialog from "@radix-ui/react-dialog";
import { Loader2, X } from "lucide-react";

import { Button } from "@/components/ui/Button";
import { cn } from "@/lib/utils";

export type DecisionKind = "approve" | "reject" | "modify";

export function DecisionDialog({
  open,
  onOpenChange,
  kind,
  runIdShort,
  scenario,
  pending,
  error,
  onConfirm,
  initialDiff,
}: {
  open: boolean;
  onOpenChange: (next: boolean) => void;
  kind: DecisionKind;
  runIdShort: string;
  scenario: string | null;
  pending: boolean;
  error: string | null;
  onConfirm: (notes: string, modifiedDiff?: string) => void | Promise<void>;
  /** kind="modify" only: the agent's original diff to prefill the editor. */
  initialDiff?: string | null;
}) {
  const [notes, setNotes] = React.useState("");
  const [diff, setDiff] = React.useState("");

  // Initial focus target — the notes textarea. Without this Radix would
  // land focus on the Close button (the first tabbable child in source
  // order), putting a destructive control under the keyboard cursor the
  // moment the dialog opens. WCAG 2.4.3.
  const notesRef = React.useRef<HTMLTextAreaElement | null>(null);

  // Reset notes whenever the dialog re-opens so a previous reject's
  // explanation doesn't leak into a fresh approve. React-19's
  // react-hooks/set-state-in-effect rule forbids the effect-driven reset
  // pattern, so we use the React-blessed "adjust state during render by
  // comparing to previous prop" idiom instead.
  //   https://react.dev/learn/you-might-not-need-an-effect
  const [prevOpen, setPrevOpen] = React.useState(open);
  if (open !== prevOpen) {
    setPrevOpen(open);
    if (open) {
      setNotes("");
      setDiff(initialDiff ?? "");
    }
  }

  const isModify = kind === "modify";
  const diffMissing = isModify && diff.trim().length === 0;

  const title =
    kind === "approve"
      ? `Approve run ${runIdShort}`
      : kind === "modify"
        ? `Modify & approve run ${runIdShort}`
        : `Reject run ${runIdShort}`;
  const description =
    kind === "approve"
      ? "Signal the workflow to proceed to GitOps. The patch will be opened as a PR."
      : kind === "modify"
        ? "Edit the proposed patch below, then approve. The PR ships YOUR version of the diff, and the edit is recorded as agent feedback."
        : "Signal the workflow to abort. The pipeline will exit with status=failed and no PR will be opened.";
  const confirmLabel =
    kind === "approve"
      ? "Approve"
      : kind === "modify"
        ? "Modify & approve"
        : "Reject";

  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-40 bg-black/40 backdrop-blur-sm" />
        <Dialog.Content
          onOpenAutoFocus={(event) => {
            // Redirect initial focus from the Close button to the notes
            // textarea — the primary input for both approve and reject.
            if (notesRef.current) {
              event.preventDefault();
              notesRef.current.focus();
            }
          }}
          className="fixed left-1/2 top-1/2 z-50 w-[min(480px,calc(100vw-2rem))] -translate-x-1/2 -translate-y-1/2 rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-6 shadow-xl outline-none"
        >
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
              if (pending || diffMissing) return;
              void onConfirm(notes.trim(), isModify ? diff : undefined);
            }}
          >
            {isModify && (
              <label className="block text-xs font-medium text-[var(--color-muted-foreground)]">
                Patch (unified diff){" "}
                <span className="font-normal">(required)</span>
                <textarea
                  value={diff}
                  onChange={(e) => setDiff(e.target.value)}
                  rows={10}
                  spellCheck={false}
                  placeholder={
                    "diff --git a/src/service.py b/src/service.py\n--- a/src/service.py\n+++ b/src/service.py\n@@ ..."
                  }
                  className="mt-1 block w-full resize-y rounded-md border border-[var(--color-border)] bg-[var(--color-background)] px-3 py-2 font-mono text-xs text-[var(--color-foreground)] placeholder:text-[var(--color-muted-foreground)] focus:outline-none focus:ring-2 focus:ring-[var(--color-ring)]"
                  disabled={pending}
                  aria-invalid={diffMissing}
                />
              </label>
            )}
            <label className="block text-xs font-medium text-[var(--color-muted-foreground)]">
              Notes <span className="font-normal">(optional)</span>
              <textarea
                ref={notesRef}
                value={notes}
                onChange={(e) => setNotes(e.target.value)}
                rows={3}
                placeholder={
                  kind === "approve"
                    ? "e.g. patch reviewed, tests green"
                    : kind === "modify"
                      ? "e.g. narrowed the retry scope before merging"
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
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  disabled={pending}
                >
                  Cancel
                </Button>
              </Dialog.Close>
              <Button
                type="submit"
                size="sm"
                disabled={pending || diffMissing}
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
