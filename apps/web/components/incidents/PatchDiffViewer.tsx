"use client";

// Phase 7 — Patch diff viewer.
//
// Renders a "View patch" disclosure that lazily fetches the unified-diff
// text from the control-plane's patch-store proxy
// (`/v1/workspaces/{ws}/pipelines/{run}/patches/{patch_key}`) when the
// user expands the <details> element. The loaded diff renders in Monaco's
// DiffEditor: we reconstruct approximate original/modified texts from the
// unified diff (see lib/unified-diff.ts) so Monaco can paint a real
// two-pane view with per-language highlighting for single-file patches
// (multi-file patches fall back to one combined plaintext pane pair).
//
// Monaco brings ~2MB of editor + worker chunks, so — same as the eval
// detail page — we lazy-load via next/dynamic with { ssr: false }. The
// editor only ever mounts once state.kind === "loaded"; every other state
// (idle/loading/missing/error) renders the same lightweight markup as
// before.
//
// Lifecycle:
//   • collapsed       → no network traffic
//   • first expand    → fetch + cache in component state
//   • subsequent expand → re-show cached body
//   • 404 from BE     → render a muted "patch not available" message
//   • fetch error     → render the error string inline (no toast — the
//                       outer page already owns user-facing toasts)

import * as React from "react";
import dynamic from "next/dynamic";
import { ChevronDown, ChevronRight, FileDiff, Loader2 } from "lucide-react";
import { useTheme } from "next-themes";

import { pipelines } from "@/lib/pipelines";
import { inferLanguage, parseUnifiedDiff } from "@/lib/unified-diff";
import { cn } from "@/lib/utils";

const DiffEditor = dynamic(
  () => import("@monaco-editor/react").then((m) => m.DiffEditor),
  { ssr: false },
);

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
          message: err instanceof Error ? err.message : "Failed to load patch.",
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
        {state.kind === "loaded" && <PatchDiffEditor diff={state.diff} />}
      </div>
    </details>
  );
}

// PatchDiffEditor renders the fetched unified diff through Monaco's
// DiffEditor, mirroring the eval detail page's PatchDiffBlock integration
// (next/dynamic + ssr:false, readOnly, side-by-side, fixed 400px height so
// the editor can't steal the page's scroll). Unlike that block we feed
// Monaco reconstructed original/modified texts instead of the raw diff
// string, and we follow next-themes' resolved theme instead of hardcoding
// light — the component is client-only and mounts well after hydration
// (post fetch), so resolvedTheme is already settled by first paint.
function PatchDiffEditor({ diff }: { diff: string }) {
  const { resolvedTheme } = useTheme();
  const { original, modified, files } = React.useMemo(
    () => parseUnifiedDiff(diff),
    [diff],
  );
  const language = React.useMemo(() => inferLanguage(files), [files]);
  return (
    <div
      data-testid="patch-diff"
      className="overflow-hidden rounded-md border border-[var(--color-border)]"
    >
      <DiffEditor
        height="400px"
        language={language}
        theme={resolvedTheme === "dark" ? "vs-dark" : "light"}
        original={original}
        modified={modified}
        options={{
          readOnly: true,
          renderSideBySide: true,
          minimap: { enabled: false },
          scrollBeyondLastLine: false,
          fontSize: 12,
          wordWrap: "on",
        }}
      />
    </div>
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
