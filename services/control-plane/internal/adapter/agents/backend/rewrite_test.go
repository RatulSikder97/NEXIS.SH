package backend

// Rewrite mode exists so the model never authors hunk headers. These tests
// pin the two things that buys us: the generated diff is a faithful,
// applyable representation of the model's rewrite, and output that would
// smuggle in an unexpected file or an empty change is refused rather than
// passed downstream as a patch.

import (
	"strings"
	"testing"
)

const origPricing = `def unit_price(line_total, quantity):
    return line_total / quantity


def apply_discount(total, percent):
    return total - (total * percent / 100)
`

func TestDiffFromRewrite_ProducesAnApplyableDiff(t *testing.T) {
	fixed := strings.Replace(origPricing,
		"    return line_total / quantity",
		"    if quantity == 0:\n        return 0\n    return line_total / quantity", 1)

	content := `{"summary":"guard the zero-quantity line","files":[{"path":"src/pricing.py","content":` +
		quote(fixed) + `}]}`

	diff, files, summary, err := diffFromRewrite(content, map[string]string{"src/pricing.py": origPricing})
	if err != nil {
		t.Fatalf("diffFromRewrite: %v", err)
	}
	if len(files) != 1 || files[0] != "src/pricing.py" {
		t.Fatalf("files = %v", files)
	}
	if summary != "guard the zero-quantity line" {
		t.Errorf("summary = %q", summary)
	}
	for _, want := range []string{
		"diff --git a/src/pricing.py b/src/pricing.py",
		"--- a/src/pricing.py",
		"+++ b/src/pricing.py",
		"@@",
		"+    if quantity == 0:",
		"+        return 0",
		" def unit_price(line_total, quantity):", // context line, unchanged
	} {
		if !strings.Contains(diff, want) {
			t.Errorf("diff missing %q\n---\n%s", want, diff)
		}
	}
	// The untouched function must not appear as a change.
	if strings.Contains(diff, "-def apply_discount") {
		t.Errorf("diff rewrote an untouched function\n%s", diff)
	}
}

func TestDiffFromRewrite_Rejections(t *testing.T) {
	originals := map[string]string{"src/pricing.py": origPricing}
	cases := map[string]string{
		"not json":          "here is your patch: def unit_price...",
		"no files":          `{"summary":"x","files":[]}`,
		"unknown path":      `{"summary":"x","files":[{"path":"src/other.py","content":"x = 1\n"}]}`,
		"empty path":        `{"summary":"x","files":[{"path":"  ","content":"x = 1\n"}]}`,
		"identical content": `{"summary":"x","files":[{"path":"src/pricing.py","content":` + quote(origPricing) + `}]}`,
		"whitespace-only diff": `{"summary":"x","files":[{"path":"src/pricing.py","content":` +
			quote(strings.TrimRight(origPricing, "\n")) + `}]}`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, _, err := diffFromRewrite(content, originals); err == nil {
				t.Fatal("expected a rejection, got a patch")
			}
		})
	}
}

func TestDiffFromRewrite_ToleratesAFencedResponse(t *testing.T) {
	fixed := strings.Replace(origPricing, "percent / 100", "percent / 100.0", 1)
	content := "```json\n" + `{"summary":"float division","files":[{"path":"src/pricing.py","content":` +
		quote(fixed) + `}]}` + "\n```"

	diff, files, _, err := diffFromRewrite(content, map[string]string{"src/pricing.py": origPricing})
	if err != nil {
		t.Fatalf("fenced response rejected: %v", err)
	}
	if len(files) != 1 || !strings.Contains(diff, "+    return total - (total * percent / 100.0)") {
		t.Fatalf("unexpected diff:\n%s", diff)
	}
}

func TestNormalise_CRLFDoesNotRewriteEveryLine(t *testing.T) {
	crlf := strings.ReplaceAll(origPricing, "\n", "\r\n")
	content := `{"summary":"no-op","files":[{"path":"src/pricing.py","content":` + quote(crlf) + `}]}`
	if _, _, _, err := diffFromRewrite(content, map[string]string{"src/pricing.py": origPricing}); err == nil {
		t.Fatal("a CRLF-only echo should be treated as no change")
	}
}

// quote renders a Go string as a JSON string literal.
func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
