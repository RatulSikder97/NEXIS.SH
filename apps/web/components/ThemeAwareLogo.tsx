"use client";

import * as React from "react";
import Image from "next/image";
import { useTheme } from "next-themes";

export function ThemeAwareLogo({ className, width = 120, height = 32 }: {
  className?: string;
  width?: number;
  height?: number;
}) {
  const { resolvedTheme } = useTheme();
  const [mounted, setMounted] = React.useState(false);
  React.useEffect(() => setMounted(true), []);
  const src = !mounted || resolvedTheme === "light" ? "/logo-light.svg" : "/logo.svg";
  return <Image src={src} alt="NEXIS" width={width} height={height} className={className} priority />;
}
