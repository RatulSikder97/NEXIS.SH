"use client";

import { motion } from "framer-motion";
import type { ReactNode } from "react";

import { reveal } from "@/components/motion/variants";
import { usePrefersReducedMotion } from "@/lib/usePrefersReducedMotion";

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
        {children}
      </motion.div>
    );
  }

  return (
    <motion.div
      className={className}
      variants={reveal}
      transition={{ delay: delayMs / 1000 }}
    >
      {children}
    </motion.div>
  );
}

