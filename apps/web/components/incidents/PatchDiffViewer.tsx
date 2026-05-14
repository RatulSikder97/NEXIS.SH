"use client";

// Phase 7 — Patch diff viewer.
//
// Renders a "View patch" disclosure that lazily fetches the unified-diff
// text from the control-plane's patch-store proxy
// (`/v1/workspaces/{ws}/pipelines/{run}/patches/{patch_key}`) when the
// user expands the <details> element. The brief explicitly defers
// syntax highlighting — we render the raw diff inside a monospace <pre>
// with `whitespace-pre-wrap` so long lines wrap rather than overflowing
// horizontally.
//
// Lifecycle:
//   • collapsed       → no network traffic
//   • first expand    → fetch + cache in component state
//   • subsequent expand → re-show cached body
//   • 404 from BE     → render a muted "patch not available" message
//   • fetch error     → render the error string inline (no toast — the
//                       outer page already owns user-facing toasts)

import * as React from "react";
import { ChevronDown, ChevronRight, FileDiff, Loader2 } from "lucide-react";

import { pipelines } from "@/lib/pipelines";
import { cn } from "@/lib/utils";

type FetchState =
  | { kind: "idle" }
  | { kind: "loading" }
  | { kind: "loaded"; diff: string }
  | { kind: "missing" }
  | { kind: "error"; message: string };

export function PatchDiffViewer({
  workspaceId,
  runId,
  patchKey,
  // Optional label override — defaults to the patch_key (truncated) so the
  // disclosure is identifiable when multiple patches land on one run.
  label,
  // Optional agent-role tag so the row can carry context (e.g. "Backend ·
  // Codegen") without the caller wrapping the disclosure in its own header.
  agentLabel,
}: {
  workspaceId: string;
  runId: string;
  patchKey: string;
  label?: string;
  agentLabel?: string;
}) {
  const [open, setOpen] = React.useState(false);
  const [state, setState] = React.useState<FetchState>({ kind: "idle" });

  // Fetch-on-expand. We deliberately don't refetch on collapse → re-expand;
  // the diff is content-addressed by patch_key so the body won't drift.
  //
  // React 19's set-state-in-effect rule forbids synchronous setState calls
  // inside an effect body. We follow the same pattern used by
  // useDiscoveryEndpoint — defer the "loading" pulse to the next microtask
  // so the effect runs as a "subscribe for updates from an external
  // system" idiom instead of a cascading render.
  React.useEffect(() => {
    if (!open) return;
    if (state.kind !== "idle") return;
    let cancelled = false;
    void Promise.resolve().then(() => {
      if (cancelled) return;
      setState({ kind: "loading" });
    });
    void (async () => {
      try {
        const diff = await pipelines.patches.get(workspaceId, runId, patchKey);
        if (cancelled) return;
        if (diff === null) {
          setState({ kind: "missing" });
          return;
        }
        setState({ kind: "loaded", diff });
      } catch (err) {
        if (cancelled) return;
        setState({
          kind: "error",
          message:
            err instanceof Error ? err.message : "Failed to load patch.",
        });
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [open, workspaceId, runId, patchKey, state.kind]);

  const displayLabel = label ?? truncateKey(patchKey);

  return (
    <details
      open={open}
      onToggle={(e) => setOpen((e.target as HTMLDetailsElement).open)}
      className="rounded-md border border-[var(--color-border)] bg-[var(--color-card)]"
    >
      <summary
        className={cn(
          "flex cursor-pointer list-none items-center gap-2 px-3 py-2 text-xs",
          "text-[var(--color-foreground)] hover:bg-[var(--color-muted)]/40",
        )}
      >
        {open ? (
          <ChevronDown
            className="h-3.5 w-3.5 text-[var(--color-muted-foreground)]"
            aria-hidden
          />
        ) : (
          <ChevronRight
            className="h-3.5 w-3.5 text-[var(--color-muted-foreground)]"
            aria-hidden
          />
        )}
        <FileDiff
          className="h-3.5 w-3.5 text-[var(--color-muted-foreground)]"
          aria-hidden
        />
        <span className="font-medium">View patch</span>
        {agentLabel && (
          <span className="text-[var(--color-muted-foreground)]">
            · {agentLabel}
          </span>
        )}
        <span className="ml-auto truncate font-mono text-[10px] text-[var(--color-muted-foreground)]">
          {displayLabel}
        </span>
      </summary>
      <div className="border-t border-[var(--color-border)] p-3">
        {state.kind === "loading" && (
          <div className="flex items-center gap-2 text-xs text-[var(--color-muted-foreground)]">
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
            Loading diff…
          </div>
        )}
        {state.kind === "missing" && (
          <p className="text-xs text-[var(--color-muted-foreground)]">
            Patch not available — the artifact may have been garbage collected
            from the patch store.
          </p>
        )}
        {state.kind === "error" && (
          <p className="text-xs text-red-700 dark:text-red-300">
            {state.message}
          </p>
        )}
        {state.kind === "loaded" && (
          <pre className="max-h-[60vh] overflow-auto rounded-md bg-[var(--color-muted)]/40 p-2 font-mono text-xs leading-snug whitespace-pre-wrap text-[var(--color-foreground)]">
            {state.diff}
          </pre>
        )}
      </div>
    </details>
  );
}

// truncateKey gives the disclosure header a short identifier when the
// patch key is a long MinIO-style object name (e.g.
// `patches/2026-05-15/4b8a…/diff.patch`). We keep the last 20 chars so
// the human-readable suffix (filename) is preserved.
function truncateKey(key: string): string {
  if (key.length <= 24) return key;
  return `…${key.slice(-22)}`;
}
