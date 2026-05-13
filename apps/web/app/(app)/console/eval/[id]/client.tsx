"use client";

// Phase 5 Stage 8 — Eval detail client.
//
// Side-by-side renderer: OpenAI column on the left, Ollama on the right.
// Each column is a vertical stack of 5 transcript rows in fixed order
// (Architect → Backend → QA → DevOps → DataEngineer). Clicking a row
// expands an inline panel showing the prompt input + structured assistant
// output as pretty-printed JSON.
//
// Polling: if either provider is still in motion we refetch every 5s so
// the transcript rows backfill as the runner records them. We don't need
// SSE here — eval runs are bounded (~5 calls × 2 providers) and the
// transcripts arrive over a couple of minutes, not seconds.
//
// We deliberately re-sort transcripts by (provider, agent) on render so the
// matrix layout is deterministic regardless of how the server orders them.

import * as React from "react";
import Link from "next/link";
import type { Route } from "next";
import { ArrowLeft, ChevronDown, ChevronRight, Loader2, RefreshCw } from "lucide-react";

import { Button } from "@/components/ui/Button";
import {
  AGENTS_IN_ORDER,
  evalApi,
  formatCentsExact,
  formatTokens,
  isInFlight,
  type EvalAgent,
  type EvalDetail,
  type EvalProvider,
  type EvalRunStatus,
  type EvalTranscript,
} from "@/lib/eval";

const POLL_MS = 5000;

function formatStartedAt(iso: string): string {
  try {
    const d = new Date(iso);
    return d.toLocaleString(undefined, {
      year: "numeric",
      month: "short",
      day: "numeric",
      hour: "2-digit",
      minute: "2-digit",
    });
  } catch {
    return iso;
  }
}

function formatDuration(ms: number): string {
  if (ms <= 0) return "—";
  if (ms < 1000) return `${ms}ms`;
  const s = ms / 1000;
  if (s < 60) return `${s.toFixed(2)}s`;
  const m = Math.floor(s / 60);
  const rem = (s - m * 60).toFixed(1);
  return `${m}m ${rem}s`;
}

// StatusBadge mirrors the list page's pill but slightly smaller for the
// per-agent row label. Same colour scheme so visual matching is one-to-one.
function StatusBadge({ status }: { status: EvalRunStatus }) {
  const tone =
    status === "succeeded"
      ? "bg-green-500/15 text-green-700 ring-green-500/30 dark:text-green-300"
      : status === "failed"
        ? "bg-red-500/15 text-red-700 ring-red-500/30 dark:text-red-300"
        : status === "running"
          ? "bg-blue-500/15 text-blue-700 ring-blue-500/30 dark:text-blue-300"
          : "bg-[var(--color-muted)] text-[var(--color-muted-foreground)] ring-[var(--color-border)]";
  return (
    <span
      className={`inline-flex items-center rounded-full px-2 py-0.5 text-[10px] font-medium uppercase tracking-wide ring-1 ${tone}`}
    >
      {status}
    </span>
  );
}

// SuccessDot renders a tiny circle reflecting per-transcript `success` —
// distinct from the run-level status pill because a transcript can come
// back with success=false (schema mismatch, refused) while the overall
// run is still "running" for the rest of the agents.
function SuccessDot({ success }: { success: boolean }) {
  return (
    <span
      title={success ? "Schema-valid output" : "Schema mismatch / failed"}
      aria-label={success ? "succeeded" : "failed"}
      className={`inline-block h-2 w-2 shrink-0 rounded-full ${
        success ? "bg-green-500" : "bg-red-500"
      }`}
    />
  );
}

// transcriptKey indexes the transcript matrix by "<provider>:<agent>" so
// the row renderer can do a single O(1) lookup per (provider, agent) cell.
function transcriptKey(provider: EvalProvider, agent: EvalAgent): string {
  return `${provider}:${agent}`;
}

function buildIndex(
  transcripts: EvalTranscript[],
): Record<string, EvalTranscript> {
  const out: Record<string, EvalTranscript> = {};
  for (const t of transcripts) {
    out[transcriptKey(t.provider, t.agent)] = t;
  }
  return out;
}

// JsonBlock pretty-prints a structured value. We render `null` explicitly
// as the literal "null" rather than an empty <pre> so an absent input is
// distinguishable from "the model returned no JSON". Phase 5 ships raw
// JSON; Phase 7 will swap in Monaco with syntax highlighting per the plan.
function JsonBlock({ value }: { value: unknown }) {
  let text: string;
  try {
    text = JSON.stringify(value, null, 2);
  } catch {
    text = String(value);
  }
  if (text === undefined) text = "null";
  return (
    <pre className="overflow-x-auto rounded-md border border-[var(--color-border)] bg-[var(--color-muted)]/40 p-3 text-[11px] leading-relaxed">
      <code className="font-mono">{text}</code>
    </pre>
  );
}

// AgentRow is one cell in the side-by-side matrix. Click to expand the
// inline JSON view; the parent owns the open state so the equivalent cell
// in the other column stays independent (you usually want to expand both
// columns in lockstep, but we don't enforce it).
function AgentRow({
  transcript,
  agent,
  provider,
  expanded,
  onToggle,
}: {
  transcript: EvalTranscript | undefined;
  agent: EvalAgent;
  provider: EvalProvider;
  expanded: boolean;
  onToggle: () => void;
}) {
  if (!transcript) {
    return (
      <div className="rounded-md border border-dashed border-[var(--color-border)] px-3 py-2">
        <div className="flex items-center gap-2 text-[var(--color-muted-foreground)]">
          <Loader2 className="h-3 w-3 animate-spin" />
          <span className="text-[13px] font-medium">{agent}</span>
          <span className="text-[10px] uppercase tracking-wide">pending</span>
        </div>
      </div>
    );
  }
  return (
    <div className="rounded-md border border-[var(--color-border)] bg-[var(--color-background)]">
      <button
        type="button"
        onClick={onToggle}
        aria-expanded={expanded}
        className="flex w-full items-center gap-2 px-3 py-2 text-left hover:bg-[var(--color-muted)]/40"
      >
        {expanded ? (
          <ChevronDown className="h-3 w-3 shrink-0 text-[var(--color-muted-foreground)]" />
        ) : (
          <ChevronRight className="h-3 w-3 shrink-0 text-[var(--color-muted-foreground)]" />
        )}
        <SuccessDot success={transcript.success} />
        <span className="text-[13px] font-medium text-[var(--color-foreground)]">
          {agent}
        </span>
        <span className="ml-auto flex items-center gap-3 font-mono text-[10px] text-[var(--color-muted-foreground)]">
          <span>
            {formatTokens(transcript.tokens_in)}↑ /{" "}
            {formatTokens(transcript.tokens_out)}↓
            {provider === "openai" && transcript.cached_tokens > 0 && (
              <span className="ml-1 text-[var(--color-primary)]">
                (cache {formatTokens(transcript.cached_tokens)})
              </span>
            )}
          </span>
          <span>{formatCentsExact(transcript.cost_cents_exact)}</span>
          <span>{formatDuration(transcript.duration_ms)}</span>
        </span>
      </button>
      {expanded && (
        <div className="space-y-3 border-t border-[var(--color-border)] px-3 py-3">
          <div className="space-y-1.5">
            <p className="text-[10px] font-medium uppercase tracking-widest text-[var(--color-muted-foreground)]">
              Input
            </p>
            <JsonBlock value={transcript.input_json} />
          </div>
          <div className="space-y-1.5">
            <p className="text-[10px] font-medium uppercase tracking-widest text-[var(--color-muted-foreground)]">
              Output
            </p>
            <JsonBlock value={transcript.output_json} />
          </div>
        </div>
      )}
    </div>
  );
}

// ProviderColumn stacks the 5 agent rows with the run-level status pill at
// the top. We render every agent slot whether or not a transcript exists
// so the matrix shape is identical on both sides while the runner is
// still mid-flight.
function ProviderColumn({
  label,
  provider,
  status,
  totalCentsExact,
  totalTokensIn,
  totalTokensOut,
  durationMs,
  index,
  expanded,
  onToggle,
}: {
  label: string;
  provider: EvalProvider;
  status: EvalRunStatus;
  totalCentsExact: number;
  totalTokensIn: number;
  totalTokensOut: number;
  durationMs: number;
  index: Record<string, EvalTranscript>;
  expanded: Record<string, boolean>;
  onToggle: (key: string) => void;
}) {
  return (
    <section
      aria-label={`${label} transcripts`}
      className="rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-4"
    >
      <header className="mb-3 flex items-center justify-between gap-2">
        <div className="flex items-center gap-2">
          <h2 className="text-sm font-semibold text-[var(--color-foreground)]">
            {label}
          </h2>
          <StatusBadge status={status} />
        </div>
        <div className="text-right font-mono text-[10px] text-[var(--color-muted-foreground)]">
          <div>{formatCentsExact(totalCentsExact)}</div>
          <div>
            {formatTokens(totalTokensIn)}↑ / {formatTokens(totalTokensOut)}↓ ·{" "}
            {formatDuration(durationMs)}
          </div>
        </div>
      </header>
      <div className="space-y-2">
        {AGENTS_IN_ORDER.map((agent) => {
          const key = transcriptKey(provider, agent);
          return (
            <AgentRow
              key={key}
              transcript={index[key]}
              agent={agent}
              provider={provider}
              expanded={!!expanded[key]}
              onToggle={() => onToggle(key)}
            />
          );
        })}
      </div>
    </section>
  );
}

// CostDeltaBadge renders the optional "fun line" the spec mentions —
// surfaces which provider was cheaper (by total dollars) with a ratio.
// Hidden when either total is zero (which would be unbounded or
// uninformative — e.g. an ollama-only run with no openai cost).
function CostDeltaBadge({
  openaiCents,
  ollamaCents,
}: {
  openaiCents: number;
  ollamaCents: number;
}) {
  if (openaiCents <= 0 || ollamaCents <= 0) return null;
  const cheaper = openaiCents < ollamaCents ? "OpenAI" : "Ollama";
  const ratio = openaiCents < ollamaCents
    ? ollamaCents / openaiCents
    : openaiCents / ollamaCents;
  const cheaperCents = Math.min(openaiCents, ollamaCents);
  return (
    <span className="inline-flex items-center rounded-full bg-blue-500/10 px-2.5 py-0.5 text-[11px] font-medium text-blue-700 ring-1 ring-blue-500/30 dark:text-blue-300">
      Δ {cheaper} {formatCentsExact(cheaperCents)} · {ratio.toFixed(1)}× cheaper
    </span>
  );
}

export function EvalDetailClient({
  workspaceId,
  runId,
  initial,
}: {
  workspaceId: string;
  runId: string;
  initial: EvalDetail | null;
}) {
  const [detail, setDetail] = React.useState<EvalDetail | null>(initial);
  const [pending, setPending] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);
  const [expanded, setExpanded] = React.useState<Record<string, boolean>>({});

  // 5s poll while either leg is still in motion. The effect re-runs when
  // the in-flight signal flips so we stop polling the moment both
  // providers terminate.
  const inFlight = detail ? isInFlight(detail.run) : false;
  React.useEffect(() => {
    if (!workspaceId || !runId) return;
    if (!inFlight) return;
    const id = window.setInterval(async () => {
      try {
        const next = await evalApi.get(workspaceId, runId);
        setDetail(next);
      } catch {
        // swallow — next tick retries.
      }
    }, POLL_MS);
    return () => window.clearInterval(id);
  }, [workspaceId, runId, inFlight]);

  async function refresh() {
    if (!workspaceId || !runId) return;
    setPending(true);
    setError(null);
    try {
      const next = await evalApi.get(workspaceId, runId);
      setDetail(next);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load");
    } finally {
      setPending(false);
    }
  }

  function toggle(key: string) {
    setExpanded((prev) => ({ ...prev, [key]: !prev[key] }));
  }

  if (!detail) {
    return (
      <div className="space-y-6">
        <div className="flex items-center gap-2">
          <Button variant="outline" size="sm" asChild>
            <Link href={"/console/eval" as Route}>
              <ArrowLeft className="h-4 w-4" />
              Back
            </Link>
          </Button>
        </div>
        <div
          role="alert"
          className="rounded-md border border-red-500/30 bg-red-500/10 px-4 py-3 text-sm text-red-700 dark:text-red-300"
        >
          {error ?? "Eval run not found."}
        </div>
      </div>
    );
  }

  const { run, transcripts } = detail;
  const index = buildIndex(transcripts);
  const totalCents = run.openai_cost_cents_exact + run.ollama_cost_cents_exact;

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between gap-4">
        <div className="space-y-2">
          <div className="flex items-center gap-2">
            <Button variant="outline" size="sm" asChild>
              <Link href={"/console/eval" as Route}>
                <ArrowLeft className="h-4 w-4" />
                Back
              </Link>
            </Button>
            <CostDeltaBadge
              openaiCents={run.openai_cost_cents_exact}
              ollamaCents={run.ollama_cost_cents_exact}
            />
          </div>
          <h1 className="text-2xl font-semibold">
            <span className="font-mono">{run.scenario}</span>
          </h1>
          <p className="text-sm text-[var(--color-muted-foreground)]">
            Started {formatStartedAt(run.started_at)} ·{" "}
            <span className="font-mono">{formatCentsExact(totalCents)}</span>{" "}
            total
            {run.finished_at && (
              <> · finished {formatStartedAt(run.finished_at)}</>
            )}
          </p>
        </div>
        <Button
          variant="outline"
          size="sm"
          onClick={refresh}
          disabled={pending}
        >
          {pending ? (
            <Loader2 className="h-4 w-4 animate-spin" />
          ) : (
            <RefreshCw className="h-4 w-4" />
          )}
          Refresh
        </Button>
      </div>

      {error && (
        <div
          role="alert"
          className="rounded-md border border-red-500/30 bg-red-500/10 px-4 py-3 text-sm text-red-700 dark:text-red-300"
        >
          {error}
        </div>
      )}

      <div className="grid gap-4 lg:grid-cols-2">
        <ProviderColumn
          label="OpenAI"
          provider="openai"
          status={run.openai_status}
          totalCentsExact={run.openai_cost_cents_exact}
          totalTokensIn={run.openai_tokens_in}
          totalTokensOut={run.openai_tokens_out}
          durationMs={run.duration_openai_ms}
          index={index}
          expanded={expanded}
          onToggle={toggle}
        />
        <ProviderColumn
          label="Ollama"
          provider="ollama"
          status={run.ollama_status}
          totalCentsExact={run.ollama_cost_cents_exact}
          totalTokensIn={run.ollama_tokens_in}
          totalTokensOut={run.ollama_tokens_out}
          durationMs={run.duration_ollama_ms}
          index={index}
          expanded={expanded}
          onToggle={toggle}
        />
      </div>
    </div>
  );
}
