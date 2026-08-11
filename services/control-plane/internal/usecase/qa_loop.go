package usecase

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	recoverywf "github.com/nexis-eco/nexis/services/control-plane/internal/workflow/recovery"
)

// QASuiteStore is the narrow qa_test_suites surface QALoop needs.
// *repo.QATestSuitesRepo satisfies it via structural typing. All methods
// bypass RLS via the admin pool — cron has no principal to pin to.
type QASuiteStore interface {
	AdminListActive(ctx context.Context, limit int) ([]domain.QATestSuite, error)
	AdminRecordRun(ctx context.Context, suiteID string, passed bool, failCount int, at time.Time) error
}

// SuiteValidator is the sandbox port QALoop replays suites through — the
// same services/validator client the BackendCodegen activity uses
// (*validator.Client satisfies it via recoverywf.ValidatorClient).
type SuiteValidator interface {
	Validate(ctx context.Context, in recoverywf.ValidateRequest) (recoverywf.ValidateResponse, error)
}

// RegressionSink lands the synthetic incident a detected regression raises.
// *repo.IncidentsRepo satisfies it via InsertAdmin — the row rides the same
// incidents_raw path the webhook adapters use, so the Sentinel detector
// picks it up and triggers a recovery pipeline exactly like a Sentry fatal.
type RegressionSink interface {
	InsertAdmin(ctx context.Context, orgID string, raw domain.RawIncident) error
}

// QALoop is the continuous QA test loop (FYP: "QA Agent ... runs
// continuously — not just on demand"). Each tick it lists active suites
// (oldest-run-first), replays each one's generated pytest files against the
// validator sandbox at the suite's repo SHA, and:
//
//   - passed → failed on a previously-PASSING suite = REGRESSION: writes a
//     severity=high 'qa.regression' audit entry AND inserts a level=fatal
//     incidents_raw row (source='qa_loop') so Sentinel triggers a recovery
//     pipeline for it.
//   - any other transition just updates the suite's last_run bookkeeping —
//     a suite that never passed is a baseline, not a regression.
//
// Every dependency is nil-tolerant: without a validator or suite store the
// Run is a no-op, matching UsageTicker's degrade behaviour.
type QALoop struct {
	Suites    QASuiteStore
	Validator SuiteValidator
	Incidents RegressionSink     // optional — regression incident trigger
	Audit     domain.AuditWriter // optional — regression audit trail
	Logger    *slog.Logger

	// MaxSuitesPerTick caps how many suites one tick replays so a large
	// backlog cannot monopolise the validator sandbox. <=0 defaults to 10.
	MaxSuitesPerTick int
}

// Run executes one loop tick.
func (l *QALoop) Run(ctx context.Context) error {
	if l.Suites == nil || l.Validator == nil {
		return nil
	}
	limit := l.MaxSuitesPerTick
	if limit <= 0 {
		limit = 10
	}
	suites, err := l.Suites.AdminListActive(ctx, limit)
	if err != nil {
		return err
	}
	for _, s := range suites {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		l.runSuite(ctx, s)
	}
	return nil
}

// runSuite replays one suite. Per-suite failures are logged and absorbed so
// a single broken suite (or a validator hiccup on it) doesn't starve the
// rest of the batch.
func (l *QALoop) runSuite(ctx context.Context, s domain.QATestSuite) {
	patch := BuildTestPatch(s.Tests)
	if patch == "" {
		return
	}
	rep, err := l.Validator.Validate(ctx, recoverywf.ValidateRequest{
		RepoSHA:   s.RepoSHA,
		PatchDiff: patch,
	})
	now := time.Now().UTC()
	if err != nil {
		if l.Logger != nil {
			l.Logger.Warn("qa_loop: validate failed", "suite_id", s.ID, "err", err)
		}
		return // transient sandbox error — don't flip last_status on it
	}

	regression := !rep.TestsPassed && s.LastStatus == domain.QASuiteRunPassed
	if regression {
		l.reportRegression(ctx, s, rep, now)
	}
	if err := l.Suites.AdminRecordRun(ctx, s.ID, rep.TestsPassed, rep.FailCount, now); err != nil && l.Logger != nil {
		l.Logger.Warn("qa_loop: record run failed", "suite_id", s.ID, "err", err)
	}
}

// reportRegression raises the audit entry + the synthetic incident for a
// passed→failed transition. Both writes are best-effort.
func (l *QALoop) reportRegression(ctx context.Context, s domain.QATestSuite, rep recoverywf.ValidateResponse, at time.Time) {
	title := fmt.Sprintf("QA regression: %d previously-passing test(s) now fail (suite %s)",
		rep.FailCount, shortID(s.ID))
	if l.Logger != nil {
		l.Logger.Error("qa_loop: regression detected",
			"suite_id", s.ID, "org_id", s.OrgID, "run_id", s.WorkflowRunID,
			"fail_count", rep.FailCount)
	}
	if l.Audit != nil {
		_ = l.Audit.Write(ctx, domain.Principal{OrgID: s.OrgID}, "qa.regression", s.ID, map[string]any{
			"severity":        "high",
			"suite_id":        s.ID,
			"workflow_run_id": s.WorkflowRunID,
			"repo_sha":        s.RepoSHA,
			"fail_count":      rep.FailCount,
			"test_count":      rep.TestCount,
		})
	}
	if l.Incidents != nil {
		// source_event_id keyed per suite + UTC day so a suite that stays
		// red doesn't spawn a fresh incident every tick — the idempotent
		// insert dedupes on (org_id, source, source_event_id).
		eventID := fmt.Sprintf("qa-regression-%s-%s", s.ID, at.Format("2006-01-02"))
		if err := l.Incidents.InsertAdmin(ctx, s.OrgID, domain.RawIncident{
			Source:        "qa_loop",
			SourceEventID: eventID,
			Title:         title,
			Level:         "fatal", // joins PollFatalSince so Sentinel triggers a recovery
			Service:       "qa-continuous-loop",
			Environment:   "production",
			Payload: map[string]any{
				"suite_id":        s.ID,
				"workflow_run_id": s.WorkflowRunID,
				"project_id":      s.ProjectID,
				"repo_sha":        s.RepoSHA,
				"fail_count":      rep.FailCount,
				"test_count":      rep.TestCount,
				"logs":            truncate(rep.Logs, 4000),
			},
		}); err != nil && l.Logger != nil {
			l.Logger.Warn("qa_loop: incident insert failed", "suite_id", s.ID, "err", err)
		}
	}
}

// BuildTestPatch renders a suite's filename→source map as one unified diff
// that creates each test file, so the validator sandbox can apply it to the
// repo at the suite's SHA and run pytest — the same wire shape the Backend
// agent's patches use. Filenames without a directory component are placed
// under tests/nexis_generated/ so replayed suites never collide with the
// target repo's own files. Keys are emitted sorted for determinism.
//
// Exported (rather than a method) so the unit tests can pin the exact diff
// shape without constructing a full QALoop.
func BuildTestPatch(tests map[string]string) string {
	if len(tests) == 0 {
		return ""
	}
	names := make([]string, 0, len(tests))
	for name := range tests {
		if name == "" || tests[name] == "" {
			continue
		}
		names = append(names, name)
	}
	if len(names) == 0 {
		return ""
	}
	sort.Strings(names)

	var b strings.Builder
	for _, name := range names {
		path := name
		if !strings.Contains(path, "/") {
			path = "tests/nexis_generated/" + path
		}
		path = strings.TrimPrefix(path, "/")
		src := tests[name]
		lines := strings.Split(strings.TrimRight(src, "\n"), "\n")
		fmt.Fprintf(&b, "diff --git a/%s b/%s\n", path, path)
		b.WriteString("new file mode 100644\n")
		b.WriteString("--- /dev/null\n")
		fmt.Fprintf(&b, "+++ b/%s\n", path)
		fmt.Fprintf(&b, "@@ -0,0 +1,%d @@\n", len(lines))
		for _, line := range lines {
			b.WriteString("+")
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	return b.String()
}

// shortID returns the first 8 chars of a uuid for log/title readability.
func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// truncate caps s at n bytes for payload hygiene.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
