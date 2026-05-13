// Shared legal page primitive. Renders an in-page TOC + numbered sections in
// long-form. Each `section.body` is rendered through a tiny inline JSX list —
// not MDX, per spec — so legal copy can carry paragraphs, sub-bullets, and
// small tables without a full MDX pipeline.
import type { ReactNode } from "react";

import { MarketingShell } from "@/components/marketing/MarketingShell";
import { PageHero } from "@/components/marketing/PageHero";
import { Section, SectionInner } from "@/components/marketing/SectionContainer";

export type LegalBlock =
  | { kind: "p"; text: string }
  | { kind: "ul"; items: string[] }
  | { kind: "table"; headers: string[]; rows: string[][] };

export type LegalSection = {
  id: string;
  title: string;
  body: LegalBlock[];
};

export function LegalPage({
  eyebrow,
  title,
  effective,
  sections,
}: {
  eyebrow: string;
  title: string;
  effective: string;
  sections: LegalSection[];
}) {
  return (
    <MarketingShell>
      <PageHero
        eyebrow={eyebrow}
        title={title}
        lead={`Effective ${effective}. Plain English first, defined terms second. If anything below is unclear, email legal@nexis.dev.`}
      />

      <Section tone="background">
        <SectionInner>
          <div className="grid grid-cols-1 gap-12 md:grid-cols-[1fr_3fr]">
            {/* Table of contents */}
            <aside className="md:sticky md:top-24 md:self-start">
              <h2 className="text-[11px] font-medium uppercase tracking-[0.15em] text-[var(--color-muted-foreground)]">
                Contents
              </h2>
              <ol className="mt-3 space-y-1">
                {sections.map((s, idx) => (
                  <li key={s.id}>
                    <a
                      href={`#${s.id}`}
                      className="block text-[13px] leading-[1.4] text-[var(--color-muted-foreground)] hover:text-[var(--color-foreground)] transition-colors"
                    >
                      <span className="font-mono text-[11px] mr-2 text-[var(--color-muted-foreground)]/70">
                        {String(idx + 1).padStart(2, "0")}
                      </span>
                      {s.title}
                    </a>
                  </li>
                ))}
              </ol>
            </aside>

            {/* Body */}
            <article className="prose-like text-[15px] leading-[1.75] text-[var(--color-foreground)]">
              {sections.map((s, idx) => (
                <section
                  key={s.id}
                  id={s.id}
                  className="scroll-mt-[100px] mb-12 border-b border-[var(--color-border)] pb-12 last:border-b-0"
                >
                  <h2 className="text-[22px] font-medium tracking-tight text-[var(--color-foreground)]">
                    <span className="font-mono text-[14px] text-[var(--color-muted-foreground)] mr-3">
                      {String(idx + 1).padStart(2, "0")}
                    </span>
                    {s.title}
                  </h2>
                  <div className="mt-5 space-y-4 text-[var(--color-muted-foreground)]">
                    {s.body.map((block, bIdx) => (
                      <Block key={`${s.id}-${bIdx}`} block={block} />
                    ))}
                  </div>
                </section>
              ))}
            </article>
          </div>
        </SectionInner>
      </Section>
    </MarketingShell>
  );
}

function Block({ block }: { block: LegalBlock }): ReactNode {
  if (block.kind === "p") {
    return <p>{block.text}</p>;
  }
  if (block.kind === "ul") {
    return (
      <ul className="list-disc space-y-2 pl-6">
        {block.items.map((item) => (
          <li key={item}>{item}</li>
        ))}
      </ul>
    );
  }
  if (block.kind === "table") {
    return (
      <div className="overflow-hidden rounded-[12px] border border-[var(--color-border)]">
        <table className="w-full text-left text-[13px]">
          <thead className="bg-[var(--color-muted)]/60">
            <tr>
              {block.headers.map((h) => (
                <th
                  key={h}
                  className="px-4 py-2 text-[11px] font-medium uppercase tracking-[0.15em] text-[var(--color-muted-foreground)]"
                >
                  {h}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {block.rows.map((row, rIdx) => (
              <tr
                key={rIdx}
                className={
                  rIdx % 2 === 0
                    ? "bg-[var(--color-card)]"
                    : "bg-[var(--color-muted)]/30"
                }
              >
                {row.map((cell, cIdx) => (
                  <td
                    key={cIdx}
                    className="border-t border-[var(--color-border)] px-4 py-2 text-[var(--color-foreground)]"
                  >
                    {cell}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    );
  }
  return null;
}
