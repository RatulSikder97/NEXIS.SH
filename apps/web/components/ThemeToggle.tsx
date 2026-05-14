"use client";

import * as React from "react";
import { Moon, Sun } from "lucide-react";
import { useTheme } from "next-themes";
import { Button } from "@/components/ui/Button";

// useHydrated returns false on the server + first client render, then
// true after hydration. Using useSyncExternalStore keeps the "render
// nothing meaningful until mount" pattern without tripping React 19's
// react-hooks/set-state-in-effect rule, which (rightly) forbids the
// classic `useEffect(() => setMounted(true), [])` idiom.
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

export function ThemeToggle() {
  const { theme, setTheme } = useTheme();
  const mounted = useHydrated();
  if (!mounted) return <Button variant="ghost" size="icon" aria-label="Toggle theme" />;
  const isDark = theme === "dark";
  return (
    <Button
      variant="ghost"
      size="icon"
      aria-label={isDark ? "Switch to light mode" : "Switch to dark mode"}
      onClick={() => setTheme(isDark ? "light" : "dark")}
    >
      {isDark ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
    </Button>
  );
}
