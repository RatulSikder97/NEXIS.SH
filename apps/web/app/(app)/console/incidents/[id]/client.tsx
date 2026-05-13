"use client";

// Phase 4 Stage 7 — Pipeline run detail client.
// Phase 6 Stage 9 — guided "Live demo" mode + approval status badge.
//
// SSE consumer + header. The server-component shell seeds us with the run
// snapshot + every persisted activity_event, so first paint is the full
// timeline as it exists in the DB. We then open the SSE stream — which the
// server replays from the DB before tailing the live broker, so duplicate
// frames are expected — and dedup by `seq`.
//
// Lifecycle:
//   • mount       → set state from initial, open EventSource via SDK
//   • onmessage   → merge frame by seq, update run status if terminal
//   • named close → SDK auto-closes the EventSource, we set runFinished
//   • unmount     → close the EventSource defensively
//
// The 1s heartbeat drives live-elapsed durations for in-flight rows.
// The run-status header tracks the latest known status — when the SSE
// closes we re-fetch the run row once to pick up the final status/duration
// (the activity events alone don't carry it).
//
// Live-demo mode (`liveMode` prop, set when ?live=1 is on the URL):
//   • A top banner says "Live demo — <scenario> at <timestamp>"; this
//     stays visible while the workflow is in-flight and dims after close.
//   • The timeline's running step shows a brighter pulse + soft halo
//     (handled inside ActivityTimeline via the `liveMode` prop).
//   • As frames arrive, the most-recent running row is scrolled into view.
//   • When the run terminates AND the approval gate fired with severity
//     `high`, we surface a callout linking to /console/approvals.
//
// Approval badge:
//   • Whenever the workflow exits with `current_step === "ApprovalGate"`
//     or one of the approval activities has run, we fetch /decision and
//     show the severity + decision pill in the header.

import * as React from "react";
import Link from "next/link";
import type { Route } from "next";
import { ChevronLeft, Loader2, ShieldCheck } from "lucide-react";

import { ActivityTimeline } from "@/components/pipelines/ActivityTimeline";
import { StatusPill } from "@/components/pipelines/StatusPill";
import { SeverityBadge } from "@/components/approvals/SeverityBadge";
import {
  AgentSummaryCard,
  ConfidencePill,
} from "@/components/incidents/AgentSummaryCard";
import { NasaTlxModal } from "@/components/incidents/NasaTlxModal";
import {
  pipelines,
  type ActivityEvent,
  type PathfinderPayload,
  type PipelineDetail,
  type SynthesiserPayload,
  type ValidatorPayload,
  type WorkflowRun,
} from "@/lib/pipelines";
import {
  approvals,
  type ApprovalDecision,
} from "@/lib/approvals";
import { nasaTlx } from "@/lib/nasa-tlx";
import { cn } from "@/lib/utils";

const TICK_MS = 1000;
const DECISION_POLL_MS = 5000;

function formatTime(iso: string | undefined): string {
  if (!iso) return "—";
  try {
    return new Date(iso).toLocaleString(undefined, {
      month: "short",
      day: "numeric",
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit",
    });
  } catch {
    return iso;
  }
}

function formatDuration(ms: number | undefined): string {
  if (typeof ms !== "number" || !Number.isFinite(ms) || ms < 0) return "—";
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)}s`;
  const m = Math.floor(ms / 60_000);
  const s = Math.round((ms % 60_000) / 1000);
  return s === 0 ? `${m}m` : `${m}m ${s}s`;
}

// scenarioFromEvents inspects the Pathfinder/Synthesiser payloads to recover
// the scenario classification. Falls back to the empty string when the
// activities haven't run yet — the live banner is intentionally generic in
// that case.
function scenarioFromEvents(events: ActivityEvent[]): string {
  for (let i = events.length - 1; i >= 0; i--) {
    const e = events[i];
    const p = e.payload;
    if (!p) continue;
    const s = (p as Record<string, unknown>).scenario;
    if (typeof s === "string" && s.length > 0) return s;
  }
  return "";
}

// titleForScenario maps the snake_case enum to a short banner-friendly name.
function titleForScenario(scenario: string): string {
  switch (scenario) {
    case "schema_drift":
      return "schema drift incident";
    case "null_deref":
      return "null dereference incident";
    case "oom":
      return "memory pressure incident";
    case "":
      return "incident";
    default:
      return `${scenario} incident`;
  }
}

// pathfinderFromEvents pulls the Pathfinder finish-frame payload and
// narrows it to the typed shape. Missing or empty fields collapse to
// undefined so the summary card renders its "—" placeholder cleanly.
function pathfinderFromEvents(
  events: ActivityEvent[],
): PathfinderPayload | undefined {
  const p = pipelines.latestFinishPayload(events, "pathfinder") as
    | (Partial<PathfinderPayload> & Record<string, unknown>)
    | undefined;
  if (!p) return undefined;
  if (typeof p.root_cause !== "string" || typeof p.confidence !== "number") {
    return undefined;
  }
  return {
    root_cause: p.root_cause,
    confidence: p.confidence,
    evidence_chain: Array.isArray(p.evidence_chain)
      ? (p.evidence_chain.filter((x) => typeof x === "string") as string[])
      : undefined,
  };
}

// synthesiserFromEvents narrows the Synthesiser finish-frame to the typed
// shape. `selected_agents` is required to surface the L1 list — we tolerate
// missing scenario / risk_score so the card can still show partials.
function synthesiserFromEvents(
  events: ActivityEvent[],
): SynthesiserPayload | undefined {
  const p = pipelines.latestFinishPayload(events, "synthesiser") as
    | (Partial<SynthesiserPayload> & Record<string, unknown>)
    | undefined;
  if (!p) return undefined;
  const selected = Array.isArray(p.selected_agents)
    ? (p.selected_agents.filter((x) => typeof x === "string") as string[])
    : [];
  return {
    selected_agents: selected,
    scenario: typeof p.scenario === "string" ? p.scenario : "",
    risk_score: typeof p.risk_score === "number" ? p.risk_score : 0,
  };
}

// validatorFromEvents narrows the Validator finish-frame to the typed
// shape. The Validator currently rides on the QA agent role on the
// timeline; we scan both roles to be resilient to either projection.
function validatorFromEvents(
  events: ActivityEvent[],
): ValidatorPayload | undefined {
  const p =
    (pipelines.latestFinishPayload(events, "validator") as
      | (Partial<ValidatorPayload> & Record<string, unknown>)
      | undefined) ??
    (pipelines.latestFinishPayload(events, "qa") as
      | (Partial<ValidatorPayload> & Record<string, unknown>)
      | undefined);
  if (!p) return undefined;
  if (typeof p.tests_passed !== "boolean") return undefined;
  const failures = Array.isArray(p.hypothesis_failures)
    ? p.hypothesis_failures.filter(
        (f): f is { input: string; counterexample: string } =>
          !!f &&
          typeof (f as { input?: unknown }).input === "string" &&
          typeof (f as { counterexample?: unknown }).counterexample === "string",
      )
    : undefined;
  return { tests_passed: p.tests_passed, hypothesis_failures: failures };
}

// DECISION_LABEL renders the approval state for the header badge.
const DECISION_LABEL: Record<string, string> = {
  pending: "Pending",
  approved: "Approved",
  rejected: "Rejected",
  auto_approved: "Auto-approved",
  timeout_rejected: "Timed out",
  approve: "Approved",
  reject: "Rejected",
  auto: "Auto-approved",
  timed_out: "Timed out",
};

export function TimelineClient({
  workspaceId,
  runId,
  initial,
  // Phase 6 Stage 9 — guided demo UI flag from `?live=1`.
  liveMode = false,
}: {
  workspaceId: string;
  runId: string;
  initial: PipelineDetail | null;
  liveMode?: boolean;
}) {
  const [run, setRun] = React.useState<WorkflowRun | null>(
    initial?.run ?? null,
  );
  const [eventsBySeq, setEventsBySeq] = React.useState<
    Record<number, ActivityEvent>
  >(() => {
    const m: Record<number, ActivityEvent> = {};
    for (const e of initial?.events ?? []) m[e.seq] = e;
    return m;
  });
  const [now, setNow] = React.useState<number>(() => Date.now());
  const [finished, setFinished] = React.useState<boolean>(() => {
    const s = initial?.run?.status;
    return s === "succeeded" || s === "failed" || s === "timed_out" || s === "cancelled";
  });
  const [streamError, setStreamError] = React.useState<string | null>(null);
  const [decision, setDecision] = React.useState<ApprovalDecision | null>(null);
  // Phase 8 — NASA-TLX modal opens once on the user's 3rd successful
  // high-severity recovery. The check is debounced via a ref so a re-
  // render of the same finished+succeeded state doesn't re-fire the
  // org-stats GET.
  const [tlxOpen, setTlxOpen] = React.useState(false);
  const tlxCheckedRef = React.useRef(false);
  // bannerStartedAt is frozen on mount so the live banner shows the user's
  // own click time even if the workflow row's started_at drifts forward.
  const bannerStartedAtRef = React.useRef<number>(Date.now());

  // 1s heartbeat for live durations.
  React.useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), TICK_MS);
    return () => window.clearInterval(id);
  }, []);

  // Open SSE on mount unless we're already terminal. Re-open if runId
  // changes (would only happen via deep-link navigation).
  React.useEffect(() => {
    if (!workspaceId || !runId) return;
    if (finished) return;
    let cancelled = false;
    const es = pipelines.events(
      workspaceId,
      runId,
      (ev) => {
        if (cancelled) return;
        setEventsBySeq((prev) => {
          if (prev[ev.seq]) return prev;
          return { ...prev, [ev.seq]: ev };
        });
      },
      () => {
        if (cancelled) return;
        setFinished(true);
        // Refresh the run row to pick up status/duration after the close.
        pipelines
          .get(workspaceId, runId)
          .then((d) => {
            if (cancelled) return;
            setRun(d.run);
            setEventsBySeq((prev) => {
              const next = { ...prev };
              for (const e of d.events) next[e.seq] = e;
              return next;
            });
          })
          .catch(() => {
            // ignore — the user can refresh manually.
          });
      },
    );
    es.onerror = () => {
      // The browser will auto-reconnect; we surface a soft hint that the
      // stream paused, then clear it once the next frame arrives.
      if (cancelled) return;
      setStreamError("Reconnecting…");
    };
    es.onopen = () => {
      if (cancelled) return;
      setStreamError(null);
    };
    return () => {
      cancelled = true;
      es.close();
    };
    // We deliberately only re-run when the addressable run changes, not
    // when `finished` flips — flipping `finished` is the signal to stop
    // listening, and re-running the effect would just re-evaluate and
    // tear down (which is what we want, hence including it).
  }, [workspaceId, runId, finished]);

  // Sort events by seq for the timeline.
  const events = React.useMemo(() => {
    const arr = Object.values(eventsBySeq);
    arr.sort((a, b) => a.seq - b.seq);
    return arr;
  }, [eventsBySeq]);

  // Approval decision polling. Once the workflow is past the L1 walk the
  // ApprovalGate activity creates a `approval_decisions` row — we want to
  // surface it on the header as soon as it exists. Poll every 5s while
  // the run is in-flight; do a final fetch once it terminates.
  React.useEffect(() => {
    if (!workspaceId || !runId) return;
    let cancelled = false;
    async function tick() {
      try {
        const d = await approvals.decision(workspaceId, runId);
        if (cancelled) return;
        setDecision(d);
      } catch {
        // swallow — non-fatal, the badge just stays empty
      }
    }
    void tick();
    if (finished) return; // final fetch above is enough
    const id = window.setInterval(tick, DECISION_POLL_MS);
    return () => {
      cancelled = true;
      window.clearInterval(id);
    };
  }, [workspaceId, runId, finished]);

  // Auto-scroll the latest running row into view in live mode. The
  // ActivityTimeline tags each <li> with data-role + data-state, so we
  // query for the most-recent `data-state="running"` row and scroll it
  // into the centre of the viewport.
  const timelineRef = React.useRef<HTMLDivElement | null>(null);
  React.useEffect(() => {
    if (!liveMode) return;
    const root = timelineRef.current;
    if (!root) return;
    const running = root.querySelectorAll<HTMLElement>(
      'li[data-state="running"]',
    );
    const target = running[running.length - 1];
    if (target) {
      target.scrollIntoView({ behavior: "smooth", block: "center" });
    }
  }, [liveMode, events]);

  const liveStatus: WorkflowRun["status"] = run?.status ?? "queued";
  const liveDuration =
    run?.duration_ms ??
    (run && (liveStatus === "running" || liveStatus === "queued")
      ? Math.max(0, now - Date.parse(run.started_at))
      : undefined);

  const scenario = React.useMemo(() => scenarioFromEvents(events), [events]);
  // Phase 6 — pull the three typed L2 payloads off the events array. Each
  // returns undefined when the activity hasn't finished yet, which the
  // summary card translates to the "—" placeholder.
  const pathfinder = React.useMemo(
    () => pathfinderFromEvents(events),
    [events],
  );
  const synthesiser = React.useMemo(
    () => synthesiserFromEvents(events),
    [events],
  );
  const validator = React.useMemo(
    () => validatorFromEvents(events),
    [events],
  );
  const decisionLabel =
    decision?.decision &&
    (DECISION_LABEL[decision.decision] ?? decision.decision);

  // Phase 8 — NASA-TLX trigger.
  //
  // Fires after a high-severity run that finished with status=succeeded.
  // We hit /v1/me/org-stats once; if the org just landed its 3rd
  // successful recovery the modal opens. The ref guard prevents the
  // effect from re-firing while the user idles on this page (the
  // dependencies legitimately stay stable past the first trigger).
  React.useEffect(() => {
    if (tlxCheckedRef.current) return;
    if (!finished) return;
    if (run?.status !== "succeeded") return;
    if (decision?.severity !== "high") return;
    tlxCheckedRef.current = true;
    let cancelled = false;
    void (async () => {
      try {
        const stats = await nasaTlx.orgStats();
        if (cancelled) return;
        if (stats.successful_recoveries_count === 3) {
          setTlxOpen(true);
        }
      } catch {
        // Non-fatal — the modal just doesn't open this time. The next
        // recovery's effect will retry. We don't surface the error
        // because the modal is opportunistic, not load-bearing.
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [finished, run?.status, decision?.severity]);
  // The approval-required callout fires only when the run is over, the
  // gate fired with `high`, and the decision is still pending — i.e. the
  // workflow timed out the human-decision wait and we want to nudge the
  // user toward /console/approvals to drive it home.
  const approvalNeeded =
    finished &&
    decision &&
    decision.severity === "high" &&
    decision.decision === "pending";

  return (
    <div className="space-y-6">
      <div>
        <Link
          href={"/console/incidents" as Route}
          className="inline-flex items-center gap-1 text-xs text-[var(--color-muted-foreground)] hover:text-[var(--color-foreground)]"
        >
          <ChevronLeft className="h-3.5 w-3.5" />
          Back to incidents
        </Link>
      </div>

      {liveMode && (
        <div
          role="status"
          className={cn(
            "flex items-center justify-between gap-3 rounded-lg border px-4 py-3 text-sm",
            finished
              ? "border-[var(--color-border)] bg-[var(--color-muted)]/40 text-[var(--color-muted-foreground)]"
              : "border-blue-500/40 bg-blue-500/10 text-blue-900 dark:text-blue-100",
          )}
        >
          <div className="flex items-center gap-2">
            <span
              className={cn(
                "inline-block h-2 w-2 rounded-full",
                finished ? "bg-[var(--color-muted-foreground)]" : "bg-blue-500 animate-pulse",
              )}
              aria-hidden
            />
            <span className="font-medium">
              Live demo — {titleForScenario(scenario)} at{" "}
              {formatTime(new Date(bannerStartedAtRef.current).toISOString())}
            </span>
          </div>
          {!finished && (
            <span className="text-xs text-blue-700 dark:text-blue-200">
              Watching the loop. SSE pinned below.
            </span>
          )}
        </div>
      )}

      <header className="flex flex-wrap items-start justify-between gap-4">
        <div className="min-w-0 space-y-1">
          <h1 className="text-2xl font-semibold">
            <span className="font-mono text-base text-[var(--color-muted-foreground)]">
              {runId.slice(0, 8)}
            </span>
            <span className="ml-2 text-[var(--color-foreground)]">
              {run?.workflow_type ?? "RecoveryPipeline"}
            </span>
          </h1>
          <div className="flex flex-wrap items-center gap-2 text-xs text-[var(--color-muted-foreground)]">
            <span>Started {formatTime(run?.started_at)}</span>
            {run?.completed_at && (
              <>
                <span aria-hidden>·</span>
                <span>Finished {formatTime(run.completed_at)}</span>
              </>
            )}
            <span aria-hidden>·</span>
            <span className="font-mono">{formatDuration(liveDuration)}</span>
          </div>
          {run?.error && (
            <p className="text-xs text-red-700 dark:text-red-300">
              {run.error}
            </p>
          )}
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {decision && (
            <span className="inline-flex items-center gap-2 rounded-md border border-[var(--color-border)] bg-[var(--color-card)] px-2.5 py-1 text-xs">
              <ShieldCheck className="h-3.5 w-3.5 text-[var(--color-muted-foreground)]" />
              <SeverityBadge severity={decision.severity} />
              {decisionLabel && (
                <span className="text-[var(--color-foreground)] font-medium">
                  {decisionLabel}
                </span>
              )}
            </span>
          )}
          <StatusPill status={liveStatus} />
          {!finished && (
            <span
              className={cn(
                "inline-flex items-center gap-1 text-xs text-[var(--color-muted-foreground)]",
              )}
            >
              <Loader2 className="h-3 w-3 animate-spin" />
              {streamError ?? "Live"}
            </span>
          )}
        </div>
      </header>

      {approvalNeeded && (
        <div className="flex items-center justify-between gap-3 rounded-lg border border-red-500/40 bg-red-500/10 px-4 py-3 text-sm">
          <div>
            <p className="font-medium text-red-900 dark:text-red-100">
              High-severity decision required.
            </p>
            <p className="mt-0.5 text-xs text-red-700 dark:text-red-200">
              The approval gate is blocked waiting for a human signal.
            </p>
          </div>
          <Link
            href={"/console/approvals" as Route}
            className="rounded-md bg-red-600 px-3 py-1.5 text-xs font-medium text-white hover:opacity-90"
          >
            Open approvals →
          </Link>
        </div>
      )}

      {/* Phase 6 — Pathfinder / Synthesiser / Validator summary row.
          Each card pulls from its agent's last finish-frame payload; the
          AgentSummaryCard collapses to a "—" placeholder until the
          activity terminates. The row is intentionally above the
          timeline so the recovered findings read as headline context
          rather than a footnote. */}
      <section
        aria-label="Phase 6 agent summary"
        className="grid grid-cols-1 gap-3 md:grid-cols-3"
      >
        <AgentSummaryCard
          role="pathfinder"
          label="Pathfinder · Root cause"
          body={
            pathfinder ? (
              <div className="space-y-2">
                <p className="font-medium text-[var(--color-foreground)] break-words">
                  {pathfinder.root_cause}
                </p>
                <ConfidencePill confidence={pathfinder.confidence} />
              </div>
            ) : null
          }
        />
        <AgentSummaryCard
          role="synthesiser"
          label="Synthesiser · Plan"
          body={
            synthesiser &&
            (synthesiser.scenario || synthesiser.selected_agents.length > 0) ? (
              <div className="space-y-2">
                {synthesiser.scenario && (
                  <p className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]">
                    {synthesiser.scenario.replace(/_/g, " ")}
                  </p>
                )}
                {synthesiser.selected_agents.length > 0 ? (
                  <ul className="flex flex-wrap gap-1.5">
                    {synthesiser.selected_agents.map((a) => (
                      <li
                        key={a}
                        className="inline-flex items-center rounded-full bg-[var(--color-muted)]/60 px-2 py-0.5 text-[11px] font-medium text-[var(--color-foreground)] ring-1 ring-[var(--color-border)]"
                      >
                        {a}
                      </li>
                    ))}
                  </ul>
                ) : (
                  <span className="text-[var(--color-muted-foreground)]">
                    No L1 agents selected
                  </span>
                )}
              </div>
            ) : null
          }
        />
        <AgentSummaryCard
          role="qa"
          label="Validator · Results"
          body={
            validator ? (
              <div className="space-y-2">
                <span
                  className={cn(
                    "inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-medium ring-1",
                    validator.tests_passed
                      ? "bg-emerald-500/15 text-emerald-700 ring-emerald-500/30 dark:text-emerald-300"
                      : "bg-red-500/15 text-red-700 ring-red-500/30 dark:text-red-300",
                  )}
                >
                  <span
                    className={cn(
                      "inline-block h-1.5 w-1.5 rounded-full",
                      validator.tests_passed ? "bg-emerald-500" : "bg-red-500",
                    )}
                    aria-hidden
                  />
                  {validator.tests_passed
                    ? "Tests passed"
                    : "Tests failed"}
                </span>
                <p className="text-xs text-[var(--color-muted-foreground)]">
                  {validator.hypothesis_failures?.length ?? 0} hypothesis{" "}
                  {(validator.hypothesis_failures?.length ?? 0) === 1
                    ? "failure"
                    : "failures"}
                </p>
              </div>
            ) : null
          }
        />
      </section>

      <section
        aria-label="Activity timeline"
        ref={timelineRef}
        className={cn(
          "rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-5",
          liveMode &&
            "max-h-[60vh] overflow-y-auto scroll-smooth ring-1 ring-blue-500/10",
        )}
      >
        <ActivityTimeline events={events} now={now} liveMode={liveMode} />
      </section>

      <NasaTlxModal
        open={tlxOpen}
        onOpenChange={setTlxOpen}
        recoveryRunId={runId}
      />
    </div>
  );
}
