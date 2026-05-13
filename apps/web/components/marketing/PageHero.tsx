// Page-level hero used by /product, /agents, /pricing, etc. Lighter weight
// than the landing Hero — eyebrow + h1 + lead + optional CTA row, set against
// a soft radial wash so light-mode pages still have texture and depth.
import type { ReactNode } from "react";

import { Section, SectionInner } from "./SectionContainer";
import { SectionHeading } from "./SectionHeading";

export function PageHero({
  eyebrow,
  title,
  lead,
  actions,
  align = "left",
}: {
  eyebrow?: string;
  title: ReactNode;
  lead?: ReactNode;
  actions?: ReactNode;
  align?: "left" | "center";
}) {
  return (
    <Section tone="background" className="overflow-hidden">
      <div
        aria-hidden
        className="pointer-events-none absolute inset-x-0 top-0 h-[420px] bg-[radial-gradient(ellipse_at_top,_color-mix(in_srgb,var(--color-primary)_9%,transparent)_0%,_transparent_60%)]"
      />
      <SectionInner>
        <div
          className={
            align === "center"
              ? "flex flex-col items-center text-center"
              : "flex flex-col items-start"
          }
        >
          <SectionHeading
            eyebrow={eyebrow}
            title={title}
            lead={lead}
            align={align}
            level={1}
          />
          {actions ? <div className="mt-8 flex flex-wrap gap-3">{actions}</div> : null}
        </div>
      </SectionInner>
    </Section>
  );
}
