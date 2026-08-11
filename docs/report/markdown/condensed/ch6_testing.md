# Chapter 6: Testing and Results

This chapter reports the verification evidence as it occurred: static verification of every service; live end-to-end runs driven by a real local Large Language Model (LLM) rather than stubs or mocks; and the defects uncovered.

## 6.1 Static Verification

Every command in Table 6.1 was executed against the final codebase — the result column is not aspirational. The Go suites include the JSON Web Token (JWT) clock-injection tests of Section 6.4 — silently failing earlier, now genuinely passing.

**Table 6.1: Static verification commands and results.**

| Component | Command | Result |
|---|---|---|
| Go modules: `control-plane`, `validator`, `gitops`, `deploy-engine` | `go build ./... && go vet ./... && go test ./...` | Zero errors, zero vet findings, all tests pass |
| Python: `causal-inference`; validator Hypothesis sidecar [9] | `pytest` | 38 tests passing; sidecar suite passing |
| `apps/web` — Next.js | `pnpm typecheck && pnpm lint && pnpm build` | All three gates pass |

## 6.2 Live End-to-End Recovery Runs

Static verification establishes per-service soundness, not that the nine agents, the Temporal workflow [2], the validator sandbox, the approval gate, and the live console cooperate. Two recovery runs were therefore executed against the real system — real PostgreSQL with Row-Level Security active, real Temporal workflows, a real Neo4j graph, and a real local LLM (Ollama [20] serving `qwen2.5-coder:14b` and `gpt-oss:20b`) generating the actual plans, diffs, and tests. Nothing was mocked or replayed.

### 6.2.1 Run 1: Approval-Gate Timeout — the Safety Mechanism Under Real Timing Pressure

A fault fixture was injected, Sentinel detected it, and the pipeline ran to completion: the Architect produced a plan, the Backend agent a unified-diff patch, the Quality Assurance (QA) agent Hypothesis property tests, and the Validator executed the patch against them in its locked-down container sandbox — every Layer 1 and 2 step green. At the human approval gate no decision arrived within the 120-second window; the workflow resolved to a rejected terminal state.

This is a *positive* result: the central safety invariant is that no patch deploys without validation and approval and the system fails closed on ambiguity — an absent decision is refusal, never consent. It discarded a real generated patch rather than ship an unreviewed change, durably recording the timeout-rejected outcome — reported deliberately: the rejection is the stronger evidence that the gate is real.

### 6.2.2 Run 2: Approved and Succeeded in 1 Minute 40 Seconds

The second run took the same detection path; approval was granted within the window via the console's decision dialog: classified Medium severity, the patch passed sandbox validation and the pipeline proceeded through deploy to a *Succeeded* state — 1 minute 40 seconds end-to-end. Two properties distinguish it from staged demonstration: the timeline records genuine per-agent token and cost figures from the live Ollama provider, and the raw JSON of the Backend agent's activity event contains the actual unified diff written by `qwen2.5-coder:14b` (`provider="ollama"` in its metadata) — the deployed patch is the patch the model wrote, not a canned fixture.

## 6.3 Live Deploy-Engine Verification

The deploy-engine ran live against the public repository `heroku/node-js-getting-started`, which deliberately contains no `Dockerfile` — the engine's hardest path. It cloned the repository, detected the stack from marker files (`detected_stack: "node"`), generated a working `Dockerfile` from scratch (`dockerfile_source: "generated"`), built the image, ran it under resource limits, and proved liveness: an HTTP `GET` on the preview URL returned the app's real rendered HTML, and the stop endpoint removed the container (three HTTP calls, public repository — reproducible). Docker was unavailable; Podman [11] served as runtime via the same Docker-compatible API [10]. The clone was unauthenticated — the GitHub App installation-token path was not exercised, per the limitations.

## 6.4 Defects Found and Fixed During This Engineering Cycle

Verification surfaced two genuine defects (Table 6.2). The JWT defect was *silent* — failing without blocking anything visible — caught only because the full `go test ./...` output was read rather than skimmed; the migration defect manifests only at apply time.

**Table 6.2: Defects discovered by verification.**

| Defect | Root Cause | Fix | Outcome |
|---|---|---|---|
| 13 JWT tests silently failing | `exp` claims checked against the wall clock, not an injected test clock | Inject clock via `jwt.WithTimeFunc` (one line) | All 13 pass deterministically; production behaviour unchanged |
| Migration failed to apply | Reserved PostgreSQL keyword `current_role` used unquoted as a column name | Column renamed to `existing_role` | Applies cleanly; Go suites remain green |

One pre-existing out-of-scope bug was deliberately left unfixed: the workspace-onboarding poller receives a 404 from `/v1/workspaces/{id}/events`, though the workspace provisions server-side; it is recorded in the limitations.

## 6.5 Testing Summary

Static verification is 100% clean across every service; atop it sit three live runs — two nine-agent recovery executions exercising both approval-gate branches, and one deploy-engine run — none simulated, every LLM step served by a real local model. Equally important, what was *not* tested: no formal load or performance testing was performed, no throughput or latency under multi-tenant traffic is claimed, and the 1-minute-40-second figure is one observed run, not a statistical claim. Load testing and a larger sample of recovery runs, supporting meaningful Mean Time To Recovery (MTTR) distributions, are future work (Chapter 7).
