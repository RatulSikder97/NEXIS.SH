"use client";

import { motion } from "framer-motion";
import type { ReactNode } from "react";

import type { StaggerPreset } from "@/components/motion/variants";
import { staggerPresets } from "@/components/motion/variants";
import { usePrefersReducedMotion } from "@/lib/usePrefersReducedMotion";

export function StaggerGroup({
  children,
  className,
  preset = "section",
  mode = "inView",
}: {
  children: ReactNode;
  className?: string;
  preset?: StaggerPreset;
  /** `immediate`: orchestrated hero load; `inView`: reveal when scrolled into view */
  mode?: "inView" | "immediate";
}) {
  const reduced = usePrefersReducedMotion();
  const stagger = staggerPresets[preset];

  if (reduced) {
    return <div className={className}>{children}</div>;
  }

  if (mode === "immediate") {
    return (
      <motion.div
        className={className}
        variants={stagger}
        initial="hidden"
        animate="visible"
      >
        {children}
      </motion.div>
    );
  }

  return (
    <motion.div
      className={className}
      variants={stagger}
      initial="hidden"
      whileInView="visible"
      viewport={{ margin: "-10% 0px -12% 0px", once: true }}
    >
      {children}
    </motion.div>
  );
}

