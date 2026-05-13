"use client";

import { motion } from "framer-motion";
import {
  BrainCircuit,
  Code2,
  Database,
  FlaskConical,
  Network,
  Radar,
  ShieldCheck,
  TerminalSquare,
} from "lucide-react";

import { usePrefersReducedMotion } from "@/lib/usePrefersReducedMotion";

// Eight-stop recovery pipeline. Sentinel → Pathfinder → Architect → (Backend
// + DevOps + DataEng + QA parallel) → Approval Gate. Visualised as a row of
// nodes with animated arrows between them so visitors get a sense of the
// hand-off shape without having to read the full /agents copy.

type Stop = { id: string; label: string; Icon: typeof Radar };

const STOPS: Stop[] = [
  { id: "sentinel", label: "Sentinel", Icon: Radar },
  { id: "pathfinder", label: "Pathfinder", Icon: Network },
  { id: "architect", label: "Architect", Icon: BrainCircuit },
  { id: "synth", label: "Synthesiser", Icon: Code2 },
  { id: "backend", label: "Backend", Icon: TerminalSquare },
  { id: "data", label: "Data Eng.", Icon: Database },
  { id: "qa", label: "QA", Icon: FlaskConical },
  { id: "gate", label: "Approval Gate", Icon: ShieldCheck },
];

export function PipelineDiagram() {
  const reduced = usePrefersReducedMotion();

  return (
    <div className="relative overflow-x-auto rounded-[14px] border border-[var(--color-border)] bg-[var(--color-card)] p-8 shadow-sm">
      <div className="relative mx-auto flex min-w-[840px] items-center justify-between gap-3">
        {STOPS.map((stop, idx) => (
          <div key={stop.id} className="contents">
            <div className="relative z-10 flex flex-col items-center gap-2">
              <span className="flex h-14 w-14 items-center justify-center rounded-2xl border border-[var(--color-border)] bg-[var(--color-card)] text-[var(--color-primary)] shadow-sm transition-all hover:shadow-md hover:-translate-y-[2px]">
                <stop.Icon className="h-6 w-6" aria-hidden />
              </span>
              <span className="text-[11px] font-medium text-[var(--color-foreground)] whitespace-nowrap">
                {stop.label}
              </span>
              <span className="font-mono text-[10px] uppercase tracking-[0.12em] text-[var(--color-muted-foreground)]">
                step {String(idx + 1).padStart(2, "0")}
              </span>
            </div>
            {idx < STOPS.length - 1 ? (
              <div
                className="relative h-px flex-1 bg-[var(--color-border)]"
                aria-hidden
              >
                {reduced ? null : (
                  <motion.span
                    className="absolute inset-y-0 h-px bg-gradient-to-r from-transparent via-[var(--color-primary)] to-transparent"
                    initial={{ x: "-100%", width: "40%" }}
                    animate={{ x: "260%" }}
                    transition={{
                      duration: 2.6,
                      repeat: Infinity,
                      ease: "linear",
                      delay: idx * 0.18,
                    }}
                  />
                )}
              </div>
            ) : null}
          </div>
        ))}
      </div>
      <p className="mt-8 text-center text-[12px] text-[var(--color-muted-foreground)]">
        Backend / Data Eng. / QA fan out in parallel during recovery — they
        share the same solution plan but emit independent diffs that QA
        reconciles before approval.
      </p>
    </div>
  );
}
