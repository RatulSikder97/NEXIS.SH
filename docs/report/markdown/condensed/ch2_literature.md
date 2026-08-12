# Chapter 2: Literature Review

## 2.1 Overview

Nexis is fundamentally a systems-integration project: established tools are composed into one closed loop that none forms alone, and the survey marks each tool as either a dependency or an extension.

**Large Language Model (LLM) coding assistants.** GitHub Copilot [1] completes code and drafts multi-file changes in the editor. It never observes a production incident, never validates against a failing system, and never deploys. Nexis's Backend agent fills that same niche differently, invoked by a fault-detection pipeline rather than a keystroke, and machine-validated before any human sees it.

**Durable workflow orchestration.** Temporal [2] provides durable, replayable workflow execution, with retries and state persistence, but no engineering semantics. The nine-agent RecoveryPipeline runs on it as a Temporal workflow, so agent steps survive restarts as durable ActivityEvents. Role routing and contract-violation detection are Nexis's logic, and so is the severity classification.

**Causal inference and graph analysis.** Neo4j [3] stores the code-dependency graph that the Pathfinder agent walks from symptom to candidate root cause, though evidence ranking is Nexis's code. DoWhy [4] and its refutation tests serve as the sidecar's methodological reference; Nexis, however, does *not* fit a DoWhy structural causal model, since no interventional production data exists. Instead, the sidecar ranks candidates by a documented proxy (evidence-chain specificity, graph proximity, textual overlap, scenario prior), together with a transparent per-component confidence breakdown.

**The self-healing/AIOps ecosystem.** Sentry [14] and Datadog [16] capture errors and aggregate telemetry, while PagerDuty [15] pages the on-call engineer; none of the three generates a fix, let alone validates one. All three feed Sentinel as webhook incident sources, and the Slack API [17] carries the reverse: one-click Approval-Gate approval or rejection of a validated patch.

**Further dependencies.** Argo CD [6] handles GitOps deploys, and its Rollback client is real, firing on a policy-gated SLO breach. Docker [10] backs the Validator's network-isolated sandbox and preview deploys; Podman [11], the API-compatible evaluation substitute, behaved identically, reported honestly. Hypothesis [9] sees a novel use: the Quality Assurance (QA) agent generates pytest/Hypothesis suites from incident and diff, and a background loop replays them, raising incidents on pass-to-fail regressions. The Data Engineer agent, in turn, emits OpenLineage [8] RunEvents per migration. OpenTelemetry [5] and WorkOS [12] are used as documented, as are Stripe [13], Row-Level Security [7], pgvector [19], and Ollama [20], the LLM behind the report's live evidence.

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

Industry already chains these tools routinely, yet none of them, alone or combined, both (a) models a software engineering *team* with explicit role specialisation and (b) closes the loop from automatic fault detection to a deployed, verified fix, with human judgement concentrated at one severity-routed decision point. Hence the break is always the same: between "a fault is known" and "a validated fix exists," a human performs the diagnosis, authoring, testing, review, and release, unaided.

Nexis closes that span. Sentinel detects faults through an Exponentially Weighted Moving Average (EWMA) baseline breached at mean-plus-three-sigma, and the Synthesiser routes only the needed agents, the workflow visibly skipping the rest. The Architect's `affected_files` contract is checked mechanically against the Backend diff; a violation forces HIGH severity, so no change auto-deploys unreviewed. QA generates property-based tests [9]; the Validator then executes patch and tests inside a sandbox, so no unvalidated patch reaches a human. Only one decision point engages: HIGH requires review, MEDIUM auto-approves after 120 seconds unless overridden, LOW passes immediately. Engineers may Approve, Reject, or Modify, and each decision is persisted for preference learning. Approval opens a real GitHub pull request under a post-deploy rollback probe [6].

Two properties set it apart from scripted tool-chaining. Role specialisation is structural: five Execution Team agents and four Self-Healing Loop agents each carry distinct inputs, outputs, and contracts under the Architect's plan. Moreover, the loop closes over Nexis's own infrastructure, since a preview deploy that fails its build or health check re-enters the pipeline as a first-class incident, so "write a working Dockerfile" becomes another ordinary repair task. To the author's knowledge, no tool in common use combines both properties within a single system.

## 2.3 Conclusion

The landscape supplies nearly every ingredient, yet leaves a human to close the detection-to-repair loop. Thus, Nexis's contribution is not a new algorithm but the composition of a role-specialised multi-agent team that detects, diagnoses, repairs, validates, and deploys autonomously, engaging human judgement once, at a severity-routed gate. The chapters that follow describe its specification, design, implementation, and testing.
