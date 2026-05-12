"use client";

// Phase 3 Stage 9 — Audit client.
//
// Owns: filter bar state, pagination state, refetching, the row inspector
// drawer, and the CSV export link.
//
// Filter inputs are uncontrolled-with-respect-to-fetching — typing only
// updates the visible inputs; the "Apply" button drives the actual refetch
// (we don't debounce because audit responses are small and the operator
// usually picks a precise window).
//
// The CSV link is a plain <a> with target="_blank". The browser sends the
// nexis_session cookie automatically because both pages live on the same
// host as the control-plane's cookie was minted for — when the URL is
// cross-origin (browser-facing public API host) the cookie's domain must
// allow it; in localhost dev they collide so it just works.

import * as React from "react";
import { Download, Loader2, RefreshCw } from "lucide-react";

import { Button } from "@/components/ui/Button";
import { InspectorDrawer } from "@/components/console/InspectorDrawer";
import { AuditTable } from "@/components/console/AuditTable";
import {
  audit,
  type AuditFilters,
  type AuditListResp,
  type AuditRow,
} from "@/lib/audit";

// rfcOrEmpty normalises an <input type="datetime-local"> value (which is
// in the user's local TZ as "YYYY-MM-DDTHH:MM") to an RFC3339 string in
// UTC — the format the control-plane's parseFilter expects.
function rfcOrEmpty(v: string): string {
  if (!v) return "";
  try {
    return new Date(v).toISOString();
  } catch {
    return "";
  }
}

export function AuditClient({
  initial,
  forbidden,
  pageSize,
}: {
  initial: AuditListResp;
  forbidden: boolean;
  pageSize: number;
}) {
  const [data, setData] = React.useState<AuditListResp>(initial);
  const [pending, setPending] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);

  // Form-bound (untriggered) values. The "applied" set lives in `filters`.
  const [since, setSince] = React.useState("");
  const [until, setUntil] = React.useState("");
  const [actor, setActor] = React.useState("");
  const [action, setAction] = React.useState("");

  // Applied filter set. We refetch when this or the offset changes.
  const [filters, setFilters] = React.useState<AuditFilters>({});
  const [offset, setOffset] = React.useState(0);

  const [inspectorRow, setInspectorRow] = React.useState<AuditRow | null>(null);

  async function refetch(next: AuditFilters, nextOffset: number) {
    setPending(true);
    setError(null);
    try {
      const resp = await audit.list({
        ...next,
        limit: pageSize,
        offset: nextOffset,
      });
      setData(resp);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load audit");
    } finally {
      setPending(false);
    }
  }

  function applyFilters() {
    const next: AuditFilters = {
      since: rfcOrEmpty(since),
      until: rfcOrEmpty(until),
      actor: actor.trim(),
      action: action.trim(),
    };
    setFilters(next);
    setOffset(0);
    refetch(next, 0);
  }

  function clearFilters() {
    setSince("");
    setUntil("");
    setActor("");
    setAction("");
    setFilters({});
    setOffset(0);
    refetch({}, 0);
  }

  function nextPage() {
    const newOffset = offset + pageSize;
    setOffset(newOffset);
    refetch(filters, newOffset);
  }

  function prevPage() {
    const newOffset = Math.max(0, offset - pageSize);
    setOffset(newOffset);
    refetch(filters, newOffset);
  }

  const csvHref = audit.csvUrl({ ...filters, limit: 0, offset: 0 });

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold">Audit</h1>
          <p className="mt-1 text-sm text-[var(--color-muted-foreground)]">
            Tamper-evident log of every action taken in this organization.
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            asChild
          >
            <a href={csvHref} target="_blank" rel="noreferrer">
              <Download className="h-4 w-4" />
              Export CSV
            </a>
          </Button>
        </div>
      </div>

      {forbidden && (
        <div
          role="alert"
          className="rounded-md border border-amber-500/30 bg-amber-500/10 px-4 py-3 text-sm text-amber-700 dark:text-amber-300"
        >
          You don&apos;t have permission to view the audit log. Ask an owner
          or admin for access.
        </div>
      )}

      <section
        aria-label="Filters"
        className="rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-4"
      >
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          <div className="space-y-1">
            <label
              htmlFor="audit-since"
              className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]"
            >
              Since
            </label>
            <input
              id="audit-since"
              type="datetime-local"
              value={since}
              onChange={(e) => setSince(e.target.value)}
              className="w-full rounded-md border border-[var(--color-border)] bg-[var(--color-background)] px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-ring)]"
            />
          </div>
          <div className="space-y-1">
            <label
              htmlFor="audit-until"
              className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]"
            >
              Until
            </label>
            <input
              id="audit-until"
              type="datetime-local"
              value={until}
              onChange={(e) => setUntil(e.target.value)}
              className="w-full rounded-md border border-[var(--color-border)] bg-[var(--color-background)] px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-ring)]"
            />
          </div>
          <div className="space-y-1">
            <label
              htmlFor="audit-actor"
              className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]"
            >
              Actor
            </label>
            <input
              id="audit-actor"
              type="text"
              value={actor}
              onChange={(e) => setActor(e.target.value)}
              placeholder="user id or email"
              className="w-full rounded-md border border-[var(--color-border)] bg-[var(--color-background)] px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-ring)]"
            />
          </div>
          <div className="space-y-1">
            <label
              htmlFor="audit-action"
              className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]"
            >
              Action
            </label>
            <input
              id="audit-action"
              type="text"
              value={action}
              onChange={(e) => setAction(e.target.value)}
              placeholder="e.g. user.login.succeeded"
              className="w-full rounded-md border border-[var(--color-border)] bg-[var(--color-background)] px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-ring)]"
            />
          </div>
        </div>
        <div className="mt-3 flex items-center justify-end gap-2">
          <Button
            variant="ghost"
            size="sm"
            onClick={clearFilters}
            disabled={pending}
          >
            Clear
          </Button>
          <Button size="sm" onClick={applyFilters} disabled={pending}>
            {pending ? <Loader2 className="h-4 w-4 animate-spin" /> : <RefreshCw className="h-4 w-4" />}
            Apply
          </Button>
        </div>
      </section>

      {error && (
        <div
          role="alert"
          className="rounded-md border border-red-500/30 bg-red-500/10 px-4 py-3 text-sm text-red-700 dark:text-red-300"
        >
          {error}
        </div>
      )}

      <AuditTable rows={data.rows} onInspect={setInspectorRow} />

      <div className="flex items-center justify-between text-xs text-[var(--color-muted-foreground)]">
        <span>
          {data.total.toLocaleString()} total · showing{" "}
          {data.rows.length === 0 ? 0 : offset + 1}–
          {offset + data.rows.length}
        </span>
        <div className="flex items-center gap-2">
          <Button
            size="sm"
            variant="outline"
            disabled={pending || offset === 0}
            onClick={prevPage}
          >
            Previous
          </Button>
          <Button
            size="sm"
            variant="outline"
            disabled={pending || offset + data.rows.length >= data.total}
            onClick={nextPage}
          >
            Next
          </Button>
        </div>
      </div>

      <InspectorDrawer
        open={inspectorRow !== null}
        onOpenChange={(open) => {
          if (!open) setInspectorRow(null);
        }}
        title="Audit event"
        description={
          inspectorRow ? (
            <span className="font-mono text-xs">{inspectorRow.id}</span>
          ) : undefined
        }
      >
        {inspectorRow && (
          <pre className="overflow-x-auto rounded-md bg-[var(--color-muted)]/40 p-3 text-xs">
            {JSON.stringify(inspectorRow, null, 2)}
          </pre>
        )}
      </InspectorDrawer>
    </div>
  );
}
