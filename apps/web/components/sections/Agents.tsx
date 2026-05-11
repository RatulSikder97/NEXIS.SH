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
      className="w-full py-[120px] scroll-mt-[88px]"
      aria-label="Nexis system architecture"
    >
      <div className="mx-auto max-w-[1100px] px-6">
        <SectionLabel>{content.agents.label}</SectionLabel>
        <h2 className="mt-6 text-[32px] font-medium leading-[1.15] text-accent md:text-[40px]">
          {content.agents.headline}
        </h2>
        <p className="mt-4 max-w-[720px] text-[16px] leading-[1.7] text-text-secondary">
          {content.agents.intro}
        </p>

        <div className="mt-14 grid grid-cols-1 gap-10 md:grid-cols-2">
          {content.agents.layers.map((layer, idx) => {
            const card = (
              <div className="min-h-[330px] rounded-[12px] border border-border bg-surface p-6">
                <div
                  className={[
                    "mb-5 h-[1px] w-[140px]",
                    idx === 0 ? "bg-primary/70" : "bg-secondary/70",
                  ].join(" ")}
                />
                <div className="flex items-start justify-between gap-4">
                  <h3 className="text-[11px] font-medium tracking-[0.15em] text-text-muted uppercase">
                    {layer.layer}
                  </h3>
                </div>
                <p className="mt-5 text-[16px] leading-[1.7] text-accent">
                  {layer.description}
                </p>
                <ul className="mt-4 space-y-3">
                  {layer.bullets.map((bullet) => (
                    <li
                      key={bullet}
                      className="flex gap-3 text-[14px] leading-[1.7] text-text-secondary"
                    >
                      <span
                        className={[
                          "mt-[8px] h-[6px] w-[6px] rounded-full",
                          idx === 0 ? "bg-primary" : "bg-secondary",
                        ].join(" ")}
                      />
                      {bullet}
                    </li>
                  ))}
                </ul>
              </div>
            );

            if (reduced) return <div key={layer.layer}>{card}</div>;

            return (
              <motion.div
                key={layer.layer}
                variants={scaleIn}
                initial="hidden"
                whileInView="visible"
                viewport={{ margin: "-100px", once: true }}
                transition={{ delay: idx * 0.08 }}
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

