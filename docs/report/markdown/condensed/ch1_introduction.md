# Chapter 1: Introduction

This chapter lays out the background, motivation, problem statement, objectives, scope and significance of **Nexis**.

## 1.1 Background

Software rarely fails when it is written; it fails later, in production, under load, against data and dependencies nobody anticipated. Industry experience cited in the project proposal puts 40–60% of engineering time into maintenance, debugging and incident response rather than new capability, meaning roughly one engineer repairing for every engineer building.

Assistants such as GitHub Copilot [1] now write a meaningful share of new code, yet each one accelerates a single task for a single human. The production incident lifecycle (detect a fault, diagnose the root cause, fix, validate, obtain approval, deploy, verify) remains a chain of manual hand-offs, with the engineer acting as transport between every stage.

Nexis closes that chain. It models an engineering organisation as nine role-specialised autonomous agents in two layers, a five-agent Execution Team and a four-agent Self-Healing Loop, orchestrated by the Temporal workflow engine [2], with human approval reduced to a single severity-routed decision.

## 1.2 Motivation

Several pain points motivate this work:

- **No tool closes the loop.** Sentry [14], Datadog [16] and PagerDuty [15] detect failures and page a human; Copilot [1] writes code once it is asked to; nothing yet connects detection to autonomous repair.
- **The engineer is the bottleneck for every incident**, even where the fault class recurs in an almost identical form: reading logs, localising, patching, testing, reviewing, deploying.
- **Root-cause analysis goes unaided.** No mainstream tooling walks the code's dependency graph or ranks candidate causes with stated evidence.
- **Repair without validation is dangerous, so nobody attempts it.** Without a sandboxed path for a machine-generated patch to prove itself, organisations reasonably refuse to trust it, and the loop stays open.
- **Approval is all-or-nothing**: gates are either absent, which makes automation risky, or heavyweight, requiring full review even for a one-line fix; nothing routes human involvement by measured severity.

## 1.3 Problem Statement

Taken together, the limitations of existing approaches are structural: isolated tools, no closed loop, a human bottleneck on every decision, no causal root-cause tooling (Neo4j [3] graph traversal, DoWhy [4] causal reasoning), and no safe autonomous deploy path with sandboxed validation and post-deploy rollback.

Hence, the problem may be stated as follows: *how can the full production-fault lifecycle be closed autonomously by cooperating role-specialised agents, while keeping a human decision in the loop exactly where, and only where, the severity of the change warrants it?*

## 1.4 Project Objectives

Nexis set out to build the following, and delivered each as described below:

1. **A nine-agent, two-layer architecture.** A five-agent Execution Team (Architect, Backend, QA, DevOps, Data Engineer) producing plans, minimal-diff patches, property-based tests, manifests and migrations; and a four-agent Self-Healing Loop (Sentinel, Pathfinder, Synthesiser, Validator) detecting faults, walking the Neo4j [3] dependency graph towards root causes, and shadow-executing every patch in a locked-down Docker [10] sandbox, the whole pipeline orchestrated as a durable, replayable Temporal workflow [2].
2. **Severity-routed human approval.** An Approval Gate classifies each validated patch as HIGH (mandatory human review), MEDIUM (a 120-second intervention window, then auto-approval), or LOW (auto-approval); Approve, Reject and diff-editing Modify decisions are all persisted as feedback.
3. **Real GitOps closure.** Approval opens a genuine GitHub pull request through a GitHub App, and a post-deploy probe watches for fresh fatal errors, invoking Argo CD rollback [6] to the prior known-good revision whenever recovery policy demands it.
4. **A Docker preview-deploy engine** that detects a cloned repository's stack, generates a missing Dockerfile, builds and runs the container under strict resource limits, health-checks it over HTTP and returns a live preview URL, feeding its failures back as first-class incidents.
5. **Intelligent role recommendation.** A heuristic engine over audit-log and login signal recommends privilege escalations and downgrades, each carrying an evidence-backed rationale that can be accepted or dismissed.
6. **A multi-tenant platform around the loop**: PostgreSQL Row-Level Security (RLS) [7], Role-Based Access Control (RBAC), MFA and SSO through WorkOS [12], Stripe billing [13], integrations with GitHub, Slack [17], Sentry [14], Datadog [16] and PagerDuty [15], a tamper-evident audit log, and an administrative console.

## 1.5 Scope of the Project

Under RBAC, Nexis serves three human roles:

- **Owner/Administrator** — provisions the organisation, connects integrations, sets recovery policy, decides on HIGH-severity approvals, manages members and billing, and acts on role recommendations.
- **Engineer/Member** — works daily with projects and incidents, watches live pipeline runs, and exercises Approve/Reject/Modify.
- **Viewer** — read-only visibility into incidents, recovery outcomes and cost, enforced through RBAC at the console level.

Beyond these roles, the **nine agents are also modelled as a first-class actor category**: the Sentinel raises incidents and the Validator renders verdicts much as a human actor would, under the same audit and policy constraints. This, in effect, narrows the human roles to supervisory decisions.

The delivered scope is the platform described above, evaluated end-to-end against a locally hosted Large Language Model through Ollama [20]. The `nexis.sh` domain has been registered, but a credentialed public production deployment falls outside this report's scope, as the limitations discuss honestly.

## 1.6 Significance of the Project

Nexis's significance lies in the closed loop itself: one continuous, durable and auditable workflow running from detection through to verified deployment, with the human reduced from a transport mechanism to a single, well-placed decision.

Chapter 6 evidences this live through two end-to-end recovery runs and an HTTP-verified preview-deploy of a Dockerfile-less public repository. In the first run, the pipeline correctly rejects the patch on approval-window timeout, the safety mechanism working as designed; in the second, it carries a fault from detection to an approved, deployed patch in under two minutes, with per-agent token and cost figures.
