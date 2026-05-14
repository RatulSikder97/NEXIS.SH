# Backend Code Audit — 2026-05-14

## Summary

Audit of `services/control-plane` Go backend (chi, pgxpool, Temporal,
pgvector). I found **3 wire-up bugs that silently disable shipped features**
(sentinel router, approval gate, project policy enforcement), **1 runtime
exception** in `/v1/workspaces/{ws}/agents` (queries a nonexistent column),
**2 multi-tenant correctness gaps** (webhook signing optional, signal
race in approval signaler), and a dozen smaller defects (float64 cents,
swallowed errors, defer-in-loop, leaked goroutines).

The dominant theme is **dead wiring**: Phase 6/7 features (approval gate,
project routing, kill-switch enforcement, slack default channel) exist as
working code but the dependency that turns them on is never set in
`cmd/server/main.go`. From the outside they look implemented; from inside
they are stubs.

## Critical (panic, data-corruption, leak)

### F-1: Sentinel Router is never constructed at boot
**Where:** `cmd/server/main.go:521-541`
**Pattern:** `sentinel.New(sentinel.Config{ … Router omitted … })` — the
`Router` field is left zero, so `Detector.tick` falls into the
`if d.router != nil` short-circuit at `internal/sentinel/detector.go:201`
and every trigger fires with `ProjectID=""`.
**Why it bites:** the entire projects/self-healing feature you shipped in
Phase 7 — `MatchByFingerprint`, kill-switch, auto-merge-low/medium,
SlackChannelID override — depends on `trigger.ProjectID` being non-empty.
Every recovery in production uses the fixture path. The repo has
`NewProjectsRepoWithAdmin`, `MatchByFingerprint`, and
`IncidentsRepo.UpdateProjectID` all working — they are just never called.
**Fix:**
```go
// after projectsRepo is constructed (line ~442)
var sentinelRouter *sentinel.Router
if projectsRepo != nil && incRepo != nil {
    sentinelRouter = sentinel.NewRouter(sentinel.RouterConfig{
        Matcher:  projectsRepo,
        Incident: incRepo,
        Logger:   logger,
    })
}
// then pass `Router: sentinelRouter` into sentinel.New(...).
```

### F-2: ApprovalGate activity is permanently stubbed
**Where:** `cmd/server/main.go:361-367`
**Pattern:** `NewActivitiesFull` builds the Activities struct, but
`acts.Approval` (and `acts.Projects`, `acts.SlackDefaultChannel`) are never
assigned. `ApprovalGateRoute` checks `if a.Approval == nil` at
`activities.go:747` and returns `a.stub(...)`.
**Why it bites:** every recovery skips the gate — no decision row, no
severity classification, no audit `approval.requested`, no kill-switch
short-circuit, no Slack notification, no signal-timer race. The
`SlackInteractivity` handler exists but has nothing to interact with.
**Fix:** build an `*approval.Service` from a `*repo.ApprovalRepository`
(which doesn't even exist as a concrete type in
`internal/adapter/repo/` yet — only `domain.ApprovalRepository` does), wire
it onto `acts.Approval`. Same for `acts.Projects = projectsRepo` and
`acts.SlackDefaultChannel = cfg.SlackDefaultChannel`. Without this the
workflow's signal/timer race in `recovery/workflow.go:251` is also
never reached (severityStr stays "" because the stub returns no severity
key).

### F-3: `activity_events.kind` column does not exist — `/v1/workspaces/{ws}/agents` will 500
**Where:** `internal/transport/http/handler/agents.go:528-535,541`
**Pattern:** `loadAgentStats` SQL filters `FILTER (WHERE kind='finish')`
five times. The migration at `migrations/0009_phase4_workflows.up.sql:30-43`
defines `activity_events.status text` (values `started|succeeded|failed|
retrying|timed_out`) — there is no `kind` column.
**Why it bites:** the moment any authenticated user hits
`GET /v1/workspaces/{ws}/agents`, the handler 500s with
`column "kind" does not exist`. `loadAgentRuns` (same file, line 292)
uses `ae.status` correctly — proof the author knew about the column
elsewhere and this slipped through.
**Fix:** replace `kind='finish'` with `(status='succeeded' OR status='failed')`,
or just `status IN ('succeeded','failed','timed_out')` to mirror the
"finish frame" intent. Same change in 5 spots.

### F-4: `workflow.Service.reap` runs UPDATE on `context.Background()` (pre-existing memory entry)
**Where:** `internal/adapter/workflow/service.go:145,175,189`
**Pattern:** Two distinct UPDATEs hit `context.Background()` against the
admin pool. The reaper at line 175 is fine (it's a long-running
goroutine). The pre-commit failure UPDATE at line 145 is the gotcha I
documented in memory `temporal_repo_tx_gotcha.md` — it runs before the
HTTP request tx commits, against the admin pool, and so writes through
RLS-free… but is racing the request tx's `InsertRun`. If
`ExecuteWorkflow` fails fast enough the UPDATE can run before the
admin connection observes the request tx's commit, so it silently
matches zero rows.
**Why it bites:** failed-to-start workflow runs can be left in
`status='queued'` forever because the failure UPDATE never landed. The
audit-chain HMAC also recomputes a row that nominally exists but isn't
visible cross-connection.
**Fix:** route line 145 through the request ctx (`s.cfg.Repo.UpdateRunStatus(ctx, …)`)
so it uses the same tx as the InsertRun above it. The reaper at 175 is
correct as-is.

### F-5: Datadog webhook accepts unsigned payloads by default
**Where:** `internal/adapter/integration/datadog/provider.go:197-210`,
`resolveWebhookSecret:263-282`
**Pattern:** when neither `DATADOG_WEBHOOK_SIGNING_SECRET` nor the per-tenant
`webhook_secret` is set, `resolveWebhookSecret` returns `(nil, nil)` and
`HandleWebhook` skips signature verification entirely.
**Why it bites:** any internet caller hitting
`POST /v1/integrations/datadog/webhook?org=<orgID>` can synthesise an
incident as that tenant, which becomes an `incidents_raw` row and (with
F-1 fixed) drives a recovery pipeline. The `IncidentSink` is the entire
trust boundary; a forged Datadog payload runs the agents on the
attacker's input.
**Fix:** flip the default to "fail closed" when no secret is configured.
Add a startup warning if `DATADOG_WEBHOOK_SIGNING_SECRET` is empty AND
no tenant has per-tenant secrets so operators are explicit about an
unsigned ingest path.

## High

### F-6: Goroutine leak in workspace state machine
**Where:** `internal/adapter/workspace/service.go:127,134-135`
**Pattern:** `go s.run(*inserted)` uses `context.Background()` inside
`run()` and `time.Sleep` between phases. There is no shutdown hook, no
wait group, no cancel signal — a SIGTERM during provisioning leaves the
goroutine running with no observer, and on a hard kill the workspace is
stranded mid-state.
**Why it bites:** under load every workspace creation leaks ~5s of goroutine
+ a few DB UPDATEs that no longer have anyone to log their results. Test
suites that spam Create() will accumulate them.
**Fix:** thread a service-level ctx through `Service`, derive a child ctx
per workspace, register a `sync.WaitGroup`, and on `Shutdown()` wait or
cancel.

### F-7: `signaler.go` races the workflow signal against the row update
**Where:** `internal/adapter/approval/signaler.go:104-109`
**Pattern:** Decide() sends the Temporal signal first, then records the
decision in the DB. The doc-comment acknowledges this:
> If the signal fails the row stays pending; if the UPDATE fails
> post-signal the workflow has already advanced and the next GetRun read
> will reconcile

But the workflow's `ApprovalGateFinalize` activity ALSO writes the same
row (`activities.go:888`). With the signal-first ordering, the workflow
can finish (Temporal signal lands, workflow.Finalize commits) BEFORE
`signaler.Decide`'s `RecordDecision` runs — that update then races the
workflow's terminal write. Last-write-wins is fine for `decided_by` /
`notes`, but the timestamp / decision can flip between the two paths.
**Fix:** make `Approval.RecordDecision` idempotent on `decision != pending`
and observe in tests; or do `RecordDecision` BEFORE the signal so a stale
signal still leaves a coherent row. The current order means a fast
workflow defeats the audit row.

### F-8: `eval_runner.RunSync` is wired in detached HTTP path but inserts a second eval_run
**Where:** `internal/transport/http/handler/eval.go:212-256`
**Pattern:** the handler `CreateRun()`s a row (line 220), then in a
goroutine calls `runner.RunSync()` (line 245) which `CreateRun`s ANOTHER
row inside (eval_runner.go:97). Both rows live; the comment on
eval.go:240-244 admits this is intentional but says the duplicate is
harmless.
**Why it bites:** the dashboard's `recent runs` list now has two rows per
HTTP-driven eval — one stuck at `queued` (the pre-created one), one
that actually runs. Operators looking at the list see false "queued
forever" rows.
**Fix:** add a `RunSyncWithExistingRun(ctx, runID)` entry on the
`EvalRunner` that skips the inner CreateRun. Or delete the pre-create
and accept the 200ms lag before the row materialises.

### F-9: Float64 cents in money columns
**Where:** `internal/usecase/usage_ticker.go:49`, `internal/adapter/agents/llm.go`
all token ledger Record calls, `internal/adapter/repo/billing_repo.go` everywhere.
**Pattern:** the SQL columns are `numeric(20,6)` (correct) but every
write goes through `float64`. `amount := fraction * u.PriceCents` then
`pgx` converts that float64 to the numeric column. The cumulative
rounding error over N usage ticks is small in absolute terms but is
non-zero and unauditable.
**Why it bites:** invoice totals computed as `SUM(amount_cents)::bigint`
(`AdminCloseInvoicesForPeriod`) — the floor() makes the per-row drift
disappear into the total, which means a tenant can never quite reconcile
"sum of my ticks" with "my invoice".
**Fix:** drive amount calculation through `*big.Rat` or pgx's
`pgtype.Numeric`. At minimum, treat per-tick quantity as integer
seconds (not hours-as-float) and divide once at the invoice surface.

### F-10: Slack legacy webhook bypasses the shared `httpx.Client`
**Where:** `internal/adapter/integration/slack/provider.go:81,106`
**Pattern:** `New()` and `NewWithOAuth()` both build a fresh
`&http.Client{Timeout: 10 * time.Second}` for the legacy webhook path
instead of taking the shared httpx wrapper that the other adapters use.
**Why it bites:** breaker, rate-limit, retries on 429, Authorization
redaction — none of these apply to the legacy webhook fallback. A 5xx
from Slack's incoming webhook pool will not trip the breaker, and the
URL itself (which contains the secret token) is logged at slog.Debug
without redaction.
**Fix:** wire the shared `*httpx.Client` into Slack's legacy path or drop
the legacy path entirely now that OAuth is wired (`real.go:89-102`).

### F-11: defer-in-loop pattern across multiple repos via early returns
**Where:** `internal/adapter/repo/billing_repo.go:135-145,170-181` and
`workflows_repo.go:153-186`
**Pattern:** the outer `defer rows.Close()` is fine, but `UsageSum` has
TWO sequential `pool.Query` calls in the SAME function. The first
explicitly calls `rows.Close()` before the second; the second uses
`defer rows.Close()`. If the first Query returns an error halfway through
scanning, the function returns and the first `rows.Close()` runs in the
non-defer path — that part is safe. But the pattern is fragile and the
ergonomics are inconsistent.
**Why it bites:** subtle resource leaks if a future maintainer adds a
third Query and forgets the manual close.
**Fix:** wrap the two iterators in helper functions so each owns its own
rows lifecycle.

### F-12: `_ = pool.QueryRow(...).Scan(...)` pattern silently swallows DB errors
**Where:** `internal/transport/http/handler/system_status.go:115,138,153`
**Pattern:** three SystemStatus probe queries discard the error and
return zeroed counters. A DB outage causes the system-status pill to say
"0 incidents, 0 approvals pending, 0 integrations" while every other
endpoint 500s. Operators trust the pill.
**Fix:** at minimum slog.Warn on err so the failure is visible.

## Medium

### F-13: `IsHMACError` substring match is fragile
**Where:** `internal/transport/http/handler/webhooks.go:362-373,
indexOfSub:388-395`
**Pattern:** the webhook handler decides 401 vs 500 by substring-matching
error message strings emitted by each adapter. There's a hand-rolled
`indexOfSub` instead of `strings.Contains`. Any adapter that adds a new
HMAC-failure wording (e.g. PagerDuty's `missing X-PagerDuty-Webhook-Signature`)
without updating this list returns 500 → the upstream keeps retrying.
**Fix:** introduce `var ErrSignatureMismatch = errors.New(...)` in the
shared integration package and have each adapter wrap it via `fmt.Errorf("...: %w", ErrSignatureMismatch)`,
then `errors.Is(err, ErrSignatureMismatch)` in the handler.

### F-14: `SentinelDetect` activity reads up to 50 unrelated rows to find one
**Where:** `internal/workflow/recovery/activities.go:636-647`
**Pattern:** PollFatalSince(epoch) → 50 rows → linear scan for the matching
`in.IncidentID`. Comment admits it's wasteful and promises a
`GetByID` port "in Phase 7". For org with >50 fatals in history, the
target row is OUTSIDE the window and the activity silently returns a
zeroed `domain.IncidentRow`.
**Fix:** add `IncidentsAdmin.GetByID(ctx, orgID, incidentID) (IncidentRow, error)`
and use it. Trivial — `SELECT … WHERE org_id=$1 AND id=$2`.

### F-15: GitHub webhook accepts the dev defaultSecret when no per-tenant row exists
**Where:** `internal/adapter/integration/github/provider.go:259-269`
**Pattern:** when the org has no `integrations` row (race window during
install, or admin manually deleted), the handler falls back to
`p.defaultSecret` for signature verification. In dev this is a known
fixed key; in prod the value is the boot-time
`GITHUB_DEFAULT_WEBHOOK_SECRET` env var. If unset, `defaultSecret=nil`
and `hmac.New(sha256.New, nil)` happily signs an empty string — any
sender can construct a matching signature.
**Fix:** when `len(secret) == 0` return `errors.New("no secret on file")`
explicitly so the webhook rejects with 401.

### F-16: `pgvector.New(adminPool)` runs all retrieval cross-tenant
**Where:** `cmd/server/main.go:330` →
`internal/adapter/retrieval/pgvector.go`
**Pattern:** retrieval store is constructed against admin pool. The
agent-side `RetrievalClient.ContextFor` does not pass orgID; any returned
similar-vectors leak across tenants.
**Action:** confirm the retrieval store filters by org_id internally; if
not, this is a Critical multi-tenancy leak. (I didn't read pgvector.go
in full — flagging for follow-up.)

### F-17: `db.FromCtx(ctx, r.pool)` returns the bare pool when no tx — system-status's three counter queries above are doing exactly this AND throwing the error away
**Where:** see F-12 — the queries are on `pool` not `db.FromCtx`. The
counterpart issue is that handlers further up (e.g. `agents.go:71,170`)
use the admin pool unconditionally, which means RLS is bypassed and the
WHERE `org_id=$1` clause is the only tenancy guard. If any future
maintainer drops that filter it becomes a cross-tenant leak.

### F-18: HTTP timeouts shorter than longest LLM call
**Where:** `cmd/server/main.go:480-484`
**Pattern:** `WriteTimeout: 60s`. The pipeline demo's L1 agents have a
5-minute Temporal activity timeout (`llmActivityOpts.StartToCloseTimeout`)
and the SSE handler explicitly clears the write deadline via
`rc.SetWriteDeadline(time.Time{})` — good for the streamers. But any
non-streaming POST that touches an L1 agent in-band (none today, but
shape lets it happen) will see the 60s drop the response mid-flight.
**Action:** keep WriteTimeout=60s only for non-streaming routes; the
streaming routes already opt out. Note: `cron.Run` callers also need
the long timeout — `usage_ticker.Run` uses cronCtx (background), so OK.

### F-19: TokenLedgerRepo.Record opens its own tx, ignoring caller's
**Where:** `internal/adapter/repo/token_ledger_repo.go:106-145`
**Pattern:** `Record` calls `r.adminPool.Begin(ctx)` — even if the caller
is inside a request RLS tx (the LLM call inside an activity isn't, so
this is fine today). But the pattern means `Record` cannot participate in
the caller's atomic boundary; a token ledger insert succeeds even when
the surrounding activity later fails. Token cost is over-counted.
**Action:** accept a `Querier` argument or thread `db.FromCtx` so callers
who DO have a tx can opt in.

### F-20: `IncidentTrigger.IncidentRawID` populated for fatal_level only
**Where:** `internal/sentinel/rules.go:54-57`
**Pattern:** the fatal_level rule sets `IncidentRawID: r.ID`. The
error_rate_spike rule (line 73-76) does NOT — `IncidentRawID=""` so
`router.Route` skips the `UpdateProjectID` persistence step at
`detector.go:127`. Spike triggers route in-memory but never stamp the
row.
**Action:** intentional? If yes, document. If no, fix by passing the
most-recent fatal's ID through.

### F-21: `dedupeLedger` map grows on every recordDedupe but only prunes on insert
**Where:** `internal/sentinel/multisource.go:107-113`
**Pattern:** the lazy sweep only fires when a NEW key is inserted. If
the detector goes idle (no fatal events for an hour) but has hundreds of
old entries in the ledger, they stay until the next insert. Bounded by
`(orgs × in-flight incidents)`, fine in practice, but worth a
defensive periodic sweep on the tick goroutine.

### F-22: `usage_ticker` invoiceRoller every 24h is offset-zero
**Where:** `cmd/server/main.go:507-510`
**Pattern:** the cron job runs immediately on startup, then every 24h.
If the control-plane restarts during a deploy at 23:59 UTC, two invoices
get rolled in the same 24h window (the second on the existing-row
ON CONFLICT path — harmless because of the unique key, but it does
re-run AdminCloseInvoicesForPeriod which UPDATEs `paid` invoices…
**Action:** add a `last_run_at` watermark or align to wall-clock midnight
UTC.

## Low

### F-23: `auth.go:117` mapAuthError catches `domain.ErrConflict` for signup-email-exists but the same code is used for ApprovalDecide where it means "already decided"
**Action:** different error codes (`ErrEmailExists`, `ErrAlreadyDecided`).

### F-24: Multiple handlers use `fmt.Sscanf` for query-int parsing (`audit.go:62-65`)
**Action:** `strconv.Atoi` is faster and clearer; not a bug, but worth aligning.

### F-25: `handler/agents.go:155-161` validAgentName is package-init-only — if a future agent is added to agentCatalog at runtime (it's not, today) the validator misses it
**Action:** ignore — agentCatalog is package-const by design.

### F-26: GitHub `installation_token_cache.go:90` has a custom `itoa` to "avoid one-symbol-per-file dance"
**Action:** purely cosmetic; `strconv.FormatInt(n, 10)` is one stdlib import
the package already has.

### F-27: `seed_sample.go` uses `loadFixtureIncident` from `pipelines.go` cross-file dependency
**Action:** not a bug; the dependency is documented. Worth extracting
into a `fixtures` helper file for clarity.

### F-28: `notifier.Multi.Send` runs children sequentially
**Where:** `internal/adapter/notifier/multi.go:43-55`
**Pattern:** doc says "the fanout is intentionally fire-and-forget" but a
slow Slack call blocks the next child notifier. With email + slack +
console, a 30s Slack hang blocks the email send.
**Action:** wrap children in goroutines with a single waitgroup; cap the
parallelism if needed.

### F-29: `audit/hmac_writer.go:120-128` canonicalJSON re-marshals values via `json.Marshal` per call
**Action:** hot path for high-volume audit; OK at current QPS but worth
benchmarking before rolling to 10x traffic.

### F-30: `httpx.Client` log-attempt clones the full header map on every request
**Where:** `internal/adapter/integration/internal/httpx/client.go:350-374`
**Pattern:** the clone happens regardless of slog level — the
`Enabled` check is at the top, but the redaction loop still allocates
a header map for every attempt. Wrap the whole body in the Enabled
check.

### F-31: Sentry/Datadog/PagerDuty all repeat the same `_ = json.Unmarshal(body, &payload)` pattern
**Where:** `sentry/provider.go:327,366`, `datadog/provider.go:227`,
`pagerduty/provider.go:257`
**Pattern:** when the body fails to JSON-decode, payload ends up nil and
the downstream `RawIncident.Payload` is nil. That's tolerated (the SQL
defaults payload to `'{}'`), but the silent path means a malformed body
lands as an empty audit-row.
**Action:** at least slog.Debug the error so a misconfigured upstream is
visible.

### F-32: `eval_runner.go:172` `retClient = &agents.RetrievalClient{}` on nil adminPool — the field is initialised but Store/Embed are nil, causing a panic if the agent calls `Store.QueryByVector`
**Action:** check inside `RetrievalClient.ContextFor` — if it nil-guards
the Store call, fine; otherwise this is a real panic path.

### F-33: `recovery/workflow.go:226-231` countdown_secs accepts int|int64|float64 but not `json.Number`
**Action:** payloads come from Temporal SDK history serialisation (always
encoded), so float64 is the canonical wire form. Three branches are
defensive but redundant — collapse to a single float64 cast.

### F-34: Sentinel `tick.workspace` swallows non-ErrNotFound errors with WARN
**Where:** `internal/sentinel/detector.go:189-192`
**Pattern:** non-NotFound DB errors get a WARN and `continue` — the org's
tick is skipped that round. Repeated DB errors for the same org means
that tenant's incidents pile up unbounded.
**Action:** add a circuit breaker / exponential backoff per org.

### F-35: `ApprovalGateRoute` payload includes `slack_channel_id` but the notifier doesn't read it
**Where:** `activities.go:862-867`
**Pattern:** the activity sets `payload["slack_channel_id"]` from
`Project.Selectors.SlackChannelID`, but `a.Approval.Notify(ctx, notif)`
on the line above uses the `domain.Notification` struct — and that struct
doesn't carry a per-project channel override. The payload key is
written but ignored by the notifier.
**Action:** add `Notification.SlackChannelID` field and consume it in
`notifier/slack.go`.

### F-36: All HTTP handlers JSON-encode response with `json.NewEncoder` (no pooled buffer)
**Action:** acceptable today, worth a `sync.Pool` of `*json.Encoder` if the
audit log endpoint becomes a hot path.

## TODO/FIXME census

Only one TODO in production code:

| File | Line | Comment |
|---|---|---|
| `internal/adapter/auth/local/pgstore.go` | 37 | `TODO(testing): add pgstore_test.go (build-tag integration) once …` |

No FIXMEs, no HACKs, no XXXes. The bar is high. The "missing" markers
are all in this audit instead.

## Quick wins (≤30 min each)

1. **Wire the sentinel router** (F-1) — 8 lines of code in main.go.
2. **Wire `acts.Approval`, `acts.Projects`, `acts.SlackDefaultChannel`** (F-2)
   — once the approval repo lands, ~15 lines.
3. **Fix `kind='finish'` → `status IN ('succeeded',…)`** (F-3) — 5
   substitutions in `agents.go`. Add a test that hits the endpoint
   against a populated activity_events table.
4. **Route `service.go:145` UpdateRunStatus through `ctx`** (F-4) — one
   line.
5. **Fail closed on missing Datadog signing secret** (F-5) — 4 lines.
6. **Add the `WorkspaceService.Shutdown()` waitgroup** (F-6) — ~30 lines
   plus call site in cmd/server/main.go.
7. **Replace `_ = pool.QueryRow(...).Scan(...)` with logged errors** in
   `system_status.go` (F-12) — 6 lines.
8. **Slack legacy webhook → shared `httpx.Client`** (F-10) — switch
   constructor signature, 5 lines.
9. **Add `IncidentsAdmin.GetByID`** (F-14) — 12 lines including SQL +
   port + activity call site.
10. **Reject GitHub webhooks when defaultSecret is empty** (F-15) — 3 lines.
