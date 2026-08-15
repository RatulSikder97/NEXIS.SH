# Client demo — break it yourself, watch it heal

Nothing here is scripted inside NEXIS. You inject a real fault from your own
terminal; the platform detects it on its own timer and drives the repair. The
only human step is the approval you were always meant to make.

---

## One-time setup (5 minutes, do this before the client arrives)

1. **Sign in** at https://nexis.sh and open **Integrations**.
2. On the **Incident intake** card click **Configure** → **Connect**.
   Copy the signing secret it shows — it is displayed once.
3. In your terminal:

   ```bash
   export NEXIS_URL=https://nexis.sh
   export NEXIS_ORG_ID=<the org id on the intake card>
   export NEXIS_WEBHOOK_SECRET=<the secret you just copied>
   ```

4. Dry run it once (`./scripts/break-it.sh`) and let a run finish so you know
   the timing on the day. Approve or reject it — either is fine.

**Check before you present:** the org needs a codegraph, or Pathfinder has
nothing to walk. Once per org:

```bash
ssh -i secrets/AI_TEST_Z.pem ubuntu@65.0.31.84
cd /opt/nexis && F="-f docker-compose.yml -f docker-compose.prod.yml"
docker compose $F run --rm --no-deps -v /opt/nexis/services/validator/fixtures:/fixtures:ro \
  --entrypoint /app/seed-neo4j control-plane --org-id <ORG> --fixtures /fixtures
docker compose $F run --rm --no-deps -v /opt/nexis/services/validator/fixtures:/fixtures:ro \
  --entrypoint /app/seed-pgvector control-plane --org-id <ORG> --fixture-dir /fixtures \
  --repo-sha fixture-seed-001
```

---

## The demo (about 4 minutes)

**Open two things:** https://nexis.sh/console/incidents on screen, and your
terminal beside it.

### 1. Break something (15 seconds)

```bash
./scripts/break-it.sh division-by-zero
```

Say what you just did: *"That's my checkout service reporting a
ZeroDivisionError — the same signed webhook any of our services would use.
Nobody has touched NEXIS."*

Other presets: `null-deref`, `pool-exhausted`, `kafka-lag`, or your own with
`--title/--service/--stack`.

### 2. Sentinel picks it up on its own (within ~10 seconds)

The incident appears without a refresh. Nobody clicked anything — the detector
polls every 10 seconds and fires on a fatal row.

### 3. The fleet works (about 2 minutes)

Open the incident and narrate the timeline as it fills in:

| Agent | What to point at |
|---|---|
| **Pathfinder** | the root-cause symbol and file:line it resolved from the code graph — not a keyword match |
| **Synthesiser** | which agents it selected, and that DevOps / Data Engineer are *skipped* because this fault doesn't need them |
| **Architect** | the plan, and the `affected_files` contract it commits to |
| **Backend** | real token counts, and the unified diff it wrote |
| **QA** | the property tests generated for that diff |
| **Validator** | `patch applied, 12 tests passed` — the sandbox verdict, on its own timeline row |

Then the line that lands: **the patch was executed in a locked-down container
before you ever saw it** — no network, read-only filesystem, all capabilities
dropped. The Validator row states two separate facts: the patch *applied*
cleanly, and the suite passed. If a patch will not apply, that row goes red and
says why, in git's own words. Open **/console/validator** to show the sandbox
run list if they want to dig.

### 4. Your decision (30 seconds)

The run stops at the Approval Gate at HIGH severity. Open
**/console/approvals**, show the diff, and choose:

- **Approve** → GitOps opens a real pull request (needs the GitHub App
  connected — see below).
- **Modify** → edit the diff first; your edit is persisted as RLHF training
  data.
- **Reject** → nothing ships.
- **Do nothing** → it times out and refuses to deploy. Worth doing once on
  purpose: *"the default is refusal."*

---

## What to say if they ask

**"Is this pre-recorded?"** Hand them the keyboard. The script takes
`--title`, so they can invent the error text themselves; the fault is signed
with your secret and lands through the same public endpoint.

**"Would this work on our stack?"** The intake is a signed POST — any language,
any framework, four lines in an exception handler. Sentry, Datadog and
PagerDuty webhooks feed the identical path.

**"What does it cost?"** /console/cost shows the real token ledger for every
run, per agent.

---

## Known limits — say these before they find them

- **The pull request needs the GitHub App connected**, and the project needs a
  repo bound on its Integrations tab. Without it GitOps fails at the deploy
  step and the run still completes.
- **Pathfinder ranks with a documented proxy**, not a fitted causal model. If
  they ask, the confidence breakdown is per-component and visible.
- **One live LLM provider.** Runs take 1–3 minutes depending on the model.
- **The model can still write a wrong fix.** It cannot write an *unapplyable*
  one — patches are computed from whole-file rewrites and the sandbox proves
  `git apply` accepts them — but "applies cleanly and passes the tests" is not
  "correct". That judgement is the Approval Gate's, i.e. yours. Say this
  plainly; it is the honest version of the claim and it is stronger than
  overselling.

## If something goes wrong mid-demo

| Symptom | Cause | Fix |
|---|---|---|
| `✗ signature rejected (401)` | `NEXIS_WEBHOOK_SECRET` doesn't match | reconnect the intake, re-copy the secret |
| `✗ no intake for this org (404)` | intake not connected for that org | Integrations → Incident intake → Connect |
| Incident appears, no run starts | org has no default workspace | create one in the console |
| Agents show "degraded (stub fallback)" | the model returned an empty structure | re-run; if it repeats, check `/v1/_diag/llm` |
| Pathfinder finds no root cause | codegraph not seeded for that org | run the seed commands above |
| Validator row red: "patch did not apply" | the diff genuinely does not fit the tree | expected behaviour — the gate is refusing an unvalidated patch. Re-run; if it repeats for one scenario, the pgvector index is stale for that repo_sha |
| Deploy now: "project has no github installation bound" | project row predates installation-id inheritance | rebind the repo on the project's Integrations tab, or backfill `projects.github_installation_id` from `integrations.installation_id` |
