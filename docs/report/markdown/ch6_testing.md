# Chapter 6: Testing and Results

This chapter reports the verification evidence for NEXIS as it actually
occurred during the final engineering cycle. Rather than presenting a
constructed matrix of numbered test cases, the chapter states three kinds of
real evidence: (i) static verification — the build, vet, lint, type-check,
and automated test commands that were run against every service, and their
results; (ii) live end-to-end integration runs performed against the real,
running system — two complete nine-agent recovery pipeline executions and
one complete deploy-engine execution, all driven by a real local Large
Language Model (LLM) rather than stubs or mocks; and (iii) the defects that
this testing process itself uncovered, together with their root causes and
fixes. The chapter closes with an honest summary of what was and was not
tested.

## 6.1 Static Verification

Every component of the platform was verified with its native toolchain. For
the four independent Go (Golang) modules, the full sequence
`go build ./...`, `go vet ./...` (the Go (Golang) toolchain's built-in
static analyser), and `go test ./...` was executed and required to complete
with zero errors, zero vet findings, and zero test failures. The two Python
components were verified with `pytest`; the causal-inference service's
suite comprises 38 tests, and the validator's Hypothesis-based sidecar [9]
has its own passing suite. The Next.js frontend was verified with the
project's three gate commands: TypeScript type checking, ESLint, and a full
production build. Table 6.1 summarises the commands and their outcomes.

**Table 6.1: Static verification commands and results across all NEXIS components.**

| Component | Command | Result |
|---|---|---|
| `services/control-plane` — Go (Golang) | `go build ./... && go vet ./... && go test ./...` | Clean — build, vet, and all tests pass |
| `services/validator` — Go (Golang) | `go build ./... && go vet ./... && go test ./...` | Clean — build, vet, and all tests pass |
| `services/gitops` — Go (Golang) | `go build ./... && go vet ./... && go test ./...` | Clean — build, vet, and all tests pass |
| `services/deploy-engine` — Go (Golang) | `go build ./... && go vet ./... && go test ./...` | Clean — build, vet, and all tests pass |
| `services/causal-inference` — Python | `pytest` | 38 tests, all passing |
| Validator Hypothesis sidecar — Python | `pytest` | All tests passing |
| `apps/web` — Next.js | `pnpm typecheck && pnpm lint && pnpm build` | Clean — type check, lint, and production build all pass |

Two aspects of this table deserve emphasis. First, the result column is not
aspirational: every command listed was actually executed against the final
codebase and completed cleanly, and the Go (Golang) test suites include the
JSON Web Token (JWT) clock-injection tests discussed in Section 6.4, which
were silently failing earlier in the cycle and now genuinely pass. Second,
static verification covers every deployable unit of the platform — there is
no service that ships without passing its build, its static analysis, and
its automated tests.

## 6.2 Live End-to-End Recovery Runs

Static verification establishes that each service is internally sound; it
says nothing about whether the nine agents, the Temporal workflow [2], the
validator sandbox, the approval gate, and the live console actually
cooperate. For that, two complete recovery pipeline runs were executed
against the real running system — real PostgreSQL with Row-Level Security
(RLS) active, real Temporal workflows, real Neo4j dependency graph, and a
real local LLM (Ollama [20] serving `qwen2.5-coder:14b` and `gpt-oss:20b`)
generating the actual plans, diffs, and tests. Nothing in either run was
mocked or replayed. Chapter 5 reproduces the console evidence captured
during both runs: the pipeline mid-flight with live per-agent token counts,
the fully green timeline awaiting approval, the honest timed-out terminal
state of the first run, and the complete Approved-to-Succeeded timeline of
the second.

### 6.2.1 Run 1: Approval-Gate Timeout — the Safety Mechanism Under Real Timing Pressure

In the first run, a fault fixture was injected, Sentinel detected it, and
the full agent pipeline executed to completion: the Architect produced a
structured plan, the Backend agent generated a unified-diff patch, the
Quality Assurance (QA) agent generated Hypothesis property tests, and the
Validator executed the patch against those tests inside its locked-down
Docker (Podman-compatible runtime) sandbox — every Layer 1 and Layer 2 step
reached a green state on the live timeline. The candidate patch then
arrived at the human approval gate, where it sat pending. No human decision
was made within the 120-second decision window, the window elapsed, and the
workflow resolved the incident to a rejected terminal state.

It is important to read this outcome correctly: it is a *positive* test
result, not a failure. The central safety invariant of the platform is that
no patch is ever deployed without passing validation and clearing the
approval gate, and that the system fails closed on ambiguity — an absent
decision is treated as a refusal, never as consent. The first run exercised
exactly this invariant under real timing pressure, with a real generated
patch waiting on the other side of the gate, and the system behaved
precisely as designed: it discarded the work rather than shipping an
unreviewed change, and it recorded the timeout-rejected outcome honestly
and durably in the incident timeline. A demonstration environment could
easily have hidden this run and shown only the success; it is reported here
deliberately, because the rejected run is the stronger evidence that the
gate is real.

### 6.2.2 Run 2: Approved and Succeeded in 1 Minute 40 Seconds

The second run followed the same detection path but the pending approval
was acted on within the window. The incident was classified at Medium
severity, the generated patch passed sandbox validation, the approval was
granted through the console's decision dialog, and the pipeline proceeded
through its deploy stage to a *Succeeded* terminal state. The complete
timeline — from fault detection through every agent step to the final
succeeded state — spanned 1 minute 40 seconds.

Two properties of this run make it strong evidence rather than a staged
demonstration. First, the accounting is real: the timeline records genuine
per-agent token consumption and cost figures for every LLM-backed step, as
produced by the live Ollama provider during the run. Second, the artifacts
are inspectable: the raw JavaScript Object Notation (JSON) payload of the
Backend agent's activity event shows the actual unified diff generated by
`qwen2.5-coder:14b`, with `provider="ollama"` recorded in the event
metadata — the patch that was validated, approved, and deployed is the
patch the model wrote, not a canned fixture.

Together the two runs test both branches of the approval gate — refusal by
timeout and explicit approval — against the same real pipeline, which is
precisely the pair of behaviours a closed-loop recovery system must get
right.

## 6.3 Live Deploy-Engine Verification

The deploy-engine was verified with its own live end-to-end run against a
real public GitHub repository, `heroku/node-js-getting-started`, which
deliberately contains no `Dockerfile`. This exercises the engine's hardest
path: it must clone the repository, detect the stack from marker files,
generate a working `Dockerfile` from scratch, build the image, run the
container under resource limits, and prove liveness over Hypertext
Transfer Protocol (HTTP). Listing 6.1 reproduces the actual
request/response transcript of the run.

**Listing 6.1: Live deploy-engine end-to-end run: clone, generate Dockerfile, build, run, HTTP-verify, stop.**

```text
POST /v1/deploy  (repo: heroku/node-js-getting-started, no Dockerfile present)
-> 200 OK
  "status": "running", "dockerfile_source": "generated", "detected_stack": "node",
  "url": "http://localhost:54671", "image_tag": "nexis-preview-test-project:22d06076c357"
  build_log: "...npm ci... EXPOSE 3000... CMD [\"npm\",\"start\"]..."
  container_log: "Listening on 3000\nRendering 'pages/index' for route '/'"

GET http://localhost:54671/  -> real rendered HTML of the Node app (verified)

POST /v1/deploy/test-e2e-001/stop -> {"status":"stopped"}; container removed.
```

The transcript demonstrates each stage genuinely occurring: the engine
detected a Node.js stack (`detected_stack: "node"`), synthesised a
Dockerfile (`dockerfile_source: "generated"`) whose build log shows the
generated `npm ci`, `EXPOSE 3000`, and `CMD` directives executing; the
container log shows the application itself booting and serving its index
route; an independent HTTP `GET` against the returned preview Uniform
Resource Locator (URL) returned the application's real rendered HyperText
Markup Language (HTML); and the stop endpoint cleanly terminated and
removed the container. Because the repository is public and the request
sequence is three HTTP calls, this is a reproducible integration test, not
a one-off observation.

One environmental substitution should be noted for reproducibility: Docker
itself was unavailable in the evaluation environment, so Podman [11] was
used as the container runtime through the same Docker-compatible
Application Programming Interface (API) [10] that the deploy-engine and
validator target. The engine's code path is identical under both runtimes,
and the run above proves the Podman path works end-to-end. The clone in
this run used unauthenticated public-repository access; the authenticated
GitHub App installation-token path exists in the code but was not exercised
in this evaluation environment, as stated in the limitations.

## 6.4 Defects Found and Fixed During This Engineering Cycle

The verification process was not merely confirmatory: it surfaced two
genuine defects, both of which were root-caused and fixed during the cycle.
Table 6.2 summarises them.

**Table 6.2: Defects discovered by the verification process, with root causes, fixes, and verification.**

| Defect | Root Cause | Fix | Verification |
|---|---|---|---|
| 13 JWT-related tests silently failing | Token `exp` claims were validated against the wall clock instead of an injected test clock, so time-sensitive assertions depended on when the suite happened to run | One-line change: inject the test clock via `jwt.WithTimeFunc` | All 13 tests now pass deterministically; production behaviour verified unchanged, because the production code's default clock is already identical to the JWT library's own default |
| Database migration failed to apply | `current_role` is a reserved PostgreSQL keyword and was used unquoted as a column name in a migration | Column renamed to `existing_role` | Migration applies cleanly; the Go (Golang) test suites over the affected schema remain green |

Both defects illustrate why the verification loop earns its cost. The JWT
defect was *silent*: the affected tests were failing without blocking
anything visible, which is exactly the failure mode that erodes a test
suite's value over time; it was found only because the full
`go test ./...` output was read rather than skimmed. The migration defect
was latent in a reserved-word collision that only manifests at
migration-apply time — the kind of bug that static review of the
Structured Query Language (SQL) text can easily miss.

For completeness, one further pre-existing bug was found during this cycle
but deliberately not fixed, as it is unrelated to the recovery pipeline and
out of scope: the workspace-onboarding progress poller requests
`/v1/workspaces/{id}/events` and receives a 404, although the workspace
itself provisions successfully server-side regardless. It is recorded
here, and in the limitations, rather than quietly omitted.

## 6.5 Testing Summary

The evidence base for NEXIS, stated plainly, is as follows. Static
verification is 100% clean across every service in the platform: four Go
(Golang) modules pass build, vet, and their full test suites with zero
errors; both Python services pass `pytest`, including the causal-inference
service's 38 tests; and the frontend passes type checking, linting, and a
full production build. On top of that foundation sit three live
integration runs observed directly against the real running system — two
complete nine-agent recovery pipeline executions (one timeout-rejected,
demonstrating the fail-closed approval gate; one Approved and Succeeded in
1 minute 40 seconds with real per-agent token and cost accounting) and one
complete deploy-engine run against a real public repository (clone,
generated Dockerfile, build, run, HTTP-verified, stopped). None of these
runs were simulated, and the LLM behind the recovery runs was a real local
model, not a stub.

Equally important is what was *not* tested. No formal load or performance
testing was performed: the platform has not been exercised under
concurrent multi-tenant traffic, no throughput or latency figures under
load were measured, and consequently none are claimed in this report. The
single measured end-to-end duration (1 minute 40 seconds) is the observed
time of one real run, not a statistical performance claim. Load testing
under realistic multi-tenant concurrency, along with a larger sample of
recovery runs from which meaningful Mean Time To Recovery (MTTR)
distributions could be drawn, is named explicitly as future work in
Chapter 7.
