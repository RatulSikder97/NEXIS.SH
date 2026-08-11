# Figure Manifest v2 — authoritative filenames + captions

All diagrams live in `docs/report/assets/diagrams/`, both `.svg` source and
rendered `.png`. All diagrams follow `STYLE_GUIDE.md` in that same directory
and reuse `icon-library.svg`'s `<defs>` block (copied inline, not linked).
`v2-context.svg`/`.png` already exists and is the visual exemplar every other
diagram must match stylistically (restrained palette: white background,
`#1f2937` ink, `#2563eb` as the ONLY accent, line-art icons, full technology
names, generous spacing, no per-category rainbow coloring).

Because chapters are authored in parallel by independent writers, figure
numbers are **per-chapter** (`Figure {chapter}.{n}`), not continuous
document-wide — e.g. the fourth figure in Chapter 5 is "Figure 5.4". Every
chapter file must number its own figures/tables starting at 1.

## Chapter 4 — System Design and Architecture (all diagrams live here)

| # | File (no ext) | Caption to use verbatim | Section |
|---|---|---|---|
| 4.1 | `v2-context` | System Context Diagram ( Nexis and the external actors and systems it exchanges data with ) | 4.1 System Context |
| 4.2 | `v2-components` | Component Architecture ( the control-plane, sidecar services, and web console that make up the platform boundary ) | 4.2 Component Architecture |
| 4.3 | `v2-data-storage` | Data and Storage Architecture ( PostgreSQL, Neo4j, Redis, and MinIO, and what each one is responsible for ) | 4.3 Data and Storage Architecture |
| 4.4 | `v2-selfheal-pipeline` | Self-Healing Loop — Agent Pipeline ( the nine-agent detect-to-deploy pipeline as a single flow ) | 4.4 Self-Healing Loop Pipeline |
| 4.5 | `v2-deployment-infra` | Deployment and Infrastructure Architecture ( containers, Infrastructure as Code, continuous delivery, and observability ) | 4.5 Deployment and Infrastructure |
| 4.6 | `v2-usecase-l0` | Use Case Diagram, Level 0 ( the three human actors and the autonomous agent fleet against the system boundary ) | 4.6.1 |
| 4.7 | `v2-usecase-l1` | Use Case Diagram, Level 1 ( expanded use cases per actor ) | 4.6.2 |
| 4.8 | `v2-dfd-l0` | Data Flow Diagram, Level 0 ( context-level data flow ) | 4.7.1 |
| 4.9 | `v2-dfd-l1-admin` | Data Flow Diagram, Level 1 — Owner / Administrator ( administrative processes and their data stores ) | 4.7.2 |
| 4.10 | `v2-dfd-l1-selfheal` | Data Flow Diagram, Level 1 — Self-Healing Loop ( the nine-agent fleet's processes and data stores ) | 4.7.3 |
| 4.11 | `v2-sequence-selfheal` | Self-Healing Loop — Sequence Diagram ( one complete recovery run, message by message, across all nine agent lifelines ) | 4.8 |

Chapter 3 (System Analysis) should NOT re-embed the use-case diagrams —
forward-reference them by section number ("see Section 4.6") the same way
the previous draft did, to avoid duplicate figures.

## Chapter 5 — Implementation (screenshots, unchanged filenames from v1)

Same ~30 curated screenshots as before, in `docs/report/assets/screenshots/`
(01-signup.png ... 43-agent-detail-backend.png) — reuse the exact grouping
and curation from the existing `chapters/ch5_implementation.tex` (Section
5.2.1 through 5.2.7 and 5.3) as your source of truth for which files to use
and in what order; just renumber captions as "Figure 5.1", "Figure 5.2", ...
sequentially in the order they appear, and expand every abbreviation per the
new full-name rule (e.g. "9-agent fleet" to "nine-agent fleet", "MFA" to
"Multi-Factor Authentication (MFA)" on first use, "API" to "Application
Programming Interface (API)" on first use, etc.)

## Full-name / no-abbreviation rule (applies to ALL chapters and captions)

Spell out on first use in each chapter, then it is fine to use the short
form for the rest of THAT chapter: Application Programming Interface (API),
Role-Based Access Control (RBAC), Row-Level Security (RLS), Single Sign-On
(SSO), Multi-Factor Authentication (MFA), Large Language Model (LLM),
Continuous Integration / Continuous Deployment (CI/CD), User Interface (UI),
Representational State Transfer (REST), JavaScript Object Notation (JSON).
Technology/product names are ALWAYS full, never abbreviated, in every
chapter, every time: "PostgreSQL" (never "Postgres"/"PG"), "Neo4j", "Redis",
"MinIO", "Temporal", "Docker" (note: "Docker (Podman-compatible runtime)"
where the report discusses what was actually run), "GitHub", "GitHub
Actions", "Slack", "OpenAI", "Ollama", "Argo CD" (two words), "Terraform",
"OpenTelemetry", "Prometheus", "Grafana", "Stripe", "Sentry", "Datadog",
"PagerDuty", "WorkOS", "Next.js", "TypeScript", "Go (Golang)", "Python".
