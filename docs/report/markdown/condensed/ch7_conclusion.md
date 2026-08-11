# Chapter 7: Conclusion and Future Work

## 7.1 Overview

This project set out to close the loop that existing Artificial Intelligence (AI) developer tools leave open — from detected software failure to deployed, verified fix — with a human as a single decision point rather than the pipeline itself. The delivered platform, Nexis, realises that goal as a working system, not a design proposal.

The central deliverable is the nine-agent closed-loop self-healing pipeline, durably orchestrated by Temporal [2]: a five-agent Execution Team (Architect, Backend, QA, DevOps, Data Engineer) performs the engineering work, while the four-agent Self-Healing Loop (Sentinel, Pathfinder, Synthesiser, Validator) detects faults, localises root causes over the Neo4j dependency graph [3], sandbox-validates every candidate patch, and routes it through a severity-classified Approval Gate offering Approve, Reject, and Modify decisions. Chapter 6 exercises this pipeline live in two end-to-end recovery runs against a real local language model [20]: one timeout-rejected exactly as the safety design intends when its 120-second approval window elapsed, and one carrying a detected fault through planning, patch generation, property-based test validation, and human approval to a Succeeded deployment in approximately one minute and forty seconds.

The second major deliverable, a from-scratch Docker-compatible preview-deploy engine [10], detects a repository's stack, generates a missing Dockerfile, builds and serves the container under resource limits, and returns a live preview URL; its build and health-check failures feed the same self-healing loop, and it was proven live on a public repository lacking a Dockerfile. Around both sits the complete multi-tenant platform — SSO/MFA authentication [12], row-level RBAC [7], Stripe billing [13], GitHub, Sentry, Datadog, PagerDuty, and Slack integrations [14, 16, 15, 17], and OpenTelemetry observability [5] with a hash-chained audit log — all of it passing full build, lint, and test suites.

## 7.2 Limitations of the Project

Academic integrity requires that the boundary between what was demonstrated and what was not be stated plainly. The following limitations are real, known, and deliberate; none is hidden behind softer wording.

- **No production credentials were exercised.** No real GitHub App or OpenAI production credentials existed in the evaluation environment used for this report. The live evidence was produced with local Ollama models [20] and an unauthenticated clone of a public repository; a fully credentialed production path — ending in a real merged GitHub pull request — was not exercised end-to-end.
- **The platform is not yet live on its domain.** The `nexis.sh` domain is registered but the platform is not yet deployed to it. All evidence in this report comes from local, native execution. Docker was unavailable in the build environment, so Podman [11] was substituted as a runtime compatible with the Docker Application Programming Interface (API) and shown to behave identically.
- **Fine-tuning is a manual step by design.** The Reinforcement Learning from Human Feedback (RLHF) data-export pipeline is real and tested — every terminal approval decision, including engineer-edited diffs, is persisted and exportable as JSON Lines (JSONL) — but actually submitting a fine-tuning job against that export is a deliberate manual step involving real money and a real external API call, not an automated one.
- **One of twenty-seven demo scenarios is fully wired.** The Live-Demo console lists twenty-seven curated fault scenarios, of which exactly one is wired end-to-end to a real fixture and classification path today; the remaining twenty-six are User Interface (UI)-complete but backend-pending.
- **The causal ranking is a proxy, not a fitted causal model.** The causal-inference sidecar ranks root-cause candidates using an honest statistical and graph-structural scoring method with a transparent per-component confidence breakdown. It is explicitly not a fitted DoWhy structural causal model [4], because no interventional production data exists against which such a model could be fitted; claiming otherwise would be an overstatement.
- **A known unrelated bug was left unfixed.** A pre-existing bug in the workspace-onboarding progress poller (a 404 from `/v1/workspaces/{id}/events`) was found during evaluation and intentionally left unfixed as out of scope; the workspace itself provisions successfully server-side regardless.
- **No formal load testing was performed.** As noted in Chapter 6, no formal concurrent-load performance benchmark was run; the reported latencies come from single-run live executions, not from a controlled load harness.

## 7.3 Future Work

Each item is grounded in the existing architecture — a concrete next step, not a generic aspiration.

- **Credentialed production deployment** to the registered `nexis.sh` domain with a real GitHub App and paid OpenAI account — the only way to exercise authenticated clone, real pull-request creation, and merge-triggered redeploy end-to-end.
- **Complete the scenario library.** Wiring the remaining twenty-six Live-Demo cards to real fixtures extends fault-injection coverage across all five scenario categories.
- **Per-service Sentinel baselines.** Splitting the per-organisation EWMA baseline per service stops a noisy service masking a quiet one.
- **A fitted causal model.** Once interventional production data exists, fit a real DoWhy model [4] and evaluate it against the deliberately replaceable graph-evidence proxy.
- **Execute a fine-tuning run** on the RLHF export, measuring whether a tuned model lowers rejection and modification rates at the gate.
- **Real preview subdomains** via a Traefik-style reverse proxy, replacing `localhost` ports so previews can be shared beyond one machine.
- **Formal load benchmarking** to characterise throughput, queueing under simultaneous incidents, and Temporal worker saturation.
- **Human-subjects usability study.** The NASA-TLX instrument [18] is already integrated into the post-incident flow, leaving only recruitment and analysis.

## 7.4 Conclusion

The central claim is deliberately narrow and deliberately strong: a software fault can travel from automatic detection, through multi-agent diagnosis, patch generation, and sandboxed validation, past a single human approval, to a deployed fix inside one continuous, durable pipeline — demonstrated, not asserted: once the loop refused to proceed when its gate went unanswered; once it carried a real fault to approved, deployed resolution in under two minutes. Equally deliberate is what is not claimed: Section 7.2 draws the evidence boundary exactly where the evidence stops, and within it Nexis shows that human-gated, closed-loop fault recovery is buildable today.
