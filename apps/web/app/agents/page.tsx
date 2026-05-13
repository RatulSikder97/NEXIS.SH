// /agents — The Fleet page. Renders detailed cards for every agent in the
// recovery pipeline plus an animated hand-off diagram. Server component;
// hand-off animation is delegated to a client child component.
import Link from "next/link";
import type { Route } from "next";
import type { Metadata } from "next";
import {
  Activity,
  Box,
  BrainCircuit,
  Briefcase,
  CircleSlash2,
  Code2,
  Database,
  FlaskConical,
  GitBranch,
  Network,
  Radar,
  ShieldCheck,
  TerminalSquare,
} from "lucide-react";

import { MarketingShell } from "@/components/marketing/MarketingShell";
import { PageHero } from "@/components/marketing/PageHero";
import { Section, SectionInner } from "@/components/marketing/SectionContainer";
import { SectionHeading } from "@/components/marketing/SectionHeading";
import { PipelineDiagram } from "@/components/marketing/PipelineDiagram";
import { Button } from "@/components/ui/Button";

export const metadata: Metadata = {
  title: "Agents — The Fleet",
  description:
    "Nine specialised agents (L1 + L2 + router + detector). Each owns one job in the closed loop. Typed hand-offs, retries, and full audit log.",
};

type Agent = {
  id: string;
  tier: "L1" | "L2" | "Router" | "Detector";
  role: string;
  Icon: typeof Radar;
  owns: string[];
  notOwns: string[];
  input: string;
  output: string;
  model: string;
  latency: string;
};

const AGENTS: Agent[] = [
  {
    id: "Sentinel",
    tier: "Detector",
    role: "Anomaly detection",
    Icon: Radar,
    owns: [
      "Streaming anomaly detection from Sentry + OTel + Datadog",
      "Severity classification (P0–P3)",
      "Cross-signal correlation within a 5-minute window",
    ],
    notOwns: ["Causal traversal", "Patch synthesis"],
    input: "events from observability adapters",
    output: "incident_id + severity + initial signal bundle",
    model: "tuned classifier (no LLM)",
    latency: "p50 1.2s · p95 4.4s",
  },
  {
    id: "Pathfinder",
    tier: "L1",
    role: "Causal RCA",
    Icon: Network,
    owns: [
      "Graph traversal over services × deploys × schemas",
      "DoWhy-backed counterfactual analysis",
      "Confidence scoring per causal path",
    ],
    notOwns: ["Patch generation", "Test authoring"],
    input: "incident_id",
    output: "ranked root-cause hypotheses",
    model: "Claude Sonnet 4.5 (tools: Neo4j, DoWhy)",
    latency: "p50 7s · p95 18s",
  },
  {
    id: "Synthesiser",
    tier: "L1",
    role: "Patch generation",
    Icon: Code2,
    owns: [
      "LLM patch synthesis constrained to your contracts",
      "Retrieval over recent diffs + design docs",
      "Patch ranking by blast radius",
    ],
    notOwns: ["Validation", "Deployment"],
    input: "root-cause hypothesis + repo context",
    output: "candidate diffs (k ≤ 5)",
    model: "Claude Sonnet 4.5",
    latency: "p50 11s · p95 28s",
  },
  {
    id: "Architect",
    tier: "L2",
    role: "Solution planning",
    Icon: BrainCircuit,
    owns: [
      "High-level solution plan against contract repo",
      "Cross-service coordination check",
      "Migration ordering",
    ],
    notOwns: ["Code emission"],
    input: "candidate diffs",
    output: "solution plan + dependency order",
    model: "Claude Sonnet 4.5",
    latency: "p50 5s · p95 12s",
  },
  {
    id: "Backend",
    tier: "L1",
    role: "Backend codegen",
    Icon: TerminalSquare,
    owns: [
      "Application-layer patches (Go, TS, Python, Rust)",
      "Type-safe edits with compiler verification",
      "Style-conformant emission",
    ],
    notOwns: ["Database migrations", "Pipeline YAML"],
    input: "solution plan slice",
    output: "code diff",
    model: "Claude Sonnet 4.5",
    latency: "p50 9s · p95 22s",
  },
  {
    id: "QA",
    tier: "L1",
    role: "Test generation",
    Icon: FlaskConical,
    owns: [
      "Unit + property test generation per candidate",
      "Regression test for the failing scenario",
      "Shadow-pipeline orchestration",
    ],
    notOwns: ["Sandbox container management"],
    input: "code diff",
    output: "test suite + pass/fail report",
    model: "Claude Sonnet 4.5",
    latency: "p50 14s · p95 38s",
  },
  {
    id: "DevOps",
    tier: "L1",
    role: "Pipeline changes",
    Icon: Box,
    owns: [
      "ArgoCD application manifests",
      "GitHub Actions / GitLab CI YAML",
      "Helm chart edits",
    ],
    notOwns: ["Application code"],
    input: "solution plan slice",
    output: "infra diff",
    model: "Claude Sonnet 4.5",
    latency: "p50 6s · p95 14s",
  },
  {
    id: "Data Engineer",
    tier: "L1",
    role: "Schema migrations",
    Icon: Database,
    owns: [
      "Backfill plans + cutover windows",
      "Forward + reverse migration emission",
      "Data quality assertions",
    ],
    notOwns: ["API layer"],
    input: "solution plan slice",
    output: "migration diff + cutover plan",
    model: "Claude Sonnet 4.5",
    latency: "p50 11s · p95 27s",
  },
  {
    id: "Approval Gate",
    tier: "Router",
    role: "Severity routing + audit",
    Icon: ShieldCheck,
    owns: [
      "Severity routing (Slack DM, channel, PagerDuty, ticket)",
      "Append-only audit log",
      "Approval policy enforcement",
    ],
    notOwns: ["Patch correctness decisions"],
    input: "validated diff + summary",
    output: "approval verdict + audit row",
    model: "router (no LLM)",
    latency: "p50 0.4s · p95 1.1s",
  },
];

function TierBadge({ tier }: { tier: Agent["tier"] }) {
  const palette = {
    Detector: "bg-[color-mix(in_srgb,var(--color-warning)_18%,transparent)] text-[var(--color-warning)]",
    L1: "bg-[color-mix(in_srgb,var(--color-primary)_15%,transparent)] text-[var(--color-primary)]",
    L2: "bg-[color-mix(in_srgb,var(--color-success)_18%,transparent)] text-[var(--color-success)]",
    Router: "bg-[var(--color-muted)] text-[var(--color-muted-foreground)]",
  }[tier];
  return (
    <span
      className={`inline-flex items-center rounded-full px-2 py-0.5 text-[10px] font-medium uppercase tracking-[0.12em] ${palette}`}
    >
      {tier}
    </span>
  );
}

export default function AgentsPage() {
  return (
    <MarketingShell>
      <PageHero
        eyebrow="THE FLEET"
        title="Nine agents. One closed loop. Typed hand-offs end to end."
        lead="Each agent owns one job in the recovery pipeline. Hand-offs are typed (with retries), latency is exposed, and every decision lands in an append-only audit log."
        actions={
          <>
            <Button size="lg" asChild>
              <Link href={"/sign-up" as Route}>Try it free</Link>
            </Button>
            <Button variant="outline" size="lg" asChild>
              <Link href={"/docs" as Route}>Agent reference →</Link>
            </Button>
          </>
        }
      />

      {/* Animated pipeline diagram */}
      <Section tone="muted">
        <SectionInner>
          <SectionHeading
            eyebrow="RECOVERY PIPELINE"
            title="The 8 stops every incident takes."
            lead="From anomaly to approval, every hand-off is typed and observable. Hover any node to see what each agent receives and emits."
          />
          <div className="mt-12">
            <PipelineDiagram />
          </div>
        </SectionInner>
      </Section>

      {/* Per-agent cards */}
      <Section tone="background">
        <SectionInner>
          <SectionHeading
            eyebrow="THE NINE"
            title="Detailed cards for every agent."
            lead="Models, latencies, inputs and outputs — exactly what you'd want from a runbook."
          />
          <div className="mt-12 grid grid-cols-1 gap-5 sm:grid-cols-2 lg:grid-cols-3">
            {AGENTS.map((a) => (
              <article
                key={a.id}
                className="group relative flex h-full flex-col rounded-[14px] border border-[var(--color-border)] bg-[var(--color-card)] p-6 shadow-sm transition-all hover:-translate-y-[2px] hover:shadow-md"
              >
                <header className="flex items-start justify-between gap-3">
                  <span className="flex h-11 w-11 items-center justify-center rounded-xl bg-[color-mix(in_srgb,var(--color-primary)_12%,var(--color-muted))] text-[var(--color-primary)]">
                    <a.Icon className="h-5 w-5" aria-hidden />
                  </span>
                  <TierBadge tier={a.tier} />
                </header>
                <h3 className="mt-4 text-[18px] font-medium text-[var(--color-foreground)]">
                  {a.id}
                </h3>
                <p className="mt-1 text-[13px] text-[var(--color-muted-foreground)]">
                  {a.role}
                </p>

                <dl className="mt-5 space-y-3 text-[12px] leading-[1.5]">
                  <div>
                    <dt className="font-mono text-[10px] uppercase tracking-[0.12em] text-[var(--color-primary)]">
                      Owns
                    </dt>
                    <dd className="mt-1 text-[var(--color-foreground)]">
                      <ul className="list-disc space-y-1 pl-4">
                        {a.owns.map((o) => (
                          <li key={o}>{o}</li>
                        ))}
                      </ul>
                    </dd>
                  </div>
                  <div>
                    <dt className="font-mono text-[10px] uppercase tracking-[0.12em] text-[var(--color-muted-foreground)] flex items-center gap-1">
                      <CircleSlash2 className="h-3 w-3" aria-hidden /> Doesn&apos;t own
                    </dt>
                    <dd className="mt-1 text-[var(--color-muted-foreground)]">
                      {a.notOwns.join(", ")}
                    </dd>
                  </div>
                </dl>

                <footer className="mt-6 grid grid-cols-2 gap-3 border-t border-[var(--color-border)] pt-4 text-[11px] text-[var(--color-muted-foreground)]">
                  <div>
                    <div className="font-mono uppercase tracking-[0.1em] text-[10px]">Input</div>
                    <div className="mt-0.5 text-[var(--color-foreground)]">{a.input}</div>
                  </div>
                  <div>
                    <div className="font-mono uppercase tracking-[0.1em] text-[10px]">Output</div>
                    <div className="mt-0.5 text-[var(--color-foreground)]">{a.output}</div>
                  </div>
                  <div className="col-span-2 mt-1 flex items-center justify-between text-[11px]">
                    <span className="inline-flex items-center gap-1 text-[var(--color-muted-foreground)]">
                      <Briefcase className="h-3 w-3" aria-hidden /> {a.model}
                    </span>
                    <span className="inline-flex items-center gap-1 text-[var(--color-muted-foreground)]">
                      <Activity className="h-3 w-3" aria-hidden /> {a.latency}
                    </span>
                  </div>
                </footer>
              </article>
            ))}
          </div>
        </SectionInner>
      </Section>

      <Section tone="accent" compact>
        <SectionInner>
          <div className="flex flex-col items-center gap-4 text-center md:flex-row md:justify-between md:text-left">
            <div>
              <h2 className="text-[24px] font-medium leading-[1.2] tracking-tight text-[var(--color-foreground)] md:text-[30px]">
                See the fleet in action.
              </h2>
              <p className="mt-2 max-w-[640px] text-[14px] text-[var(--color-muted-foreground)]">
                Run a synthetic incident in the live console — no production
                integration required.
              </p>
            </div>
            <div className="flex flex-wrap gap-3">
              <Button asChild>
                <Link href={"/sign-up" as Route}>Try synthetic incidents</Link>
              </Button>
              <Button variant="outline" asChild>
                <Link href={"/integrations" as Route}>
                  <GitBranch className="h-4 w-4" aria-hidden /> Integrations
                </Link>
              </Button>
            </div>
          </div>
        </SectionInner>
      </Section>
    </MarketingShell>
  );
}
