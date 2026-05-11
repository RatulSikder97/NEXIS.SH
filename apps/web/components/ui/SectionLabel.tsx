export function SectionLabel({ children }: { children: string }) {
  return (
    <div className="text-[11px] font-medium tracking-[0.15em] text-[var(--color-muted-foreground)] uppercase">
      {children}
    </div>
  );
}
