"use client";

import { useEffect, useState } from "react";

export function usePrefersReducedMotion() {
  const forceMotion = process.env.NEXT_PUBLIC_FORCE_MOTION === "1";
  const [reduced, setReduced] = useState(() => {
    if (forceMotion) return false;
    if (typeof window === "undefined") return false;
    return window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  });

  useEffect(() => {
    if (forceMotion) return;

    const mediaQuery = window.matchMedia("(prefers-reduced-motion: reduce)");
    const update = () => setReduced(Boolean(mediaQuery.matches));

    mediaQuery.addEventListener("change", update);
    return () => mediaQuery.removeEventListener("change", update);
  }, [forceMotion]);

  return reduced;
}

