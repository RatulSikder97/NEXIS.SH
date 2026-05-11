import Link from "next/link";

import { content } from "@/lib/content";
import { ThemeAwareLogo } from "@/components/ThemeAwareLogo";

export default function Footer() {
  return (
    <footer className="border-t border-[var(--color-border)] bg-[var(--color-background)]">
      <div className="mx-auto max-w-[1200px] px-6 py-16">
        <div className="grid grid-cols-2 gap-10 md:grid-cols-6">
          <div className="col-span-2 md:col-span-1">
            <Link href="/" aria-label="NEXIS home">
              <ThemeAwareLogo />
            </Link>
            <p className="mt-3 max-w-[260px] text-[13px] leading-[1.6] text-[var(--color-muted-foreground)]">
              {content.footer.tagline}
            </p>
            <div className="mt-5 flex flex-col gap-2">
              <span className="inline-flex items-center gap-2 text-[12px] text-[var(--color-muted-foreground)]">
                <span className="h-2 w-2 rounded-full bg-[var(--color-success)] shadow-[0_0_8px_var(--color-success)]" />
                {content.footer.status}
              </span>
              <span className="inline-flex w-fit items-center rounded-full border border-[var(--color-border)] bg-[var(--color-muted)] px-2 py-0.5 text-[11px] font-medium tracking-[0.08em] text-[var(--color-muted-foreground)]">
                {content.footer.compliance}
              </span>
            </div>
          </div>

          {content.footer.columns.map((column) => (
            <div key={column.title} className="flex flex-col gap-3">
              <div className="text-[11px] font-medium uppercase tracking-[0.15em] text-[var(--color-muted-foreground)]">
                {column.title}
              </div>
              <ul className="flex flex-col gap-2">
                {column.links.map((link) => (
                  <li key={link.href}>
                    <a
                      href={link.href}
                      className="text-[14px] text-[var(--color-muted-foreground)] transition-colors duration-150 hover:text-[var(--color-foreground)]"
                    >
                      {link.label}
                    </a>
                  </li>
                ))}
              </ul>
            </div>
          ))}
        </div>

        <div className="mt-12 flex flex-col items-start justify-between gap-2 border-t border-[var(--color-border)] pt-6 text-[13px] text-[var(--color-muted-foreground)] md:flex-row md:items-center">
          <div>
            © {new Date().getFullYear()} NEXIS. {content.footer.copyright}
          </div>
        </div>
      </div>
    </footer>
  );
}
