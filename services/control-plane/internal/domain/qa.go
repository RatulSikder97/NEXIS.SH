package domain

import "time"

// QATestSuite is one persisted set of QA-agent-generated pytest files,
// re-runnable by the continuous QA loop (usecase.QALoop) long after the
// recovery run that produced it completed. Mirrors the qa_test_suites table.
//
// Tests is the QA agent's structured "tests" map verbatim: filename →
// pytest source. CoversFiles is the agent's declared coverage surface —
// informational only, the loop replays every file in Tests.
type QATestSuite struct {
	ID            string
	OrgID         string
	WorkspaceID   string
	ProjectID     string
	WorkflowRunID string
	RepoSHA       string
	Tests         map[string]string
	CoversFiles   []string
	Status        string // active | disabled
	LastStatus    string // unknown | passed | failed
	LastFailCount int
	LastRunAt     time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// QA suite lifecycle enums — string-typed to match the CHECK constraints in
// migration 0031.
const (
	QASuiteStatusActive   = "active"
	QASuiteStatusDisabled = "disabled"

	QASuiteRunUnknown = "unknown"
	QASuiteRunPassed  = "passed"
	QASuiteRunFailed  = "failed"
)
