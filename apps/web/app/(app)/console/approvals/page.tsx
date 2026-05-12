// Phase 3 Stage 6 — Approvals placeholder. Ships in Phase 4.

export default function ApprovalsPage() {
  return (
    <div className="space-y-4">
      <div className="flex items-center gap-3">
        <h1 className="text-2xl font-semibold">Approvals</h1>
        <span className="rounded-full bg-[var(--color-accent)]/30 text-[var(--color-accent-foreground)] px-2 py-0.5 text-xs font-medium">
          Phase 4
        </span>
      </div>
      <p className="text-sm text-[var(--color-muted-foreground)] max-w-2xl">
        Human-in-the-loop approval queue for high-risk agent actions. Approvers can review the proposed diff, the recovery plan, and the affected blast radius before authorizing execution. Ships in Phase 4.
      </p>
    </div>
  );
}
