# Chapter 1: Introduction

This chapter lays out why **Nexis** was built in the first place: the pain points that drove it, where existing tools fall short, the objectives set for the project, and who, human and non-human alike, it actually serves. It ends by explaining why a genuinely closed loop, running from fault detection through to verified deployment, actually matters.

## 1.1 Background

Software rarely fails at the moment it is written. It fails later, in production, under load, against data and dependencies that its authors never anticipated. It may be noted that this cost is not merely anecdotal: the original proposal for this project draws on industry experience placing it at 40–60% of an engineering team's time, spent on maintenance, debugging, and incident response rather than on building anything new. Consequently, for a small team this means that for every engineer creating value, there is roughly one more engineer employed full-time just to keep it working.

Artificial Intelligence (AI)-assisted software engineering has grown rapidly over the last few years. Code-completion assistants such as GitHub Copilot [1] now write a meaningful share of new code, and the tools built around them generate tests, review pull requests, and summarise logs. Yet each of these remains a point solution: it accelerates one task performed by one human, and nothing more. The production incident lifecycle makes the point clearly. It runs through seven stages: detect a fault, diagnose its root cause, write a fix, validate it, obtain approval, deploy it, and verify the deployment. At every hand-off between these stages, a human engineer remains the transport mechanism; thus, the chain stays manual from end to end.

Nexis exists to close that chain. Rather than assisting one engineer at one task, it models an entire software engineering organisation as nine role-specialised autonomous agents, arranged in two cooperating layers: a five-agent Execution Team and a four-agent Self-Healing Loop, both durably orchestrated by the Temporal workflow engine [2]. The human approval decision, in turn, is reduced to a single severity-routed click rather than an open-ended manual process.

## 1.2 Motivation

The pain points behind this project are not abstract; they show up in some form in almost any engineering organisation:

- **No tool closes the loop.** Monitoring products such as Sentry [14], Datadog [16], and PagerDuty [15] can detect a failure and page a human about it; code assistants such as GitHub Copilot [1] write code, but only once a human has asked for it. Nothing on the market currently connects the two, so that a detected failure leads on its own to an autonomous repair.
- **The engineer is the bottleneck for every incident.** Even a routine alert requires a human to read the logs, localise the fault, write a patch, run the tests, request review, deploy, and then watch the rollout. That human sits on the critical path for all seven stages, even for fault classes that recur in an almost identical form each time.
- **Root-cause analysis is unaided.** Localising a fault, in practice, means tracing call chains and service dependencies by hand and from memory, since no mainstream incident tool walks the code dependency graph or ranks candidate root causes against any stated evidence.
- **Repair without validation is dangerous, so it is not attempted.** Because there is no safe, sandboxed path for a machine-generated patch to prove itself before a human ever sees it, organisations reasonably refuse to let machines patch anything at all; as such, the loop stays open.
- **Approval is all-or-nothing.** Deployment gates are typically either absent (which is risky automation) or heavyweight (a full review cycle for what may be a one-line fix), and no existing tool scales the depth of human involvement to the measured severity of the change.

## 1.3 Problem Statement

Existing approaches to AI-assisted engineering and incident response, taken together, share a set of structural limitations. They are **isolated tools**, in that detection, diagnosis, code generation, testing, and deployment each live in separate products with no shared state or protocol connecting them; they provide **no closed loop**, since a detected fault never automatically becomes a validated, deployable repair; and they keep a **human as the bottleneck for every decision**, however small, repetitive, or low-risk the change may be. They also offer **no causal root-cause tooling**, as nothing traverses the actual dependency structure of the code the way a graph database such as Neo4j [3] makes possible, and nothing applies causal reasoning of the kind formalised by libraries such as DoWhy [4] to explain *why* a fault occurred. Finally, they provide **no safe autonomous deploy path**: even where a machine can propose a patch, there is no sandboxed stage to validate it, no severity-aware approval gate, and nothing to verify or roll back the deployment automatically, so shipping that patch can never really be defended.

Hence the problem this project addresses may be stated as follows: *how can the full production-fault lifecycle (detect, diagnose, repair, validate, approve, deploy, and verify) be closed autonomously by a team of cooperating role-specialised agents, while still keeping a human decision in the loop exactly where, and only where, the severity of the change warrants it?*

## 1.4 Project Objectives

Nexis set out to build the following, and delivered each as described below:

1. **A nine-agent, two-layer architecture.** A five-agent Execution Team (Architect, Backend, QA, DevOps, Data Engineer) producing plans, minimal-diff patches, property-based tests, manifests and migrations; and a four-agent Self-Healing Loop (Sentinel, Pathfinder, Synthesiser, Validator) detecting faults, walking the Neo4j [3] dependency graph towards root causes, and shadow-executing every patch in a locked-down Docker [10] sandbox, the whole pipeline orchestrated as a durable, replayable Temporal workflow [2].
2. **Severity-routed human approval.** An Approval Gate classifies each validated patch as HIGH (mandatory human review, for example schema changes, security-sensitive paths, or an agent's contract violation), MEDIUM (a 120-second intervention window, then auto-approval), or LOW (auto-approval for trivial UI-only diffs); Approve, Reject and diff-editing Modify decisions are all persisted as feedback.
3. **Real GitOps closure.** Approval opens a genuine GitHub pull request through a GitHub App, and a post-deploy probe watches for fresh fatal errors, invoking Argo CD rollback [6] to the prior known-good revision whenever recovery policy demands it.
4. **A Docker preview-deploy engine** that detects a cloned repository's stack, generates a missing Dockerfile, builds and runs the container under strict resource limits, health-checks it over HTTP and returns a live preview URL, feeding its failures back as first-class incidents.
5. **Intelligent role recommendation.** A heuristic engine over audit-log and login signal recommends privilege escalations and downgrades, each carrying an evidence-backed rationale that can be accepted or dismissed.
6. **A multi-tenant platform around the loop**: PostgreSQL Row-Level Security (RLS) [7], Role-Based Access Control (RBAC), MFA and SSO through WorkOS [12], Stripe billing [13], integrations with GitHub, Slack [17], Sentry [14], Datadog [16] and PagerDuty [15], a tamper-evident audit log, and an administrative console.

## 1.5 Scope of the Project

Under RBAC, Nexis serves three human roles:

- **Owner/Administrator** — provisions the organisation, connects integrations, sets recovery policy, decides on HIGH-severity approvals, manages members, billing and API keys, and acts on role recommendations.
- **Engineer/Member** — works daily with projects and incidents, watches live pipeline runs, and exercises Approve/Reject/Modify.
- **Viewer** — read-only visibility into incidents, recovery outcomes and cost, enforced through RBAC at the console level.

Beyond these roles, Nexis makes a deliberate departure from conventional systems analysis: the **nine agents are modelled as a first-class actor category** rather than as internal implementation detail. In a typical information system every actor in the use-case model is human, whereas here the Sentinel raises incidents, the Architect plans, the Backend agent writes code, and the Validator renders verdicts, each initiating and completing use cases as a human actor would, under the same audit and policy constraints. Treating agents as actors is precisely what lets the human roles shrink to supervisory decisions, and the framing carries through the requirement analysis and design chapters consistently.

The delivered scope is the platform described above, evaluated end-to-end against a locally hosted Large Language Model through Ollama [20]. The `nexis.sh` domain has been registered, but a credentialed public production deployment falls outside this report's scope, as the limitations discuss honestly.

## 1.6 Significance of the Project

The significance of Nexis lies chiefly in closing a loop that existing tools leave open. Detection without repair produces pager fatigue; code generation without validation produces risk; and automation without a human gate produces distrust that is entirely justified. Every individual stage of the lifecycle has been automated somewhere before; what has been missing is a system in which detect → diagnose → repair → validate → approve → deploy → verify runs as one continuous, durable and auditable workflow, with the human reduced from the transport mechanism between stages to a single, well-placed decision.

This report does not argue that claim in the abstract, but presents live evidence for it instead. Chapter 6 reports two genuine end-to-end recovery runs, both executed against a real, locally hosted language model [20]. In one, the approval window elapsed and the pipeline correctly rejected the patch on timeout, the safety mechanism working exactly as designed; in the other, a fault travelled the entire loop, from detection to an approved and deployed patch, in under two minutes, with per-agent token and cost figures recorded at every step. The same chapter also presents a live, HTTP-verified run of the preview-deploy engine, building and serving a real public repository that contained no Dockerfile to begin with.
