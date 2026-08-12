# Chapter 6: Testing and Results

The verification evidence for NEXIS is laid out here in the order it actually
occurred during the final engineering cycle, not as a matrix of numbered test
cases assembled after the fact. Three kinds of real evidence follow. Static
verification comes first: the build, vet, lint, type-check, and automated test
commands run against every service, along with what each one returned. Next
are the live end-to-end integration runs carried out against the real, running
system, namely two complete nine-agent recovery pipeline executions and one
complete deploy-engine execution, all driven by a real local Large Language
Model (LLM) rather than by stubs or mocks. The third kind is the set of
defects that the testing process itself turned up, together with their root
causes and the fixes applied. What was tested, and, just as importantly, what
was not, is set out honestly at the close of the chapter.

## 6.1 Static Verification

Each component of the platform was verified through its own native toolchain,
not a single uniform check. Across the four independent Go (Golang) modules,
the full sequence of `go build ./...`, `go vet ./...` (the Go (Golang)
toolchain's built-in static analyser), and `go test ./...` was run, with a
pass requiring zero build errors, zero vet findings, and zero failing tests.
For the two Python components, `pytest` was the tool of choice: the
causal-inference service's suite runs 38 tests, and the validator's
Hypothesis-based sidecar [9] carries its own suite, which likewise passes in
full. The Next.js frontend, for its part, was verified through the project's
three gate commands, namely TypeScript type checking, ESLint, and a full
production build. Table 6.1 sets out the commands used against each
component, along with the outcomes obtained.

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

The result column in this table is not aspirational. Every command listed was
actually run against the final codebase and returned clean, and this includes
the Go (Golang) suites, which now cover the JSON Web Token (JWT)
clock-injection tests taken up in Section 6.4, tests that were silently
failing earlier in the cycle and which genuinely pass at this point. Static
verification, moreover, reaches every deployable unit of the platform;
consequently, no service ships without first clearing its own build, its own
static analysis, and its own test suite.

## 6.2 Live End-to-End Recovery Runs

Static verification, it may be noted, only establishes that each service is
sound on its own; it says nothing about whether the nine agents, the Temporal
workflow [2], the validator sandbox, the approval gate, and the live console
actually work together as a system. Two complete recovery pipeline runs were
carried out against the real running system to test exactly that: real
PostgreSQL with Row-Level Security (RLS) active, real Temporal workflows, a
real Neo4j dependency graph, and a real local LLM, Ollama [20] serving
`qwen2.5-coder:14b` and `gpt-oss:20b`, generating the actual plans, diffs, and
tests. Nothing in either run was mocked or replayed. Chapter 5 reproduces the
console evidence captured across both runs, from the pipeline mid-flight with
live per-agent token counts and the fully green timeline awaiting approval,
through to the honest timed-out terminal state of the first run and the
complete Approved-to-Succeeded timeline of the second.

### 6.2.1 Run 1: Approval-Gate Timeout — the Safety Mechanism Under Real Timing Pressure

A fault fixture was injected in the first run, Sentinel picked it up, and the
agent pipeline then ran through to completion: the Architect produced a
structured plan, the Backend agent generated a unified-diff patch, the Quality
Assurance (QA) agent wrote Hypothesis property tests, and the Validator ran
the patch against those tests inside its locked-down Docker (Podman-compatible
runtime) sandbox. Every step across Layer 1 and Layer 2 reached a green state
on the live timeline. It was at the human approval gate that the candidate
patch then came to rest, pending a decision; since none was made within the
120-second decision window, the workflow resolved the incident to a rejected
terminal state once that window had elapsed.

In this regard, the outcome deserves to be read correctly: it counts as a
*positive* test result, not a failure. The platform's central safety
invariant holds that no patch is ever deployed without passing validation and
clearing the approval gate, and further, that the system fails closed on
ambiguity, treating an absent decision as a refusal and never as consent. Real
timing pressure was exactly what the first run exercised against this
invariant, with a real generated patch sitting on the other side of the gate,
and the system responded precisely as it had been designed to. Rather than
ship an unreviewed change, it discarded the work, and the timeout-rejected
outcome was recorded honestly and durably in the incident timeline. A
demonstration environment could easily have hidden this particular run and
shown only the success story; it is reported here deliberately, since a
rejected run is, if anything, the stronger evidence that the approval gate is
real.

### 6.2.2 Run 2: Approved and Succeeded in 1 Minute 40 Seconds

The same detection path was followed in the second run, except that the
pending approval this time was acted on within the window. Classified at
Medium severity, the incident produced a generated patch that passed sandbox
validation; approval then came through the console's decision dialog, and the
pipeline moved on through its deploy stage to a *Succeeded* terminal state.
From fault detection through every agent step to that final succeeded state,
the complete timeline spanned 1 minute 40 seconds.

This run is strong evidence rather than a staged demonstration. The
accounting, to begin with, is real: the timeline records genuine per-agent
token consumption and cost figures for every LLM-backed step, produced by the
live Ollama provider as the run actually happened. The artefacts, moreover,
are inspectable, since the raw JavaScript Object Notation (JSON) payload of
the Backend agent's activity event shows the actual unified diff generated by
`qwen2.5-coder:14b`, with `provider="ollama"` recorded in the event metadata.
The patch that was validated, approved, and deployed is, in other words, the
patch the model wrote and not a canned fixture.

Between them, the two runs cover both branches of the approval gate, refusal
by timeout on one side and explicit approval on the other, against one and the
same real pipeline. These are exactly the two behaviours a closed-loop
recovery system must get right.

## 6.3 Live Deploy-Engine Verification

The deploy-engine was verified in its own live end-to-end run against a real
public GitHub repository, `heroku/node-js-getting-started`, chosen
deliberately for containing no `Dockerfile`. This is the engine's hardest path
to exercise: it has to clone the repository, work out the stack from marker
files, generate a working `Dockerfile` from nothing, build the image, run the
container under resource limits, and only then prove liveness over Hypertext
Transfer Protocol (HTTP). The actual request/response transcript of this run
is reproduced in Listing 6.1.

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

Each stage in the transcript genuinely took place. A Node.js stack was
detected by the engine (`detected_stack: "node"`), and a Dockerfile was
synthesised (`dockerfile_source: "generated"`) whose build log shows the
generated `npm ci`, `EXPOSE 3000`, and `CMD` directives actually executing.
The container log, in turn, shows the application booting and serving its
index route, while an independent HTTP `GET` against the returned preview
Uniform Resource Locator (URL) came back with the application's real rendered
HyperText Markup Language (HTML); finally, the stop endpoint terminated and
removed the container cleanly. Since the repository is public and the entire
request sequence amounts to only three HTTP calls, this counts as a
reproducible integration test and not merely a one-off observation.

For reproducibility, one environmental substitution is worth flagging. Docker
was not available in the evaluation environment, so Podman [11] stood in as
the container runtime, working through the same Docker-compatible Application
Programming Interface (API) [10] that both the deploy-engine and the validator
target. Since the engine's code path does not differ between the two
runtimes, the run described above is proof enough that the Podman path works
end-to-end. Unauthenticated public-repository access was what the clone in
this run relied on; the authenticated GitHub App installation-token path is
present in the code but was not exercised in this evaluation environment, a
point also made in the limitations.

## 6.4 Defects Found and Fixed During This Engineering Cycle

More than merely confirmatory, the verification process turned out to surface
two genuine defects, and both were root-caused and fixed within the same
cycle. These are summarised in Table 6.2.

**Table 6.2: Defects discovered by the verification process, with root causes, fixes, and verification.**

| Defect | Root Cause | Fix | Verification |
|---|---|---|---|
| 13 JWT-related tests silently failing | Token `exp` claims were validated against the wall clock instead of an injected test clock, so time-sensitive assertions depended on when the suite happened to run | One-line change: inject the test clock via `jwt.WithTimeFunc` | All 13 tests now pass deterministically; production behaviour verified unchanged, because the production code's default clock is already identical to the JWT library's own default |
| Database migration failed to apply | `current_role` is a reserved PostgreSQL keyword and was used unquoted as a column name in a migration | Column renamed to `existing_role` | Migration applies cleanly; the Go (Golang) test suites over the affected schema remain green |

Why the verification loop earns its cost is illustrated well by both defects.
The JWT defect was *silent* in the sense that the affected tests kept failing
without blocking anything visible, precisely the failure mode that erodes a
test suite's value over time; it came to light, hence, only because the full
`go test ./...` output was actually read rather than skimmed over. The
migration defect was different in character, latent in a reserved-word
collision that shows itself only at migration-apply time, the kind of bug a
static review of the Structured Query Language (SQL) text can easily walk
past.

One further pre-existing bug turned up during this cycle, for completeness,
though it was deliberately left unfixed on the grounds that it is unrelated to
the recovery pipeline and falls outside this project's scope: the
workspace-onboarding progress poller requests `/v1/workspaces/{id}/events` and
gets back a 404, even though the workspace itself still provisions
successfully on the server side regardless. Rather than being quietly left
out, it is recorded here and again in the limitations.

## 6.5 Testing Summary

Put plainly, this is the evidence base for NEXIS. Static verification comes
back 100% clean across every service in the platform: the four Go (Golang)
modules pass their build, vet, and full test suites with zero errors between
them; both Python services pass `pytest`, the causal-inference service's 38
tests included; and the frontend clears type checking, linting, and a full
production build. Sitting on top of that foundation are three live
integration runs, each observed directly against the real running system: two
complete nine-agent recovery pipeline executions (one timeout-rejected in a
way that demonstrates the fail-closed approval gate, the other Approved and
Succeeded in 1 minute 40 seconds with real per-agent token and cost
accounting), together with one complete deploy-engine run against a real
public repository (clone, generated Dockerfile, build, run, HTTP-verified,
stopped). Not one of these runs was simulated, and the LLM driving the
recovery runs was a real local model rather than a stub.

What was *not* tested matters just as much. No formal load or performance
testing took place: the platform has not been run under concurrent
multi-tenant traffic, no throughput or latency figures under load were ever
measured, and none, consequently, are claimed in this report. That single
measured end-to-end duration of 1 minute 40 seconds is the observed time of
one real run, nothing more, and should not be read as a statistical
performance claim. Load testing under realistic multi-tenant concurrency,
together with a larger sample of recovery runs from which meaningful Mean Time
To Recovery (MTTR) distributions could actually be drawn, is named explicitly
as future work in Chapter 7.
