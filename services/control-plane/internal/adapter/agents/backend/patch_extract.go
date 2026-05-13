package backend

import (
	"errors"
	"regexp"
	"strings"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

var diffBlockRe = regexp.MustCompile("(?s)```diff\\s*\\n(.*?)```")
var fileHeaderRe = regexp.MustCompile(`(?m)^\+\+\+ b/(.+)$`)

// ExtractDiff yanks the canonical unified diff out of the model's response.
// First tries a fenced ```diff … ``` block; falls back to scanning for a
// leading "diff --git" header. Returns ErrAgentSchemaMismatch when no diff
// shape is recognised or when the diff has no file headers.
func ExtractDiff(content string) (diff string, files []string, err error) {
	m := diffBlockRe.FindStringSubmatch(content)
	switch {
	case m != nil:
		diff = strings.TrimSpace(m[1])
	case strings.Contains(content, "diff --git"):
		idx := strings.Index(content, "diff --git")
		diff = strings.TrimSpace(content[idx:])
	default:
		return "", nil, errors.Join(domain.ErrAgentSchemaMismatch,
			errors.New("no ```diff``` block or 'diff --git' header found"))
	}
	files = extractFiles(diff)
	if len(files) == 0 {
		return diff, nil, errors.Join(domain.ErrAgentSchemaMismatch,
			errors.New("diff contains no '+++ b/<path>' file headers"))
	}
	return diff, files, nil
}

func extractFiles(diff string) []string {
	matches := fileHeaderRe.FindAllStringSubmatch(diff, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, m[1])
	}
	return out
}

// SummaryFromContent extracts the leading prose (up to 200 chars) above the
// first diff block. Used to populate the "summary" field of the Structured
// payload when the model includes explanatory text.
func SummaryFromContent(content string) string {
	idx := strings.Index(content, "```diff")
	if idx <= 0 {
		idx = strings.Index(content, "diff --git")
	}
	if idx <= 0 {
		return ""
	}
	s := strings.TrimSpace(content[:idx])
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}
