// Package approval implements the Phase 6 Approval Gate domain: severity
// classification, the activity-side service that creates pending rows + fires
// the notifier, and the WaitForDecision channel orchestration used by the
// workflow signal handler.
//
// The Temporal signal pattern (workflow.GetSignalChannel) lives in the
// workflow function (recovery/workflow.go) — this package owns the pure-Go
// logic so it can be unit-tested without a Temporal server.
package approval

import (
	"path/filepath"
	"strings"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// sensitiveGlobs lists the patch-path patterns that bump severity to HIGH
// regardless of scenario. Globs ending with "/" match anywhere in the path
// (treated as prefix substrings); bare globs are matched against filepath.Base.
//
// Extend this slice — never weaken it — when adding new sensitive areas.
var sensitiveGlobs = []string{
	"*.sql",
	"migrations/",
	"auth/",
	"security/",
	"crypto/",
}

// Default risk-score buckets emitted alongside the severity. Stored on the
// approval_decisions row so the UI can render a numeric score next to the
// severity badge.
const (
	RiskScoreLow    = 15.0
	RiskScoreMedium = 50.0
	RiskScoreHigh   = 85.0
)

// Classify returns (severity, riskScore) given the synthesised scenario and a
// unified-diff patch. Spec §9.1 routing:
//
//   - scenario "schema_drift" OR patch touches a sensitive glob → HIGH
//   - scenario "unknown" (Synthesiser failed to label) → HIGH
//   - scenario "oom" → MEDIUM (timer race, 2 min)
//   - tiny UI-only patch (< 10 changed lines, all under apps/web/components/) → LOW
//   - everything else → MEDIUM
//
// An empty patch + empty scenario falls into MEDIUM (the "we have no idea
// what's going on" default — better to ask the human than auto-approve).
func Classify(scenario string, patchDiff string) (domain.Severity, float64) {
	touched := touchedFiles(patchDiff)
	if scenario == "schema_drift" || hasSensitive(touched) {
		return domain.SeverityHigh, RiskScoreHigh
	}
	if scenario == "unknown" {
		return domain.SeverityHigh, RiskScoreHigh
	}
	if scenario == "oom" {
		return domain.SeverityMedium, RiskScoreMedium
	}
	lines := countChangedLines(patchDiff)
	if lines > 0 && lines < 10 && allUnder(touched, "apps/web/components/") {
		return domain.SeverityLow, RiskScoreLow
	}
	return domain.SeverityMedium, RiskScoreMedium
}

// touchedFiles extracts the "b/" paths from "diff --git a/X b/Y" headers.
// Returns nil for an empty diff. Renames + binary files surface as the
// destination path here; sensitivity scoring treats those identically to
// adds/modifies.
func touchedFiles(diff string) []string {
	if diff == "" {
		return nil
	}
	var out []string
	for _, ln := range strings.Split(diff, "\n") {
		if strings.HasPrefix(ln, "diff --git ") {
			parts := strings.Fields(ln)
			if len(parts) >= 4 {
				out = append(out, strings.TrimPrefix(parts[3], "b/"))
			}
		}
	}
	return out
}

// hasSensitive returns true when any path matches a sensitive glob. Suffix
// "/" globs match anywhere in the path; bare globs match against the base
// filename only.
func hasSensitive(files []string) bool {
	for _, f := range files {
		for _, g := range sensitiveGlobs {
			if strings.HasSuffix(g, "/") {
				if strings.Contains(f, g) {
					return true
				}
				continue
			}
			if ok, _ := filepath.Match(g, filepath.Base(f)); ok {
				return true
			}
		}
	}
	return false
}

// countChangedLines tallies + / - lines from a unified diff, ignoring the
// +++ / --- header markers. Conservative — it overcounts hunks that include
// the file-header markers, but the threshold (10) is loose enough that this
// is harmless in practice.
func countChangedLines(diff string) int {
	n := 0
	for _, ln := range strings.Split(diff, "\n") {
		if strings.HasPrefix(ln, "+") && !strings.HasPrefix(ln, "+++") {
			n++
		}
		if strings.HasPrefix(ln, "-") && !strings.HasPrefix(ln, "---") {
			n++
		}
	}
	return n
}

// allUnder returns true when every path in files starts with prefix.
// Returns false when files is empty so we never auto-approve a no-touch diff
// as a "trivial UI change".
func allUnder(files []string, prefix string) bool {
	if len(files) == 0 {
		return false
	}
	for _, f := range files {
		if !strings.HasPrefix(f, prefix) {
			return false
		}
	}
	return true
}
