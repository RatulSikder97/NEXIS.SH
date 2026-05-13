// Shared display formatters for the agent fleet + drill-down surfaces.
//
// Extracted from app/(app)/console/agents/page.tsx so the fleet list and the
// per-agent detail page can render identical numbers without duplicating the
// formatting rules. Keep these pure — no DOM, no React, no env.

// formatUSD formats a cent integer as a localised dollar amount. Adaptive
// precision: ≥ $0.01 (or exactly zero) renders 2 decimals, sub-cent rolls
// out to 4 so a $0.0023 cost doesn't display as "$0.00".
export function formatUSD(cents: number): string {
  const dollars = cents / 100;
  const decimals = Math.abs(dollars) >= 0.01 || dollars === 0 ? 2 : 4;
  return new Intl.NumberFormat(undefined, {
    style: "currency",
    currency: "USD",
    minimumFractionDigits: decimals,
    maximumFractionDigits: decimals,
  }).format(dollars);
}

// formatTokens compresses an absolute token count into a "1.2k" / "3.4M"
// label. Below 1k we return the raw integer so small experiments don't
// collapse to "0.0k".
export function formatTokens(n: number): string {
  if (n >= 1_000_000) return (n / 1_000_000).toFixed(1) + "M";
  if (n >= 1_000) return (n / 1_000).toFixed(1) + "k";
  return String(n);
}

// formatRelative renders an RFC3339 timestamp as "just now" / "Nm ago" /
// "Nh ago" / "Nd ago". Used by every list that wants a recency hint
// without a heavyweight i18n lib. Returns "—" for empty / undefined.
export function formatRelative(iso?: string): string {
  if (!iso) return "—";
  try {
    const diff = Date.now() - new Date(iso).getTime();
    const m = Math.floor(diff / 60_000);
    if (m < 1) return "just now";
    if (m < 60) return `${m}m ago`;
    const h = Math.floor(m / 60);
    if (h < 24) return `${h}h ago`;
    const d = Math.floor(h / 24);
    return `${d}d ago`;
  } catch {
    return iso;
  }
}

// formatDurationMs renders a millisecond count as a short human label.
// Mirrors the formatter used by the incidents table / pipeline timeline
// so durations read consistently across the console.
export function formatDurationMs(ms: number | undefined): string {
  if (typeof ms !== "number" || !Number.isFinite(ms) || ms < 0) return "—";
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)}s`;
  const m = Math.floor(ms / 60_000);
  const s = Math.round((ms % 60_000) / 1000);
  return s === 0 ? `${m}m` : `${m}m ${s}s`;
}

// formatTimeMs renders an RFC3339 timestamp at millisecond precision.
// Used by the per-event timeline where two events can land in the same
// second and we want the order to be readable.
export function formatTimeMs(iso: string | undefined): string {
  if (!iso) return "—";
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return iso;
  try {
    const d = new Date(t);
    const hh = String(d.getHours()).padStart(2, "0");
    const mm = String(d.getMinutes()).padStart(2, "0");
    const ss = String(d.getSeconds()).padStart(2, "0");
    const ms = String(d.getMilliseconds()).padStart(3, "0");
    return `${hh}:${mm}:${ss}.${ms}`;
  } catch {
    return iso;
  }
}
