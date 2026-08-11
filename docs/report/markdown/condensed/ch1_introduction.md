# Chapter 1: Introduction

This chapter presents the background, motivation, problem, objectives, scope, and significance of **Nexis**.

## 1.1 Background

Software rarely fails when written; it fails in production, under load, against unanticipated data and dependencies. Industry experience, cited in the project proposal, places 40–60% of engineering time on maintenance, debugging, and incident response rather than new capability — roughly one engineer repairing for every engineer building.

Assistants such as GitHub Copilot [1] now write a meaningful fraction of new code, yet each accelerates a single task for a single human. The production incident lifecycle — detect a fault, diagnose the root cause, fix, validate, obtain approval, deploy, verify — remains a chain of manual hand-offs with the engineer as transport between every stage.

Nexis closes that chain, modelling an engineering organisation as nine role-specialised autonomous agents in two layers — a five-agent Execution Team and a four-agent Self-Healing Loop — orchestrated by the Temporal workflow engine [2], with human approval reduced to a single severity-routed decision.

## 1.2 Motivation

The motivating pain points:

- **No tool closes the loop.** Sentry [14], Datadog [16], and PagerDuty [15] detect failures and page a human; Copilot [1] writes code once asked; nothing connects detection to autonomous repair.
- **The engineer is the bottleneck for every incident** — reading logs, localising, patching, testing, reviewing, deploying — even for near-identically recurring fault classes.
- **Root-cause analysis is unaided.** No mainstream tooling walks the code dependency graph or ranks candidate causes with stated evidence.
- **Repair without validation is dangerous, so it is not attempted.** With no sandboxed path for a machine-generated patch to prove itself, organisations reasonably refuse, and the loop stays open.
- **Approval is all-or-nothing** — gates are absent (risky automation) or heavyweight (full review for a one-line fix); nothing routes human involvement by measured severity.

## 1.3 Problem Statement

The limitations of existing approaches are structural — isolated tools, no closed loop, a human bottleneck on every decision, no causal root-cause tooling (Neo4j [3] graph traversal, DoWhy [4] causal reasoning), and no safe autonomous deploy path with sandboxed validation and post-deploy rollback.

The problem is therefore: *how can the full production-fault lifecycle be closed autonomously by cooperating role-specialised agents, while keeping a human decision in the loop exactly where, and only where, the severity of the change warrants it?*

## 1.4 Project Objectives

Nexis set out to build, and this report documents as built:

1. **A nine-agent, two-layer architecture.** A five-agent Execution Team (Architect, Backend, QA, DevOps, Data Engineer) producing plans, minimal-diff patches, property-based tests, manifests, and migrations; and a four-agent Self-Healing Loop (Sentinel, Pathfinder, Synthesiser, Validator) detecting faults, walking the Neo4j [3] dependency graph toward root causes, and shadow-executing every patch in a locked-down Docker [10] sandbox — orchestrated as a durable, replayable Temporal workflow [2].
2. **Severity-routed human approval.** An Approval Gate classifies each validated patch HIGH (mandatory human review), MEDIUM (120-second intervention window, then auto-approval), or LOW (auto-approval); Approve, Reject, and diff-editing Modify decisions are persisted as feedback.
3. **Real GitOps closure.** Approval opens a genuine GitHub pull request via a GitHub App; a post-deploy probe watches for fresh fatal errors, invoking Argo CD rollback [6] to the prior known-good revision when recovery policy demands.
4. **A Docker preview-deploy engine** that detects a cloned repository's stack, generates a missing Dockerfile, builds and runs the container under strict resource limits, health-checks it over HTTP, and returns a live preview URL, feeding its failures back as first-class incidents.
5. **Intelligent role recommendation.** A heuristic engine over audit-log and login signal recommends privilege escalations and downgrades, each with an evidence-backed rationale to accept or dismiss.
6. **A multi-tenant platform around the loop**: PostgreSQL Row-Level Security (RLS) [7], Role-Based Access Control (RBAC), MFA and SSO via WorkOS [12], Stripe billing [13], integrations (GitHub, Slack [17], Sentry [14], Datadog [16], PagerDuty [15]), a tamper-evident audit log, and an administrative console.

## 1.5 Scope of the Project

Nexis serves three human roles under RBAC:

- **Owner/Administrator** — provisions the organisation, connects integrations, sets recovery policy, decides HIGH-severity approvals, manages members and billing, acts on role recommendations.
- **Engineer/Member** — works daily with projects and incidents, observes live pipeline runs, exercises Approve/Reject/Modify.
- **Viewer** — read-only console visibility, RBAC-enforced, into incidents, recovery outcomes, and cost.

Beyond these, the **nine agents are modelled as a first-class actor category**: the Sentinel raises incidents and the Validator renders verdicts as human actors would, under the same audit and policy constraints — shrinking the human roles to supervisory decisions.

The delivered scope is the platform above, evaluated end-to-end against a locally hosted Large Language Model via Ollama [20]; the `nexis.sh` domain is registered, but a credentialed public production deployment is outside this report's scope, as the limitations discuss honestly.

## 1.6 Significance of the Project

Nexis's significance is the closed loop itself: one continuous, durable, auditable workflow from detection to verified deployment, the human reduced from transport mechanism to a single well-placed decision.

Chapter 6 evidences this live: two end-to-end recovery runs — one correctly rejecting the patch on approval-window timeout, the safety mechanism working as designed; one carrying a fault from detection to an approved, deployed patch in under two minutes, with per-agent token and cost figures — and an HTTP-verified preview-deploy of a Dockerfile-less public repository.
