// Shared spacing + container primitives for marketing surfaces. Two flavours:
//   * <Section> — full-width band; controls vertical rhythm + background tone
//   * <SectionInner> — max-width content holder; consistent gutters
//
// `tone` controls the visual alternation: "background" / "muted" / "accent".
// Light-mode polish lives here so /product, /pricing, /about, etc. share the
// same alternating-band look as the landing.
import type { ReactNode } from "react";

type Tone = "background" | "muted" | "accent";

type SectionProps = {
  id?: string;
  tone?: Tone;
  /** Tighter vertical rhythm for hero strips or banded CTAs. */
  compact?: boolean;
  className?: string;
  children: ReactNode;
};

function toneClass(tone: Tone | undefined) {
  switch (tone) {
    case "muted":
      return "bg-[var(--color-muted)] border-y border-[var(--color-border)]";
    case "accent":
      return "bg-[color-mix(in_srgb,var(--color-primary)_5%,var(--color-background))]";
    case "background":
    default:
      return "bg-[var(--color-background)]";
  }
}

export function Section({
  id,
  tone,
  compact,
  className,
  children,
}: SectionProps) {
  const pad = compact ? "py-16 md:py-20" : "py-20 md:py-[120px]";
  return (
    <section
      id={id}
      className={[
        "relative w-full scroll-mt-[88px]",
        toneClass(tone),
        pad,
        className ?? "",
      ].join(" ")}
    >
      {children}
    </section>
  );
}

export function SectionInner({
  className,
  children,
}: {
  className?: string;
  children: ReactNode;
}) {
  return (
    <div className={["mx-auto max-w-[1200px] px-6", className ?? ""].join(" ")}>
      {children}
    </div>
  );
}
