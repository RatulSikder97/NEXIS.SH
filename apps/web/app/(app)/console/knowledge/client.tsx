"use client";

// KnowledgeClient — pgvector retrieval status, per workspace.

import * as React from "react";
import { AlertOctagon, BookOpen, Loader2, RefreshCw } from "lucide-react";

import { cn } from "@/lib/utils";
import { EmptyState } from "@/components/empty-state/EmptyState";
import {
  OperationalSegments,
  type OperationalSegment,
  type SegmentStatus,
} from "@/components/console/OperationalSegments";
import { formatDurationMs, formatRelative } from "@/lib/agents-format";

const API = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

export type KnowledgeWorkspace = {
  workspace_id: string;
  workspace_name?: string;
  chunks_indexed: number;
  last_indexed_at?: string;
  last_query_latency_ms?: number;
  index_status?: "ready" | "degraded" | "indexing" | "error";
  error?: string;
};

type KnowledgeResp = {
  workspaces: KnowledgeWorkspace[];
  total_chunks?: number;
};

async function reindex(workspaceId: string): Promise<{
  ok: boolean;
  error?: string;
}> {
  try {
    const r = await fetch(
      `${API}/v1/workspaces/${workspaceId}/knowledge/reindex`,
      {
        method: "POST",
        credentials: "include",
      },
    );
    if (!r.ok) {
      const body = (await r.json().catch(() => ({}))) as { error?: string };
      return { ok: false, error: body.error ?? r.statusText };
    }
    return { ok: true };
  } catch (err) {
    return {
      ok: false,
      error: err instanceof Error ? err.message : "reindex failed",
    };
  }
}

function StatusBadge({ status }: { status: KnowledgeWorkspace["index_status"] }) {
  const s = status ?? "ready";
  const cls: Record<NonNullable<KnowledgeWorkspace["index_status"]>, string> = {
    ready:
      "bg-emerald-500/15 text-emerald-700 ring-emerald-500/30 dark:text-emerald-300",
    degraded:
      "bg-amber-500/15 text-amber-700 ring-amber-500/30 dark:text-amber-300",
    indexing: "bg-blue-500/15 text-blue-700 ring-blue-500/30 dark:text-blue-300",
    error: "bg-red-500/15 text-red-700 ring-red-500/30 dark:text-red-300",
  };
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[10px] font-medium uppercase tracking-widest ring-1",
        cls[s],
      )}
    >
      {s === "indexing" && (
        <Loader2 className="h-2.5 w-2.5 animate-spin" aria-hidden />
      )}
      {s}
    </span>
  );
}

function WorkspaceCard({
  row,
}: {
  row: KnowledgeWorkspace;
}) {
  const [pending, setPending] = React.useState(false);
  const [msg, setMsg] = React.useState<{
    tone: "ok" | "err";
    text: string;
  } | null>(null);

  async function onReindex() {
    setPending(true);
    setMsg(null);
    const result = await reindex(row.workspace_id);
    setPending(false);
    if (result.ok) setMsg({ tone: "ok", text: "Reindex started." });
    else setMsg({ tone: "err", text: result.error ?? "Reindex failed." });
  }

  return (
    <div className="rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-5">
      <div className="flex items-start justify-between gap-3">
        <div>
          <p className="text-sm font-semibold text-[var(--color-foreground)]">
            {row.workspace_name ?? row.workspace_id.slice(0, 8)}
          </p>
          <p className="font-mono text-[10px] text-[var(--color-muted-foreground)]">
            {row.workspace_id}
          </p>
        </div>
        <StatusBadge status={row.index_status} />
      </div>
      <dl className="mt-3 grid grid-cols-2 gap-2 text-[11px]">
        <div>
          <dt className="text-[var(--color-muted-foreground)]">Chunks</dt>
          <dd className="font-mono text-[var(--color-foreground)]">
            {row.chunks_indexed.toLocaleString()}
          </dd>
        </div>
        <div>
          <dt className="text-[var(--color-muted-foreground)]">Last index</dt>
          <dd className="font-mono text-[var(--color-foreground)]">
            {formatRelative(row.last_indexed_at)}
          </dd>
        </div>
        <div>
          <dt className="text-[var(--color-muted-foreground)]">Query latency</dt>
          <dd className="font-mono text-[var(--color-foreground)]">
            {typeof row.last_query_latency_ms === "number"
              ? formatDurationMs(row.last_query_latency_ms)
              : "—"}
          </dd>
        </div>
        {row.error && (
          <div className="col-span-2">
            <dt className="text-[var(--color-muted-foreground)]">Error</dt>
            <dd className="truncate font-mono text-red-700 dark:text-red-300">
              {row.error}
            </dd>
          </div>
        )}
      </dl>
      <div className="mt-3 flex items-center gap-2">
        <button
          type="button"
          onClick={onReindex}
          disabled={pending}
          className="inline-flex items-center gap-1.5 rounded-md border border-[var(--color-border)] bg-[var(--color-card)] px-3 py-1.5 text-xs font-medium text-[var(--color-foreground)] transition-colors hover:border-[var(--color-primary)]/40 hover:bg-[var(--color-muted)] disabled:opacity-50"
        >
          {pending ? (
            <Loader2 className="h-3 w-3 animate-spin" />
          ) : (
            <RefreshCw className="h-3 w-3" />
          )}
          Reindex
        </button>
        {msg && (
          <span
            className={cn(
              "text-[11px]",
              msg.tone === "ok"
                ? "text-emerald-700 dark:text-emerald-300"
                : "text-red-700 dark:text-red-300",
            )}
          >
            {msg.text}
          </span>
        )}
      </div>
    </div>
  );
}

function segmentsFor(rows: KnowledgeWorkspace[]): OperationalSegment[] {
  return rows.map((r) => {
    const status: SegmentStatus =
      r.index_status === "ready"
        ? "succeeded"
        : r.index_status === "indexing"
          ? "running"
          : r.index_status === "degraded"
            ? "running"
            : r.index_status === "error"
              ? "failed"
              : "pending";
    return {
      label: r.workspace_name ?? r.workspace_id.slice(0, 8),
      started_at: r.last_indexed_at ?? new Date().toISOString(),
      finished_at: r.last_indexed_at,
      duration_ms: r.last_query_latency_ms,
      status,
      detail:
        r.error ??
        `${r.chunks_indexed.toLocaleString()} chunks indexed · status ${r.index_status ?? "ready"}`,
    };
  });
}

export function KnowledgeClient({
  initial,
}: {
  initial: KnowledgeResp | null;
}) {
  if (initial === null) {
    return (
      <div className="space-y-6">
        <div className="space-y-1">
          <p className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]">
            Operations
          </p>
          <h1 className="text-2xl font-semibold flex items-center gap-2">
            <BookOpen className="h-5 w-5 text-[var(--color-primary)]" />
            Knowledge base
          </h1>
          <p className="max-w-2xl text-sm text-[var(--color-muted-foreground)]">
            Retrieval health for the pgvector indexes that power Pathfinder
            and the L1 agents.
          </p>
        </div>
        <EmptyState
          icon={AlertOctagon}
          title="Knowledge status unavailable"
          description="The control-plane endpoint /v1/knowledge/status isn't online yet. Retrieval stats will appear here once the backend ships this surface."
        />
      </div>
    );
  }

  const rows = initial.workspaces ?? [];

  return (
    <div className="space-y-6">
      <div className="space-y-1">
        <p className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]">
          Operations
        </p>
        <h1 className="text-2xl font-semibold flex items-center gap-2">
          <BookOpen className="h-5 w-5 text-[var(--color-primary)]" />
          Knowledge base
        </h1>
        <p className="max-w-2xl text-sm text-[var(--color-muted-foreground)]">
          {typeof initial.total_chunks === "number"
            ? `${initial.total_chunks.toLocaleString()} chunks indexed across ${rows.length} workspace${rows.length === 1 ? "" : "s"}.`
            : `Retrieval health across ${rows.length} workspace${rows.length === 1 ? "" : "s"}.`}
        </p>
      </div>

      {rows.length === 0 ? (
        <EmptyState
          icon={BookOpen}
          title="No indexes yet"
          description="Workspaces show up here as soon as the indexer fires on them. Ingest a repo or knowledge source to populate."
        />
      ) : (
        <section className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
          {rows.map((r) => (
            <WorkspaceCard key={r.workspace_id} row={r} />
          ))}
        </section>
      )}

      <OperationalSegments
        title="Operational segments"
        description="One segment per workspace index."
        segments={segmentsFor(rows)}
        emptyMessage="No indexes to segment yet."
      />
    </div>
  );
}
