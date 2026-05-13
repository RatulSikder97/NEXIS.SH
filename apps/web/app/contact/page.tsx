// /contact — Server-rendered shell + client child for the form. The form
// posts to /api/contact which is a no-op stub today. On success the form
// shows an inline confirmation message (no toast library to keep things
// dependency-light).
import type { Metadata } from "next";
import {
  AtSign,
  Github,
  HeartHandshake,
  Linkedin,
  LifeBuoy,
  Mail,
  ShieldCheck,
  Twitter,
} from "lucide-react";

import { ContactForm } from "@/components/marketing/ContactForm";
import { MarketingShell } from "@/components/marketing/MarketingShell";
import { PageHero } from "@/components/marketing/PageHero";
import { Section, SectionInner } from "@/components/marketing/SectionContainer";
import { SectionHeading } from "@/components/marketing/SectionHeading";

export const metadata: Metadata = {
  title: "Contact — Talk to us",
  description: "Sales, support, security, careers. Or just say hi.",
};

const CHANNELS = [
  { label: "Sales", value: "sales@nexis.dev", Icon: HeartHandshake, href: "mailto:sales@nexis.dev" },
  { label: "Support", value: "support@nexis.dev", Icon: LifeBuoy, href: "mailto:support@nexis.dev" },
  { label: "Security", value: "security@nexis.dev", Icon: ShieldCheck, href: "mailto:security@nexis.dev" },
  { label: "Careers", value: "careers@nexis.dev", Icon: Mail, href: "mailto:careers@nexis.dev" },
];

const SOCIAL = [
  { label: "GitHub", href: "https://github.com/nexis-eco", Icon: Github },
  { label: "X", href: "https://x.com/nexis_eco", Icon: Twitter },
  { label: "LinkedIn", href: "https://www.linkedin.com/company/nexis-eco", Icon: Linkedin },
];

export default function ContactPage() {
  return (
    <MarketingShell>
      <PageHero
        eyebrow="CONTACT"
        title="We&apos;d love to hear from you."
        lead="Sales conversations, security disclosures, support tickets, hiring questions — there&apos;s a real human at the other end of every channel below."
      />

      <Section tone="background">
        <SectionInner>
          <div className="grid grid-cols-1 gap-10 md:grid-cols-[1.2fr_1fr]">
            <div>
              <SectionHeading
                eyebrow="MESSAGE US"
                title="Send a note."
                lead="We aim to respond within one working day. For urgent security disclosures, please use security@nexis.dev directly."
              />
              <div className="mt-8">
                <ContactForm />
              </div>
            </div>

            <aside className="space-y-8">
              <div>
                <h3 className="text-[12px] font-medium uppercase tracking-[0.15em] text-[var(--color-muted-foreground)]">
                  Direct channels
                </h3>
                <ul className="mt-4 divide-y divide-[var(--color-border)] rounded-[14px] border border-[var(--color-border)] bg-[var(--color-card)] shadow-sm">
                  {CHANNELS.map((c) => (
                    <li key={c.label}>
                      <a
                        href={c.href}
                        className="flex items-center gap-3 px-5 py-4 hover:bg-[var(--color-muted)] transition-colors"
                      >
                        <span className="flex h-9 w-9 items-center justify-center rounded-lg bg-[color-mix(in_srgb,var(--color-primary)_12%,var(--color-muted))] text-[var(--color-primary)]">
                          <c.Icon className="h-4 w-4" aria-hidden />
                        </span>
                        <span className="flex-1">
                          <span className="block text-[14px] font-medium text-[var(--color-foreground)]">
                            {c.label}
                          </span>
                          <span className="block text-[12px] text-[var(--color-muted-foreground)]">
                            {c.value}
                          </span>
                        </span>
                        <AtSign className="h-3.5 w-3.5 text-[var(--color-muted-foreground)]" aria-hidden />
                      </a>
                    </li>
                  ))}
                </ul>
              </div>

              <div>
                <h3 className="text-[12px] font-medium uppercase tracking-[0.15em] text-[var(--color-muted-foreground)]">
                  Social
                </h3>
                <div className="mt-4 flex flex-wrap gap-2">
                  {SOCIAL.map((s) => (
                    <a
                      key={s.label}
                      href={s.href}
                      target="_blank"
                      rel="noreferrer"
                      className="inline-flex items-center gap-2 rounded-full border border-[var(--color-border)] bg-[var(--color-card)] px-3 py-1.5 text-[13px] text-[var(--color-foreground)] shadow-sm transition-colors hover:bg-[var(--color-muted)]"
                    >
                      <s.Icon className="h-3.5 w-3.5" aria-hidden /> {s.label}
                    </a>
                  ))}
                </div>
              </div>

              <div className="rounded-[14px] border border-[var(--color-border)] bg-[color-mix(in_srgb,var(--color-primary)_5%,var(--color-card))] p-5 shadow-sm">
                <h3 className="text-[14px] font-medium text-[var(--color-foreground)]">
                  Office hours
                </h3>
                <p className="mt-1 text-[12px] leading-[1.65] text-[var(--color-muted-foreground)]">
                  Open community office hours every Thursday at 10am ET in our
                  GitHub Discussions. Bring your incident replays — we love
                  walking through them.
                </p>
              </div>
            </aside>
          </div>
        </SectionInner>
      </Section>
    </MarketingShell>
  );
}
