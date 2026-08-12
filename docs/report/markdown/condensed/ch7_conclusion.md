# Chapter 7: Conclusion and Future Work

## 7.1 Overview

This project set out to close a loop that existing Artificial Intelligence (AI) developer tools leave open: from a detected software failure to a deployed, verified fix, with a human retained as a single decision point rather than the pipeline itself. Nexis, the platform delivered here, realises that goal as a working system, not a design proposal.

The central deliverable is the nine-agent closed-loop self-healing pipeline, durably orchestrated by Temporal [2]. A five-agent Execution Team (Architect, Backend, QA, DevOps, Data Engineer) carries out the engineering work, while the four-agent Self-Healing Loop (Sentinel, Pathfinder, Synthesiser, Validator) detects faults, localises root causes over the Neo4j dependency graph [3], sandbox-validates every candidate patch, and routes it through a severity-classified Approval Gate offering Approve, Reject, and Modify. Chapter 6 exercised this pipeline live in two end-to-end recovery runs against a real local language model [20]. In one run, the patch was timeout-rejected exactly as the safety design intends, its 120-second approval window having elapsed; in the other, a detected fault travelled through planning, patch generation, property-based test validation, and human approval to a Succeeded deployment in roughly one minute and forty seconds.

The second major deliverable is a from-scratch, Docker-compatible preview-deploy engine [10]: it detects a repository's stack, generates a missing Dockerfile, builds and serves the container under resource limits, and returns a live preview URL. Its build and health-check failures feed back into the same self-healing loop, and the engine was proven live against a public repository that had no Dockerfile. Around both sits the complete multi-tenant platform: SSO/MFA authentication [12], row-level RBAC [7], Stripe billing [13], integrations with GitHub, Sentry, Datadog, PagerDuty, and Slack [14, 16, 15, 17], and OpenTelemetry observability [5] backed by a hash-chained audit log, all of it passing full build, lint, and test suites.

## 7.2 Limitations of the Project

Academic integrity demands that the boundary between what was demonstrated and what was not be stated in plain terms, not softened by hedging. As such, every limitation below is real, known, and deliberate; none of it is dressed up in gentler wording.

- **No production credentials were exercised.** The evaluation environment used for this report held no real GitHub App or OpenAI production credentials. Live evidence was produced instead with local Ollama models [20] and an unauthenticated clone of a public repository, so a fully credentialed production path, ending in a real, merged GitHub pull request, was never exercised end-to-end.
- **The platform is not yet live on its domain.** The `nexis.sh` domain is registered, but the platform has not yet been deployed to it, and every piece of evidence in this report comes from local, native execution. Docker itself was unavailable in the build environment, so Podman [11] was substituted as a runtime compatible with the Docker Application Programming Interface (API) and shown to behave identically.
- **Fine-tuning is a manual step by design.** The Reinforcement Learning from Human Feedback (RLHF) data-export pipeline is real and has been tested: every terminal approval decision, including engineer-edited diffs, is persisted and exportable as JSON Lines (JSONL). Submitting a fine-tuning job against that export, however, is left as a deliberate manual step, since it involves real money and a real external API call rather than an automated one.
- **Only one of twenty-seven demo scenarios is fully wired.** The Live-Demo console lists twenty-seven curated fault scenarios, of which exactly one is wired end-to-end to a real fixture and classification path today. The remaining twenty-six are complete on the User Interface (UI) side but still backend-pending.
- **The causal ranking is a proxy, not a fitted causal model.** The causal-inference sidecar ranks root-cause candidates through an honest statistical and graph-structural scoring method, backed by a transparent, per-component confidence breakdown. Explicitly, it is not a fitted DoWhy structural causal model [4], since no interventional production data exists against which such a model could be fitted, and claiming otherwise would overstate what has been built.
- **A known, unrelated bug was left unfixed.** A pre-existing bug in the workspace-onboarding progress poller, a 404 returned from `/v1/workspaces/{id}/events`, turned up during evaluation and was intentionally left unfixed as out of scope. Regardless, the workspace itself provisions successfully on the server side.
- **No formal load testing was performed.** As Chapter 6 notes, no formal concurrent-load performance benchmark was run, and the latencies reported there come from single-run live executions rather than from a controlled load harness.

## 7.3 Future Work

Each item below is grounded in the existing architecture: it names a concrete next step rather than a generic aspiration.

- **Credentialed production deployment** to the registered `nexis.sh` domain, with a real GitHub App and a paid OpenAI account, is the only way to exercise authenticated cloning, real pull-request creation, and merge-triggered redeploy end-to-end.
- **Complete the scenario library.** Wiring the remaining twenty-six Live-Demo cards to real fixtures would extend fault-injection coverage across all five scenario categories.
- **Per-service Sentinel baselines.** Splitting the per-organisation EWMA baseline down to the level of each service would stop one noisy service from masking a quiet one.
- **A fitted causal model.** Once interventional production data exists, a real DoWhy model [4] can be fitted and evaluated against the deliberately replaceable graph-evidence proxy.
- **Execute a fine-tuning run** on the RLHF export to measure whether a tuned model lowers the rejection and modification rates seen at the gate.
- **Real preview subdomains**, served through a Traefik-style reverse proxy instead of `localhost` ports, would let previews be shared beyond one machine.
- **Formal load benchmarking** to characterise throughput, queueing behaviour under simultaneous incidents, and Temporal worker saturation.
- **Human-subjects usability study.** The NASA-TLX instrument [18] is already wired into the post-incident flow; only recruitment and analysis remain.

## 7.4 Conclusion

The central claim is deliberately narrow, yet deliberately strong. A software fault can travel from automatic detection, through multi-agent diagnosis, patch generation, and sandboxed validation, past a single human approval, to a deployed fix, inside one continuous, durable pipeline. This is demonstrated rather than asserted: once, the loop refused to proceed when its approval gate went unanswered; once, it carried a real fault to an approved, deployed resolution in under two minutes. Equally deliberate is what the project does not claim. Section 7.2 draws the evidence boundary exactly where the evidence stops, and within it, Nexis thus shows that human-gated, closed-loop fault recovery is buildable today.
