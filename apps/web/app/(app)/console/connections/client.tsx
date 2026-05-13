"use client";

// ConnectionsClient — integration cards with detailed health + probe-now.
//
// Each card shows: health pill, last check, latency, last error, and a
// "Probe now" button. The probe POST hits /v1/integrations/{provider}/probe
// — may 404; we surface the error inline.

import * as React from "react";
import { Loader2, Network, RefreshCw } from "lucide-react";

import { cn } from "@/lib/utils";
import { EmptyState } from "@/components/empty-state/EmptyState";
import {
  OperationalSegments,
  type OperationalSegment,
  type SegmentStatus,
} from "@/components/console/OperationalSegments";
import { HealthPill } from "@/components/integrations/HealthPill";
import {
  integrations,
  type IntegrationConnection,
  type IntegrationProvider,
} from "@/lib/integrations";
import { formatRelative } from "@/lib/agents-format";

const API = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

const INTEGRATION_LABEL: Record<IntegrationProvider, string> = {
  github: "GitHub",
  sentry: "Sentry",
  argocd: "ArgoCD",
  slack: "Slack",
  datadog: "Datadog",
  pagerduty: "PagerDuty",
};

async function probeProvider(provider: string): Promise<{
  ok: boolean;
  error?: string;
}> {
  try {
    const r = await fetch(`${API}/v1/integrations/${provider}/probe`, {
      method: "POST",
      credentials: "include",
    });
    if (!r.ok) {
      const body = (await r.json().catch(() => ({}))) as { error?: string };
      return { ok: false, error: body.error ?? r.statusText };
    }
    return { ok: true };
  } catch (err) {
    return { ok: false, error: err instanceof Error ? err.message : "probe failed" };
  }
}

function ConnectionCard({
  connection,
  onRefetch,
}: {
  connection: IntegrationConnection;
  onRefetch: () => void;
}) {
  const [pending, setPending] = React.useState(false);
  const [feedback, setFeedback] = React.useState<{
    tone: "ok" | "err";
    message: string;
  } | null>(null);

  async function probe() {
    setPending(true);
    setFeedback(null);
    const result = await probeProvider(connection.provider);
    setPending(false);
    if (result.ok) {
      setFeedback({ tone: "ok", message: "Probe succeeded." });
      onRefetch();
    } else {
      setFeedback({
        tone: "err",
        message: result.error ?? "Probe failed.",
      });
    }
  }

  return (
    <div className="space-y-3 rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-5">
      <div className="flex items-start justify-between gap-3">
        <div>
          <p className="text-sm font-semibold text-[var(--color-foreground)]">
            {INTEGRATION_LABEL[connection.provider] ?? connection.provider}
          </p>
          <p className="mt-0.5 text-[11px] text-[var(--color-muted-foreground)]">
            {connection.connected ? "Connected" : "Not connected"}
          </p>
        </div>
        <HealthPill
          state={connection.health.state}
          latency_ms={connection.health.latency_ms}
          last_check_at={connection.health.last_check_at}
          last_error={connection.health.last_error}
        />
      </div>
      <dl className="grid grid-cols-2 gap-2 text-[11px]">
        <div>
          <dt className="text-[var(--color-muted-foreground)]">Last check</dt>
          <dd className="font-mono text-[var(--color-foreground)]">
            {formatRelative(connection.health.last_check_at)}
          </dd>
        </div>
        <div>
          <dt className="text-[var(--color-muted-foreground)]">Latency</dt>
          <dd className="font-mono text-[var(--color-foreground)]">
            {typeof connection.health.latency_ms === "number"
              ? `${connection.health.latency_ms}ms`
              : "—"}
          </dd>
        </div>
        {connection.health.last_error && (
          <div className="col-span-2">
            <dt className="text-[var(--color-muted-foreground)]">Last error</dt>
            <dd className="truncate font-mono text-red-700 dark:text-red-300">
              {connection.health.last_error}
            </dd>
          </div>
        )}
      </dl>
      <div className="flex items-center gap-2">
        <button
          type="button"
          onClick={probe}
          disabled={pending || !connection.connected}
          className="inline-flex items-center gap-1.5 rounded-md border border-[var(--color-border)] bg-[var(--color-card)] px-3 py-1.5 text-xs font-medium text-[var(--color-foreground)] transition-colors hover:border-[var(--color-primary)]/40 hover:bg-[var(--color-muted)] disabled:opacity-50"
        >
          {pending ? (
            <Loader2 className="h-3 w-3 animate-spin" />
          ) : (
            <RefreshCw className="h-3 w-3" />
          )}
          Probe now
        </button>
        {feedback && (
          <span
            className={cn(
              "text-[11px]",
              feedback.tone === "ok"
                ? "text-emerald-700 dark:text-emerald-300"
                : "text-red-700 dark:text-red-300",
            )}
          >
            {feedback.message}
          </span>
        )}
      </div>
    </div>
  );
}

function segmentsFor(rows: IntegrationConnection[]): OperationalSegment[] {
  return rows.map((c) => {
    const status: SegmentStatus =
      c.health.state === "healthy"
        ? "succeeded"
        : c.health.state === "degraded"
          ? "running"
          : c.health.state === "down"
            ? "failed"
            : "skipped";
    return {
      label: INTEGRATION_LABEL[c.provider] ?? c.provider,
      started_at: c.health.last_check_at ?? new Date().toISOString(),
      finished_at: c.health.last_check_at,
      duration_ms: c.health.latency_ms,
      status,
      detail: c.health.last_error ?? `Health state: ${c.health.state}`,
    };
  });
}

export function ConnectionsClient({
  initial,
}: {
  initial: IntegrationConnection[];
}) {
  const [connections, setConnections] = React.useState<IntegrationConnection[]>(
    initial,
  );

  const refetch = React.useCallback(async () => {
    try {
      const next = await integrations.listConnections();
      setConnections(next);
    } catch {
      // ignore
    }
  }, []);

  React.useEffect(() => {
    const id = window.setInterval(() => {
      void refetch();
    }, 30_000);
    return () => window.clearInterval(id);
  }, [refetch]);

  const degraded = connections.filter(
    (c) => c.connected && c.health.state !== "healthy",
  ).length;

  return (
    <div className="space-y-6">
      <div className="space-y-1">
        <p className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]">
          Integrations
        </p>
        <h1 className="text-2xl font-semibold">Connection health</h1>
        <p className="max-w-2xl text-sm text-[var(--color-muted-foreground)]">
          {degraded > 0
            ? `${degraded} integration${degraded === 1 ? "" : "s"} not currently healthy. Probe individually to re-check.`
            : "All connected integrations are healthy. Probe individually to re-check on demand."}
        </p>
      </div>

      {connections.length === 0 ? (
        <EmptyState
          icon={Network}
          title="No integration connections"
          description="Connect a provider from the Integrations page to monitor its health."
        />
      ) : (
        <section className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
          {connections.map((c) => (
            <ConnectionCard
              key={c.provider}
              connection={c}
              onRefetch={refetch}
            />
          ))}
        </section>
      )}

      <OperationalSegments
        title="Operational segments"
        description="One segment per integration, most recent probe."
        segments={segmentsFor(connections)}
        emptyMessage="No connections to segment."
      />
    </div>
  );
}
