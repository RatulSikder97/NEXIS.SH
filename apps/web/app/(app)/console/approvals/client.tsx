"use client";

// Phase 6 Stage 9 — Approvals (pending decisions) client.
//
// Owns: the pending-rows array, a 1s "now" tick for the relative-time
// column, a 5s polling refetch (always — pending approvals matter
// regardless of whether the user is actively interacting with the table),
// and the approve / reject dialog state machine.
//
// Polling cadence: 5s. We poll unconditionally because a new pending row
// may appear at any time (Sentinel triggers an unrelated workflow) and
// the operator wants to see it without a manual refresh.
//
// Decision flow:
//   click Approve / Reject → open DecisionDialog (kind=approve|reject)
//   submit with optional notes → POST /pipelines/{run}/approve
//   on 2xx → optimistic remove from the table + close dialog
//   on error → keep dialog open, show error inline
//
// Empty state: a friendly "all caught up" message + a CTA to the Live Demo
// page. The CTA is the same trigger users hit elsewhere; routing through it
// keeps the demo flow deterministic.

import * as React from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import type { Route } from "next";
import { CheckSquare, Loader2, PlayCircle, RefreshCw } from "lucide-react";

import { Button } from "@/components/ui/Button";
import { EmptyState } from "@/components/empty-state/EmptyState";
import { DecisionDialog } from "@/components/approvals/DecisionDialog";
import { PendingApprovalsTable } from "@/components/approvals/PendingApprovalsTable";
import { approvals, type PendingApproval } from "@/lib/approvals";

const POLL_MS = 5_000;
const TICK_MS = 1_000;

type DialogState =
  | { open: false }
  | { open: true; kind: "approve" | "reject" | "modify"; row: PendingApproval };

export function ApprovalsClient({
  workspaceId,
  initial,
}: {
  workspaceId: string;
  initial: PendingApproval[];
}) {
  const router = useRouter();
  const [rows, setRows] = React.useState<PendingApproval[]>(initial);
  const [now, setNow] = React.useState<number>(() => Date.now());
  const [refreshing, setRefreshing] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);

  const [dialog, setDialog] = React.useState<DialogState>({ open: false });
  const [submitting, setSubmitting] = React.useState(false);
  const [dialogError, setDialogError] = React.useState<string | null>(null);

  // 1s ticker for live relative-time. Cheap; left running for the lifetime
  // of the page.
  React.useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), TICK_MS);
    return () => window.clearInterval(id);
  }, []);

  // 5s poll. Always on — unlike incidents, the pending approvals list can
  // gain rows independently of any local action, so we never stop polling.
  React.useEffect(() => {
    if (!workspaceId) return;
    const id = window.setInterval(async () => {
      try {
        const next = await approvals.pending(workspaceId);
        setRows(next);
      } catch {
        // swallow — the next tick will retry.
      }
    }, POLL_MS);
    return () => window.clearInterval(id);
  }, [workspaceId]);

  async function refresh() {
    if (!workspaceId) return;
    setRefreshing(true);
    setError(null);
    try {
      const next = await approvals.pending(workspaceId);
      setRows(next);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load");
    } finally {
      setRefreshing(false);
    }
  }

  function openApprove(row: PendingApproval) {
    setDialogError(null);
    setDialog({ open: true, kind: "approve", row });
  }

  function openReject(row: PendingApproval) {
    setDialogError(null);
    setDialog({ open: true, kind: "reject", row });
  }

  function openModify(row: PendingApproval) {
    setDialogError(null);
    setDialog({ open: true, kind: "modify", row });
  }

  function closeDialog() {
    if (submitting) return;
    setDialog({ open: false });
  }

  async function confirmDecision(notes: string, modifiedDiff?: string) {
    if (!dialog.open || !workspaceId) return;
    setSubmitting(true);
    setDialogError(null);
    try {
      if (dialog.kind === "approve") {
        await approvals.approve(workspaceId, dialog.row.run_id, notes);
      } else if (dialog.kind === "modify") {
        await approvals.modify(
          workspaceId,
          dialog.row.run_id,
          modifiedDiff ?? "",
          notes,
        );
      } else {
        await approvals.reject(workspaceId, dialog.row.run_id, notes);
      }
      // Optimistic remove. The next poll will reconcile if the backend
      // reports the row still pending (race against the workflow signal).
      setRows((prev) => prev.filter((r) => r.run_id !== dialog.row.run_id));
      setDialog({ open: false });
      // Refresh the layout so the sidebar's pending-count badge picks the
      // change up without waiting for its own poll tick.
      router.refresh();
    } catch (err) {
      setDialogError(
        err instanceof Error ? err.message : "Failed to record decision",
      );
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold">Approvals</h1>
          <p className="mt-1 text-sm text-[var(--color-muted-foreground)]">
            Pending human-in-the-loop decisions for this workspace. Approving
            signals the workflow to proceed to GitOps; rejecting blocks the
            patch from opening a PR.
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={refresh}
            disabled={refreshing || !workspaceId}
          >
            {refreshing ? (
              <Loader2 className="h-4 w-4 animate-spin" />
            ) : (
              <RefreshCw className="h-4 w-4" />
            )}
            Refresh
          </Button>
        </div>
      </div>

      {error && (
        <div
          role="alert"
          className="rounded-md border border-red-500/30 bg-red-500/10 px-4 py-3 text-sm text-red-700 dark:text-red-300"
        >
          {error}
        </div>
      )}

      {!workspaceId && (
        <div
          role="status"
          className="rounded-md border border-amber-500/30 bg-amber-500/10 px-4 py-3 text-sm text-amber-700 dark:text-amber-300"
        >
          No workspace selected. Pick one in the sidebar to view pending
          approvals.
        </div>
      )}

      {rows.length === 0 ? (
        <EmptyState
          icon={CheckSquare}
          title="All caught up"
          description="High-severity incidents and timed-out medium ones surface here for a human signal. Trigger a synthetic incident to see the flow."
          cta={
            <Link href={"/console/live-demo" as Route}>
              <Button size="sm" disabled={!workspaceId}>
                <PlayCircle className="h-4 w-4" />
                Run synthetic incident
              </Button>
            </Link>
          }
        />
      ) : (
        <PendingApprovalsTable
          rows={rows}
          now={now}
          onApprove={openApprove}
          onReject={openReject}
          onModify={openModify}
        />
      )}

      <DecisionDialog
        open={dialog.open}
        onOpenChange={(next) => {
          if (!next) closeDialog();
        }}
        kind={dialog.open ? dialog.kind : "approve"}
        runIdShort={dialog.open ? dialog.row.run_id.slice(0, 8) : ""}
        scenario={dialog.open ? dialog.row.scenario : null}
        pending={submitting}
        error={dialogError}
        onConfirm={confirmDecision}
      />
    </div>
  );
}
