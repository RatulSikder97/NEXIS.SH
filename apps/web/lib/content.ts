// Marketing copy + nav graph for the public surface. Every internal route
// here is a real, typed Next.js route. In-page anchors (#features, #how-it-
// works, #agents) remain inside `/` because those sections live on the
// landing.
//
// Internal hrefs that start with "/" are passed to <Link href={...} /> after
// being cast as `Route` at the call site — typedRoutes:true is on.
// External hrefs (github, x, linkedin) are passed to <a>.

export const content = {
  navbar: {
    links: [
      { label: "Product", href: "/product" },
      { label: "Agents", href: "/agents" },
      { label: "Integrations", href: "/integrations" },
      { label: "Pricing", href: "/pricing" },
      { label: "Docs", href: "/docs" },
    ],
    cta: "Get started",
  },
  hero: {
    statusPill: { label: "Public beta — invite-only", href: "/status" },
    preHeading: "AUTONOMOUS ENGINEERING, SUPERVISED BY YOU.",
    heading:
      "Nine AI agents. One engineering team that ships fixes while you sleep.",
    subheading:
      "Detect → diagnose → patch → validate → approve → deploy. Closed-loop fault recovery for CI/CD and production pipelines, with the engineer always in the loop on what matters.",
    ctaPrimary: "Get started",
    ctaSecondary: "See how it works →",
    socialProof:
      "For SRE and platform teams who are done with 3am pages.",
    terminalTitle: "nexis.sh",
    terminalLines: [
      "[00:02:14] sentinel › anomaly detected — pipeline: etl_orders",
      "[00:02:15] pathfinder › traversing dependency graph...",
      "[00:02:17] pathfinder › root cause: schema drift on orders.total_amount (float → string)",
      "[00:02:18] synthesiser › generating candidate patches...",
      "[00:02:21] synthesiser › patch_001 ready — cast coercion + downstream migration",
      "[00:02:22] validator › deploying to shadow pipeline...",
      "[00:02:29] validator › 2,847 property tests passed. 0 failures.",
      "[00:02:30] nexis › patch awaiting approval ✓",
    ],
  },
  problem: {
    label: "THE PROBLEM",
    headline:
      "Engineers spend 40–60% of their time fighting fires, not building.",
    intro:
      "Incident response is still fragmented: detection, root-cause analysis, and remediation live in different tools, so engineers manually stitch the story while outages burn minutes on the clock.",
    points: [
      {
        title: "Silent failures",
        description:
          "Pipelines break hours before anyone notices. Damage is already done.",
      },
      {
        title: "Manual root cause",
        description:
          "Every incident means digging through logs, graphs, and schemas by hand.",
      },
      {
        title: "No closed loop",
        description:
          "Existing tools detect or suggest. None go from failure to a deployed, validated fix.",
      },
    ],
  },
  howItWorks: {
    label: "HOW NEXIS WORKS",
    headline: "Incidents in. Verified patches out.",
    lead:
      "A six-step autonomous remediation flow—from first anomaly signal to an engineer-approved rollout—with auditability at every hop.",
    steps: [
      {
        number: "01",
        title: "Detect",
        description:
          "Monitor logs, traces, and metrics. Flag anomalies fast—before impact spreads.",
      },
      {
        number: "02",
        title: "Diagnose",
        description:
          "Correlate signals across services and dependencies to isolate the root cause.",
      },
      {
        number: "03",
        title: "Synthesise",
        description:
          "Generate candidate patches constrained by your contracts, schemas, and API expectations.",
      },
      {
        number: "04",
        title: "Validate",
        description:
          "Run each patch in a Docker-isolated shadow pipeline with targeted tests and checks.",
      },
      {
        number: "05",
        title: "Execute",
        description:
          "Apply the chosen fix through your deployment workflow (staged rollout with safety checks).",
      },
      {
        number: "06",
        title: "Approve",
        description:
          "Review the diff, results, and a plain-English summary. Approve with one click when ready.",
      },
    ],
  },
  agents: {
    label: "THE FLEET",
    headline: "Nine specialised agents. One closed loop.",
    intro:
      "Each agent owns one job in the pipeline. Hand-offs are typed, retried, and audit-logged.",
    list: [
      {
        id: "sentinel",
        role: "Detection",
        owns: "Anomaly detection from Sentry/OTel",
        notOwns: "Graph traversal",
      },
      {
        id: "pathfinder",
        role: "Diagnosis",
        owns: "Causal RCA via Neo4j + DoWhy",
        notOwns: "Patch synthesis",
      },
      {
        id: "synthesiser",
        role: "Patch generation",
        owns: "LLM patch synthesis + retrieval",
        notOwns: "Validation",
      },
      {
        id: "architect",
        role: "Plan",
        owns: "Solution plan against contracts",
        notOwns: "Code emission",
      },
      {
        id: "backend",
        role: "Codegen",
        owns: "Backend patch synthesis",
        notOwns: "DB migrations",
      },
      {
        id: "qa",
        role: "Test gen",
        owns: "Unit + property test generation",
        notOwns: "Sandbox execution",
      },
      {
        id: "devops",
        role: "Pipeline",
        owns: "ArgoCD / GH Actions YAML changes",
        notOwns: "App code",
      },
      {
        id: "data engineer",
        role: "Migrations",
        owns: "Schema migrations + data backfill",
        notOwns: "API layer",
      },
      {
        id: "approval gate",
        role: "Routing",
        owns: "Severity routing + audit log",
        notOwns: "Patch decisions",
      },
    ],
  },
  metrics: {
    label: "OUTCOMES",
    headline: "Pipeline recovery you can measure.",
    stats: [
      {
        prefix: "MTTR ↓ ",
        number: 60,
        suffix: "%",
        label: "vs. manual response baseline",
      },
      {
        prefix: "≥ ",
        number: 80,
        suffix: "%",
        label: "patch correctness rate",
      },
      {
        prefix: "< ",
        number: 90,
        suffix: "s",
        label: "to fault detection",
      },
    ],
  },
  cta: {
    headline: "Give your team back its weekends.",
    sub: "NEXIS runs an autonomous incident-response loop with the engineer in the loop on what matters. Approve once. Sleep through the rest.",
    button: "Request access",
  },
  footer: {
    tagline:
      "Closed-loop fault recovery for CI/CD and production pipelines.",
    columns: [
      {
        title: "Product",
        links: [
          { label: "Overview", href: "/product" },
          { label: "Agents", href: "/agents" },
          { label: "Integrations", href: "/integrations" },
          { label: "Pricing", href: "/pricing" },
        ],
      },
      {
        title: "Resources",
        links: [
          { label: "Docs", href: "/docs" },
          { label: "Changelog", href: "/changelog" },
          { label: "Status", href: "/status" },
        ],
      },
      {
        title: "Company",
        links: [
          { label: "About", href: "/about" },
          { label: "Careers", href: "/careers" },
          { label: "Contact", href: "/contact" },
        ],
      },
      {
        title: "Legal",
        links: [
          { label: "Privacy", href: "/privacy" },
          { label: "Terms", href: "/terms" },
          { label: "Security", href: "/security" },
        ],
      },
      {
        title: "Connect",
        links: [
          { label: "GitHub", href: "https://github.com/nexis-eco" },
          { label: "X", href: "https://x.com/nexis_eco" },
          { label: "LinkedIn", href: "https://www.linkedin.com/company/nexis-eco" },
        ],
      },
    ],
    status: "All systems operational",
    compliance: "SOC 2 in progress",
    copyright: "All rights reserved.",
  },
};
