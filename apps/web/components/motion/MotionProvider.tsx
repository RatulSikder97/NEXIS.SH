"use client";

import { MotionConfig } from "framer-motion";
import type { ReactNode } from "react";

// React 19's ReactNode shape doesn't yet match framer-motion 12's MotionConfig
// children prop — the runtime is unaffected; we mask the declaration drift
// with a narrow cast, same approach as FadeUp / StaggerGroup.
type Slot = React.ReactElement | null;

export function MotionProvider({ children }: { children: ReactNode }) {
  return <MotionConfig reducedMotion="user">{children as Slot}</MotionConfig>;
}
