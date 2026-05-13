"use client";

// Phase 8 — Client-side mount wrapper for the Shepherd.js tour.
//
// Next 16 forbids `dynamic(..., { ssr: false })` inside server components,
// so the console layout (a server component) can't lazy-import Tour
// directly. This thin client wrapper does the dynamic import on the
// client only — the wrapper itself is statically imported by the
// layout, but it's small (~1 KB) and conditional on the tour-completed
// flag, so the cost is negligible.

import * as React from "react";
import dynamic from "next/dynamic";

// The dynamic() call here lives in a client module, so `ssr: false`
// is allowed. The Tour component itself dynamically imports shepherd.js
// on mount — that second layer keeps the ~30 KB bundle out of the
// initial page weight even for users who do see the tour.
const Tour = dynamic(() => import("@/components/onboarding/Tour"), {
  ssr: false,
});

export function TourMount() {
  return <Tour />;
}
