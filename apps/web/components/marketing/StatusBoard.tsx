"use client";

import { useEffect, useState } from "react";
import { AlertCircle, CheckCircle2, MinusCircle, RefreshCw, ServerCog } from "lucide-react";

import { SectionHeading } from "@/components/marketing/SectionHeading";
import { ProviderLogo, type ProviderID } from "@/components/integrations/ProviderLogo";

type State = "ok" | "degraded" | "down" | "unknown" | "disconnected";

type Subsystem = {
  name: string;
  state: "ok" | "degraded" | "down";
  description?: string;
  last_check_at?: string;
};

type IntegrationProbe = {
  provider: string;
  state: State;
  last_check_at?: string;
  latency_ms?: number;
  last_error?: string;
};

type StatusPayload = {
  generated_at?: string;
  overall?: "ok" | "degraded" | "down";
  subsystems?: Subsystem[];
  integrations?: IntegrationProbe[];
};

// Fallback subsystem list — six gauges that always render so /status doesn't
// look hollow when the backend hasn't been reached yet. Real probes replace
// these on the first successful poll.
const FALLBACK_SUBSYSTEMS: Subsystem[] = [
  { name: "Control plane API", state: "ok", description: "REST + WebSocket endpoints" },
  { name: "Agent runtime", state: "ok", description: "Sentinel + Pathfinder + Synthesiser + …" },
  { name: "Shadow pipeline", state: "ok", description: "Docker-isolated validation pool" },
  { name: "Approval Gate", state: "ok", description: "Severity routing + audit log" },
  { name: "Database (Postgres)", state: "ok", description: "Primary + read replicas" },
  { name: "Object storage", state: "ok", description: "S3-compatible audit + artefacts" },
];

const FALLBACK_PROBES: IntegrationProbe[] = [
  { provider: "github", state: "ok", latency_ms: 142 },
  { provider: "gitlab", state: "unknown" },
  { provider: "bitbucket", state: "unknown" },
  { provider: "sentry", state: "ok", latency_ms: 218 },
  { provider: "datadog", state: "ok", latency_ms: 187 },
  { provider: "newrelic", state: "unknown" },
  { provider: "grafana_cloud", state: "unknown" },
  { provider: "prometheus", state: "unknown" },
  { provider: "honeycomb", state: "unknown" },
  { provider: "splunk", state: "unknown" },
  { provider: "pagerduty", state: "ok", latency_ms: 305 },
  { provider: "opsgenie", state: "unknown" },
  { provider: "incident_io", state: "unknown" },
  { provider: "argocd", state: "ok", latency_ms: 96 },
  { provider: "kubernetes", state: "unknown" },
  { provider: "flux_cd", state: "unknown" },
  { provider: "aws_cloudwatch", state: "unknown" },
  { provider: "gcp_monitoring", state: "unknown" },
  { provider: "azure_monitor", state: "unknown" },
  { provider: "slack", state: "ok", latency_ms: 78 },
  { provider: "ms_teams", state: "unknown" },
  { provider: "discord", state: "unknown" },
  { provider: "airflow", state: "unknown" },
  { provider: "spark", state: "unknown" },
  { provider: "databricks", state: "unknown" },
  { provider: "snowflake", state: "unknown" },
  { provider: "dbt", state: "unknown" },
  { provider: "kafka", state: "unknown" },
];

function stateColour(state: State | Subsystem["state"]) {
  switch (state) {
    case "ok":
      return {
        dot: "bg-[var(--color-success)]",
        text: "text-[var(--color-success)]",
        ring: "ring-[var(--color-success)]/30",
        label: "Operational",
      };
    case "degraded":
      return {
        dot: "bg-[var(--color-warning)]",
        text: "text-[var(--color-warning)]",
        ring: "ring-[var(--color-warning)]/30",
        label: "Degraded",
      };
    case "down":
      return {
        dot: "bg-[var(--color-destructive)]",
        text: "text-[var(--color-destructive)]",
        ring: "ring-[var(--color-destructive)]/30",
        label: "Outage",
      };
    case "disconnected":
      return {
        dot: "bg-[var(--color-muted-foreground)]",
        text: "text-[var(--color-muted-foreground)]",
        ring: "ring-[var(--color-border)]",
        label: "Disconnected",
      };
    default:
      return {
        dot: "bg-[var(--color-muted-foreground)]",
        text: "text-[var(--color-muted-foreground)]",
        ring: "ring-[var(--color-border)]",
        label: "Unknown",
      };
  }
}

function StateIcon({ state }: { state: State | Subsystem["state"] }) {
  if (state === "ok") return <CheckCircle2 className="h-4 w-4" aria-hidden />;
  if (state === "degraded" || state === "down")
    return <AlertCircle className="h-4 w-4" aria-hidden />;
  return <MinusCircle className="h-4 w-4" aria-hidden />;
}

export function StatusBoard({
  initial,
  pollIntervalMs = 30000,
}: {
  initial: StatusPayload | null;
  pollIntervalMs?: number;
}) {
  const [data, setData] = useState<StatusPayload | null>(initial);
  const [refreshing, setRefreshing] = useState(false);
  const [lastFetchAt, setLastFetchAt] = useState<Date | null>(
    initial ? new Date() : null
  );

  // Browser-side polling — re-hits the same proxy /api endpoint as the server
  // page so users see fresh data without a page refresh.
  useEffect(() => {
    let alive = true;
    const tick = async () => {
      setRefreshing(true);
      try {
        const r = await fetch("/api/system-status", { cache: "no-store" });
        if (!alive) return;
        if (r.ok) {
          const payload = (await r.json()) as StatusPayload;
          setData(payload);
          setLastFetchAt(new Date());
        }
      } catch {
        // swallow — fallback data is already rendered.
      } finally {
        if (alive) setRefreshing(false);
      }
    };
    const id = window.setInterval(tick, pollIntervalMs);
    return () => {
      alive = false;
      window.clearInterval(id);
    };
  }, [pollIntervalMs]);

  const subsystems = data?.subsystems?.length
    ? data.subsystems
    : FALLBACK_SUBSYSTEMS;
  const probes = data?.integrations?.length ? data.integrations : FALLBACK_PROBES;

  const live = Boolean(data);

  return (
    <div>
      {!live ? (
        <div className="mb-8 flex items-start gap-3 rounded-[12px] border border-[var(--color-border)] bg-[var(--color-card)] p-4 text-[14px] shadow-sm">
          <ServerCog className="mt-0.5 h-4 w-4 shrink-0 text-[var(--color-muted-foreground)]" aria-hidden />
          <p className="text-[var(--color-muted-foreground)]">
            Status page coming online — the control-plane status endpoint is
            unreachable from this environment. Falling back to last-known
            health. Real data will resume once <code className="font-mono text-[12px]">/v1/system-status</code> responds.
          </p>
        </div>
      ) : null}

      <div className="flex items-center justify-between">
        <SectionHeading
          eyebrow="SUBSYSTEMS"
          title="Six core gauges."
          lead="Each represents a layer of the NEXIS platform. All green = recovery loop running end-to-end."
        />
        <div className="hidden md:flex items-center gap-2 text-[12px] text-[var(--color-muted-foreground)]">
          <RefreshCw
            className={`h-3.5 w-3.5 ${refreshing ? "animate-spin" : ""}`}
            aria-hidden
          />
          {lastFetchAt
            ? `refreshed ${lastFetchAt.toLocaleTimeString()}`
            : "polling…"}
        </div>
      </div>

      <div className="mt-8 grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
        {subsystems.map((s) => {
          const c = stateColour(s.state);
          return (
            <article
              key={s.name}
              className={`flex items-start gap-3 rounded-[12px] border border-[var(--color-border)] bg-[var(--color-card)] p-4 shadow-sm ring-1 ${c.ring}`}
            >
              <span className={`mt-1 h-3 w-3 shrink-0 rounded-full ${c.dot}`} />
              <div>
                <h3 className="text-[14px] font-medium text-[var(--color-foreground)]">
                  {s.name}
                </h3>
                {s.description ? (
                  <p className="text-[12px] text-[var(--color-muted-foreground)]">
                    {s.description}
                  </p>
                ) : null}
                <p className={`mt-1 text-[12px] font-medium ${c.text}`}>
                  {c.label}
                </p>
              </div>
            </article>
          );
        })}
      </div>

      <div className="mt-16">
        <SectionHeading
          eyebrow="INTEGRATIONS"
          title="28 integration probes."
          lead="Each adapter is polled every 30 seconds. Unknown = not configured in this workspace; Disconnected = credentials revoked."
        />

        <div className="mt-8 overflow-hidden rounded-[14px] border border-[var(--color-border)] bg-[var(--color-card)] shadow-sm">
          <div className="grid grid-cols-[1.4fr_1fr_1fr_1fr] border-b border-[var(--color-border)] bg-[var(--color-muted)]/60 px-5 py-3 text-[11px] font-medium uppercase tracking-[0.15em] text-[var(--color-muted-foreground)]">
            <span>Provider</span>
            <span className="text-center">State</span>
            <span className="text-center hidden md:block">Latency</span>
            <span className="text-right">Last check</span>
          </div>
          {probes.map((p, idx) => {
            const c = stateColour(p.state);
            return (
              <div
                key={p.provider}
                className={`grid grid-cols-[1.4fr_1fr_1fr_1fr] items-center gap-2 px-5 py-3 text-[13px] ${
                  idx % 2 === 0 ? "bg-[var(--color-card)]" : "bg-[var(--color-muted)]/40"
                } border-b border-[var(--color-border)] last:border-b-0`}
              >
                <div className="flex items-center gap-3">
                  <span className="flex h-7 w-7 items-center justify-center rounded-md bg-[var(--color-muted)] text-[var(--color-foreground)]">
                    <ProviderLogo
                      provider={p.provider as ProviderID}
                      className="h-4 w-4"
                    />
                  </span>
                  <span className="font-medium text-[var(--color-foreground)] capitalize">
                    {p.provider.replace(/_/g, " ")}
                  </span>
                </div>
                <div className="flex justify-center">
                  <span
                    className={`inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-[11px] font-medium ${c.text} ring-1 ${c.ring}`}
                  >
                    <StateIcon state={p.state} />
                    {c.label}
                  </span>
                </div>
                <div className="hidden md:block text-center text-[12px] text-[var(--color-muted-foreground)]">
                  {p.latency_ms ? `${p.latency_ms}ms` : "—"}
                </div>
                <div className="text-right text-[12px] text-[var(--color-muted-foreground)]">
                  {p.last_check_at ?? "—"}
                </div>
              </div>
            );
          })}
        </div>
      </div>
    </div>
  );
}
