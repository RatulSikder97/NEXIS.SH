package usecase

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseUnifiedDiff_SingleFileAppendOnly(t *testing.T) {
	diff := strings.Join([]string{
		"diff --git a/foo.txt b/foo.txt",
		"--- a/foo.txt",
		"+++ b/foo.txt",
		"@@ -1,2 +1,3 @@",
		" line1",
		" line2",
		"+line3",
		"",
	}, "\n")
	baseline := "line1\nline2\n"
	files, err := parseUnifiedDiff(diff, func(path string) (string, error) {
		require.Equal(t, "foo.txt", path)
		return baseline, nil
	})
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Equal(t, "foo.txt", files[0].Path)
	require.Equal(t, "line1\nline2\nline3\n", files[0].NewBody)
}

func TestParseUnifiedDiff_NewFileFromDevNull(t *testing.T) {
	diff := strings.Join([]string{
		"diff --git a/new.txt b/new.txt",
		"new file mode 100644",
		"--- /dev/null",
		"+++ b/new.txt",
		"@@ -0,0 +1,2 @@",
		"+hello",
		"+world",
		"",
	}, "\n")
	files, err := parseUnifiedDiff(diff, func(path string) (string, error) {
		t.Fatalf("baseline fetch should NOT be called for new files; got %s", path)
		return "", nil
	})
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.True(t, files[0].IsNew)
	require.Equal(t, "hello\nworld\n", files[0].NewBody)
}

func TestParseUnifiedDiff_MultiFile(t *testing.T) {
	diff := strings.Join([]string{
		"diff --git a/a.txt b/a.txt",
		"--- a/a.txt",
		"+++ b/a.txt",
		"@@ -1 +1,2 @@",
		" a",
		"+b",
		"diff --git a/c.txt b/c.txt",
		"new file mode 100644",
		"--- /dev/null",
		"+++ b/c.txt",
		"@@ -0,0 +1 @@",
		"+c",
		"",
	}, "\n")
	files, err := parseUnifiedDiff(diff, func(path string) (string, error) {
		switch path {
		case "a.txt":
			return "a\n", nil
		}
		t.Fatalf("unexpected path %s", path)
		return "", nil
	})
	require.NoError(t, err)
	require.Len(t, files, 2)
	require.Equal(t, "a\nb\n", files[0].NewBody)
	require.Equal(t, "c\n", files[1].NewBody)
}

func TestParseUnifiedDiff_RejectsBinary(t *testing.T) {
	diff := strings.Join([]string{
		"diff --git a/img.png b/img.png",
		"Binary files a/img.png and b/img.png differ",
	}, "\n")
	_, err := parseUnifiedDiff(diff, func(string) (string, error) { return "", nil })
	require.ErrorIs(t, err, errBadDiff)
	require.Contains(t, err.Error(), "binary")
}

func TestParseUnifiedDiff_RejectsRename(t *testing.T) {
	diff := strings.Join([]string{
		"diff --git a/old.txt b/new.txt",
		"similarity index 100%",
		"rename from old.txt",
		"rename to new.txt",
	}, "\n")
	_, err := parseUnifiedDiff(diff, func(string) (string, error) { return "", nil })
	require.ErrorIs(t, err, errBadDiff)
}

func TestParseUnifiedDiff_RejectsModeChange(t *testing.T) {
	diff := strings.Join([]string{
		"diff --git a/x.sh b/x.sh",
		"old mode 100644",
		"new mode 100755",
		"@@ -1 +1 @@",
		" x",
	}, "\n")
	_, err := parseUnifiedDiff(diff, func(string) (string, error) { return "x\n", nil })
	require.ErrorIs(t, err, errBadDiff)
	require.Contains(t, err.Error(), "mode change")
}

func TestParseUnifiedDiff_ContextMismatchFails(t *testing.T) {
	diff := strings.Join([]string{
		"diff --git a/foo.txt b/foo.txt",
		"--- a/foo.txt",
		"+++ b/foo.txt",
		"@@ -1,1 +1,2 @@",
		" something_else",
		"+added",
	}, "\n")
	_, err := parseUnifiedDiff(diff, func(string) (string, error) { return "actual_line\n", nil })
	require.Error(t, err)
	require.True(t, errors.Is(err, errBadDiff) || strings.Contains(err.Error(), "mismatch"))
}

func TestParseUnifiedDiff_EmptyDiff(t *testing.T) {
	_, err := parseUnifiedDiff("", func(string) (string, error) { return "", nil })
	require.ErrorIs(t, err, errBadDiff)
}

func TestParseHunkHeader(t *testing.T) {
	cases := []struct {
		in    string
		old   int
		neW   int
		errOK bool
	}{
		{"@@ -1,3 +1,4 @@", 1, 1, false},
		{"@@ -0,0 +1 @@", 0, 1, false},
		{"@@ -10 +12,5 @@ func foo()", 10, 12, false},
		{"not a header", 0, 0, true},
	}
	for _, tc := range cases {
		o, n, err := parseHunkHeader(tc.in)
		if tc.errOK {
			require.Error(t, err)
			continue
		}
		require.NoError(t, err, tc.in)
		require.Equal(t, tc.old, o, tc.in)
		require.Equal(t, tc.neW, n, tc.in)
	}
}

// TestParseUnifiedDiff_RelocatesOnContextDrift covers the LLM-authored diff
// shape: the deleted line is quoted exactly, but the surrounding context was
// half-remembered (req.args.get vs req.get) and the line numbers are off.
// The edit is correct, so it must apply — and the drifted context lines must
// come back as the baseline had them, not as the diff imagined them.
func TestParseUnifiedDiff_RelocatesOnContextDrift(t *testing.T) {
	baseline := strings.Join([]string{
		"def handler(req):",
		`    hits = int(req.get("hits", 0))`,
		`    total = int(req.get("total", 0))`,
		"",
		`    return safe_div(hits, total).quantize(Decimal("0.01"))`,
		"",
	}, "\n")
	diff := strings.Join([]string{
		"diff --git a/api.py b/api.py",
		"--- a/api.py",
		"+++ b/api.py",
		"@@ -21,7 +21,9 @@ def handler(req):",
		`     hits = int(req.args.get("hits", 0))`,
		`     total = int(req.args.get("total", 0))`,
		`-    return safe_div(hits, total).quantize(Decimal("0.01"))`,
		"+    result = safe_div(hits, total)",
		"+    if result is None:",
		`+        raise ValueError("total must not be zero")`,
		`+    return result.quantize(Decimal("0.01"))`,
	}, "\n")

	out, err := parseUnifiedDiff(diff, func(string) (string, error) { return baseline, nil })
	require.NoError(t, err)
	require.Len(t, out, 1)
	body := out[0].NewBody
	require.Contains(t, body, "if result is None:")
	// The buggy line is gone...
	require.NotContains(t, body, `return safe_div(hits, total).quantize`)
	// ...and the drifted context kept the file's real text, not the diff's.
	require.Contains(t, body, `hits = int(req.get("hits", 0))`)
	require.NotContains(t, body, "req.args.get")
}

// TestParseUnifiedDiff_RefusesUnseenDeletion guards the asymmetry: a "-" line
// that does not exist in the baseline must never be relocated onto some other
// line, because applying it would delete code the patch never described.
func TestParseUnifiedDiff_RefusesUnseenDeletion(t *testing.T) {
	diff := strings.Join([]string{
		"diff --git a/foo.txt b/foo.txt",
		"--- a/foo.txt",
		"+++ b/foo.txt",
		"@@ -1,2 +1,2 @@",
		" keep_me",
		"-line_that_does_not_exist",
		"+replacement",
	}, "\n")
	_, err := parseUnifiedDiff(diff, func(string) (string, error) {
		return "keep_me\nsomething_real\n", nil
	})
	require.Error(t, err)
	require.ErrorIs(t, err, errBadDiff)
}
