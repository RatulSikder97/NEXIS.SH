"use client";

import { motion } from "framer-motion";
import type { ReactNode } from "react";

import { reveal } from "@/components/motion/variants";
import { usePrefersReducedMotion } from "@/lib/usePrefersReducedMotion";

// React 19's ReactNode adds a wider ReactPortal variant; framer-motion 12's
// motion.div children prop hasn't picked the change up yet. The forwarded
// slot is runtime-safe, so we mask the transient declaration mismatch via
// a cast — narrower than `as any`, and bounded to the JSX expression.
type Slot = React.ReactElement | null;

export function FadeUp({
  children,
  className,
  delayMs = 0,
  useWhileInView = true,
}: {
  children: ReactNode;
  className?: string;
  delayMs?: number;
  useWhileInView?: boolean;
}) {
  const reduced = usePrefersReducedMotion();

  if (reduced) {
    return <div className={className}>{children}</div>;
  }

  if (useWhileInView) {
    return (
      <motion.div
        className={className}
        variants={reveal}
        initial="hidden"
        whileInView="visible"
        viewport={{ margin: "-100px", once: true }}
        transition={{ delay: delayMs / 1000 }}
      >
        {children as Slot}
      </motion.div>
    );
  }

  return (
    <motion.div
      className={className}
      variants={reveal}
      transition={{ delay: delayMs / 1000 }}
    >
      {children as Slot}
    </motion.div>
  );
}

