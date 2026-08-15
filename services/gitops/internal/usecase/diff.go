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

// diffOp is one line of a parsed hunk body.
type diffOp struct {
	kind  opKind
	text  string
	loose bool // unrecognised line: bind to the baseline, never compare
}

type opKind int

const (
	opCtx opKind = iota
	opDel
	opAdd
)

// locateHunk finds where a hunk actually applies in baseLines.
//
// Unified diffs produced by an LLM regularly carry the right edit with the
// wrong coordinates: off-by-a-few line numbers, or context lines that were
// paraphrased from a neighbouring file. Refusing those wastes a correct
// patch, so this mirrors what patch(1) does — search outward from the
// declared position for somewhere the hunk fits.
//
// The safety rule is asymmetric, and deliberately so:
//
//   - every "-" line must match the baseline EXACTLY. Those lines get
//     deleted, so a fuzzy match there would silently destroy code the patch
//     never meant to touch.
//   - " " context lines may drift. They are only positional hints, and the
//     applier re-emits the baseline's own text for them.
//
// A hunk of pure additions (no deletions, no context) has no anchor to
// search on, so it applies at its declared offset. Returns the 0-based index
// where the hunk starts, or an error when no position satisfies the rule.
func locateHunk(baseLines []string, ops []diffOp, declared, minPos int) (int, error) {
	var anchors []diffOp // the "-" and " " lines, in order
	dels := 0
	for _, op := range ops {
		if op.kind == opAdd {
			continue
		}
		anchors = append(anchors, op)
		if op.kind == opDel {
			dels++
		}
	}
	if len(anchors) == 0 {
		if declared < minPos {
			return minPos, nil
		}
		return declared, nil
	}

	fits := func(at int) (int, bool) {
		if at < minPos || at+len(anchors) > len(baseLines) {
			return 0, false
		}
		drift, agree := 0, 0
		for k, op := range anchors {
			have := baseLines[at+k]
			switch {
			case op.kind == opDel:
				if have != op.text {
					return 0, false // never delete a line we can't see
				}
				agree++
			case op.loose:
				// unknown marker — accept whatever is there
			case have == op.text, strings.TrimSpace(have) == strings.TrimSpace(op.text):
				agree++
			default:
				drift++
			}
		}
		// A hunk with no deletions has no exact anchor, so it needs at least
		// one context line that genuinely matches. Without that the hunk is
		// describing some other file and "relocating" it would mean splicing
		// additions into an arbitrary spot.
		if agree == 0 {
			return 0, false
		}
		return drift, true
	}

	// Prefer the declared position, then the nearest position that fits with
	// the least context drift. Scanning by increasing distance keeps the
	// choice stable and close to the model's intent.
	if drift, ok := fits(declared); ok && drift == 0 {
		return declared, nil
	}
	best, bestDrift := -1, 1<<30
	for radius := 0; radius <= len(baseLines); radius++ {
		for _, at := range [2]int{declared - radius, declared + radius} {
			drift, ok := fits(at)
			if !ok || drift >= bestDrift {
				continue
			}
			best, bestDrift = at, drift
			if drift == 0 {
				return best, nil
			}
		}
	}
	if best >= 0 {
		return best, nil
	}
	if dels > 0 {
		return 0, fmt.Errorf("no position matches the hunk's %d deleted line(s); "+
			"the patch targets code that is not in the file", dels)
	}
	return 0, fmt.Errorf("hunk context does not fit the file")
}

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
			// The declared old-start is only a hint — locateHunk decides the
			// real position and the unchanged lines in between are emitted
			// afterwards. Walking the cursor forward here instead would pin
			// it past the true location and make relocation impossible.
			// Hunks against /dev/null declare "-0,0" (oldIdx -1) — clamp
			// those to the current cursor so the empty-baseline path walks
			// through cleanly.
			oldIdx := oldStart - 1 // 0-based; "-0,0" produces -1 here
			if oldIdx < 0 || oldIdx > len(baseLines) {
				oldIdx = cursor
			}
			if oldIdx < cursor {
				oldIdx = cursor
			}

			i++ // consume hunk header

			// Collect the hunk body before touching the baseline. Applying in
			// two passes lets us relocate the hunk when the model's line
			// numbers or context lines drift, which LLM-authored diffs do
			// routinely — the edit is right but the surrounding lines are
			// half-remembered.
			var ops []diffOp
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
				case strings.HasPrefix(cur, "+"):
					ops = append(ops, diffOp{kind: opAdd, text: cur[1:]})
				case strings.HasPrefix(cur, "-"):
					ops = append(ops, diffOp{kind: opDel, text: cur[1:]})
				case strings.HasPrefix(cur, " "):
					ops = append(ops, diffOp{kind: opCtx, text: cur[1:]})
				default:
					// Unknown line inside hunk — a stray blank or malformed
					// header. Treat as context with unknown text so it binds
					// to whatever the baseline holds.
					ops = append(ops, diffOp{kind: opCtx, text: cur, loose: true})
				}
				i++
			}

			at, err := locateHunk(baseLines, ops, oldIdx, cursor)
			if err != nil {
				return nil, fmt.Errorf("%w on %s: %v", errBadDiff, newPath, err)
			}
			// Emit untouched lines skipped by relocation.
			for cursor < at && cursor < len(baseLines) {
				built = append(built, baseLines[cursor])
				cursor++
			}
			for _, op := range ops {
				switch op.kind {
				case opAdd:
					built = append(built, op.text)
				case opDel:
					if cursor >= len(baseLines) {
						return nil, fmt.Errorf("%w: deletion past EOF on %s", errBadDiff, newPath)
					}
					cursor++
				case opCtx:
					if cursor >= len(baseLines) {
						return nil, fmt.Errorf("%w: context past EOF on %s", errBadDiff, newPath)
					}
					// Take the baseline's text, not the diff's: context drift
					// must not rewrite lines the patch never intended to touch.
					built = append(built, baseLines[cursor])
					cursor++
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
