"use client";

import { motion } from "framer-motion";

import { easeSoftOut } from "@/components/motion/variants";
import { usePrefersReducedMotion } from "@/lib/usePrefersReducedMotion";

export function SystemVector() {
  const reduced = usePrefersReducedMotion();

  if (reduced) {
    return (
      <div className="rounded-[12px] border border-border bg-surface p-4">
        <svg
          viewBox="0 0 560 220"
          className="h-auto w-full"
          fill="none"
          xmlns="http://www.w3.org/2000/svg"
          aria-label="Nexis system flow diagram"
        >
          <DiagramFrame />
          <Wiring />
        </svg>
      </div>
    );
  }

  const edgeDraw = {
    duration: 0.62,
    ease: easeSoftOut,
  } as const;

  return (
    <motion.div
      className="rounded-[12px] border border-border bg-surface p-4"
      initial={{ opacity: 0, y: 14 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.55, ease: easeSoftOut, delay: 0.45 }}
    >
      <svg
        viewBox="0 0 560 220"
        className="h-auto w-full"
        fill="none"
        xmlns="http://www.w3.org/2000/svg"
        aria-label="Nexis system flow diagram"
      >
        <DiagramFrame />
        <motion.path
          d="M108 42h16"
          stroke="var(--primary)"
          strokeWidth={2}
          strokeLinecap="round"
          initial={{ pathLength: 0, opacity: 0.35 }}
          animate={{
            pathLength: 1,
            opacity: [1, 0.72, 1],
          }}
          transition={{
            pathLength: { ...edgeDraw, delay: 0.52 },
            opacity: { duration: 2.6, repeat: Infinity, ease: "easeInOut", delay: 1.35 },
          }}
        />
        <motion.path
          d="M220 42h16"
          stroke="var(--primary)"
          strokeWidth={2}
          strokeLinecap="round"
          initial={{ pathLength: 0, opacity: 0.35 }}
          animate={{
            pathLength: 1,
            opacity: [1, 0.72, 1],
          }}
          transition={{
            pathLength: { ...edgeDraw, delay: 0.64 },
            opacity: { duration: 2.75, repeat: Infinity, ease: "easeInOut", delay: 1.48 },
          }}
        />
        <motion.path
          d="M332 42h16"
          stroke="var(--primary)"
          strokeWidth={2}
          strokeLinecap="round"
          initial={{ pathLength: 0, opacity: 0.35 }}
          animate={{
            pathLength: 1,
            opacity: [1, 0.72, 1],
          }}
          transition={{
            pathLength: { ...edgeDraw, delay: 0.76 },
            opacity: { duration: 2.9, repeat: Infinity, ease: "easeInOut", delay: 1.6 },
          }}
        />
        <motion.path
          d="M444 42h16"
          stroke="var(--primary)"
          strokeWidth={2}
          strokeLinecap="round"
          initial={{ pathLength: 0, opacity: 0.35 }}
          animate={{
            pathLength: 1,
            opacity: [1, 0.72, 1],
          }}
          transition={{
            pathLength: { ...edgeDraw, delay: 0.88 },
            opacity: { duration: 3.05, repeat: Infinity, ease: "easeInOut", delay: 1.72 },
          }}
        />
        <motion.path
          d="M164 64v54"
          stroke="var(--border-hover)"
          strokeWidth={1.5}
          strokeLinecap="round"
          initial={{ pathLength: 0 }}
          animate={{ pathLength: 1 }}
          transition={{ duration: 0.52, ease: easeSoftOut, delay: 1.02 }}
        />
        <motion.path
          d="M398 64v54"
          stroke="var(--border-hover)"
          strokeWidth={1.5}
          strokeLinecap="round"
          initial={{ pathLength: 0 }}
          animate={{ pathLength: 1 }}
          transition={{ duration: 0.52, ease: easeSoftOut, delay: 1.12 }}
        />
        <motion.circle
          cx={164}
          cy={122}
          r={4.5}
          fill="var(--primary)"
          initial={{ scale: 0.55, opacity: 0 }}
          animate={{ scale: 1, opacity: 1 }}
          transition={{ type: "spring", stiffness: 320, damping: 18, delay: 1.18 }}
        />
        <motion.circle
          cx={398}
          cy={122}
          r={4.5}
          fill="var(--secondary)"
          initial={{ scale: 0.55, opacity: 0 }}
          animate={{ scale: 1, opacity: 1 }}
          transition={{ type: "spring", stiffness: 320, damping: 18, delay: 1.26 }}
        />
      </svg>
    </motion.div>
  );
}

function DiagramFrame() {
  return (
    <>
      <rect x="12" y="20" width="96" height="44" rx="8" stroke="var(--border)" />
      <rect x="124" y="20" width="96" height="44" rx="8" stroke="var(--border)" />
      <rect x="236" y="20" width="96" height="44" rx="8" stroke="var(--border)" />
      <rect x="348" y="20" width="96" height="44" rx="8" stroke="var(--border)" />
      <rect x="460" y="20" width="88" height="44" rx="8" stroke="var(--border)" />

      <text x="30" y="47" fill="var(--text-muted)" fontSize="11" fontFamily="monospace">
        DETECT
      </text>
      <text x="140" y="47" fill="var(--text-muted)" fontSize="11" fontFamily="monospace">
        DIAGNOSE
      </text>
      <text x="250" y="47" fill="var(--text-muted)" fontSize="11" fontFamily="monospace">
        SYNTHESISE
      </text>
      <text x="364" y="47" fill="var(--text-muted)" fontSize="11" fontFamily="monospace">
        VALIDATE
      </text>
      <text x="478" y="47" fill="var(--text-muted)" fontSize="11" fontFamily="monospace">
        EXECUTE
      </text>

      <rect x="80" y="132" width="168" height="60" rx="10" stroke="var(--border)" />
      <rect x="314" y="132" width="168" height="60" rx="10" stroke="var(--border)" />
      <text x="102" y="158" fill="var(--text-primary)" fontSize="13">
        Layer 1: Execution
      </text>
      <text x="336" y="158" fill="var(--text-primary)" fontSize="13">
        Layer 2: Self-Healing
      </text>
      <text x="102" y="177" fill="var(--text-muted)" fontSize="11">
        detect · diagnose · patch
      </text>
      <text x="336" y="177" fill="var(--text-muted)" fontSize="11">
        guardrails · rollback · audit
      </text>
    </>
  );
}

function Wiring() {
  return (
    <>
      <path d="M108 42h16" stroke="var(--primary)" strokeWidth={2} />
      <path d="M220 42h16" stroke="var(--primary)" strokeWidth={2} />
      <path d="M332 42h16" stroke="var(--primary)" strokeWidth={2} />
      <path d="M444 42h16" stroke="var(--primary)" strokeWidth={2} />
      <path d="M164 64v54" stroke="var(--border)" />
      <path d="M398 64v54" stroke="var(--border)" />
      <circle cx="164" cy="122" r="4.5" fill="var(--primary)" />
      <circle cx="398" cy="122" r="4.5" fill="var(--secondary)" />
    </>
  );
}
