"use client";

// Auth-scoped error boundary. The (auth) layout owns a centered card, so
// we render inside that frame: a destructive icon, a short explanation,
// and the retry/back actions stack the same way the sign-in / sign-up
// forms do.

import * as React from "react";
import Link from "next/link";
import { AlertTriangle } from "lucide-react";

import { Button } from "@/components/ui/Button";

export default function AuthError({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  React.useEffect(() => {
    console.error("Auth error boundary captured:", error);
  }, [error]);

  return (
    <div className="w-full">
      <div className="mb-4 inline-flex h-10 w-10 items-center justify-center rounded-full bg-[var(--color-destructive)]/10 text-[var(--color-destructive)]">
        <AlertTriangle className="h-5 w-5" aria-hidden />
      </div>
      <h2 className="mb-2 text-2xl font-semibold text-[var(--color-foreground)]">
        Sign-in is having trouble
      </h2>
      <p className="mb-6 text-sm text-[var(--color-muted-foreground)]">
        We couldn&apos;t complete that step. Try again, or head back home to
        try a different path.
      </p>
      {error.digest && (
        <p className="mb-6 font-mono text-[11px] text-[var(--color-muted-foreground)]">
          ref: {error.digest}
        </p>
      )}
      <div className="flex flex-wrap gap-3">
        <Button onClick={() => reset()}>Try again</Button>
        <Button variant="outline" asChild>
          <Link href="/">Return home</Link>
        </Button>
      </div>
    </div>
  );
}
