"use client";

// Console-scoped error boundary. Lives inside the (app) route group so the
// shared layout (sidebar + topbar) keeps rendering — only the page body is
// replaced with the "Something went wrong" card. That preserves navigation
// to other console routes even when one server fetch blew up.

import * as React from "react";
import { AlertTriangle } from "lucide-react";

import { Button } from "@/components/ui/Button";

export default function ConsoleError({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  React.useEffect(() => {
    // eslint-disable-next-line no-console
    console.error("Console error boundary captured:", error);
  }, [error]);

  return (
    <div className="flex min-h-[60vh] items-center justify-center px-6 py-12">
      <div className="w-full max-w-[560px] rounded-xl border border-[var(--color-border)] bg-[var(--color-card)] p-8 shadow-sm">
        <div className="mb-4 inline-flex h-10 w-10 items-center justify-center rounded-full bg-[var(--color-destructive)]/10 text-[var(--color-destructive)]">
          <AlertTriangle className="h-5 w-5" aria-hidden />
        </div>
        <h2 className="mb-2 text-lg font-semibold text-[var(--color-foreground)]">
          We couldn&apos;t load this page
        </h2>
        <p className="mb-6 text-sm text-[var(--color-muted-foreground)]">
          The console hit an unexpected error talking to the control-plane.
          Retry the request, or jump back to the dashboard while we sort it
          out.
        </p>
        {error.digest && (
          <p className="mb-6 font-mono text-[11px] text-[var(--color-muted-foreground)]">
            ref: {error.digest}
          </p>
        )}
        <div className="flex flex-wrap gap-3">
          <Button onClick={() => reset()}>Try again</Button>
          <Button
            variant="outline"
            onClick={() => {
              if (typeof window !== "undefined")
                window.location.assign("/console");
            }}
          >
            Back to dashboard
          </Button>
        </div>
      </div>
    </div>
  );
}
