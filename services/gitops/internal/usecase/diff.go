package usecase

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// errBadDiff signals a patch shape the simple unified-diff parser refuses
// to apply — binary, rename, mode change, missing hunk header, etc. The
// caller maps this to a 400 response.
var errBadDiff = errors.New("gitops: unsupported diff shape")

// fileChange captures the post-state of one file in the diff. NewBody is
// the full file content after applying every hunk in the diff to the
// pre-existing baseline. IsNew is true when the original side of the diff
// is /dev/null.
type fileChange struct {
	Path    string
	NewBody string
	IsNew   bool
}

// parseUnifiedDiff walks a unified-diff blob and yields one fileChange per
// "diff --git a/X b/X" section. The function is intentionally restrictive:
// it rejects renames, binary patches, mode changes, and any hunk that adds
// "\ No newline at end of file" markers. fetchBaseline is invoked to load
// the current contents of a file before applying additive hunks; when
// IsNew is true the baseline is "".
//
// Returns errBadDiff on any unsupported shape; nil on success.
func parseUnifiedDiff(diff string, fetchBaseline func(path string) (string, error)) ([]fileChange, error) {
	if diff == "" {
		return nil, errBadDiff
	}
	lines := strings.Split(diff, "\n")
	var out []fileChange

	i := 0
	for i < len(lines) {
		ln := lines[i]
		if !strings.HasPrefix(ln, "diff --git ") {
			i++
			continue
		}
		parts := strings.Fields(ln)
		if len(parts) < 4 {
			return nil, fmt.Errorf("%w: malformed diff header: %s", errBadDiff, ln)
		}
		oldPath := strings.TrimPrefix(parts[2], "a/")
		newPath := strings.TrimPrefix(parts[3], "b/")
		if oldPath != newPath {
			return nil, fmt.Errorf("%w: rename %s → %s not supported", errBadDiff, oldPath, newPath)
		}

		i++ // consume the diff --git line

		isNew := false
		// Walk preamble lines until we hit "@@" (a hunk header) or the
		// next "diff --git". Reject unsupported preamble markers along
		// the way.
		for i < len(lines) {
			cur := lines[i]
			switch {
			case strings.HasPrefix(cur, "@@"):
				goto hunks
			case strings.HasPrefix(cur, "diff --git "):
				return nil, fmt.Errorf("%w: file %s has no hunks", errBadDiff, newPath)
			case strings.HasPrefix(cur, "Binary "):
				return nil, fmt.Errorf("%w: binary patch on %s", errBadDiff, newPath)
			case strings.HasPrefix(cur, "rename "):
				return nil, fmt.Errorf("%w: rename detected on %s", errBadDiff, newPath)
			case strings.HasPrefix(cur, "similarity index"),
				strings.HasPrefix(cur, "dissimilarity index"),
				strings.HasPrefix(cur, "copy from"),
				strings.HasPrefix(cur, "copy to"):
				return nil, fmt.Errorf("%w: copy detected on %s", errBadDiff, newPath)
			case strings.HasPrefix(cur, "deleted file mode"):
				return nil, fmt.Errorf("%w: deletion on %s", errBadDiff, newPath)
			case strings.HasPrefix(cur, "new file mode"):
				isNew = true
			case strings.HasPrefix(cur, "old mode"),
				strings.HasPrefix(cur, "new mode"):
				return nil, fmt.Errorf("%w: mode change on %s", errBadDiff, newPath)
			case strings.HasPrefix(cur, "--- "):
				if cur == "--- /dev/null" {
					isNew = true
				}
			case strings.HasPrefix(cur, "+++ "):
				// no-op
			case strings.HasPrefix(cur, "index "):
				// no-op
			default:
				// Unknown header — be permissive.
			}
			i++
		}
		return nil, fmt.Errorf("%w: file %s has no hunks", errBadDiff, newPath)

	hunks:
		var baseline string
		if !isNew {
			b, err := fetchBaseline(newPath)
			if err != nil {
				return nil, fmt.Errorf("baseline fetch %s: %w", newPath, err)
			}
			baseline = b
		}
		baseLines := splitLines(baseline)
		var built []string
		cursor := 0 // 1-based old-file line cursor as we apply hunks

		for i < len(lines) && strings.HasPrefix(lines[i], "@@") {
			hdr := lines[i]
			oldStart, _, err := parseHunkHeader(hdr)
			if err != nil {
				return nil, fmt.Errorf("%w: %s", errBadDiff, hdr)
			}
			// Append any unchanged lines between the previous hunk's
			// cursor and this hunk's old-start. Hunks against /dev/null
			// (new files) declare "-0,0" — we clamp to the current cursor
			// so the empty baseline path still walks through cleanly.
			oldIdx := oldStart - 1 // 0-based; "-0,0" produces -1 here
			if oldIdx < 0 {
				oldIdx = cursor
			}
			if oldIdx < cursor {
				return nil, fmt.Errorf("%w: hunk overlap at %s", errBadDiff, hdr)
			}
			for cursor < oldIdx && cursor < len(baseLines) {
				built = append(built, baseLines[cursor])
				cursor++
			}

			i++ // consume hunk header
			for i < len(lines) {
				cur := lines[i]
				if strings.HasPrefix(cur, "@@") || strings.HasPrefix(cur, "diff --git ") {
					break
				}
				if cur == "\\ No newline at end of file" {
					return nil, fmt.Errorf("%w: \\ No newline at end of file marker on %s", errBadDiff, newPath)
				}
				if cur == "" && i == len(lines)-1 {
					// Trailing empty line from the final "\n" split; skip.
					i++
					continue
				}
				switch {
				case strings.HasPrefix(cur, "+++"), strings.HasPrefix(cur, "---"):
					// Header inside a hunk — defensive; treat as opaque.
					i++
				case strings.HasPrefix(cur, "+"):
					built = append(built, cur[1:])
					i++
				case strings.HasPrefix(cur, "-"):
					if cursor >= len(baseLines) {
						return nil, fmt.Errorf("%w: deletion past EOF on %s", errBadDiff, newPath)
					}
					if baseLines[cursor] != cur[1:] {
						return nil, fmt.Errorf("%w: context mismatch on %s line %d (have %q, diff %q)",
							errBadDiff, newPath, cursor+1, baseLines[cursor], cur[1:])
					}
					cursor++
					i++
				case strings.HasPrefix(cur, " "):
					if cursor >= len(baseLines) {
						return nil, fmt.Errorf("%w: context past EOF on %s", errBadDiff, newPath)
					}
					if baseLines[cursor] != cur[1:] {
						return nil, fmt.Errorf("%w: context mismatch on %s line %d (have %q, diff %q)",
							errBadDiff, newPath, cursor+1, baseLines[cursor], cur[1:])
					}
					built = append(built, cur[1:])
					cursor++
					i++
				default:
					// Unknown line inside hunk — could be a stray blank
					// or a malformed header. Treat as context.
					if cursor >= len(baseLines) {
						return nil, fmt.Errorf("%w: hunk extends past EOF on %s", errBadDiff, newPath)
					}
					built = append(built, baseLines[cursor])
					cursor++
					i++
				}
			}
		}

		// Append any trailing baseline lines after the last hunk.
		for cursor < len(baseLines) {
			built = append(built, baseLines[cursor])
			cursor++
		}

		newBody := strings.Join(built, "\n")
		if !strings.HasSuffix(newBody, "\n") {
			newBody += "\n"
		}
		out = append(out, fileChange{Path: newPath, NewBody: newBody, IsNew: isNew})
	}

	if len(out) == 0 {
		return nil, errBadDiff
	}
	return out, nil
}

// splitLines returns the line array of body. Trailing "\n" is dropped so
// re-joining with "\n" reproduces the original content. Empty input → nil.
func splitLines(body string) []string {
	if body == "" {
		return nil
	}
	trimmed := strings.TrimSuffix(body, "\n")
	return strings.Split(trimmed, "\n")
}

// parseHunkHeader extracts (oldStart, newStart) from "@@ -A,B +C,D @@".
// Returns (0, 0, err) on malformed input. We ignore B and D — the applier
// uses the +/- prefixes directly rather than the declared counts.
func parseHunkHeader(hdr string) (int, int, error) {
	// "@@ -A[,B] +C[,D] @@ optional context"
	if !strings.HasPrefix(hdr, "@@") {
		return 0, 0, fmt.Errorf("not a hunk header: %s", hdr)
	}
	body := strings.TrimPrefix(hdr, "@@")
	body = strings.TrimSpace(body)
	endIdx := strings.Index(body, "@@")
	if endIdx == -1 {
		return 0, 0, fmt.Errorf("missing trailing @@: %s", hdr)
	}
	body = strings.TrimSpace(body[:endIdx])
	parts := strings.Fields(body)
	if len(parts) < 2 {
		return 0, 0, fmt.Errorf("hunk header missing ranges: %s", hdr)
	}
	oldStart, err := parseRangeStart(parts[0], '-')
	if err != nil {
		return 0, 0, err
	}
	newStart, err := parseRangeStart(parts[1], '+')
	if err != nil {
		return 0, 0, err
	}
	return oldStart, newStart, nil
}

func parseRangeStart(s string, prefix byte) (int, error) {
	if len(s) == 0 || s[0] != prefix {
		return 0, fmt.Errorf("expected %q prefix in %q", string(prefix), s)
	}
	body := s[1:]
	if idx := strings.Index(body, ","); idx >= 0 {
		body = body[:idx]
	}
	return strconv.Atoi(body)
}
