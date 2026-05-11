"use client";

import { useEffect, useRef, useState } from "react";
import {
  animate,
  useInView,
  useMotionValue,
  useMotionValueEvent,
  useSpring,
} from "framer-motion";

import { usePrefersReducedMotion } from "@/lib/usePrefersReducedMotion";

export type StatCardProps = {
  prefix: string;
  number: number;
  suffix: string;
  label: string;
};

export function StatCard({ prefix, number, suffix, label }: StatCardProps) {
  const ref = useRef<HTMLDivElement | null>(null);
  const inView = useInView(ref, { once: true, margin: "-100px" });
  const reduced = usePrefersReducedMotion();

  const base = useMotionValue(0);
  const spring = useSpring(base, { stiffness: 120, damping: 18, mass: 0.9 });

  const [current, setCurrent] = useState(0);

  useMotionValueEvent(spring, "change", (latest) => {
    setCurrent(Math.round(latest));
  });

  useEffect(() => {
    if (reduced) return;
    if (!inView) return;
    const controls = animate(base, number, {
      duration: 1.2,
      ease: "easeOut",
    });
    return () => controls.stop();
  }, [base, inView, number, reduced]);

  const display = reduced ? number : current;

  return (
    <div ref={ref}>
      <div className="font-mono text-[56px] font-medium tracking-tight text-[var(--color-foreground)]">
        {prefix}
        {display}
        {suffix}
      </div>
      <p className="mt-2 text-[13px] text-[var(--color-muted-foreground)]">
        {label}
      </p>
    </div>
  );
}
