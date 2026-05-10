export function ApprovalVector() {
  return (
    <div className="rounded-[12px] border border-border bg-surface p-4">
      <svg
        viewBox="0 0 560 170"
        className="h-auto w-full"
        fill="none"
        xmlns="http://www.w3.org/2000/svg"
        aria-label="Approval workflow vector"
      >
        <rect x="16" y="24" width="240" height="120" rx="10" stroke="var(--border)" />
        <rect x="304" y="24" width="240" height="120" rx="10" stroke="var(--border)" />

        <text x="34" y="48" fill="var(--text-primary)" fontSize="12">
          Patch Summary
        </text>
        <text x="34" y="68" fill="var(--text-muted)" fontSize="11">
          Root cause: schema drift
        </text>
        <text x="34" y="84" fill="var(--text-muted)" fontSize="11">
          Tests passed: 2,847 / 2,847
        </text>
        <text x="34" y="100" fill="var(--text-muted)" fontSize="11">
          Risk score: low
        </text>

        <rect x="34" y="110" width="84" height="20" rx="6" stroke="var(--primary)" />
        <text x="46" y="124" fill="var(--primary)" fontSize="10">
          APPROVE
        </text>

        <text x="324" y="48" fill="var(--text-primary)" fontSize="12">
          Audit Record
        </text>
        <text x="324" y="68" fill="var(--text-muted)" fontSize="11">
          who: release-engineer
        </text>
        <text x="324" y="84" fill="var(--text-muted)" fontSize="11">
          what: patch_003 applied
        </text>
        <text x="324" y="100" fill="var(--text-muted)" fontSize="11">
          when: 02:33:17 UTC
        </text>
        <text x="324" y="116" fill="var(--text-muted)" fontSize="11">
          rollback plan attached
        </text>

        <path d="M256 84h48" stroke="var(--primary)" />
        <path d="M294 80l10 4-10 4" stroke="var(--primary)" />
      </svg>
    </div>
  );
}
