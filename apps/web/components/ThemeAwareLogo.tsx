"use client";

import * as React from "react";
import Image from "next/image";
import { useTheme } from "next-themes";

// useHydrated returns false on the server + first client render, then
// true after hydration. Lets us swap the logo source post-hydration
// without tripping React 19's react-hooks/set-state-in-effect rule.
const subscribe = () => () => {};
const getClientSnapshot = () => true;
const getServerSnapshot = () => false;

function useHydrated(): boolean {
  return React.useSyncExternalStore(
    subscribe,
    getClientSnapshot,
    getServerSnapshot,
  );
}

export function ThemeAwareLogo({ className, width = 120, height = 32 }: {
  className?: string;
  width?: number;
  height?: number;
}) {
  const { resolvedTheme } = useTheme();
  const mounted = useHydrated();
  const src = !mounted || resolvedTheme === "light" ? "/logo-light.svg" : "/logo.svg";
  return <Image src={src} alt="NEXIS" width={width} height={height} className={className} priority />;
}
