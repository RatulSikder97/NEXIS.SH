# Chapter 2: Literature Review

## 2.1 Overview

Nexis is a systems-integration project: it composes established tools into a
closed loop none forms alone; the survey marks, per tool, dependency versus
extension.

**Large Language Model (LLM) coding assistants.** GitHub Copilot [1] completes
code and drafts multi-file changes in-editor, yet never observes a production
incident, never validates against a failing system, and never deploys. Nexis's
Backend agent fills the same niche, invoked by a fault-detection pipeline
rather than a keystroke, machine-validated before any human sees it.

**Durable workflow orchestration.** Temporal [2] supplies durable, replayable
workflow execution with retries and state persistence but no engineering
semantics. The nine-agent RecoveryPipeline is a Temporal workflow, so agent
steps survive restarts as durable ActivityEvents; the role routing,
contract-violation detection, and severity classification are Nexis's logic.

**Causal inference and graph analysis.** Neo4j [3] stores the code-dependency
graph the Pathfinder agent walks from symptom to candidate root causes; the
evidence ranking is Nexis code. DoWhy [4], with its refutation tests, is the
sidecar's methodological reference, but Nexis does *not* fit a DoWhy
structural causal model: no interventional production data exists. The sidecar
instead ranks candidates by a documented proxy (evidence-chain specificity,
graph proximity, textual overlap, scenario prior) with a transparent
per-component confidence breakdown.

**The self-healing/AIOps ecosystem.** Sentry [14], Datadog [16], and PagerDuty
[15] capture errors, aggregate telemetry, and page on-call humans;
none generates a fix, let alone validates one. All three feed Sentinel as
webhook incident sources; the Slack API [17] carries the reverse, one-click
Approval-Gate approve/reject of validated patches.

**Further dependencies.** Argo CD [6] handles GitOps deploys, its real
Rollback client firing on a policy-gated SLO breach. Docker [10] backs the
Validator's network-isolated sandbox and preview deploys; Podman [11], the
API-compatible evaluation substitute, behaved identically, reported honestly.
Hypothesis [9] sees novel use: the Quality Assurance (QA) agent generates
pytest/Hypothesis suites from incident and diff; a background loop replays
them, raising incidents on pass-to-fail regressions. The Data Engineer agent
emits OpenLineage [8] RunEvents per migration. OpenTelemetry [5], WorkOS [12],
Stripe [13], Row-Level Security [7], pgvector [19], and Ollama [20] (the
report's live-evidence LLM) are used as documented.

**Table 2.1: Surveyed tools relative to Nexis.**

| Tool | Relation to Nexis |
|---|---|
| GitHub Copilot [1] | Backend-agent analogue only |
| Temporal [2] | Hosts RecoveryPipeline |
| Neo4j [3] | Dependency; Pathfinder traversal |
| DoWhy [4] | Reference; proxy used |
| OpenTelemetry [5] | Instrumentation dependency |
| Argo CD [6] | Deploy, rollback probe |
| Docker/Podman [10, 11] | Sandbox, deploy engine |
| Hypothesis [9] | Novel use: QA suites |
| OpenLineage [8] | Data Engineer conformance |
| Sentry/Datadog/PagerDuty [14, 16, 15] | Webhook sources |
| Slack API [17] | One-click approvals |

## 2.2 Research Gap

Industry routinely chains these tools, yet none, alone or combined, both (a) models a software engineering *team* with explicit role
specialisation and (b) closes the loop from automatic fault detection to a
deployed, verified fix with human judgement concentrated at one
severity-routed decision point. The break is always the same: between "a fault
is known" and "a validated fix exists," a human performs diagnosis, authoring,
testing, review, and release unaided.

Nexis closes that span: Sentinel detects faults via an Exponentially Weighted
Moving Average (EWMA) baseline breached at mean-plus-three-sigma. The
Synthesiser routes only needed agents, the workflow visibly skipping the rest.
The Architect's `affected_files` contract is mechanically checked against the
Backend diff; a violation forces HIGH severity, so no change auto-deploys
unreviewed. QA generates property-based tests [9]; the Validator executes
patch and tests sandboxed, so no unvalidated patch reaches a human. One
decision point engages: HIGH requires review, MEDIUM auto-approves after 120
seconds unless overridden, LOW passes immediately. Engineers may Approve,
Reject, or Modify, decisions persisted for preference learning. Approval opens
a real GitHub pull request under a post-deploy rollback probe [6].

Two properties distinguish it from scripted tool-chaining. Role specialisation
is structural: five Execution Team agents and four Self-Healing Loop agents
have distinct inputs, outputs, and contracts under the Architect's plan. And
the loop closes over Nexis's own infrastructure: a preview deploy that fails
its build or health check re-enters the pipeline as a first-class incident, so
"write a working Dockerfile" becomes an ordinary repair task. To the author's
knowledge, no tool in common use combines these properties in a single system.

## 2.3 Conclusion

The landscape supplies every ingredient yet leaves a human to close the
detection-to-repair loop. Nexis's contribution is not a new algorithm but the
composition of a role-specialised multi-agent team that detects, diagnoses,
repairs, validates, and deploys autonomously, engaging human judgement once at
a severity-routed gate. Later chapters describe its specification, design,
implementation, and testing.
