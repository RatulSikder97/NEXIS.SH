"use client";

// Phase 3 Stage 9 — Audit table.
//
// TanStack Table v8 headless. Pagination is server-driven (the page lifts
// limit/offset state and refetches); the table itself only renders rows.
//
// Columns:
//   * Timestamp — formatted local datetime.
//   * Actor — first 8 chars of the actor uuid, monospace.
//   * Action — verb (e.g. "user.login.succeeded").
//   * Target — id or string the action acted on (truncated).
//   * View — opens the InspectorDrawer with the full row JSON.

import * as React from "react";
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
  type ColumnDef,
} from "@tanstack/react-table";
import { Eye } from "lucide-react";

import { Button } from "@/components/ui/Button";
import type { AuditRow } from "@/lib/audit";

function formatTime(iso: string): string {
  try {
    return new Date(iso).toLocaleString(undefined, {
      year: "numeric",
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

function truncate(s: string, n: number): string {
  if (!s) return "—";
  return s.length > n ? s.slice(0, n) + "…" : s;
}

// TanStack Table v8's `useReactTable` returns functions whose identities
// the React Compiler can't memoize without risking stale UI (the table's
// internal cache outlives a single render). The official escape hatch is
// the `"use no memo"` directive, which opts this component out of compiler
// memoization. The compiler still emits an informational
// `react-hooks/incompatible-library` warning at the call site even with
// the directive in place — it's an FYI rule, not gated by opt-out — so we
// disable that specific lint line with a pointer to the TanStack guidance.
// See https://tanstack.com/table/v8/docs/framework/react/react-compiler
export function AuditTable({
  rows,
  onInspect,
}: {
  rows: AuditRow[];
  onInspect: (row: AuditRow) => void;
}) {
  "use no memo";
  const columns = React.useMemo<ColumnDef<AuditRow>[]>(
    () => [
      {
        accessorKey: "created_at",
        header: "Timestamp",
        cell: ({ getValue }) => (
          <span className="whitespace-nowrap text-[var(--color-muted-foreground)]">
            {formatTime(String(getValue()))}
          </span>
        ),
      },
      {
        accessorKey: "actor",
        header: "Actor",
        cell: ({ getValue }) => (
          <span className="font-mono text-xs">
            {truncate(String(getValue() ?? ""), 8)}
          </span>
        ),
      },
      {
        accessorKey: "action",
        header: "Action",
        cell: ({ getValue }) => (
          <span className="font-medium">{String(getValue() ?? "")}</span>
        ),
      },
      {
        accessorKey: "target",
        header: "Target",
        cell: ({ getValue }) => (
          <span className="font-mono text-xs text-[var(--color-muted-foreground)]">
            {truncate(String(getValue() ?? ""), 36)}
          </span>
        ),
      },
      {
        id: "view",
        header: "",
        cell: ({ row }) => (
          <Button
            size="sm"
            variant="ghost"
            onClick={() => onInspect(row.original)}
            aria-label="Inspect row"
          >
            <Eye className="h-4 w-4" />
            View
          </Button>
        ),
      },
    ],
    [onInspect],
  );

  // eslint-disable-next-line react-hooks/incompatible-library -- TanStack Table v8: "use no memo" directive above opts the component out of compiler memoization; this rule still flags the call site as informational.
  const table = useReactTable({
    data: rows,
    columns,
    getCoreRowModel: getCoreRowModel(),
  });

  return (
    <div className="overflow-hidden rounded-lg border border-[var(--color-border)] bg-[var(--color-card)]">
      <div className="overflow-x-auto">
      <table className="w-full min-w-[640px] text-sm">
        <thead className="bg-[var(--color-muted)]/40 text-left text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]">
          {table.getHeaderGroups().map((hg) => (
            <tr key={hg.id}>
              {hg.headers.map((h) => (
                <th key={h.id} className="px-4 py-2 font-medium">
                  {h.isPlaceholder
                    ? null
                    : flexRender(h.column.columnDef.header, h.getContext())}
                </th>
              ))}
            </tr>
          ))}
        </thead>
        <tbody>
          {rows.length === 0 ? (
            <tr>
              <td
                colSpan={5}
                className="px-4 py-8 text-center text-sm text-[var(--color-muted-foreground)]"
              >
                No audit events match the current filters.
              </td>
            </tr>
          ) : (
            table.getRowModel().rows.map((row) => (
              <tr
                key={row.id}
                className="border-t border-[var(--color-border)]"
              >
                {row.getVisibleCells().map((cell) => (
                  <td key={cell.id} className="px-4 py-3">
                    {flexRender(cell.column.columnDef.cell, cell.getContext())}
                  </td>
                ))}
              </tr>
            ))
          )}
        </tbody>
      </table>
      </div>
    </div>
  );
}
