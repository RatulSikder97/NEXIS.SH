// Live Demo — sector-grouped fault scenario catalog.
//
// Each entry maps a single user-facing fault scenario onto the control-plane
// recovery pipeline. The `id` is the canonical scenario name that the
// backend allowlists in services/control-plane/internal/transport/http/
// handler/pipelines.go — only entries with `ready: true` are wired today.
//
// New scenarios are added by:
//   1. Dropping a fixture into services/control-plane/fixtures/scenarios/
//      <id>.json (the L2 fleet consumes it via `incident`).
//   2. Allowlisting the id + fixture mapping in pipelines.go.
//   3. Flipping `ready` to true here.
//
// Wire-format contract:
//   * `id` is snake_case and mirrors the JSON `scenario` field on
//     POST /v1/workspaces/{ws}/pipelines (or /pipelines/demo).
//   * `severity` carries the severity bucket the Synthesiser is expected to
//     classify the run into. It's also the value posted on the wire as the
//     `severity` field.
//   * `source_provider` matches the integration provider id surfaced via
//     ProviderLogo — the demo posts it as `source` so audit trails can
//     pretend the incident came in via that integration.
//   * `expected_agents` is presentation-only; it tells the operator which
//     L2/L1 agents will fire so they know what to look for in the timeline.
//   * `expected_duration_ms` is a rough wall-clock estimate for the
//     synthetic run — it drives the card's "~30s" hint.
//   * `icon_hint` is the lucide-react icon name; the page resolves it to a
//     LucideIcon via the local map below.
//   * `ready: true` means the backend already has a fixture+allowlist; the
//     "Run scenario" button is enabled.

import type { ProviderID } from "@/components/integrations/ProviderLogo";

export type DemoScenarioSector =
  | "application"
  | "observability"
  | "deploy"
  | "data"
  | "security"
  | "infrastructure";

export type DemoScenarioSeverity = "low" | "medium" | "high" | "critical";

export type DemoScenarioSourceProvider = Extract<
  ProviderID,
  "sentry" | "datadog" | "pagerduty" | "github" | "argocd"
> | "synthetic";

export type DemoScenario = {
  // Canonical scenario name. Sent on the wire as `scenario`.
  id: string;
  // Top-level grouping in the catalog UI.
  sector: DemoScenarioSector;
  // Card title — short, user-facing.
  title: string;
  // One-line description rendered on the card.
  description: string;
  // 2-3 sentence narrative shown in the "Run scenario" modal.
  details: string;
  // Severity bucket the Synthesiser is expected to assign.
  severity: DemoScenarioSeverity;
  // Source provider the incident is shaped to look like it came from.
  // `synthetic` means the demo isn't tied to a specific real integration.
  source_provider: DemoScenarioSourceProvider;
  // Agent roles that will fire during the run (presentation-only).
  expected_agents: string[];
  // Rough wall-clock for the synthetic run.
  expected_duration_ms: number;
  // lucide-react icon name (see ICON_MAP in the page).
  icon_hint: string;
  // Whether the backend can actually run this scenario today. `false`
  // hides the run button and surfaces a "Coming soon" affordance.
  ready: boolean;
};

// SECTOR_META gives every sector a label + intro line used as the section
// header on the catalog page. Order is the rendering order top-to-bottom.
export const SECTOR_META: Record<
  DemoScenarioSector,
  { label: string; intro: string }
> = {
  application: {
    label: "Application bugs",
    intro: "Runtime exceptions and logic faults captured by APM tooling.",
  },
  data: {
    label: "Database & data pipelines",
    intro:
      "Storage, schema, query, batch, and streaming faults across the data plane.",
  },
  deploy: {
    label: "Deploy & rollout",
    intro:
      "Bad canaries, manifest mistakes, and other GitOps + CI/CD failures.",
  },
  infrastructure: {
    label: "Infrastructure",
    intro: "Kubernetes, networking, and certificate-level faults.",
  },
  observability: {
    label: "Observability & runtime",
    intro: "Slow leaks, throttling, and SLO breaches surfaced by metrics.",
  },
  security: {
    label: "Security",
    intro: "Credentials, auth, and policy violations flagged by guardrails.",
  },
};

// SCENARIOS is hand-curated. The user wants visibility into the full plan
// so we ship every scenario the roadmap calls for, even those with no
// fixture wired yet — `ready: false` keeps them visible but disabled.
export const SCENARIOS: DemoScenario[] = [
  // ── Application bugs ──────────────────────────────────────────────────
  {
    // `null-deref` is the canonical id already on the backend allowlist
    // (see services/control-plane/internal/transport/http/handler/
    // pipelines.go::demoScenarios). It maps to demo-null-pointer.json.
    // Using the wired id keeps the catalog → backend hop a single hop.
    id: "null-deref",
    sector: "application",
    title: "Null-pointer in Orders API",
    description:
      "An Orders endpoint dereferences an optional field that wasn't guarded.",
    details:
      "Sentinel ingests the Sentry crash. Pathfinder isolates the unsafe access, Architect drafts a defensive patch, Backend codes it, and QA generates a regression test before ApprovalGate routes the change.",
    severity: "high",
    source_provider: "sentry",
    expected_agents: ["sentinel", "pathfinder", "architect", "backend", "qa"],
    expected_duration_ms: 35_000,
    icon_hint: "Bug",
    ready: true,
  },
  {
    id: "unhandled_promise_rejection",
    sector: "application",
    title: "Unhandled promise rejection",
    description:
      "An async error escapes the request scope and tears down the worker.",
    details:
      "Sentinel correlates the unhandled rejection with the worker restart, Pathfinder traces it to the missing await, and Backend wraps the call in a try/catch before QA adds an async regression.",
    severity: "high",
    source_provider: "sentry",
    expected_agents: ["sentinel", "pathfinder", "backend", "qa"],
    expected_duration_ms: 40_000,
    icon_hint: "Zap",
    ready: false,
  },
  {
    id: "regex_catastrophic_backtracking",
    sector: "application",
    title: "ReDoS in input validator",
    description: "A user-supplied string triggers catastrophic backtracking.",
    details:
      "Datadog flags p99 latency spike. Pathfinder identifies the offending regex, Architect proposes a linear-time alternative, and DevOps stages a hotfix behind a feature flag.",
    severity: "critical",
    source_provider: "datadog",
    expected_agents: [
      "sentinel",
      "pathfinder",
      "architect",
      "backend",
      "devops",
    ],
    expected_duration_ms: 55_000,
    icon_hint: "AlertOctagon",
    ready: false,
  },
  {
    id: "division_by_zero",
    sector: "application",
    title: "Division by zero in pricing",
    description:
      "A pricing calculator divides by a quantity that can be zero.",
    details:
      "Sentinel pulls the stack trace from Sentry. Pathfinder narrows it to the unguarded denominator, Backend ships a clamped fallback, and QA pins the edge case with property-based tests.",
    severity: "medium",
    source_provider: "sentry",
    expected_agents: ["sentinel", "pathfinder", "backend", "qa"],
    expected_duration_ms: 30_000,
    icon_hint: "Divide",
    ready: false,
  },
  {
    id: "string_index_out_of_bounds",
    sector: "application",
    title: "Off-by-one in string parser",
    description:
      "An index expression overruns the buffer for empty inputs.",
    details:
      "Sentinel grabs the crash. Pathfinder walks the parser to the over-read, Backend adds an explicit length check, and QA seeds the corpus with empty + single-char inputs.",
    severity: "medium",
    source_provider: "sentry",
    expected_agents: ["sentinel", "pathfinder", "backend", "qa"],
    expected_duration_ms: 30_000,
    icon_hint: "ListOrdered",
    ready: false,
  },

  // ── Database / persistence ────────────────────────────────────────────
  {
    id: "connection_pool_exhausted",
    sector: "data",
    title: "Connection pool exhausted",
    description:
      "All Postgres pool slots in use; new requests time out at the boundary.",
    details:
      "Datadog sees the queue depth climb. Pathfinder ties leaked connections to a long-running cursor, Architect proposes a context-bound iterator, and DevOps tunes pool sizing.",
    severity: "high",
    source_provider: "datadog",
    expected_agents: ["sentinel", "pathfinder", "architect", "backend", "devops"],
    expected_duration_ms: 50_000,
    icon_hint: "Plug",
    ready: false,
  },
  {
    id: "query_timeout_p99_spike",
    sector: "data",
    title: "Slow query p99 spike",
    description: "A new query plan degrades all reads on the orders table.",
    details:
      "Datadog flags the p99 breach. Pathfinder pulls the EXPLAIN plan, Architect designs a covering index, and DataEngineer schedules the migration behind ApprovalGate.",
    severity: "high",
    source_provider: "datadog",
    expected_agents: [
      "sentinel",
      "pathfinder",
      "architect",
      "data_engineer",
      "approval_gate",
    ],
    expected_duration_ms: 60_000,
    icon_hint: "Timer",
    ready: false,
  },
  {
    id: "migration_failed_mid_deploy",
    sector: "data",
    title: "Migration failed mid-deploy",
    description:
      "A schema migration crashed halfway with a partially-applied change.",
    details:
      "PagerDuty pages on the failed migration. Pathfinder diffs schema state, Architect drafts a forward-only fixup, and DataEngineer stages the recovery script behind a manual approval.",
    severity: "critical",
    source_provider: "pagerduty",
    expected_agents: [
      "sentinel",
      "pathfinder",
      "architect",
      "data_engineer",
      "approval_gate",
    ],
    expected_duration_ms: 70_000,
    icon_hint: "Database",
    ready: false,
  },
  {
    id: "pgvector_index_corrupted",
    sector: "data",
    title: "pgvector index corrupted",
    description:
      "Embedding lookups return wrong rows after a bad reindex run.",
    details:
      "Sentinel ingests the mismatch. Pathfinder reproduces with a known query, Architect schedules a REINDEX with cutover, and DataEngineer runs it under approval.",
    severity: "medium",
    source_provider: "sentry",
    expected_agents: ["sentinel", "pathfinder", "architect", "data_engineer"],
    expected_duration_ms: 45_000,
    icon_hint: "Layers",
    ready: false,
  },
  {
    id: "deadlock_detected",
    sector: "data",
    title: "Cross-tx deadlock in checkout",
    description:
      "Postgres aborts a checkout transaction with a deadlock victim.",
    details:
      "Datadog flags the rate of 40P01 errors. Pathfinder reconstructs the lock acquisition order, Architect proposes a fixed traversal, and Backend ships the reorder.",
    severity: "high",
    source_provider: "datadog",
    expected_agents: ["sentinel", "pathfinder", "architect", "backend"],
    expected_duration_ms: 40_000,
    icon_hint: "Lock",
    ready: false,
  },

  // ── Deploy / rollout ──────────────────────────────────────────────────
  {
    id: "bad_canary_deploy_rollback",
    sector: "deploy",
    title: "Bad canary deploy",
    description:
      "The latest canary is 503-ing — Argo refused to promote.",
    details:
      "Sentinel correlates Argo's degraded health with the new image. Pathfinder bisects the change, Architect picks a rollback target, and DevOps cuts over via GitOps.",
    severity: "critical",
    source_provider: "argocd",
    expected_agents: [
      "sentinel",
      "pathfinder",
      "architect",
      "devops",
      "approval_gate",
    ],
    expected_duration_ms: 50_000,
    icon_hint: "Rocket",
    ready: false,
  },
  {
    id: "oom_kill_loop",
    sector: "deploy",
    title: "Pod OOMKilled in a loop",
    description:
      "A pod keeps OOMKilling on the new image and never reaches Ready.",
    details:
      "Datadog flags the restart count climb. Pathfinder pulls the heap dump from the last terminated container, Architect bumps the request limit, and DevOps re-rolls.",
    severity: "high",
    source_provider: "datadog",
    expected_agents: ["sentinel", "pathfinder", "architect", "devops"],
    expected_duration_ms: 45_000,
    icon_hint: "MemoryStick",
    ready: false,
  },
  {
    id: "image_pull_backoff",
    sector: "deploy",
    title: "ImagePullBackOff",
    description: "Wrong image tag in the manifest after a careless edit.",
    details:
      "ArgoCD reports the missing tag. Pathfinder diffs the manifest against the registry, Architect picks the closest valid tag, and DevOps reconciles.",
    severity: "medium",
    source_provider: "argocd",
    expected_agents: ["sentinel", "pathfinder", "architect", "devops"],
    expected_duration_ms: 35_000,
    icon_hint: "Image",
    ready: false,
  },
  {
    id: "pdb_blocks_drain",
    sector: "deploy",
    title: "PDB blocks node drain",
    description:
      "PodDisruptionBudget refuses to evict for a routine node maintenance.",
    details:
      "Datadog flags the long drain. Pathfinder inspects the PDB minAvailable, Architect proposes a temporary relaxation, and DevOps cycles the node under approval.",
    severity: "medium",
    source_provider: "datadog",
    expected_agents: [
      "sentinel",
      "pathfinder",
      "architect",
      "devops",
      "approval_gate",
    ],
    expected_duration_ms: 40_000,
    icon_hint: "Shield",
    ready: false,
  },
  {
    id: "cert_expiring_soon",
    sector: "deploy",
    title: "TLS cert expiring soon",
    description: "Cert-manager missed the renewal window for an inbound host.",
    details:
      "PagerDuty pages on the upcoming expiry. Pathfinder verifies the ACME challenge path, Architect retriggers the issuer, and DevOps validates the new chain.",
    severity: "medium",
    source_provider: "pagerduty",
    expected_agents: ["sentinel", "pathfinder", "architect", "devops"],
    expected_duration_ms: 40_000,
    icon_hint: "ShieldCheck",
    ready: false,
  },

  // ── Infrastructure (kept tight; many infra faults live in deploy) ─────
  // (no extra infra-only scenarios in this wave; we keep the sector
  //  registered for future fixtures so the chip strip is forward-compat.)

  // ── Observability / runtime ───────────────────────────────────────────
  {
    id: "memory_leak_24h_climb",
    sector: "observability",
    title: "Heap climbs over 24h",
    description: "Heap usage shows a slow but steady leak across a release.",
    details:
      "Datadog flags the climbing RSS. Pathfinder bisects to the new caching layer, Architect bounds the cache, and Backend ships the eviction policy.",
    severity: "medium",
    source_provider: "datadog",
    expected_agents: ["sentinel", "pathfinder", "architect", "backend"],
    expected_duration_ms: 50_000,
    icon_hint: "TrendingUp",
    ready: false,
  },
  {
    id: "goroutine_leak",
    sector: "observability",
    title: "Goroutine leak in Go service",
    description:
      "Goroutine count climbs steadily; pprof traces fan out to leaked channels.",
    details:
      "Datadog flags the climb. Pathfinder lifts the offending goroutine stack, Architect adds an explicit cancellation, and Backend tightens the lifecycle.",
    severity: "medium",
    source_provider: "datadog",
    expected_agents: ["sentinel", "pathfinder", "architect", "backend"],
    expected_duration_ms: 45_000,
    icon_hint: "Workflow",
    ready: false,
  },
  {
    id: "cpu_throttling_spike",
    sector: "observability",
    title: "CPU throttling spike",
    description:
      "Container hitting the CPU limit; latency rises with throttled time.",
    details:
      "Datadog flags the throttling. Pathfinder ties it to a synchronous JSON encode, Architect proposes streaming, and Backend implements with bench coverage.",
    severity: "high",
    source_provider: "datadog",
    expected_agents: ["sentinel", "pathfinder", "architect", "backend", "qa"],
    expected_duration_ms: 50_000,
    icon_hint: "Cpu",
    ready: false,
  },
  {
    id: "latency_p99_breach",
    sector: "observability",
    title: "P99 latency above SLO",
    description: "The /v1/orders endpoint is breaching its 250ms SLO.",
    details:
      "Datadog flags the breach. Pathfinder profiles, Architect picks the hot path to refactor, and Backend lands the change with a perf-regression test.",
    severity: "high",
    source_provider: "datadog",
    expected_agents: ["sentinel", "pathfinder", "architect", "backend", "qa"],
    expected_duration_ms: 55_000,
    icon_hint: "Gauge",
    ready: false,
  },

  // ── Data pipelines ────────────────────────────────────────────────────
  {
    id: "airflow_dag_stuck",
    sector: "data",
    title: "Airflow DAG stuck queued",
    description:
      "An upstream task is stuck in queued; downstream consumers are blocked.",
    details:
      "PagerDuty pages on the SLA miss. Pathfinder reads the scheduler logs, Architect picks a safe retry strategy, and DataEngineer kicks the DAG.",
    severity: "medium",
    source_provider: "pagerduty",
    expected_agents: ["sentinel", "pathfinder", "data_engineer"],
    expected_duration_ms: 45_000,
    icon_hint: "Box",
    ready: false,
  },
  {
    id: "spark_job_oom",
    sector: "data",
    title: "Spark executor OOM",
    description:
      "A join exploded an executor's heap; the job died after three retries.",
    details:
      "PagerDuty pages on the failure. Pathfinder identifies the skew, Architect proposes a broadcast hint, and DataEngineer retries with the new plan.",
    severity: "high",
    source_provider: "pagerduty",
    expected_agents: ["sentinel", "pathfinder", "architect", "data_engineer"],
    expected_duration_ms: 60_000,
    icon_hint: "Sparkles",
    ready: false,
  },
  {
    id: "dbt_model_compile_fail",
    sector: "data",
    title: "dbt model compile failure",
    description:
      "A macro change broke lineage; downstream models can't compile.",
    details:
      "GitHub Actions flags the failed run. Pathfinder reproduces the compile locally, Architect rolls forward with a fix, and DataEngineer reruns lineage.",
    severity: "medium",
    source_provider: "github",
    expected_agents: ["sentinel", "pathfinder", "architect", "data_engineer"],
    expected_duration_ms: 45_000,
    icon_hint: "Container",
    ready: false,
  },
  {
    id: "kafka_consumer_lag",
    sector: "data",
    title: "Kafka consumer lag",
    description: "Consumer offset is falling behind the producer rate.",
    details:
      "Datadog flags the lag. Pathfinder profiles the consumer loop, Architect proposes parallel partitions, and Backend implements the worker pool.",
    severity: "high",
    source_provider: "datadog",
    expected_agents: ["sentinel", "pathfinder", "architect", "backend"],
    expected_duration_ms: 50_000,
    icon_hint: "Radio",
    ready: false,
  },
  {
    id: "snowflake_query_cost_spike",
    sector: "data",
    title: "Snowflake query cost spike",
    description:
      "One query is consuming a large share of warehouse credits.",
    details:
      "Datadog flags the warehouse spend. Pathfinder finds the culprit query, Architect rewrites with clustering, and DataEngineer re-runs to confirm savings.",
    severity: "medium",
    source_provider: "datadog",
    expected_agents: ["sentinel", "pathfinder", "architect", "data_engineer"],
    expected_duration_ms: 50_000,
    icon_hint: "DollarSign",
    ready: false,
  },

  // ── Security ──────────────────────────────────────────────────────────
  {
    id: "secret_committed_to_repo",
    sector: "security",
    title: "Secret committed to repo",
    description:
      "An API key landed in a public branch and tripped the guard.",
    details:
      "GitHub secret scanning fires. Pathfinder identifies the leaked key, Architect schedules a rotation, and DevOps invalidates the credential and force-pushes a redacted history.",
    severity: "critical",
    source_provider: "github",
    expected_agents: ["sentinel", "pathfinder", "architect", "devops"],
    expected_duration_ms: 55_000,
    icon_hint: "KeyRound",
    ready: false,
  },
  {
    id: "failed_auth_brute_force",
    sector: "security",
    title: "Auth brute-force burst",
    description: "50+ failed logins for a single account within 30s.",
    details:
      "Sentinel correlates the failed-auth burst. Pathfinder profiles the source IPs, Architect picks a rate-limit response, and Backend lands the throttle.",
    severity: "high",
    source_provider: "sentry",
    expected_agents: ["sentinel", "pathfinder", "architect", "backend"],
    expected_duration_ms: 45_000,
    icon_hint: "ShieldAlert",
    ready: false,
  },
  {
    id: "expired_secret_rotation",
    sector: "security",
    title: "Expired secret rotation",
    description:
      "A Vault-managed secret expired before its rotation succeeded.",
    details:
      "PagerDuty pages on the expiry. Pathfinder verifies the dependent services, Architect triggers an emergency rotate, and DevOps redeploys consumers with the new credential.",
    severity: "high",
    source_provider: "pagerduty",
    expected_agents: ["sentinel", "pathfinder", "architect", "devops"],
    expected_duration_ms: 50_000,
    icon_hint: "KeyRound",
    ready: false,
  },
];

// readyCount returns the number of scenarios whose backend is wired. Used
// by the sidebar badge so the count grows in lockstep with the allowlist
// in pipelines.go without anyone having to edit two files for each new
// scenario.
export function readyCount(): number {
  let n = 0;
  for (const s of SCENARIOS) if (s.ready) n++;
  return n;
}

// scenariosBySector groups the catalog for rendering. Preserves the
// declaration order within each sector.
export function scenariosBySector(): {
  sector: DemoScenarioSector;
  meta: (typeof SECTOR_META)[DemoScenarioSector];
  items: DemoScenario[];
}[] {
  // Preserve SECTOR_META key order so the page renders sectors top→bottom
  // in a deterministic order regardless of SCENARIOS array order.
  const order: DemoScenarioSector[] = [
    "application",
    "data",
    "deploy",
    "infrastructure",
    "observability",
    "security",
  ];
  return order
    .map((sector) => ({
      sector,
      meta: SECTOR_META[sector],
      items: SCENARIOS.filter((s) => s.sector === sector),
    }))
    .filter((group) => group.items.length > 0);
}
