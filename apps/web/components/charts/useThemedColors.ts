"use client";

// useThemedColors — single source of truth for the chart palette inside
// React components. Resolves the palette on mount via `chartColors()` and
// re-resolves whenever the `<html>` class list changes — that's how
// next-themes flips between light + dark, so this hook lets every chart
// re-paint without manually wiring an effect.

import * as React from "react";

import { chartColors, type ChartColors } from "@/lib/chart-theme";

export function useThemedColors(): ChartColors {
  const [palette, setPalette] = React.useState<ChartColors>(() => chartColors());
  React.useEffect(() => {
    if (typeof document === "undefined") return;
    const target = document.documentElement;
    // Re-resolve the palette only on actual class-list mutations — the
    // initial state is already seeded by useState's initializer above,
    // so we don't call setPalette synchronously on mount.
    const obs = new MutationObserver(() => setPalette(chartColors()));
    obs.observe(target, { attributes: true, attributeFilter: ["class"] });
    return () => obs.disconnect();
  }, []);
  return palette;
}
