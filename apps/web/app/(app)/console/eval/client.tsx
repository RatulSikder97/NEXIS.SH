"use client";

// Phase 5 Stage 8 — Eval list client.
//
// Owns: the runs array, a 5s polling refetch while any row is still in
// motion (openai_status or ollama_status in queued|running), and the
// "Run new eval" dialog state.
//
// Polling cadence: 5s. We only poll while at least one row has either
// provider still queued/running — once both legs of every row terminate
// the interval tears down and the table is frozen. This matches the
// incidents page's pattern exactly.
//
// The dialog whitelists three scenarios (schema-drift, null-deref, oom)
// to match the backend's eval catalog; anything off-list would be a 400
// from the runner. We don't try to discover the catalog from the API in
// Phase 5 — Phase 7 can expose GET /v1/eval/scenarios if the list grows.

import * as React from "react";
import Link from "next/link";
import type { Route } from "next";
import { Beaker, Loader2, PlayCircle, RefreshCw } from "lucide-react";

import { Button } from "@/components/ui/Button";
import { EmptyState } from "@/components/empty-state/EmptyState";
import { InspectorDrawer } from "@/components/console/InspectorDrawer";
import {
  evalApi,
  formatCentsExact,
  formatTokens,
  isInFlight,
  SCENARIOS,
  type EvalRunStatus,
  type EvalRunSummary,
} from "@/lib/eval";

const POLL_MS = 5000;

function hasInFlight(rows: EvalRunSummary[]): boolean {
  return rows.some(isInFlight);
}

// StatusPill renders a provider's status as a small coloured chip. Colour
// scheme matches the incidents table (green=succeeded, red=failed,
// blue=running, neutral=queued) so the eval surface feels consistent.
function StatusPill({ status }: { status: EvalRunStatus }) {
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

// ProviderCell stacks the status pill above a compact "12.4k in / 3.2k out"
// summary so each row makes the per-provider cost legible at a glance.
function ProviderCell({
  status,
  tokensIn,
  tokensOut,
}: {
  status: EvalRunStatus;
  tokensIn: number;
  tokensOut: number;
}) {
  return (
    <div className="flex flex-col items-start gap-1">
      <StatusPill status={status} />
      <span className="font-mono text-[11px] text-[var(--color-muted-foreground)]">
        {formatTokens(tokensIn)} in / {formatTokens(tokensOut)} out
      </span>
    </div>
  );
}

// formatStartedAt collapses a server-emitted RFC3339 timestamp to a stable
// localised "MMM d, HH:mm" rendering. We avoid relative time here because
// the list is a historical record — knowing "Oct 14, 09:12" is more useful
// than "3 days ago" when you're cross-referencing with a deploy log.
function formatStartedAt(iso: string): string {
  try {
    const d = new Date(iso);
    return d.toLocaleString(undefined, {
      month: "short",
      day: "numeric",
      hour: "2-digit",
      minute: "2-digit",
    });
  } catch {
    return iso;
  }
}

// formatDeltaDuration renders a millisecond duration as "1m 23s" or
// "823ms" — used to compare OpenAI vs Ollama runtimes per row.
function formatDuration(ms: number): string {
  if (ms <= 0) return "—";
  if (ms < 1000) return `${ms}ms`;
  const s = ms / 1000;
  if (s < 60) return `${s.toFixed(1)}s`;
  const m = Math.floor(s / 60);
  const rem = Math.round(s - m * 60);
  return `${m}m ${rem}s`;
}

// NewEvalDialog mounts the scenario picker as a right-anchored drawer.
// Reusing InspectorDrawer avoids importing another Radix primitive; the
// drawer is wide enough for a 3-button radio group + Submit.
function NewEvalDialog({
  open,
  onOpenChange,
  workspaceId,
  onCreated,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  workspaceId: string;
  onCreated: (runId: string) => void;
}) {
  const [scenario, setScenario] = React.useState<string>(SCENARIOS[0]);
  const [pending, setPending] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);

  // Reset the form whenever the dialog opens so a previously-submitted
  // scenario doesn't ghost into the next attempt.
  React.useEffect(() => {
    if (open) {
      setScenario(SCENARIOS[0]);
      setError(null);
      setPending(false);
    }
  }, [open]);

  async function submit() {
    if (!workspaceId) return;
    setPending(true);
    setError(null);
    try {
      const { run_id } = await evalApi.create(workspaceId, scenario);
      onCreated(run_id);
      onOpenChange(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to start eval");
      setPending(false);
    }
  }

  return (
    <InspectorDrawer
      open={open}
      onOpenChange={onOpenChange}
      title="Run a new eval"
      description="Compare OpenAI and Ollama on the same incident."
      footer={
        <div className="flex items-center justify-end gap-2">
          <Button
            variant="ghost"
            size="sm"
            onClick={() => onOpenChange(false)}
            disabled={pending}
          >
            Cancel
          </Button>
          <Button size="sm" onClick={submit} disabled={pending || !workspaceId}>
            {pending ? (
              <Loader2 className="h-4 w-4 animate-spin" />
            ) : (
              <PlayCircle className="h-4 w-4" />
            )}
            Start eval
          </Button>
        </div>
      }
    >
      <div className="space-y-4">
        <fieldset className="space-y-2">
          <legend className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]">
            Scenario
          </legend>
          <div className="space-y-2">
            {SCENARIOS.map((s) => (
              <label
                key={s}
                className="flex cursor-pointer items-center gap-3 rounded-md border border-[var(--color-border)] bg-[var(--color-background)] px-3 py-2 text-sm hover:bg-[var(--color-muted)]"
              >
                <input
                  type="radio"
                  name="scenario"
                  value={s}
                  checked={scenario === s}
                  onChange={() => setScenario(s)}
                  className="h-4 w-4 accent-[var(--color-primary)]"
                />
                <span className="font-mono">{s}</span>
              </label>
            ))}
          </div>
        </fieldset>
        {error && (
          <div
            role="alert"
            className="rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-sm text-red-700 dark:text-red-300"
          >
            {error}
          </div>
        )}
      </div>
    </InspectorDrawer>
  );
}

export function EvalClient({
  workspaceId,
  initial,
}: {
  workspaceId: string;
  initial: EvalRunSummary[];
}) {
  const [rows, setRows] = React.useState<EvalRunSummary[]>(initial);
  const [pending, setPending] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);
  const [dialogOpen, setDialogOpen] = React.useState(false);

  // 5s poll while any row is in motion. The effect re-runs when the
  // in-flight signal flips, so once everything terminates the interval
  // is torn down and we go idle — matches the incidents page pattern.
  const inFlight = hasInFlight(rows);
  React.useEffect(() => {
    if (!workspaceId) return;
    if (!inFlight) return;
    const id = window.setInterval(async () => {
      try {
        const next = await evalApi.list(workspaceId);
        setRows(next);
      } catch {
        // swallow — next tick retries.
      }
    }, POLL_MS);
    return () => window.clearInterval(id);
  }, [workspaceId, inFlight]);

  async function refresh() {
    if (!workspaceId) return;
    setPending(true);
    setError(null);
    try {
      const next = await evalApi.list(workspaceId);
      setRows(next);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load");
    } finally {
      setPending(false);
    }
  }

  function onCreated(runId: string) {
    // Optimistically add a queued row at the top so the operator gets
    // immediate feedback; the 5s poll will replace it with the real
    // server-side row (matching id) within a few seconds.
    const optimistic: EvalRunSummary = {
      id: runId,
      scenario: "(pending)",
      openai_status: "queued",
      ollama_status: "queued",
      openai_cost_cents_exact: 0,
      ollama_cost_cents_exact: 0,
      openai_tokens_in: 0,
      openai_tokens_out: 0,
      ollama_tokens_in: 0,
      ollama_tokens_out: 0,
      duration_openai_ms: 0,
      duration_ollama_ms: 0,
      started_at: new Date().toISOString(),
    };
    setRows((prev) => [optimistic, ...prev]);
    // Force-refresh so the optimistic row gets replaced with the real
    // server-side scenario label as soon as the runner persists it.
    void refresh();
  }

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold">Eval</h1>
          <p className="mt-1 text-sm text-[var(--color-muted-foreground)]">
            Side-by-side comparison of OpenAI and Ollama on the same incident.
            Drill into a row to see per-agent transcripts.
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={refresh}
            disabled={pending || !workspaceId}
          >
            {pending ? (
              <Loader2 className="h-4 w-4 animate-spin" />
            ) : (
              <RefreshCw className="h-4 w-4" />
            )}
            Refresh
          </Button>
          <Button
            size="sm"
            onClick={() => setDialogOpen(true)}
            disabled={!workspaceId}
          >
            <PlayCircle className="h-4 w-4" />
            Run new eval
          </Button>
        </div>
      </div>

      {error && (
        <div
          role="alert"
          className="rounded-md border border-red-500/30 bg-red-500/10 px-4 py-3 text-sm text-red-700 dark:text-red-300"
        >
          {error}
        </div>
      )}

      {!workspaceId && (
        <div
          role="status"
          className="rounded-md border border-amber-500/30 bg-amber-500/10 px-4 py-3 text-sm text-amber-700 dark:text-amber-300"
        >
          No workspace selected. Pick one in the sidebar to view eval runs.
        </div>
      )}

      {rows.length === 0 ? (
        <EmptyState
          icon={Beaker}
          title="No eval runs yet"
          description="Trigger a scenario to compare OpenAI and Ollama side-by-side on the recovery loop."
          cta={
            <Button
              size="sm"
              onClick={() => setDialogOpen(true)}
              disabled={!workspaceId}
            >
              <PlayCircle className="h-4 w-4" />
              Run new eval
            </Button>
          }
        />
      ) : (
        <div className="overflow-hidden rounded-lg border border-[var(--color-border)] bg-[var(--color-card)]">
          <table className="w-full text-sm">
            <thead className="border-b border-[var(--color-border)] bg-[var(--color-muted)]/40">
              <tr className="text-left text-[10px] uppercase tracking-widest text-[var(--color-muted-foreground)]">
                <th className="px-4 py-2 font-medium">Started</th>
                <th className="px-4 py-2 font-medium">Scenario</th>
                <th className="px-4 py-2 font-medium">OpenAI</th>
                <th className="px-4 py-2 font-medium">Ollama</th>
                <th className="px-4 py-2 font-medium">Cost</th>
                <th className="px-4 py-2 font-medium">Δ Duration</th>
                <th className="px-4 py-2 font-medium text-right">Open</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((r) => {
                const totalCents =
                  r.openai_cost_cents_exact + r.ollama_cost_cents_exact;
                const deltaMs = Math.abs(
                  r.duration_openai_ms - r.duration_ollama_ms,
                );
                const faster =
                  r.duration_openai_ms > 0 && r.duration_ollama_ms > 0
                    ? r.duration_openai_ms < r.duration_ollama_ms
                      ? "OpenAI"
                      : "Ollama"
                    : null;
                return (
                  <tr
                    key={r.id}
                    className="border-b border-[var(--color-border)] last:border-0 hover:bg-[var(--color-muted)]/30"
                  >
                    <td className="px-4 py-3 align-top font-mono text-xs text-[var(--color-muted-foreground)]">
                      {formatStartedAt(r.started_at)}
                    </td>
                    <td className="px-4 py-3 align-top">
                      <span className="font-mono text-[13px] text-[var(--color-foreground)]">
                        {r.scenario}
                      </span>
                    </td>
                    <td className="px-4 py-3 align-top">
                      <ProviderCell
                        status={r.openai_status}
                        tokensIn={r.openai_tokens_in}
                        tokensOut={r.openai_tokens_out}
                      />
                    </td>
                    <td className="px-4 py-3 align-top">
                      <ProviderCell
                        status={r.ollama_status}
                        tokensIn={r.ollama_tokens_in}
                        tokensOut={r.ollama_tokens_out}
                      />
                    </td>
                    <td className="px-4 py-3 align-top">
                      <div className="flex flex-col gap-0.5">
                        <span className="font-mono text-[13px] text-[var(--color-foreground)]">
                          {formatCentsExact(totalCents)}
                        </span>
                        <span className="font-mono text-[10px] text-[var(--color-muted-foreground)]">
                          oai {formatCentsExact(r.openai_cost_cents_exact)} · oll{" "}
                          {formatCentsExact(r.ollama_cost_cents_exact)}
                        </span>
                      </div>
                    </td>
                    <td className="px-4 py-3 align-top">
                      {faster ? (
                        <div className="flex flex-col gap-0.5">
                          <span className="font-mono text-[13px] text-[var(--color-foreground)]">
                            {formatDuration(deltaMs)}
                          </span>
                          <span className="text-[10px] text-[var(--color-muted-foreground)]">
                            {faster} faster
                          </span>
                        </div>
                      ) : (
                        <span className="font-mono text-xs text-[var(--color-muted-foreground)]">
                          —
                        </span>
                      )}
                    </td>
                    <td className="px-4 py-3 text-right align-top">
                      <Button variant="outline" size="sm" asChild>
                        {/* typedRoutes wants Route here; the dynamic
                            segment forces a cast like the incidents page. */}
                        <Link href={`/console/eval/${r.id}` as Route}>Open</Link>
                      </Button>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}

      <NewEvalDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        workspaceId={workspaceId}
        onCreated={onCreated}
      />
    </div>
  );
}
