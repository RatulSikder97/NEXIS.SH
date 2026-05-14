package notifier

// Pure-rendering coverage for the slack notifier's helper functions. The
// SendBlock path requires a wired Slack provider with credentials; that is
// exercised under tests/integration. Here we cover trimRight + short +
// buildApprovalBlocks rendering branches.

import (
	"context"
	"strings"
	"testing"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// TestShort_TruncatesAt8 verifies the id-shortening helper covers both the
// long (truncated) and the short (passthrough) branches.
func TestShort_TruncatesAt8(t *testing.T) {
	long := "abcdef1234567890"
	if got := short(long); got != "abcdef12" {
		t.Fatalf("long: got %q want abcdef12", got)
	}
	if got := short("short"); got != "short" {
		t.Fatalf("short: got %q want short", got)
	}
	if got := short(""); got != "" {
		t.Fatalf("empty: got %q", got)
	}
}

// TestTrimRight walks every branch: trailing rune trimmed, no trailing
// rune left intact, empty string passes through.
func TestTrimRight(t *testing.T) {
	if got := trimRight("https://example.com/", '/'); got != "https://example.com" {
		t.Fatalf("trim slash: %q", got)
	}
	if got := trimRight("https://example.com", '/'); got != "https://example.com" {
		t.Fatalf("no trim: %q", got)
	}
	if got := trimRight("", '/'); got != "" {
		t.Fatalf("empty: %q", got)
	}
	// Non-matching last rune passes through.
	if got := trimRight("foo!", '/'); got != "foo!" {
		t.Fatalf("no-match: %q", got)
	}
}

// TestBuildApprovalBlocks_HappyPath verifies the rendered block kit JSON
// contains the headline, scenario, severity, short run id, and the deep
// link URL. The block-kit shape is intentionally not asserted byte-for-byte —
// we just confirm every interpolation point landed in the output.
func TestBuildApprovalBlocks_HappyPath(t *testing.T) {
	n := domain.Notification{
		OrgID:         "org-1",
		WorkflowRunID: "abcdef1234567890",
		Severity:      domain.SeverityHigh,
		Scenario:      "null-deref",
		Title:         "Approval needed",
		Body:          "Pipeline parked",
		LinkURL:       "https://app.example.com/console/approvals/abc",
	}
	blocks := buildApprovalBlocks(n, "https://app.example.com")
	if len(blocks) != 4 {
		t.Fatalf("expected 4 blocks, got %d", len(blocks))
	}
	// Header block must carry the title.
	header := blocks[0]["text"].(map[string]any)
	if header["text"] != "Approval needed" {
		t.Fatalf("header text: %v", header["text"])
	}
	// Run id must be shortened to 8 chars in the section fields.
	concat := concatBlockText(blocks)
	if !strings.Contains(concat, "abcdef12") {
		t.Fatalf("short run id missing: %s", concat)
	}
	if strings.Contains(concat, "abcdef1234567890") {
		t.Fatalf("full id leaked: %s", concat)
	}
	// Severity must surface verbatim.
	if !strings.Contains(concat, "high") {
		t.Fatalf("severity missing: %s", concat)
	}
	// Action button URL must match the explicit LinkURL.
	actions := blocks[3]["elements"].([]map[string]any)
	if actions[0]["url"] != "https://app.example.com/console/approvals/abc" {
		t.Fatalf("link url: %v", actions[0]["url"])
	}
}

// TestBuildApprovalBlocks_DefaultsFallBackToConsoleBase — when LinkURL is
// empty, the helper falls back to baseURL + /console/approvals/<run_id>,
// stripping any trailing slash.
func TestBuildApprovalBlocks_DefaultsFallBackToConsoleBase(t *testing.T) {
	n := domain.Notification{
		WorkflowRunID: "wf-x",
	}
	blocks := buildApprovalBlocks(n, "https://app.example.com/")
	actions := blocks[3]["elements"].([]map[string]any)
	want := "https://app.example.com/console/approvals/wf-x"
	if actions[0]["url"] != want {
		t.Fatalf("fallback url: %v want %q", actions[0]["url"], want)
	}
}

// TestBuildApprovalBlocks_EmptyTitleAndBodyDefaults — when title/body are
// empty, the helper injects the canonical default strings so the message
// is never blank.
func TestBuildApprovalBlocks_EmptyTitleAndBodyDefaults(t *testing.T) {
	n := domain.Notification{
		WorkflowRunID: "wf-z",
		Scenario:      "drift",
	}
	blocks := buildApprovalBlocks(n, "https://app.example.com")
	header := blocks[0]["text"].(map[string]any)
	if !strings.Contains(header["text"].(string), "drift") {
		t.Fatalf("scenario not in default title: %v", header["text"])
	}
	concat := concatBlockText(blocks)
	if !strings.Contains(concat, "Pipeline is parked at the Approval Gate") {
		t.Fatalf("default body missing: %s", concat)
	}
}

// concatBlockText flattens every visible text field in the block list into a
// single string. Used as a coarse-grained search target so the tests above
// can grep for interpolated values without hand-walking the schema.
func concatBlockText(blocks []map[string]any) string {
	var sb strings.Builder
	for _, b := range blocks {
		if t, ok := b["text"].(map[string]any); ok {
			if s, ok := t["text"].(string); ok {
				sb.WriteString(s)
				sb.WriteRune(' ')
			}
		}
		if fields, ok := b["fields"].([]map[string]any); ok {
			for _, f := range fields {
				if s, ok := f["text"].(string); ok {
					sb.WriteString(s)
					sb.WriteRune(' ')
				}
			}
		}
		if elements, ok := b["elements"].([]map[string]any); ok {
			for _, e := range elements {
				if t, ok := e["text"].(map[string]any); ok {
					if s, ok := t["text"].(string); ok {
						sb.WriteString(s)
						sb.WriteRune(' ')
					}
				}
				if u, ok := e["url"].(string); ok {
					sb.WriteString(u)
					sb.WriteRune(' ')
				}
			}
		}
	}
	return sb.String()
}

// TestSlackNotifier_SendErrorsWhenNotConfigured — calling Send on a Slack
// notifier with a nil provider returns an explicit error so the multi
// fanout layer can log and continue.
func TestSlackNotifier_SendErrorsWhenNotConfigured(t *testing.T) {
	s := &Slack{prov: nil}
	err := s.Send(context.Background(), domain.Notification{OrgID: "org-1"})
	if err == nil {
		t.Fatalf("expected error when provider is nil")
	}
}

// TestSlackNotifier_Channel returns the canonical "slack" name.
func TestSlackNotifier_Channel(t *testing.T) {
	s := &Slack{}
	if got := s.Channel(); got != "slack" {
		t.Fatalf("channel: %q", got)
	}
}
