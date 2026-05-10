export function Badge({ children }: { children: string }) {
  return (
    <span className="inline-flex items-center rounded-[999px] border border-border-hover px-3 py-1 text-[12px] text-text-secondary">
      {children}
    </span>
  );
}

