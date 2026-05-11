"use client";

import { motion } from "framer-motion";
import { content } from "@/lib/content";
import { SectionLabel } from "@/components/ui/SectionLabel";
import { usePrefersReducedMotion } from "@/lib/usePrefersReducedMotion";

const scaleIn = {
  hidden: { opacity: 0, scale: 0.96 },
  visible: {
    opacity: 1,
    scale: 1,
    transition: { duration: 0.3 },
  },
};

export default function Agents() {
  const reduced = usePrefersReducedMotion();

  return (
    <section
      id="agents"
      className="w-full bg-[var(--color-background)] py-[120px] scroll-mt-[88px]"
      aria-label="Nexis agent fleet"
    >
      <div className="mx-auto max-w-[1200px] px-6">
        <SectionLabel>{content.agents.label}</SectionLabel>
        <h2 className="mt-6 text-[32px] font-medium leading-[1.15] tracking-tight text-[var(--color-foreground)] md:text-[40px]">
          {content.agents.headline}
        </h2>
        <p className="mt-4 max-w-[760px] text-[16px] leading-[1.7] text-[var(--color-muted-foreground)]">
          {content.agents.intro}
        </p>

        <div className="mt-14 grid grid-cols-1 gap-6 sm:grid-cols-2 md:grid-cols-3">
          {content.agents.list.map((agent, idx) => {
            const card = (
              <div className="group relative h-full min-h-[200px] rounded-[12px] border border-[var(--color-border)] bg-[var(--color-card)] p-6 transition-all duration-150 hover:shadow-md hover:ring-1 hover:ring-[var(--color-accent)]">
                <div className="flex items-center justify-between">
                  <span className="font-mono text-[11px] uppercase tracking-[0.12em] text-[var(--color-muted-foreground)]">
                    {agent.role}
                  </span>
                  <span className="font-mono text-[10px] text-[var(--color-muted-foreground)]/70">
                    0{idx + 1}
                  </span>
                </div>
                <h3 className="mt-4 text-[18px] font-medium tracking-[-0.01em] text-[var(--color-foreground)] capitalize">
                  {agent.id}
                </h3>
                <div className="mt-4 space-y-2 text-[13px] leading-[1.6]">
                  <div>
                    <span className="font-mono uppercase text-[10px] tracking-[0.12em] text-[var(--color-primary)]">
                      Owns
                    </span>
                    <p className="text-[var(--color-foreground)]">
                      {agent.owns}
                    </p>
                  </div>
                  <div>
                    <span className="font-mono uppercase text-[10px] tracking-[0.12em] text-[var(--color-muted-foreground)]">
                      Doesn&apos;t own
                    </span>
                    <p className="text-[var(--color-muted-foreground)]">
                      {agent.notOwns}
                    </p>
                  </div>
                </div>
              </div>
            );

            if (reduced) return <div key={agent.id}>{card}</div>;

            return (
              <motion.div
                key={agent.id}
                variants={scaleIn}
                initial="hidden"
                whileInView="visible"
                viewport={{ margin: "-100px", once: true }}
                transition={{ delay: (idx % 3) * 0.06 }}
              >
                {card}
              </motion.div>
            );
          })}
        </div>
      </div>
    </section>
  );
}
