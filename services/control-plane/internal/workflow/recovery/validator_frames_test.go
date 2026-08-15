package recovery

// The Validator Sandbox console reads activity_events rows with
// agent_role='validator_l2'. Nothing emitted them, so the page reported
// "validator runs not yet emitted" no matter how many patches had been
// shadow-executed. These tests pin the two helpers behind those frames.

import (
	"strings"
	"testing"
)

func TestPatchSHA_IsStableShortAndContentAddressed(t *testing.T) {
	const diff = "diff --git a/x.py b/x.py\n--- a/x.py\n+++ b/x.py\n@@ -1 +1 @@\n-a\n+b\n"

	got := patchSHA(diff)
	if len(got) != 12 {
		t.Fatalf("patchSHA length = %d, want 12", len(got))
	}
	if got != patchSHA(diff) {
		t.Error("patchSHA is not deterministic")
	}
	if same := patchSHA(diff + " "); same == got {
		t.Error("patchSHA collided on different content")
	}
	// Hex only — the value is rendered raw in the console and used in URLs.
	for _, r := range got {
		if !strings.ContainsRune("0123456789abcdef", r) {
			t.Fatalf("patchSHA contains a non-hex rune %q in %q", r, got)
		}
	}
	// An empty diff still hashes rather than panicking; the caller guards on
	// patchDiff != "" but the helper must not be a trap.
	if patchSHA("") == "" {
		t.Error("patchSHA(\"\") returned empty")
	}
}

func TestHead_TruncatesWithoutSplittingRunes(t *testing.T) {
	if got := head("  short  ", 400); got != "short" {
		t.Errorf("head did not trim: %q", got)
	}

	long := strings.Repeat("x", 500)
	got := head(long, 400)
	if !strings.HasSuffix(got, "…") {
		t.Errorf("truncated value has no ellipsis: %q", got[len(got)-10:])
	}
	if n := len([]rune(got)); n != 401 { // 400 runes + the ellipsis
		t.Errorf("truncated rune count = %d, want 401", n)
	}

	// Multi-byte input: truncating by bytes here would emit invalid UTF-8 and
	// the payload would fail to marshal into the event row.
	multi := strings.Repeat("é", 500)
	out := head(multi, 400)
	if !strings.ContainsRune(out, '…') {
		t.Fatal("multi-byte input was not truncated")
	}
	for _, r := range out {
		if r == '�' {
			t.Fatal("truncation split a rune")
		}
	}
}
