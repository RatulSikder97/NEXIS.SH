# Chapter 1: Introduction

This chapter introduces the context in which **Nexis** was conceived, the concrete pain points that motivated it, the limitations of existing approaches it responds to, the objectives the project set out to achieve, its scope in terms of the human and non-human actors it serves, and the significance of delivering a genuinely closed loop from fault detection to verified deployment.

## 1.1 Background

Software rarely fails at the moment it is written; it fails in production, under load, against data and dependencies its authors did not anticipate. Industry experience, reflected in the original proposal for this project, places the cost of that reality at 40–60% of an engineering team's time spent on maintenance, debugging, and incident response rather than on building new capability. For a small team, this means that for every engineer creating value, roughly one other engineer is effectively employed full-time repairing it.

The last few years have seen a rapid rise of Artificial Intelligence (AI)-assisted software engineering. Code-completion assistants such as GitHub Copilot [1] now write a meaningful fraction of new code, and adjacent tools generate tests, review pull requests, and summarise logs. Yet these tools are point solutions: each one accelerates a single task performed by a single human. The production incident lifecycle — detect a fault, diagnose its root cause, write a fix, validate it, obtain approval, deploy it, and verify the deployment — remains a chain of manual hand-offs in which a human engineer is the transport mechanism between every pair of stages.

Nexis exists to close that chain. Rather than assisting one engineer at one task, it models a software engineering organisation as nine role-specialised autonomous agents arranged in two cooperating layers — a five-agent Execution Team and a four-agent Self-Healing Loop — durably orchestrated by the Temporal workflow engine [2], with a human approval decision reduced to a single severity-routed click rather than an open-ended manual process.

## 1.2 Motivation

The specific pain points that motivated this project are concrete and observable in any engineering organisation:

- **No tool closes the loop.** Monitoring products such as Sentry [14], Datadog [16], and PagerDuty [15] detect failures and page a human; code assistants such as GitHub Copilot [1] write code once a human asks. Nothing on the market connects the detection of a failure to its autonomous repair.
- **The engineer is the bottleneck for every incident.** Every alert, however routine, requires a human to read logs, localise the fault, write a patch, run tests, request review, deploy, and watch the rollout. The human is on the critical path of all seven stages, even for fault classes that recur in near-identical form.
- **Root-cause analysis is unaided.** Localising a fault means manually tracing call chains and service dependencies from memory; no mainstream incident tooling walks the code dependency graph or ranks candidate root causes with any stated evidence.
- **Repair without validation is dangerous, so it is not attempted.** Because there is no safe, sandboxed path for a machine-generated patch to prove itself before a human sees it, organisations reasonably refuse to let machines patch anything — and the loop stays open.
- **Approval is all-or-nothing.** Deployment gates are either absent (risky automation) or heavyweight (a full review cycle for a one-line fix). No existing tool routes the depth of human involvement by the measured severity of the change.

## 1.3 Problem Statement

Existing approaches to Artificial Intelligence (AI)-assisted engineering and incident response share a set of structural limitations:

- They are **isolated tools** — detection, diagnosis, code generation, testing, and deployment each live in separate products with no shared state or protocol between them.
- They provide **no closed loop**: a detected fault never automatically becomes a validated, deployable repair.
- They keep a **human as the bottleneck for every decision**, regardless of how small, repetitive, or low-risk the change is.
- They offer **no causal root-cause tooling**: nothing traverses the actual dependency structure of the code, in the manner a graph database such as Neo4j [3] makes possible, or applies causal reasoning of the kind formalised by libraries such as DoWhy [4], to explain *why* a fault occurred.
- They provide **no safe autonomous deploy path**: even where a machine can propose a patch, there is no sandboxed validation stage, no severity-aware approval gate, and no automatic post-deploy verification and rollback to make shipping that patch defensible.

The problem this project addresses is therefore: *how can the full production-fault lifecycle — detect, diagnose, repair, validate, approve, deploy, and verify — be closed autonomously by a team of cooperating role-specialised agents, while keeping a human decision in the loop exactly where, and only where, the severity of the change warrants it?*

## 1.4 Project Objectives

Nexis set out to build, and this report documents as built, the following:

1. **A nine-agent, two-layer architecture.** A five-agent Execution Team — Architect, Backend, Quality Assurance (QA), DevOps, and Data Engineer — that plans, generates minimal-diff patches, writes property-based tests, produces deployment manifests, and proposes database migrations; and a four-agent Self-Healing Loop — Sentinel, Pathfinder, Synthesiser, and Validator — that detects faults statistically, walks the Neo4j [3] code dependency graph toward candidate root causes, routes the incident to the agents it actually needs, and shadow-executes every candidate patch in a locked-down Docker [10] sandbox before any human sees it. The entire pipeline is orchestrated as a durable Temporal workflow [2], so every step survives process restarts and is replayable.
2. **Severity-routed human approval.** An Approval Gate that classifies each validated patch as HIGH (mandatory human review, for example schema changes, security-sensitive paths, or a contract violation by an agent), MEDIUM (a 120-second window in which a human may intervene before auto-approval), or LOW (immediate auto-approval for trivial User Interface (UI)-only diffs), with three human decision options: Approve, Reject, or Modify — the last allowing the engineer to edit the diff before it ships, with every terminal decision persisted as feedback data for future model improvement.
3. **Real GitOps closure.** On approval, the platform opens a genuine GitHub pull request through a GitHub App installation, and a post-deploy rollback probe watches for fresh fatal errors, invoking Argo CD's rollback client [6] to the prior known-good revision when the recovery policy demands it.
4. **A Docker preview-deploy engine.** A dedicated service that clones any connected repository, detects its technology stack, generates a working Dockerfile when none exists, builds and runs the container under strict resource limits, health-checks it over Hypertext Transfer Protocol (HTTP), and returns a live preview Uniform Resource Locator (URL) — and, crucially, feeds its own build or health-check failures back into the same self-healing pipeline as first-class incidents.
5. **Intelligent role recommendation.** A heuristic engine over real audit-log and login signal that recommends privilege escalations and downgrades for organisation members, each with a plain-language, evidence-backed rationale that an administrator can accept or dismiss.
6. **A complete multi-tenant platform around the loop.** A control plane with PostgreSQL Row-Level Security (RLS) [7] for tenant isolation, Role-Based Access Control (RBAC), full authentication (including Multi-Factor Authentication (MFA) and Single Sign-On (SSO) via WorkOS [12]), billing via Stripe [13], integrations (GitHub, Slack [17], Sentry [14], Datadog [16], PagerDuty [15]), a tamper-evident audit log, and an administrative console covering incidents, approvals, agents, cost, and system health.

## 1.5 Scope of the Project

Nexis serves three categories of human user, distinguished by Role-Based Access Control (RBAC):

- **Owner / Administrator** — provisions the organisation, connects integrations, sets recovery policy, decides HIGH-severity approvals, manages members, billing, and Application Programming Interface (API) keys, and acts on role recommendations.
- **Engineer / Member** — works with projects and incidents day to day, observes live pipeline runs, and exercises the Approve / Reject / Modify decision on patches within their remit.
- **Viewer** — a read-only console experience, enforced by the same Role-Based Access Control gating, for stakeholders who need visibility into incidents, recovery outcomes, and cost without the ability to change anything.

Beyond these, Nexis makes a deliberate conceptual departure from conventional systems analysis: the **nine autonomous agents are modelled as a distinct actor category in their own right**, not as internal implementation detail. In a typical information system, every actor in the use-case model is human; in Nexis, the Sentinel raises incidents, the Architect plans, the Backend agent writes code, and the Validator renders verdicts — each initiating and completing use cases exactly as a human actor would, subject to the same audit logging and the same policy constraints. Treating agents as first-class actors is what allows the human roles above to shrink to supervisory decisions, and this framing is carried consistently through the requirement analysis and design chapters of this report.

The scope of the delivered system is the full platform described in the objectives, evaluated end-to-end in a local environment with a locally hosted Large Language Model (LLM) via Ollama [20]. The `nexis.sh` domain is registered for the platform but a credentialed public production deployment is outside the scope of this report, as discussed honestly in the limitations.

## 1.6 Significance of the Project

The significance of Nexis lies in closing a loop that existing tools leave open. Detection without repair produces pager fatigue; code generation without validation produces risk; automation without a human gate produces justified distrust. Each stage of the lifecycle has been automated somewhere before — what has been missing is a system in which detect → diagnose → repair → validate → approve → deploy → verify runs as one continuous, durable, auditable workflow, with the human reduced from the transport mechanism between stages to a single, well-placed decision.

This report does not argue that claim in the abstract; it presents live evidence for it. Chapter 6 reports two genuine end-to-end recovery runs executed against a real locally hosted language model [20]: one in which the approval window elapsed and the pipeline correctly rejected the patch on timeout — the safety mechanism working as designed — and one in which a fault travelled the entire loop from detection to an approved, deployed patch in under two minutes, with per-agent token and cost figures recorded at every step. It likewise presents a live, HTTP-verified run of the preview-deploy engine building and serving a real public repository that contained no Dockerfile. A platform that can demonstrably turn a production fault into a validated, human-approved repair in the time it takes an on-call engineer to open their laptop is a meaningful step toward engineering teams spending their time building software rather than nursing it.
