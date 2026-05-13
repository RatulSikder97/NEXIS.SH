// Generic card used across marketing pages. Wraps `<article>` so a card with
// a heading + body works inside any grid without the parent having to inline
// border/shadow rules. The top-edge accent gradient gives light-mode pages a
// little chrome so cards don't look like flat rectangles on white.
import type { ReactNode } from "react";

export function PrettyCard({
  children,
  className = "",
  accent = false,
}: {
  children: ReactNode;
  className?: string;
  /** Whether to render the top primary-coloured accent stripe. */
  accent?: boolean;
}) {
  return (
    <article
      className={[
        "relative rounded-[14px] border border-[var(--color-border)] bg-[var(--color-card)] p-6 shadow-sm",
        "transition-all duration-200 ease-out hover:shadow-md hover:-translate-y-[2px]",
        className,
      ].join(" ")}
    >
      {accent ? (
        <span
          aria-hidden
          className="pointer-events-none absolute inset-x-0 top-0 h-[2px] rounded-t-[14px] bg-gradient-to-r from-transparent via-[var(--color-primary)]/70 to-transparent"
        />
      ) : null}
      {children}
    </article>
  );
}
