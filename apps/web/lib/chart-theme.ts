// chart-theme.ts — runtime palette helper for the charts surface.
//
// All chart colours read CSS custom properties off `:root` so the chart
// renders correctly in light + dark mode without us hard-coding values.
// The palette is computed once per render — we explicitly re-call this
// inside an effect when next-themes flips a theme, so the readout stays
// in sync with the document class list.
//
// `getComputedStyle` only works in the browser; the helpers guard with a
// `typeof document` check and return a sane light-mode fallback during
// SSR so the first paint doesn't flash empty colours.

export type ChartColors = {
  primary: string;
  primarySoft: string;
  foreground: string;
  mutedForeground: string;
  border: string;
  card: string;
  background: string;
  destructive: string;
  warning: string;
  success: string;
  // Categorical series palette — used for stacked bar + donut.
  series: string[];
};

const SSR_FALLBACK: ChartColors = {
  primary: "hsl(217 91% 60%)",
  primarySoft: "hsla(217, 91%, 60%, 0.18)",
  foreground: "hsl(222 47% 11%)",
  mutedForeground: "hsl(215 16% 47%)",
  border: "hsl(214 32% 91%)",
  card: "hsl(0 0% 100%)",
  background: "hsl(0 0% 100%)",
  destructive: "hsl(0 72% 51%)",
  warning: "hsl(38 92% 50%)",
  success: "hsl(160 84% 39%)",
  series: [
    "hsl(217 91% 60%)",
    "hsl(160 84% 39%)",
    "hsl(38 92% 50%)",
    "hsl(0 72% 51%)",
    "hsl(280 70% 60%)",
    "hsl(190 80% 50%)",
    "hsl(340 75% 55%)",
    "hsl(140 60% 45%)",
  ],
};

function readVar(root: CSSStyleDeclaration, name: string, fallback: string): string {
  const raw = root.getPropertyValue(name).trim();
  if (!raw) return fallback;
  // HSL values arrive as `222 47% 11%` from our @theme inline block — wrap
  // them so consumers can use the result as a CSS colour value directly.
  if (/^\d/.test(raw)) return `hsl(${raw})`;
  return raw;
}

export function chartColors(): ChartColors {
  if (typeof document === "undefined") return SSR_FALLBACK;
  const root = getComputedStyle(document.documentElement);
  const primary = readVar(root, "--primary", SSR_FALLBACK.primary);
  return {
    primary,
    primarySoft: `color-mix(in srgb, ${primary} 18%, transparent)`,
    foreground: readVar(root, "--foreground", SSR_FALLBACK.foreground),
    mutedForeground: readVar(
      root,
      "--muted-foreground",
      SSR_FALLBACK.mutedForeground,
    ),
    border: readVar(root, "--border", SSR_FALLBACK.border),
    card: readVar(root, "--card", SSR_FALLBACK.card),
    background: readVar(root, "--background", SSR_FALLBACK.background),
    destructive: readVar(root, "--destructive", SSR_FALLBACK.destructive),
    warning: readVar(root, "--warning", SSR_FALLBACK.warning),
    success: readVar(root, "--success", SSR_FALLBACK.success),
    // Categorical palette is deliberately static: we want consistent agent
    // colours across the dashboard regardless of theme. The hues are tuned
    // to read in both light + dark.
    series: SSR_FALLBACK.series,
  };
}

// Severity-mapped palette used by the donut + per-project bar chart so the
// list-side SeverityPill and the chart fill match without hard-coding hex
// values in two places.
export const SEVERITY_COLOURS = {
  none: "hsl(215 16% 55%)",
  low: "hsl(160 84% 39%)",
  medium: "hsl(38 92% 50%)",
  high: "hsl(0 72% 51%)",
  critical: "hsl(340 75% 50%)",
} as const;

// Status palette mirrors the workflow_run statuses we surface in charts.
export const STATUS_COLOURS = {
  succeeded: "hsl(160 84% 39%)",
  running: "hsl(217 91% 60%)",
  queued: "hsl(215 16% 55%)",
  failed: "hsl(0 72% 51%)",
  timed_out: "hsl(38 92% 50%)",
  cancelled: "hsl(280 70% 60%)",
  degraded: "hsl(38 92% 50%)",
} as const;
