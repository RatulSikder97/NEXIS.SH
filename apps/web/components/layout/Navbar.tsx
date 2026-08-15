"use client";

import * as React from "react";
import Link from "next/link";
import type { Route } from "next";
import { Menu, X } from "lucide-react";

import { ThemeAwareLogo } from "@/components/ThemeAwareLogo";
import { ThemeToggle } from "@/components/ThemeToggle";
import { Button } from "@/components/ui/Button";
import { content } from "@/lib/content";
import { useSession } from "@/lib/useSession";

export default function Navbar() {
  const [scrolled, setScrolled] = React.useState(false);
  const [mobileOpen, setMobileOpen] = React.useState(false);
  // Signed-in visitors landing on the marketing site must not be asked to
  // sign in again — swap both CTAs for a single Dashboard link. The probe is
  // client-side because these pages are static (see lib/useSession).
  const session = useSession();
  const signedIn = session.status === "authenticated";

  React.useEffect(() => {
    const onScroll = () => setScrolled(window.scrollY > 8);
    onScroll();
    window.addEventListener("scroll", onScroll, { passive: true });
    return () => window.removeEventListener("scroll", onScroll);
  }, []);

  // Lock body scroll while mobile sheet is open.
  React.useEffect(() => {
    if (mobileOpen) {
      const prev = document.body.style.overflow;
      document.body.style.overflow = "hidden";
      return () => {
        document.body.style.overflow = prev;
      };
    }
  }, [mobileOpen]);

  return (
    <>
      {/* Skip link — visually hidden until keyboard-focused, lets keyboard /
          screen-reader users jump past the navbar to <main>. */}
      <a
        href="#main"
        className="sr-only focus:not-sr-only focus:fixed focus:left-4 focus:top-4 focus:z-[60] focus:rounded-md focus:bg-[var(--color-primary)] focus:px-4 focus:py-2 focus:text-sm focus:font-medium focus:text-[var(--color-primary-foreground)] focus:shadow-lg focus:outline-none focus:ring-2 focus:ring-[var(--color-ring)] focus:ring-offset-2"
      >
        Skip to content
      </a>
      <header
        className={`sticky top-0 z-40 h-16 backdrop-blur-md transition-[background-color,box-shadow,border-color] duration-150 ${
          scrolled
            ? "bg-[color-mix(in_srgb,var(--color-background)_85%,transparent)] border-b border-[var(--color-border)] shadow-sm"
            : "bg-[color-mix(in_srgb,var(--color-background)_55%,transparent)]"
        }`}
      >
        <div className="mx-auto flex h-full max-w-[1200px] items-center justify-between px-6">
          <Link href={"/" as Route} aria-label="NEXIS home">
            <ThemeAwareLogo />
          </Link>

          {/* Desktop nav */}
          <nav
            className="hidden md:flex items-center gap-6 text-sm text-[var(--color-muted-foreground)]"
            aria-label="Primary"
          >
            {content.navbar.links.map((n) => (
              <Link
                key={n.href}
                href={n.href as Route}
                className="hover:text-[var(--color-foreground)] transition-colors"
              >
                {n.label}
              </Link>
            ))}
          </nav>

          <div className="flex items-center gap-2">
            <ThemeToggle />
            {signedIn ? (
              <Button size="sm" asChild className="hidden sm:inline-flex">
                <Link href={"/console" as Route}>Dashboard</Link>
              </Button>
            ) : (
              <>
                <Button
                  variant="ghost"
                  size="sm"
                  asChild
                  className="hidden sm:inline-flex"
                >
                  <Link href={"/sign-in" as Route}>Sign in</Link>
                </Button>
                <Button size="sm" asChild className="hidden sm:inline-flex">
                  <Link href={"/sign-up" as Route}>{content.navbar.cta}</Link>
                </Button>
              </>
            )}
            {/* Mobile hamburger */}
            <button
              type="button"
              aria-label="Open menu"
              aria-expanded={mobileOpen}
              onClick={() => setMobileOpen(true)}
              className="md:hidden inline-flex h-9 w-9 items-center justify-center rounded-md text-[var(--color-foreground)] hover:bg-[var(--color-muted)]"
            >
              <Menu className="h-5 w-5" />
            </button>
          </div>
        </div>
      </header>

      {/* Mobile sheet — full screen overlay, same links as desktop. */}
      {mobileOpen ? (
        <div
          role="dialog"
          aria-modal="true"
          aria-label="Mobile navigation"
          className="fixed inset-0 z-50 md:hidden bg-[var(--color-background)]"
        >
          <div className="flex h-16 items-center justify-between px-6 border-b border-[var(--color-border)]">
            <Link
              href={"/" as Route}
              aria-label="NEXIS home"
              onClick={() => setMobileOpen(false)}
            >
              <ThemeAwareLogo />
            </Link>
            <button
              type="button"
              aria-label="Close menu"
              onClick={() => setMobileOpen(false)}
              className="inline-flex h-9 w-9 items-center justify-center rounded-md text-[var(--color-foreground)] hover:bg-[var(--color-muted)]"
            >
              <X className="h-5 w-5" />
            </button>
          </div>
          <nav
            className="flex flex-col gap-1 px-6 py-8 text-[18px]"
            aria-label="Primary mobile"
          >
            {content.navbar.links.map((n) => (
              <Link
                key={n.href}
                href={n.href as Route}
                onClick={() => setMobileOpen(false)}
                className="rounded-md px-3 py-3 text-[var(--color-foreground)] hover:bg-[var(--color-muted)]"
              >
                {n.label}
              </Link>
            ))}
            <div className="mt-6 flex flex-col gap-2 border-t border-[var(--color-border)] pt-6">
              {signedIn ? (
                <Button size="lg" asChild>
                  <Link
                    href={"/console" as Route}
                    onClick={() => setMobileOpen(false)}
                  >
                    Dashboard
                  </Link>
                </Button>
              ) : (
                <>
                  <Button variant="outline" size="lg" asChild>
                    <Link
                      href={"/sign-in" as Route}
                      onClick={() => setMobileOpen(false)}
                    >
                      Sign in
                    </Link>
                  </Button>
                  <Button size="lg" asChild>
                    <Link
                      href={"/sign-up" as Route}
                      onClick={() => setMobileOpen(false)}
                    >
                      {content.navbar.cta}
                    </Link>
                  </Button>
                </>
              )}
            </div>
          </nav>
        </div>
      ) : null}
    </>
  );
}
