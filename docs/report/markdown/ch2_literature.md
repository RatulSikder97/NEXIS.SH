# Chapter 2: Literature Review

## 2.1 Overview

Nexis is a systems-integration project: its contribution lies in composing
established, well-engineered tools into a closed loop that none of them forms
on its own. Intellectual honesty therefore requires a clear account of what
each underlying tool already does, and where Nexis merely depends on it versus
where Nexis extends it. This section surveys the relevant landscape in that
spirit.

**Large Language Model (LLM) coding assistants.** GitHub Copilot [1] is the
most widely deployed Artificial Intelligence (AI) pair-programming tool; it
completes code and, in its agent modes, drafts multi-file changes inside an
editor session. It operates entirely within the developer's authoring context:
it never observes a production incident, never validates its own output
against a failing system, and never deploys anything. Nexis's Backend agent
occupies the same "generate a minimal code change" niche, but is invoked by a
fault-detection pipeline rather than a keystroke, and its output is
machine-validated before any human sees it.

**Durable workflow orchestration.** Temporal [2] provides durable, replayable
workflow execution with automatic retry and state persistence. Nexis uses
Temporal as-is, as a dependency: the entire nine-agent RecoveryPipeline is a
Temporal workflow, which is why every agent step survives process restarts and
is recorded as a durable ActivityEvent. The workflow's engineering semantics —
role routing, contract-violation detection, severity classification — are
Nexis's own logic layered on top.

**Graph databases for dependency analysis.** Neo4j [3] is a native graph
database. Nexis stores its code-dependency graph in Neo4j and the Pathfinder
agent walks it from a fault symptom to candidate root-cause nodes, forwarding
hop distance and node degree as evidence. Neo4j is a storage and traversal
dependency; the evidence-ranking performed on the traversal results is Nexis
code.

**Causal inference tooling.** DoWhy [4] is Microsoft Research's library for
explicit causal modelling with refutation tests. It is the methodological
reference point for Nexis's causal-inference sidecar, but it must be stated
plainly that Nexis does *not* fit a DoWhy structural causal model: no
interventional production data exists to fit one against. The sidecar instead
implements a documented proxy — ranking candidates by evidence-chain
specificity, graph-structural proximity, textual overlap, and a scenario
prior, with a transparent per-component confidence breakdown.

**Observability standards.** OpenTelemetry [5] standardises traces, metrics,
and logs. Nexis adopts it as its instrumentation layer (exported to
Prometheus, Loki, and Tempo behind Grafana). It is a dependency; Nexis adds
nothing to the standard itself.

**GitOps deployment.** Argo CD [6] reconciles a cluster against a Git
repository and exposes Sync and Rollback operations. Nexis's DevOps agent
emits Argo CD manifests, and the post-deploy rollback probe calls Argo CD's
real Rollback client when a policy-gated Service Level Objective (SLO) breach
is detected — integration, not extension.

**Container runtimes.** Docker [10] is the target Application Programming
Interface (API) for both the Validator's sandbox (network-isolated, read-only,
capability-dropped shadow execution of every candidate patch) and the
preview-deploy engine. Podman [11] was substituted in the evaluation
environment as a Docker-API-compatible runtime and behaved identically; this
substitution is reported honestly rather than hidden.

**Property-based testing.** Hypothesis [9] generates test inputs from declared
properties. Nexis's Quality Assurance (QA) agent auto-generates
pytest/Hypothesis suites from the incident and the Backend diff — a novel
*use* of the library, with Hypothesis itself unmodified. A background QA loop
replays stored suites and raises a fresh incident on a pass-to-fail
regression.

**Data lineage.** OpenLineage [8] defines an open specification for run-level
lineage events. Nexis's Data Engineer agent emits OpenLineage-compatible
RunEvents for every migration it proposes or applies; Nexis conforms to the
specification rather than extending it.

**The incident-response ecosystem.** Sentry [14], Datadog [16], and PagerDuty
[15] respectively capture application errors, aggregate observability data,
and route alerts to on-call humans. All three are integrated into Nexis as
webhook-driven incident *sources* feeding the same Sentinel pipeline. The
Slack API [17] is used in the opposite direction: the Approval Gate pushes a
decision into Slack, where an engineer can approve or reject a validated
patch with one click. Supporting platform dependencies — WorkOS for Single
Sign-On (SSO) [12], Stripe for billing [13], PostgreSQL Row-Level Security
(RLS) for tenant isolation [7], pgvector for knowledge-base retrieval [19],
and Ollama [20] as the local Large Language Model provider used for this
report's live evidence — are used as documented, without modification.
Table 2.1 summarises the dependency-versus-contribution boundary.

**Table 2.1: Position of surveyed tools relative to Nexis.**

| Tool | What it provides | Relation to Nexis |
|---|---|---|
| GitHub Copilot [1] | Editor-time code generation | Comparable to one agent (Backend) only |
| Temporal [2] | Durable workflow execution | Dependency; hosts the RecoveryPipeline |
| Neo4j [3] | Graph storage and traversal | Dependency; Pathfinder walks it |
| DoWhy [4] | Structural causal models | Reference point; proxy method used instead |
| OpenTelemetry [5] | Telemetry standard | Dependency; instrumentation layer |
| Argo CD [6] | GitOps sync/rollback | Integrated by DevOps agent and rollback probe |
| Docker [10] / Podman [11] | Container runtime | Dependency; sandbox and deploy engine |
| Hypothesis [9] | Property-based testing | Novel use: Quality Assurance agent generates suites |
| OpenLineage [8] | Lineage specification | Conformance by Data Engineer agent |
| Sentry / Datadog / PagerDuty [14, 16, 15] | Error/alert capture | Webhook incident sources |
| Slack API [17] | Team messaging | One-click approval channel |

## 2.2 Research Gap

Each tool above is strong within its boundary, and several are routinely
combined in industry — Sentry alerting into PagerDuty, an on-call engineer
opening an editor with GitHub Copilot, Argo CD deploying the eventual merge.
What no single tool, and no combination in common use, provides is a system
that (a) models a software engineering *team* with explicit role
specialisation, and (b) closes the loop from automatic fault detection to a
deployed, verified fix, with human judgement concentrated at one
severity-routed decision point.

The gap is visible at every seam of the conventional tool chain. GitHub
Copilot generates code but never sees a production incident; it has no notion
of a fault, a blast radius, or a deployment [1]. Observability platforms such
as Datadog and Sentry detect and display faults with great fidelity but never
generate a candidate fix, let alone validate one [16, 14]. PagerDuty routes a
human to the problem; the repair itself remains entirely manual [15]. Argo CD
deploys whatever a repository contains but authors nothing [6]. Temporal can
durably orchestrate any of these steps but carries no engineering semantics
of its own [2]. In every case the loop is broken at the same place: between
"a fault is known" and "a validated fix exists," a human performs unaided
diagnosis, authoring, testing, review, and release.

Nexis closes precisely that span, end to end. Sentinel detects a fault using
a statistical-process-control rule (an Exponentially Weighted Moving Average
(EWMA) baseline with a mean-plus-three-sigma breach condition), not a fixed
threshold. Pathfinder walks the real Neo4j dependency graph to candidate root
causes, which the causal-inference sidecar ranks with a transparent
confidence breakdown. The Synthesiser classifies the scenario and selects
which Execution Team agents are actually needed — and the workflow enforces
that routing, visibly skipping unselected agents. The Architect produces a
structured plan whose `affected_files` contract is mechanically checked
against the Backend agent's actual diff; a violation forces HIGH severity so
the change can never auto-deploy unreviewed. Quality Assurance generates
property-based tests from the incident and the diff [9]; the Validator
executes patch and tests in a locked-down container sandbox, so no
unvalidated patch ever reaches a human. Only then does the single human
decision point engage — and only when severity warrants it: HIGH-severity
changes require explicit review, MEDIUM-severity changes auto-approve after a
120-second window unless overridden, and trivial LOW-severity changes pass
immediately. The decision itself is richer than a binary gate: the engineer
may Approve, Reject, or Modify the diff before it ships, with every terminal
decision persisted for future preference learning. On approval, a real GitHub
pull request is opened and a post-deploy rollback probe watches for
regression [6].

Two further properties distinguish this from a scripted pipeline of existing
tools. First, role specialisation is structural, not cosmetic: five Execution
Team agents (Architect, Backend, Quality Assurance, DevOps, Data Engineer)
and four Self-Healing Loop agents (Sentinel, Pathfinder, Synthesiser,
Validator) have distinct inputs, outputs, and contracts, and the Architect's
plan constrains the others. Second, the loop is closed over the platform's
own infrastructure: when Nexis's preview-deploy engine fails to build or
health-check a repository, that failure is inserted as a first-class incident
into the very same pipeline, and "write a working Dockerfile" becomes an
ordinary repair task. To the author's knowledge, no tool in common use today
combines these properties in a single system.

## 2.3 Conclusion

The surveyed landscape supplies every ingredient Nexis needs — durable
orchestration [2], graph traversal [3], sandboxed execution [10, 11],
property-based testing [9], GitOps delivery [6], and a mature
incident-capture ecosystem [14, 16, 15] — but leaves the loop between
detection and repair open, to be closed by a human. Nexis's contribution is
not any single new algorithm; it is the demonstrated composition of these
tools into a role-specialised multi-agent team that detects, diagnoses,
repairs, validates, and deploys autonomously, engaging human judgement once,
at a severity-routed gate. The remaining chapters describe how that
composition was specified, designed, implemented, and tested.
