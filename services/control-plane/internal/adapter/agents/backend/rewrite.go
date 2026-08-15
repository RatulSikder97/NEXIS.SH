package backend

// Whole-file rewrite mode.
//
// Asking a model for a unified diff makes it responsible for two things at
// once: deciding the change, and reproducing the surrounding lines and hunk
// offsets byte-for-byte. It reliably gets the first right and the second
// wrong — the diffs that came back named imports the file did not have, so
// `git apply` rejected them and the sandbox validated nothing.
//
// When we know the file's exact current text (it is indexed in pgvector), we
// can take the second job away: ask for the FULL updated file and compute the
// diff here, where it is a mechanical operation that cannot hallucinate.
//
// The output contract is unchanged — callers still get patch_diff +
// files_changed — so nothing downstream of the agent had to move.

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/pmezard/go-difflib/difflib"
)

// rewriteOutput is what the model returns in rewrite mode.
type rewriteOutput struct {
	Summary string `json:"summary"`
	Files   []struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	} `json:"files"`
}

// diffFromRewrite parses the model's JSON, diffs each returned file against
// the original text we supplied, and renders one unified diff for the lot.
//
// originals is path → current file content. A returned path that is not in
// originals is refused rather than treated as a new file: the agent is bound
// by the Architect's affected_files contract, and inventing a file here would
// bypass the check that forces HIGH severity for out-of-contract changes.
func diffFromRewrite(content string, originals map[string]string) (diff string, files []string, summary string, err error) {
	raw := strings.TrimSpace(stripFence(content))
	var out rewriteOutput
	if uerr := json.Unmarshal([]byte(raw), &out); uerr != nil {
		return "", nil, "", fmt.Errorf("rewrite output is not JSON: %w", uerr)
	}
	if len(out.Files) == 0 {
		return "", nil, "", fmt.Errorf("rewrite output listed no files")
	}

	var b strings.Builder
	for _, f := range out.Files {
		path := strings.TrimSpace(f.Path)
		if path == "" {
			return "", nil, "", fmt.Errorf("rewrite output has a file with no path")
		}
		orig, known := originals[path]
		if !known {
			return "", nil, "", fmt.Errorf("rewrite output names %q, which was not in the supplied file set", path)
		}
		if normalise(orig) == normalise(f.Content) {
			continue // unchanged file — nothing to emit
		}
		ud := difflib.UnifiedDiff{
			A:        difflib.SplitLines(normalise(orig)),
			B:        difflib.SplitLines(normalise(f.Content)),
			FromFile: "a/" + path,
			ToFile:   "b/" + path,
			Context:  3,
		}
		text, derr := difflib.GetUnifiedDiffString(ud)
		if derr != nil {
			return "", nil, "", fmt.Errorf("render diff for %s: %w", path, derr)
		}
		if strings.TrimSpace(text) == "" {
			continue
		}
		fmt.Fprintf(&b, "diff --git a/%s b/%s\n", path, path)
		b.WriteString(text)
		files = append(files, path)
	}

	if len(files) == 0 {
		return "", nil, "", fmt.Errorf("rewrite output changed nothing")
	}
	sort.Strings(files)
	return b.String(), files, strings.TrimSpace(out.Summary), nil
}

// normalise makes the two sides comparable: trailing whitespace on the final
// line is the most common source of a spurious one-line diff, and CRLF from a
// model that echoed Windows line endings would otherwise rewrite every line.
func normalise(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.TrimRight(s, "\n") + "\n"
}

// stripFence removes a ```json / ``` wrapper when the model adds one despite
// being told not to.
func stripFence(s string) string {
	t := strings.TrimSpace(s)
	if !strings.HasPrefix(t, "```") {
		return t
	}
	if i := strings.Index(t, "\n"); i >= 0 {
		t = t[i+1:]
	}
	if i := strings.LastIndex(t, "```"); i >= 0 {
		t = t[:i]
	}
	return strings.TrimSpace(t)
}
