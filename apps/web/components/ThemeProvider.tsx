"use client";

// Phase 3 Stage 8 — Theme provider.
//
// Wraps next-themes. The default theme is "light" so the marketing site
// (which is not gated behind a session) renders consistently. The console
// supports a "system" choice via Preferences; we enable enableSystem so
// resolvedTheme falls through to the prefers-color-scheme media query when
// the user selects it.
//
// Callers can override defaultTheme — RootLayout reads the server-side
// preference and passes it in so authenticated users see their persisted
// choice on first paint.

import * as React from "react";
import { ThemeProvider as NextThemesProvider } from "next-themes";

export function ThemeProvider({
  children,
  ...props
}: React.ComponentProps<typeof NextThemesProvider>) {
  return (
    <NextThemesProvider
      attribute="class"
      defaultTheme="light"
      enableSystem
      {...props}
    >
      {children}
    </NextThemesProvider>
  );
}
