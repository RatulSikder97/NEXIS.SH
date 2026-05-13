// Standardised heading block for marketing sections. Three-part:
//   * eyebrow (kicker)
//   * headline
//   * lead paragraph (optional)
//
// All marketing pages use this so type sizes + spacing stay in sync with the
// landing's existing rhythm.
import type { ReactNode } from "react";

import { SectionLabel } from "@/components/ui/SectionLabel";

export function SectionHeading({
  eyebrow,
  title,
  lead,
  align = "left",
  level = 2,
}: {
  eyebrow?: string;
  title: ReactNode;
  lead?: ReactNode;
  align?: "left" | "center";
  level?: 1 | 2;
}) {
  const Heading = level === 1 ? "h1" : "h2";
  const wrap = align === "center" ? "text-center mx-auto" : "";
  const headingSize =
    level === 1
      ? "text-[40px] md:text-[56px] font-medium leading-[1.08] tracking-tight"
      : "text-[32px] md:text-[40px] font-medium leading-[1.15] tracking-tight";
  return (
    <div className={`max-w-[820px] ${wrap}`}>
      {eyebrow ? <SectionLabel>{eyebrow}</SectionLabel> : null}
      <Heading
        className={`mt-4 ${headingSize} text-[var(--color-foreground)]`}
      >
        {title}
      </Heading>
      {lead ? (
        <p className="mt-4 text-[16px] leading-[1.7] text-[var(--color-muted-foreground)]">
          {lead}
        </p>
      ) : null}
    </div>
  );
}
