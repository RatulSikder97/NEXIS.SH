import { content } from "@/lib/content";

import { Logo } from "@/components/ui/Logo";

export default function Footer() {
  return (
    <footer className="border-t border-border">
      <div className="mx-auto max-w-[1100px] px-6 py-16">
        <div className="flex flex-col gap-10 md:flex-row md:items-start md:justify-between">
          <div className="max-w-[420px]">
            <Logo variant="footer" />
            <div className="mt-3 text-[14px] text-text-secondary">
              {content.footer.tagline}
            </div>
          </div>

          <div className="flex flex-col gap-3">
            <div className="text-[11px] font-medium tracking-[0.15em] text-text-muted uppercase">
              Links
            </div>
            <div className="flex flex-wrap gap-x-6 gap-y-3">
              {content.footer.links.map((link) => (
                <a
                  key={link.href}
                  href={link.href}
                  className="text-[14px] text-text-secondary transition-colors duration-150 hover:text-accent"
                >
                  {link.label}
                </a>
              ))}
            </div>
          </div>
        </div>

        <div className="mt-12 text-[13px] text-text-muted">
          © {new Date().getFullYear()} Nexis. {content.footer.copyright}
        </div>
      </div>
    </footer>
  );
}

