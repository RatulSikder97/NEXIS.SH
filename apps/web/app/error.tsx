"use client";

// Root error boundary. Next requires this to be a client component because
// it receives a runtime `error` value and a `reset()` callback. We render a
// branded "Something went wrong" page with a retry button so the user is
// never stranded on Next's default framework error screen.

import * as React from "react";
import Link from "next/link";

import { Button } from "@/components/ui/Button";
import { Logo } from "@/components/ui/Logo";

export default function RootError({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  React.useEffect(() => {
    console.error("Root error boundary captured:", error);
  }, [error]);

  return (
    <html lang="en">
      <body className="bg-[var(--color-background)] text-[var(--color-foreground)]">
        <div className="flex min-h-screen flex-col items-center justify-center gap-8 px-6 py-24 text-center">
          <Logo variant="hero" priority />
          <div className="space-y-3">
            <h1 className="text-2xl font-semibold tracking-tight">
              Something went wrong
            </h1>
            <p className="max-w-prose text-sm text-[var(--color-muted-foreground)]">
              The page hit an unexpected error. Try again, or head back home.
              If the problem keeps happening, please contact support.
            </p>
            {error.digest && (
              <p className="font-mono text-[11px] text-[var(--color-muted-foreground)]">
                ref: {error.digest}
              </p>
            )}
          </div>
          <div className="flex flex-wrap items-center justify-center gap-3">
            <Button onClick={() => reset()}>Try again</Button>
            <Button variant="outline" asChild>
              <Link href="/">Return home</Link>
            </Button>
          </div>
        </div>
      </body>
    </html>
  );
}
