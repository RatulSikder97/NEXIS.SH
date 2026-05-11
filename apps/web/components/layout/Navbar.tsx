"use client";

import * as React from "react";
import Link from "next/link";
import { ThemeAwareLogo } from "@/components/ThemeAwareLogo";
import { ThemeToggle } from "@/components/ThemeToggle";
import { Button } from "@/components/ui/Button";

const NAV = [
  { label: "Features", href: "#features" },
  { label: "How it works", href: "#how-it-works" },
  { label: "Agents", href: "#agents" },
  { label: "Pricing", href: "#pricing" },
  { label: "Docs", href: "#docs" },
];

export default function Navbar() {
  const [scrolled, setScrolled] = React.useState(false);
  React.useEffect(() => {
    const onScroll = () => setScrolled(window.scrollY > 8);
    onScroll();
    window.addEventListener("scroll", onScroll, { passive: true });
    return () => window.removeEventListener("scroll", onScroll);
  }, []);
  return (
    <header
      className={`sticky top-0 z-40 h-16 backdrop-blur-md transition-colors ${
        scrolled
          ? "bg-[color-mix(in_srgb,var(--color-background)_88%,transparent)] border-b border-[var(--color-border)]"
          : "bg-transparent"
      }`}
    >
      <div className="mx-auto flex h-full max-w-[1200px] items-center justify-between px-6">
        <Link href="/" aria-label="NEXIS home">
          <ThemeAwareLogo />
        </Link>
        <nav className="hidden md:flex items-center gap-6 text-sm text-[var(--color-muted-foreground)]">
          {NAV.map((n) => (
            <a
              key={n.href}
              href={n.href}
              className="hover:text-[var(--color-foreground)] transition-colors"
            >
              {n.label}
            </a>
          ))}
        </nav>
        <div className="flex items-center gap-2">
          <ThemeToggle />
          <Button variant="ghost" size="sm" asChild>
            <a href="/sign-in">Sign in</a>
          </Button>
          <Button size="sm" asChild>
            <a href="#waitlist">Get started</a>
          </Button>
        </div>
      </div>
    </header>
  );
}
