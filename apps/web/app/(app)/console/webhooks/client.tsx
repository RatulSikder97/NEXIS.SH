"use client";

// WebhooksClient — recent webhook deliveries table + payload viewer.

import * as React from "react";
import {
  AlertOctagon,
  ChevronDown,
  ChevronRight,
  Webhook,
} from "lucide-react";

import { cn } from "@/lib/utils";
import { EmptyState } from "@/components/empty-state/EmptyState";
import {
  OperationalSegments,
  type OperationalSegment,
  type SegmentStatus,
} from "@/components/console/OperationalSegments";
import { formatDurationMs, formatRelative } from "@/lib/agents-format";

export type WebhookStatus =
  | "verified"
  | "rejected"
  | "processed"
  | "failed"
  | "pending";

export type WebhookDelivery = {
  id: string;
  provider: string;
  event_type: string;
  status: WebhookStatus;
  received_at: string;
  latency_ms?: number;
  payload_bytes?: number;
  source_ip?: string;
  payload?: Record<string, unknown>;
  headers?: Record<string, string>;
  error?: string;
};

type WebhooksResp = {
  rows: WebhookDelivery[];
  total?: number;
};

const STATUS_PILL: Record<WebhookStatus, string> = {
  verified:
    "bg-emerald-500/15 text-emerald-700 ring-emerald-500/30 dark:text-emerald-300",
  rejected: "bg-red-500/15 text-red-700 ring-red-500/30 dark:text-red-300",
  processed:
    "bg-blue-500/15 text-blue-700 ring-blue-500/30 dark:text-blue-300",
  failed: "bg-red-500/15 text-red-700 ring-red-500/30 dark:text-red-300",
  pending: "bg-[var(--color-muted)] text-[var(--color-muted-foreground)] ring-[var(--color-border)]",
};

function StatusPill({ s }: { s: WebhookStatus }) {
  return (
    <span
      className={cn(
        "inline-flex items-center rounded-full px-2 py-0.5 text-[10px] font-medium uppercase tracking-widest ring-1",
        STATUS_PILL[s],
      )}
    >
      {s}
    </span>
  );
}

function PayloadPane({
  label,
  data,
}: {
  label: string;
  data: Record<string, unknown> | undefined;
}) {
  if (!data || Object.keys(data).length === 0) return null;
  return (
    <div>
      <p className="mb-1 text-[10px] font-semibold uppercase tracking-widest text-[var(--color-muted-foreground)]">
        {label}
      </p>
      <pre className="max-h-80 overflow-auto rounded-md bg-[var(--color-muted)]/40 p-3 text-[11px] font-mono leading-snug">
        {JSON.stringify(data, null, 2)}
      </pre>
    </div>
  );
}

function Row({ row }: { row: WebhookDelivery }) {
  const [open, setOpen] = React.useState(false);
  return (
    <>
      <tr
        className={cn(
          "border-t border-[var(--color-border)]",
          open ? "bg-[var(--color-muted)]/30" : "hover:bg-[var(--color-muted)]/20",
        )}
      >
        <td className="px-3 py-2">
          <button
            type="button"
            onClick={() => setOpen((v) => !v)}
            aria-expanded={open}
            aria-label={open ? "Collapse" : "Expand"}
            className="inline-flex h-6 w-6 items-center justify-center rounded-md text-[var(--color-muted-foreground)] hover:bg-[var(--color-muted)] hover:text-[var(--color-foreground)]"
          >
            {open ? (
              <ChevronDown className="h-3 w-3" />
            ) : (
              <ChevronRight className="h-3 w-3" />
            )}
          </button>
        </td>
        <td className="px-3 py-2 font-mono text-[11px] text-[var(--color-muted-foreground)]">
          {formatRelative(row.received_at)}
        </td>
        <td className="px-3 py-2 text-xs text-[var(--color-foreground)]">
          {row.provider}
        </td>
        <td className="px-3 py-2 text-xs text-[var(--color-foreground)]">
          {row.event_type}
        </td>
        <td className="px-3 py-2">
          <StatusPill s={row.status} />
        </td>
        <td className="px-3 py-2 font-mono text-[11px] text-[var(--color-muted-foreground)]">
          {formatDurationMs(row.latency_ms)}
        </td>
        <td className="px-3 py-2 font-mono text-[11px] text-[var(--color-muted-foreground)]">
          {typeof row.payload_bytes === "number"
            ? `${row.payload_bytes} B`
            : "—"}
        </td>
        <td className="px-3 py-2 font-mono text-[11px] text-[var(--color-muted-foreground)]">
          {row.source_ip ?? "—"}
        </td>
      </tr>
      {open && (
        <tr className="border-t border-[var(--color-border)] bg-[var(--color-background)]">
          <td colSpan={8} className="space-y-3 px-3 py-3">
            {row.error && (
              <p className="rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-xs text-red-700 dark:text-red-300">
                {row.error}
              </p>
            )}
            <PayloadPane label="Payload" data={row.payload} />
            <PayloadPane label="Headers" data={row.headers} />
          </td>
        </tr>
      )}
    </>
  );
}

function segmentsFor(rows: WebhookDelivery[]): OperationalSegment[] {
  return rows.slice(0, 20).map((r) => {
    const status: SegmentStatus =
      r.status === "verified" || r.status === "processed"
        ? "succeeded"
        : r.status === "rejected" || r.status === "failed"
          ? "failed"
          : "pending";
    return {
      label: `${r.provider} · ${r.event_type}`,
      started_at: r.received_at,
      finished_at: r.received_at,
      duration_ms: r.latency_ms,
      status,
      detail:
        r.error ??
        `${r.status.toUpperCase()} · ${r.source_ip ?? "unknown source"}`,
    };
  });
}

export function WebhooksClient({ initial }: { initial: WebhooksResp | null }) {
  if (initial === null) {
    return (
      <div className="space-y-6">
        <div className="space-y-1">
          <p className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]">
            Integrations
          </p>
          <h1 className="text-2xl font-semibold">Webhook activity</h1>
          <p className="max-w-2xl text-sm text-[var(--color-muted-foreground)]">
            Recent webhook deliveries across every configured integration.
          </p>
        </div>
        <EmptyState
          icon={AlertOctagon}
          title="Webhook activity unavailable"
          description="The control-plane endpoint /v1/integrations/webhooks isn't online yet. Once the backend exposes delivery logs, every webhook event will appear here."
        />
      </div>
    );
  }

  const rows = initial.rows ?? [];

  return (
    <div className="space-y-6">
      <div className="space-y-1">
        <p className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]">
          Integrations
        </p>
        <h1 className="text-2xl font-semibold">Webhook activity</h1>
        <p className="max-w-2xl text-sm text-[var(--color-muted-foreground)]">
          Last {rows.length} webhook deliveries. Click a row to see the full
          headers + payload as JSON.
        </p>
      </div>

      {rows.length === 0 ? (
        <EmptyState
          icon={Webhook}
          title="No recent webhook activity"
          description="Once an integration delivers a webhook, the event will appear here with payload, headers, and verification status."
        />
      ) : (
        <section className="overflow-hidden rounded-lg border border-[var(--color-border)] bg-[var(--color-card)]">
          <table className="w-full text-sm">
            <thead className="bg-[var(--color-muted)]/40 text-left text-[10px] uppercase tracking-widest text-[var(--color-muted-foreground)]">
              <tr>
                <th className="w-8 px-3 py-2" aria-label="Expand" />
                <th className="px-3 py-2 font-medium">Received</th>
                <th className="px-3 py-2 font-medium">Provider</th>
                <th className="px-3 py-2 font-medium">Event</th>
                <th className="px-3 py-2 font-medium">Status</th>
                <th className="px-3 py-2 font-medium">Latency</th>
                <th className="px-3 py-2 font-medium">Size</th>
                <th className="px-3 py-2 font-medium">Source IP</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((r) => (
                <Row key={r.id} row={r} />
              ))}
            </tbody>
          </table>
        </section>
      )}

      <OperationalSegments
        title="Operational segments"
        description="Last 20 deliveries, sorted as received."
        segments={segmentsFor(rows)}
        emptyMessage="No deliveries to surface."
      />
    </div>
  );
}
