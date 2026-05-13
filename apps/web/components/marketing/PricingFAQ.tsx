"use client";

import { useState } from "react";
import { ChevronDown } from "lucide-react";

type QA = { q: string; a: string };

const FAQS: QA[] = [
  {
    q: "What counts as a “successful recovery”?",
    a: "A patch that lands in your repo via PR and is approved. We don’t charge for failed candidates, abandoned investigations, or rolled-back fixes.",
  },
  {
    q: "How are LLM tokens metered?",
    a: "Each agent reports input + output tokens per call, broken down per model. You can see the live spend in /console/ai-energy. Token rates pass through at provider cost + 25% margin for Pro; Enterprise customers can bring their own model gateway and bypass our margin entirely.",
  },
  {
    q: "Is there a free trial of Pro?",
    a: "Yes — every new workspace gets 30 days of Pro features enabled by default. After that you stay on Pro unless you downgrade. No credit card required for the Free tier.",
  },
  {
    q: "Can we cap monthly spend?",
    a: "Yes. Workspace admins set per-project budget ceilings on /console/ai-energy. When 80% is hit, NEXIS pauses non-critical agents and alerts the workspace owner; at 100% only severity-0 detection runs.",
  },
  {
    q: "Do you offer non-profit / academic pricing?",
    a: "We offer 70% off Pro for academic research labs and approved open-source orgs. Email sales@nexis.dev with proof of affiliation.",
  },
  {
    q: "What happens to my data if I cancel?",
    a: "Audit logs are retained for the lifetime of the workspace plus 30 days, then permanently deleted. We can export a JSON archive on request before deletion. Source code we touch never leaves your repository — NEXIS reads via your existing GitHub App scopes.",
  },
  {
    q: "Are there volume discounts?",
    a: "Pro tier rates are flat. Enterprise customers get committed-use discounts based on annual recovery volume — typically 30–50% off list at our top tier.",
  },
  {
    q: "Do you charge for failed integrations?",
    a: "No. If a provider’s API rejects our requests or your credentials are revoked, the integration shows up as ‘down’ in /console/integrations and no agent runs. You only pay for work that produced output.",
  },
];

export function PricingFAQ() {
  const [openIdx, setOpenIdx] = useState<number | null>(0);

  return (
    <ul className="divide-y divide-[var(--color-border)] overflow-hidden rounded-[14px] border border-[var(--color-border)] bg-[var(--color-card)] shadow-sm">
      {FAQS.map((item, idx) => {
        const open = openIdx === idx;
        return (
          <li key={item.q}>
            <button
              type="button"
              onClick={() => setOpenIdx(open ? null : idx)}
              aria-expanded={open}
              className="flex w-full items-center justify-between gap-6 px-6 py-5 text-left transition-colors hover:bg-[var(--color-muted)]/40"
            >
              <span className="text-[15px] font-medium text-[var(--color-foreground)]">
                {item.q}
              </span>
              <ChevronDown
                aria-hidden
                className={`h-4 w-4 shrink-0 text-[var(--color-muted-foreground)] transition-transform ${
                  open ? "rotate-180" : ""
                }`}
              />
            </button>
            <div
              className={`grid overflow-hidden transition-all duration-200 ease-out ${
                open
                  ? "grid-rows-[1fr] opacity-100"
                  : "grid-rows-[0fr] opacity-0"
              }`}
            >
              <div className="overflow-hidden">
                <p className="px-6 pb-6 text-[14px] leading-[1.7] text-[var(--color-muted-foreground)]">
                  {item.a}
                </p>
              </div>
            </div>
          </li>
        );
      })}
    </ul>
  );
}
