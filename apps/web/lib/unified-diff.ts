// Phase 7 — Unified-diff → two-sides reconstruction for Monaco's DiffEditor.
//
// Monaco's DiffEditor wants full `original` + `modified` texts and computes
// the diff itself, but the patch store hands us a single unified-diff
// string. This module reconstructs the two sides from the hunks.
//
// Scope: the diffs rendered here are produced exclusively by our own
// Backend agent (see services/control-plane/internal/adapter/agents/
// backend/patch_extract.go) — git-style unified diffs with `diff --git` /
// `--- a/…` / `+++ b/…` file headers and standard `@@ -l,s +l,s @@` hunk
// headers, possibly spanning multiple files. We deliberately do NOT try to
// handle arbitrary git edge cases (binary patches, mode-only changes,
// copy/rename score lines) — those never come out of ExtractDiff.
//
// Reconstruction rules:
//   • context lines (" x")  → both sides
//   • removals      ("-x")  → original only
//   • additions     ("+x")  → modified only
//   • hunk headers          → echoed verbatim on BOTH sides so elided
//     context reads as a visible separator and the panes stay aligned
//     (Monaco renders identical lines as unchanged)
//   • multi-file diffs      → a `+++ b/<path>` banner echoed on both sides
//     at each file boundary; per the brief we render one combined pane
//     pair rather than splitting per-file editors
//   • "\ No newline at end of file" markers → dropped
//
// Hunk bodies are consumed count-driven: the `@@ -l,s +l,s @@` header says
// exactly how many old-side and new-side lines follow, so a removed line
// whose content itself starts with "-- " (a SQL comment, rendering as
// "--- x") can't be mistaken for a file header — standard patch semantics.
// If the counts are wrong (LLM-authored diffs occasionally are) the
// structural markers `diff --git` and `@@` still hard-terminate the hunk,
// and any line that matches none of the body prefixes ends it too.

const HUNK_RE = /^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@/;

export type ParsedDiff = {
  original: string;
  modified: string;
  /** File paths mined from `+++ b/<path>` headers, in order of appearance. */
  files: string[];
};

type FileSection = {
  path: string | null;
  originalLines: string[];
  modifiedLines: string[];
};

/**
 * parseUnifiedDiff reconstructs approximate original/modified texts from a
 * unified diff. When the input contains no recognisable hunks (defensive
 * path — ExtractDiff should never let that through) we fall back to an
 * empty original and the raw text as modified, so the viewer still shows
 * the content instead of a blank pane.
 */
export function parseUnifiedDiff(diff: string): ParsedDiff {
  const lines = diff.split("\n");
  const sections: FileSection[] = [];
  let current: FileSection | null = null;
  // Old/new-side lines still owed to the current hunk per its @@ header.
  let remainingOld = 0;
  let remainingNew = 0;
  let sawHunk = false;

  const ensureSection = (): FileSection => {
    if (!current) {
      current = { path: null, originalLines: [], modifiedLines: [] };
      sections.push(current);
    }
    return current;
  };

  for (const line of lines) {
    const inHunk = remainingOld > 0 || remainingNew > 0;

    // Structural markers end any hunk regardless of remaining counts —
    // this is the safety valve for miscounted LLM-authored hunk headers.
    const hunk = HUNK_RE.exec(line);
    if (hunk) {
      const section = ensureSection();
      remainingOld = hunk[2] !== undefined ? parseInt(hunk[2], 10) : 1;
      remainingNew = hunk[4] !== undefined ? parseInt(hunk[4], 10) : 1;
      sawHunk = true;
      // Echo the header on both sides — identical on both panes, so Monaco
      // renders it as an unchanged separator between hunks.
      section.originalLines.push(line);
      section.modifiedLines.push(line);
      continue;
    }
    if (line.startsWith("diff --git ")) {
      current = { path: null, originalLines: [], modifiedLines: [] };
      sections.push(current);
      remainingOld = 0;
      remainingNew = 0;
      continue;
    }

    if (inHunk) {
      if (line.startsWith("\\")) {
        // "\ No newline at end of file"
        continue;
      }
      const section = ensureSection();
      if (line.startsWith("-") && remainingOld > 0) {
        section.originalLines.push(line.slice(1));
        remainingOld--;
        continue;
      }
      if (line.startsWith("+") && remainingNew > 0) {
        section.modifiedLines.push(line.slice(1));
        remainingNew--;
        continue;
      }
      if (line.startsWith(" ") || line === "") {
        // Context line. A fully-empty line inside a hunk is an empty
        // context line whose trailing space was trimmed in transit.
        const body = line === "" ? "" : line.slice(1);
        section.originalLines.push(body);
        section.modifiedLines.push(body);
        remainingOld--;
        remainingNew--;
        continue;
      }
      // Unrecognised line — the hunk is over despite unexhausted counts;
      // fall through to the non-hunk handling below for this same line.
      remainingOld = 0;
      remainingNew = 0;
    }

    if (line.startsWith("+++ ")) {
      const section = ensureSection();
      const path = line.replace(/^\+\+\+ (?:b\/)?/, "");
      section.path = path === "/dev/null" ? null : path;
      continue;
    }
    // Everything else between hunks — `--- a/…`, `index …`, mode lines,
    // stray prose — is skipped. The new-side `+++` header is authoritative
    // for the path (matches patch_extract.go, which only reads `+++ b/`).
  }

  if (!sawHunk) {
    return { original: "", modified: diff, files: [] };
  }

  const populated = sections.filter(
    (s) => s.originalLines.length > 0 || s.modifiedLines.length > 0,
  );
  const files = populated
    .map((s) => s.path)
    .filter((p): p is string => p !== null);
  const multiFile = populated.length > 1;

  const originalParts: string[] = [];
  const modifiedParts: string[] = [];
  for (const section of populated) {
    if (multiFile) {
      const banner = `+++ ${section.path ?? "(unknown file)"}`;
      originalParts.push(banner);
      modifiedParts.push(banner);
    }
    originalParts.push(...section.originalLines);
    modifiedParts.push(...section.modifiedLines);
  }

  return {
    original: originalParts.join("\n"),
    modified: modifiedParts.join("\n"),
    files,
  };
}

// Extension → Monaco language id. Deliberately small: the Backend agent
// patches application code in this monorepo, so this covers the stacks we
// actually generate diffs for. Anything else falls back to plaintext.
const LANGUAGE_BY_EXT: Record<string, string> = {
  ts: "typescript",
  tsx: "typescript",
  mts: "typescript",
  cts: "typescript",
  js: "javascript",
  jsx: "javascript",
  mjs: "javascript",
  cjs: "javascript",
  go: "go",
  py: "python",
  json: "json",
  css: "css",
  scss: "scss",
  html: "html",
  sql: "sql",
  yaml: "yaml",
  yml: "yaml",
  md: "markdown",
  sh: "shell",
  bash: "shell",
  tf: "hcl",
  java: "java",
  rs: "rust",
};

/**
 * inferLanguage picks a Monaco language mode for the reconstructed panes.
 * Single-file diff → language by extension; multi-file (or unknown) →
 * "plaintext", since one editor pair can only carry one language mode.
 */
export function inferLanguage(files: string[]): string {
  if (files.length !== 1) return "plaintext";
  const ext = files[0].split(".").pop()?.toLowerCase() ?? "";
  return LANGUAGE_BY_EXT[ext] ?? "plaintext";
}
