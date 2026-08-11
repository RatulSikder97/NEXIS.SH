import { describe, it, expect } from "vitest";

import { inferLanguage, parseUnifiedDiff } from "@/lib/unified-diff";

// The shapes under test mirror what the Backend agent's ExtractDiff
// (services/control-plane/internal/adapter/agents/backend/patch_extract.go)
// actually emits: git-style unified diffs with `diff --git` + `--- a/` +
// `+++ b/` headers and `@@ -l,s +l,s @@` hunks, single- or multi-file.

const SINGLE_FILE_DIFF = [
  "diff --git a/src/handler.go b/src/handler.go",
  "index 3f9c2ab..8d1e4f0 100644",
  "--- a/src/handler.go",
  "+++ b/src/handler.go",
  "@@ -10,7 +10,8 @@ func Handle(w http.ResponseWriter, r *http.Request) {",
  " \tctx := r.Context()",
  "-\tres, err := svc.Do(ctx)",
  "+\tres, err := svc.DoWithRetry(ctx, 3)",
  '+\tmetrics.Inc("handler_retry")',
  " \tif err != nil {",
  " \t\thttp.Error(w, err.Error(), 500)",
  " \t\treturn",
  " \t}",
].join("\n");

const MULTI_FILE_DIFF = [
  "diff --git a/a.ts b/a.ts",
  "--- a/a.ts",
  "+++ b/a.ts",
  "@@ -1,2 +1,2 @@",
  " const x = 1;",
  "-export default x;",
  "+export default x + 1;",
  "diff --git a/b.sql b/b.sql",
  "--- a/b.sql",
  "+++ b/b.sql",
  "@@ -1,2 +1,1 @@",
  "--- drop the legacy comment",
  " SELECT 1;",
].join("\n");

describe("parseUnifiedDiff", () => {
  it("splits a single-file diff into original/modified sides", () => {
    const { original, modified, files } = parseUnifiedDiff(SINGLE_FILE_DIFF);
    expect(files).toEqual(["src/handler.go"]);

    // Removal lands only on the original side.
    expect(original).toContain("res, err := svc.Do(ctx)");
    expect(modified).not.toContain("res, err := svc.Do(ctx)");

    // Additions land only on the modified side.
    expect(modified).toContain("res, err := svc.DoWithRetry(ctx, 3)");
    expect(modified).toContain('metrics.Inc("handler_retry")');
    expect(original).not.toContain("DoWithRetry");

    // Context lines land on both, with the leading marker stripped.
    expect(original).toContain("\tctx := r.Context()");
    expect(modified).toContain("\tctx := r.Context()");
    expect(original).not.toContain(" \tctx");

    // Hunk header is echoed on both sides so it renders as an unchanged
    // separator, and file headers never leak into the panes.
    const header = "@@ -10,7 +10,8 @@";
    expect(original).toContain(header);
    expect(modified).toContain(header);
    expect(original).not.toContain("+++ b/");
    expect(modified).not.toContain("diff --git");
  });

  it("handles multi-file diffs with aligned per-file banners", () => {
    const { original, modified, files } = parseUnifiedDiff(MULTI_FILE_DIFF);
    expect(files).toEqual(["a.ts", "b.sql"]);
    expect(original).toContain("+++ a.ts");
    expect(original).toContain("+++ b.sql");
    expect(modified).toContain("+++ a.ts");
    expect(modified).toContain("+++ b.sql");
    expect(original).toContain("export default x;");
    expect(modified).toContain("export default x + 1;");
  });

  it("treats a removed SQL comment ('--- x') as a removal, not a header", () => {
    // b.sql's hunk removes the line "-- drop the legacy comment", which
    // renders as "--- drop the legacy comment" — count-driven parsing must
    // route it to the original side instead of ending the hunk.
    const { original, modified } = parseUnifiedDiff(MULTI_FILE_DIFF);
    expect(original).toContain("-- drop the legacy comment");
    expect(modified).not.toContain("drop the legacy comment");
    expect(original).toContain("SELECT 1;");
    expect(modified).toContain("SELECT 1;");
  });

  it("supports omitted hunk counts (@@ -l +l @@ means one line each)", () => {
    const diff = [
      "--- a/x.txt",
      "+++ b/x.txt",
      "@@ -1 +1 @@",
      "-old",
      "+new",
    ].join("\n");
    const { original, modified, files } = parseUnifiedDiff(diff);
    expect(files).toEqual(["x.txt"]);
    expect(original).toContain("old");
    expect(original).not.toContain("new");
    expect(modified).toContain("new");
    expect(modified).not.toContain("old");
  });

  it("drops '\\ No newline at end of file' markers", () => {
    const diff = [
      "--- a/x.txt",
      "+++ b/x.txt",
      "@@ -1 +1 @@",
      "-old",
      "\\ No newline at end of file",
      "+new",
      "\\ No newline at end of file",
    ].join("\n");
    const { original, modified } = parseUnifiedDiff(diff);
    expect(original).not.toContain("No newline");
    expect(modified).not.toContain("No newline");
    expect(original).toContain("old");
    expect(modified).toContain("new");
  });

  it("handles new-file diffs (--- /dev/null) without a phantom path", () => {
    const diff = [
      "diff --git a/new.py b/new.py",
      "--- /dev/null",
      "+++ b/new.py",
      "@@ -0,0 +1,2 @@",
      "+import os",
      "+print(os.getcwd())",
    ].join("\n");
    const { original, modified, files } = parseUnifiedDiff(diff);
    expect(files).toEqual(["new.py"]);
    expect(modified).toContain("import os");
    // Original side carries only the echoed hunk header.
    expect(original.trim()).toBe("@@ -0,0 +1,2 @@");
  });

  it("falls back to raw text on the modified side when no hunks exist", () => {
    const notADiff = "the model said something that is not a diff";
    const { original, modified, files } = parseUnifiedDiff(notADiff);
    expect(original).toBe("");
    expect(modified).toBe(notADiff);
    expect(files).toEqual([]);
  });
});

describe("inferLanguage", () => {
  it("maps a single file's extension to a Monaco language id", () => {
    expect(inferLanguage(["src/handler.go"])).toBe("go");
    expect(inferLanguage(["app/page.tsx"])).toBe("typescript");
    expect(inferLanguage(["migrations/001.sql"])).toBe("sql");
  });

  it("falls back to plaintext for multi-file, unknown, or empty inputs", () => {
    expect(inferLanguage(["a.ts", "b.sql"])).toBe("plaintext");
    expect(inferLanguage(["weird.xyz"])).toBe("plaintext");
    expect(inferLanguage([])).toBe("plaintext");
  });
});
