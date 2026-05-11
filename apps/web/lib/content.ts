export const content = {
  navbar: {
    links: [
      { label: "Features", href: "#features" },
      { label: "How It Works", href: "#how-it-works" },
      { label: "Agents", href: "#agents" },
    ],
    cta: "Request Early Access",
  },
  hero: {
    preHeading: "AUTONOMOUS ENGINEERING PLATFORM",
    heading: "Your pipeline breaks. Nexis fixes it.",
    subheading:
      "Nexis watches CI/CD and production pipelines, spots failures fast, traces root cause across services and schemas, synthesizes a verified patch in an isolated shadow pipeline, and queues it for your approval—so MTTR drops from hours to minutes.",
    ctaPrimary: "Request Early Access",
    ctaSecondary: "See how it works →",
    socialProof: "Built for engineering teams tired of 3am incidents.",
    terminalTitle: "nexis.sh",
    terminalLines: [
      "[00:02:14] sentinel › anomaly detected — pipeline: etl_orders",
      "[00:02:15] pathfinder › traversing dependency graph...",
      "[00:02:17] pathfinder › root cause: schema drift on orders.total_amount (float → string)",
      "[00:02:18] synthesiser › generating candidate patches...",
      "[00:02:21] synthesiser › patch_001 ready — cast coercion + downstream migration",
      "[00:02:22] validator › deploying to shadow pipeline...",
      "[00:02:29] validator › 2,847 property tests passed. 0 failures.",
      "[00:02:30] nexis › patch approved for review ✓",
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
    label: "THE SYSTEM",
    headline: "Execution, validation, and self-healing—end to end.",
    intro:
      "Nexis runs a dedicated closed-loop pipeline across discovery, synthesis, validation, and deployment. You get a verified patch with a plain-English explanation—then you approve.",
    layers: [
      {
        layer: "Layer 1 — Execution",
        description:
          "Turns live failures into contract-constrained candidate fixes.",
        bullets: [
          "Detect anomalies in minutes",
          "Diagnose root cause via dependency traversal",
          "Synthesize candidate patches under system contracts",
          "Stage deployment after approval",
        ],
      },
      {
        layer: "Layer 2 — Self-Healing",
        description:
          "Protects correctness when schemas drift or interfaces change.",
        bullets: [
          "Repair schema drift automatically",
          "Enforce API/contract boundaries",
          "Validate in a Docker-isolated shadow pipeline",
          "Maintain a full audit trail for every decision",
        ],
      },
    ],
  },
  metrics: {
    label: "IMPACT",
    headline: "Pipeline recovery you can measure",
    stats: [
      { prefix: "< ", number: 90, suffix: "s", label: "Time to fault detection" },
      {
        prefix: "≥ ",
        number: 80,
        suffix: "%",
        label: "Patch correctness rate",
      },
      {
        prefix: "",
        number: 60,
        suffix: "%+",
        label: "MTTR reduction vs. manual baseline",
      },
    ],
  },
  cta: {
    headline: "Stop debugging. Start shipping.",
    sub: "Nexis runs autonomous incident response for your pipelines and services end to end. You stay in control with a single approval before anything reaches production.",
    button: "Request Early Access",
  },
  footer: {
    tagline: "Your pipeline breaks. Nexis fixes it.",
    links: [
      { label: "Features", href: "#features" },
      { label: "How It Works", href: "#how-it-works" },
      { label: "Agents", href: "#agents" },
    ],
    copyright: "All rights reserved.",
  },
};

