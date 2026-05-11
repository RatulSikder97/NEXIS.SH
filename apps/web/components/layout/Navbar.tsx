"use client";

import { motion, useMotionTemplate, useScroll, useTransform } from "framer-motion";

import { content } from "@/lib/content";
import { easeSoftOut } from "@/components/motion/variants";
import { Button } from "@/components/ui/Button";
import { Logo } from "@/components/ui/Logo";
import { usePrefersReducedMotion } from "@/lib/usePrefersReducedMotion";

export default function Navbar() {
  const reduced = usePrefersReducedMotion();
  const { scrollY } = useScroll();
  const shadowAlpha = useTransform(scrollY, [0, 72], [0.08, 0.42]);
  const boxShadow = useMotionTemplate`0 18px 48px rgba(0, 0, 0, ${shadowAlpha})`;
  const borderStrength = useTransform(scrollY, [0, 72], [0.55, 1]);
  const borderColor = useMotionTemplate`rgba(42, 63, 92, ${borderStrength})`;

  if (reduced) {
    return (
      <header
        className="sticky top-0 z-50 h-[72px] w-full border-b border-border bg-navbar-bg backdrop-blur-[12px]"
        aria-label="Primary"
      >
        <NavbarInner />
      </header>
    );
  }

  return (
    <motion.header
      className="sticky top-0 z-50 h-[72px] w-full border-b bg-navbar-bg backdrop-blur-[12px]"
      aria-label="Primary"
      style={{ boxShadow, borderColor }}
    >
      <NavbarInner />
    </motion.header>
  );
}

function NavbarInner() {
  return (
    <div className="mx-auto flex h-full max-w-[1100px] items-center justify-between px-6">
      <motion.a
        href="#"
        className="flex items-center"
        whileHover={{ y: -1 }}
        transition={{ duration: 0.2, ease: easeSoftOut }}
      >
        <Logo variant="nav" priority />
      </motion.a>

      <nav className="hidden items-center justify-center gap-8 md:flex" aria-label="Sections">
        {content.navbar.links.map((link) => (
          <motion.a
            key={link.href}
            href={link.href}
            className="text-[14px] text-text-secondary transition-colors duration-150 hover:text-accent"
            whileHover={{ y: -1 }}
            transition={{ duration: 0.2, ease: easeSoftOut }}
          >
            {link.label}
          </motion.a>
        ))}
      </nav>

      <div className="flex items-center gap-3">
        <Button
          href="#early-access"
          variant="ghost"
          size="nav"
          className="border-primary text-primary hover:bg-primary hover:text-on-primary"
        >
          {content.navbar.cta}
        </Button>
      </div>
    </div>
  );
}
