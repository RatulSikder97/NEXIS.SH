# Chapter 6: Testing and Results

This chapter sets out the verification evidence as it was gathered: static checks across every service, live end-to-end runs driven by a real local Large Language Model (LLM) rather than stubs or mocks, and the defects that verification turned up.

## 6.1 Static Verification

Every command in Table 6.1 was executed against the final codebase; the result column is not aspirational. The Go suites include the JSON Web Token (JWT) clock-injection tests discussed in Section 6.4, which were silently failing earlier and are now genuinely passing.

**Table 6.1: Static verification commands and results.**

| Component | Command | Result |
|---|---|---|
| Go modules: `control-plane`, `validator`, `gitops`, `deploy-engine` | `go build ./... && go vet ./... && go test ./...` | Zero errors, zero vet findings, all tests pass |
| Python: `causal-inference`; validator Hypothesis sidecar [9] | `pytest` | 38 tests passing; sidecar suite passing |
| `apps/web` — Next.js | `pnpm typecheck && pnpm lint && pnpm build` | All three gates pass |

## 6.2 Live End-to-End Recovery Runs

Static verification establishes per-service soundness, not that the nine agents, the Temporal workflow [2], the validator sandbox, the approval gate, and the live console actually cooperate. Two recovery runs were therefore carried out against the real system: genuine PostgreSQL with Row-Level Security switched on, genuine Temporal workflows, a genuine Neo4j graph, and a real local LLM (Ollama [20] serving `qwen2.5-coder:14b` and `gpt-oss:20b`) generating the actual plans, diffs, and tests. Nothing was mocked or replayed.

### 6.2.1 Run 1: Approval-Gate Timeout — the Safety Mechanism Under Real Timing Pressure

A fault fixture was injected, Sentinel detected it, and the pipeline ran to completion: the Architect produced a plan, the Backend agent a unified-diff patch, the Quality Assurance (QA) agent Hypothesis property tests, and the Validator executed the patch against them inside its locked-down container sandbox, with every Layer 1 and 2 step turning up green. At the human approval gate, however, no decision arrived within the 120-second window, so the workflow resolved to a rejected terminal state.

This is a *positive* result: the central safety invariant is that no patch deploys without validation and approval, and that the system fails closed on any ambiguity, where an absent decision counts as refusal, never as consent. Rather than ship an unreviewed change, the system discarded a genuinely generated patch and durably recorded the timeout-rejected outcome. It is reported deliberately: a rejection is the stronger evidence that the gate is real.

### 6.2.2 Run 2: Approved and Succeeded in 1 Minute 40 Seconds

In the second run, the same detection path was followed, but approval was granted within the window through the console's decision dialog. Classified as Medium severity, the patch passed sandbox validation, and the pipeline proceeded through deploy to a *Succeeded* state, 1 minute 40 seconds end-to-end. Two properties set this run apart from a staged demonstration: the timeline records genuine per-agent token and cost figures from the live Ollama provider, and the raw JSON of the Backend agent's activity event contains the actual unified diff written by `qwen2.5-coder:14b` (`provider="ollama"` appears in its metadata): the deployed patch is the very patch the model wrote, not a canned fixture.

## 6.3 Live Deploy-Engine Verification

The deploy-engine was run live against the public repository `heroku/node-js-getting-started`, chosen because it contains no `Dockerfile`, the engine's hardest path. It cloned the repository, detected the stack from marker files (`detected_stack: "node"`), generated a working `Dockerfile` from scratch (`dockerfile_source: "generated"`), built the image, and ran it under resource limits. Liveness was then proved directly: an HTTP `GET` on the preview URL returned the app's real rendered HTML, and the stop endpoint removed the container, three HTTP calls against a public repository, so the run is reproducible. Docker was unavailable on the development machine, so Podman [11] served as runtime through the same Docker-compatible API [10]. The clone was unauthenticated, so the GitHub App installation-token path went unexercised, a point noted among the limitations.

## 6.4 Defects Found and Fixed During This Engineering Cycle

Verification surfaced two genuine defects (Table 6.2). The JWT defect was *silent*, failing without blocking anything visible, and it was caught only because the full `go test ./...` output was read rather than skimmed. The migration defect, by contrast, manifests only at apply time.

**Table 6.2: Defects discovered by verification.**

| Defect | Root Cause | Fix | Outcome |
|---|---|---|---|
| 13 JWT tests silently failing | `exp` claims checked against the wall clock, not an injected test clock | Inject clock via `jwt.WithTimeFunc` (one line) | All 13 pass deterministically; production behaviour unchanged |
| Migration failed to apply | Reserved PostgreSQL keyword `current_role` used unquoted as a column name | Column renamed to `existing_role` | Applies cleanly; Go suites remain green |

One further bug, pre-existing and out of scope, was deliberately left unfixed: the workspace-onboarding poller receives a 404 from `/v1/workspaces/{id}/events`, though the workspace provisions server-side. This is recorded among the limitations.

## 6.5 Testing Summary

Static verification came back 100% clean across every service, and on top of it sit three live runs: two nine-agent recovery executions exercising both approval-gate branches, plus one deploy-engine run, none simulated, every LLM step served by a real local model. Equally important is what was *not* tested: no formal load or performance testing was carried out, no throughput or latency under multi-tenant traffic is claimed, and the 1-minute-40-second figure reflects a single observed run, not a statistical claim. Load testing and a larger sample of recovery runs, sufficient to support meaningful Mean Time To Recovery (MTTR) distributions, remain future work (Chapter 7).
