export function ApprovalVector() {
  return (
    <div className="rounded-[12px] border border-[var(--color-border)] bg-[var(--color-card)] p-4">
      <svg
        viewBox="0 0 560 170"
        className="h-auto w-full"
        fill="none"
        xmlns="http://www.w3.org/2000/svg"
        aria-label="Approval workflow vector"
      >
        <rect x="16" y="24" width="240" height="120" rx="10" fill="var(--color-card)" stroke="var(--color-border)" />
        <rect x="304" y="24" width="240" height="120" rx="10" fill="var(--color-card)" stroke="var(--color-border)" />

        <text x="34" y="48" fill="var(--color-foreground)" fontSize="12" fontWeight="500">
          Patch Summary
        </text>
        <text x="34" y="68" fill="var(--color-muted-foreground)" fontSize="11">
          Root cause: schema drift
        </text>
        <text x="34" y="84" fill="var(--color-muted-foreground)" fontSize="11">
          Tests passed: 2,847 / 2,847
        </text>
        <text x="34" y="100" fill="var(--color-muted-foreground)" fontSize="11">
          Risk score: low
        </text>

        <rect x="34" y="110" width="84" height="20" rx="6" stroke="var(--color-primary)" />
        <text x="46" y="124" fill="var(--color-primary)" fontSize="10" fontWeight="600">
          APPROVE
        </text>

        <text x="324" y="48" fill="var(--color-foreground)" fontSize="12" fontWeight="500">
          Audit Record
        </text>
        <text x="324" y="68" fill="var(--color-muted-foreground)" fontSize="11">
          who: release-engineer
        </text>
        <text x="324" y="84" fill="var(--color-muted-foreground)" fontSize="11">
          what: patch_003 applied
        </text>
        <text x="324" y="100" fill="var(--color-muted-foreground)" fontSize="11">
          when: 02:33:17 UTC
        </text>
        <text x="324" y="116" fill="var(--color-muted-foreground)" fontSize="11">
          rollback plan attached
        </text>

        <path d="M256 84h48" stroke="var(--color-primary)" strokeWidth={1.5} />
        <path d="M294 80l10 4-10 4" stroke="var(--color-primary)" strokeWidth={1.5} />
      </svg>
    </div>
  );
}
