package usecase

// Coverage for the continuous QA test loop: the BuildTestPatch diff shape,
// the regression-detection state machine (passed → failed fires an incident
// + audit; a never-passed suite is only a baseline), and the nil-dependency
// no-op contract every cron job in this package honours.

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	recoverywf "github.com/nexis-eco/nexis/services/control-plane/internal/workflow/recovery"
)

// ---- fakes -----------------------------------------------------------------

type fakeSuiteStore struct {
	mu      sync.Mutex
	suites  []domain.QATestSuite
	listErr error
	runs    []recordedRun
}

type recordedRun struct {
	suiteID   string
	passed    bool
	failCount int
}

func (f *fakeSuiteStore) AdminListActive(_ context.Context, _ int) ([]domain.QATestSuite, error) {
	return f.suites, f.listErr
}

func (f *fakeSuiteStore) AdminRecordRun(_ context.Context, suiteID string, passed bool, failCount int, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runs = append(f.runs, recordedRun{suiteID, passed, failCount})
	return nil
}

type fakeValidator struct {
	resp recoverywf.ValidateResponse
	err  error
	got  []recoverywf.ValidateRequest
}

func (f *fakeValidator) Validate(_ context.Context, in recoverywf.ValidateRequest) (recoverywf.ValidateResponse, error) {
	f.got = append(f.got, in)
	return f.resp, f.err
}

type fakeIncidentSink struct {
	mu       sync.Mutex
	inserted []struct {
		orgID string
		raw   domain.RawIncident
	}
}

func (f *fakeIncidentSink) InsertAdmin(_ context.Context, orgID string, raw domain.RawIncident) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inserted = append(f.inserted, struct {
		orgID string
		raw   domain.RawIncident
	}{orgID, raw})
	return nil
}

type recordingAudit struct {
	mu      sync.Mutex
	actions []string
	metas   []map[string]any
}

func (f *recordingAudit) Write(_ context.Context, _ domain.Principal, action, _ string, meta map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.actions = append(f.actions, action)
	f.metas = append(f.metas, meta)
	return nil
}

func passingSuite(lastStatus string) domain.QATestSuite {
	return domain.QATestSuite{
		ID: "suite-1", OrgID: "org-1", WorkflowRunID: "run-1",
		RepoSHA: "abc123", LastStatus: lastStatus,
		Tests: map[string]string{"test_orders.py": "def test_ok():\n    assert True"},
	}
}

// ---- BuildTestPatch --------------------------------------------------------

func TestBuildTestPatch_ShapeAndDeterminism(t *testing.T) {
	tests := map[string]string{
		"test_b.py":              "def test_b():\n    assert 1",
		"tests/custom/test_a.py": "def test_a():\n    assert 2",
	}
	got := BuildTestPatch(tests)
	want := "diff --git a/test_b.py... (order check below)"
	_ = want

	// Bare filename lands under tests/nexis_generated/; pathed name stays.
	if !strings.Contains(got, "+++ b/tests/nexis_generated/test_b.py") {
		t.Fatalf("bare filename must be nested under tests/nexis_generated/:\n%s", got)
	}
	if !strings.Contains(got, "+++ b/tests/custom/test_a.py") {
		t.Fatalf("pathed filename must be preserved:\n%s", got)
	}
	// New-file headers.
	if !strings.Contains(got, "new file mode 100644") || !strings.Contains(got, "--- /dev/null") {
		t.Fatalf("must render new-file diff headers:\n%s", got)
	}
	// Hunk line count matches the source lines.
	if !strings.Contains(got, "@@ -0,0 +1,2 @@") {
		t.Fatalf("hunk header must count 2 lines:\n%s", got)
	}
	// Sorted keys ⇒ deterministic output across runs.
	if got != BuildTestPatch(tests) {
		t.Fatalf("BuildTestPatch must be deterministic")
	}
	// Sorted order: "test_b.py" > "tests/custom/test_a.py" lexicographically,
	// so test_b.py's diff comes first.
	if strings.Index(got, "test_b.py") > strings.Index(got, "tests/custom/test_a.py") {
		t.Fatalf("files must be emitted in sorted key order:\n%s", got)
	}
}

func TestBuildTestPatch_EmptyInputs(t *testing.T) {
	if BuildTestPatch(nil) != "" {
		t.Fatalf("nil map must render empty patch")
	}
	if BuildTestPatch(map[string]string{"": "x", "f.py": ""}) != "" {
		t.Fatalf("blank names/sources must be skipped entirely")
	}
}

// ---- Run: nil deps ---------------------------------------------------------

func TestQALoop_NilDepsNoOp(t *testing.T) {
	l := &QALoop{}
	if err := l.Run(context.Background()); err != nil {
		t.Fatalf("nil deps must short-circuit, got %v", err)
	}
	l = &QALoop{Suites: &fakeSuiteStore{}}
	if err := l.Run(context.Background()); err != nil {
		t.Fatalf("nil validator must short-circuit, got %v", err)
	}
}

// ---- Run: regression state machine ----------------------------------------

func TestQALoop_RegressionFiresIncidentAndAudit(t *testing.T) {
	store := &fakeSuiteStore{suites: []domain.QATestSuite{passingSuite(domain.QASuiteRunPassed)}}
	val := &fakeValidator{resp: recoverywf.ValidateResponse{TestsPassed: false, FailCount: 2, TestCount: 5}}
	sink := &fakeIncidentSink{}
	aud := &recordingAudit{}
	l := &QALoop{Suites: store, Validator: val, Incidents: sink, Audit: aud}

	if err := l.Run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	// Regression → audit entry with severity high.
	if len(aud.actions) != 1 || aud.actions[0] != "qa.regression" {
		t.Fatalf("want one qa.regression audit, got %v", aud.actions)
	}
	if aud.metas[0]["severity"] != "high" {
		t.Fatalf("regression audit must carry severity=high, got %v", aud.metas[0])
	}
	// Regression → fatal incident on the qa_loop source, deduped per day.
	if len(sink.inserted) != 1 {
		t.Fatalf("want one incident, got %d", len(sink.inserted))
	}
	raw := sink.inserted[0].raw
	if raw.Source != "qa_loop" || raw.Level != "fatal" {
		t.Fatalf("incident must be source=qa_loop level=fatal, got %+v", raw)
	}
	if !strings.HasPrefix(raw.SourceEventID, "qa-regression-suite-1-") {
		t.Fatalf("source_event_id must dedupe per suite+day, got %q", raw.SourceEventID)
	}
	// Bookkeeping updated to failed.
	if len(store.runs) != 1 || store.runs[0].passed || store.runs[0].failCount != 2 {
		t.Fatalf("suite run must be recorded as failed(2), got %+v", store.runs)
	}
	// The replayed patch reached the validator at the suite's SHA.
	if len(val.got) != 1 || val.got[0].RepoSHA != "abc123" || val.got[0].PatchDiff == "" {
		t.Fatalf("validator must receive repo sha + patch, got %+v", val.got)
	}
}

func TestQALoop_NeverPassedFailureIsBaselineNotRegression(t *testing.T) {
	store := &fakeSuiteStore{suites: []domain.QATestSuite{passingSuite(domain.QASuiteRunUnknown)}}
	val := &fakeValidator{resp: recoverywf.ValidateResponse{TestsPassed: false, FailCount: 1}}
	sink := &fakeIncidentSink{}
	aud := &recordingAudit{}
	l := &QALoop{Suites: store, Validator: val, Incidents: sink, Audit: aud}

	if err := l.Run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(sink.inserted) != 0 || len(aud.actions) != 0 {
		t.Fatalf("first-ever failure must not raise a regression (incidents=%d audits=%d)",
			len(sink.inserted), len(aud.actions))
	}
	if len(store.runs) != 1 || store.runs[0].passed {
		t.Fatalf("baseline failure must still be recorded, got %+v", store.runs)
	}
}

func TestQALoop_PassRecordsQuietly(t *testing.T) {
	store := &fakeSuiteStore{suites: []domain.QATestSuite{passingSuite(domain.QASuiteRunPassed)}}
	val := &fakeValidator{resp: recoverywf.ValidateResponse{TestsPassed: true, TestCount: 5}}
	sink := &fakeIncidentSink{}
	l := &QALoop{Suites: store, Validator: val, Incidents: sink}

	if err := l.Run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(sink.inserted) != 0 {
		t.Fatalf("green pass must not raise incidents")
	}
	if len(store.runs) != 1 || !store.runs[0].passed {
		t.Fatalf("pass must be recorded, got %+v", store.runs)
	}
}

func TestQALoop_ValidatorErrorLeavesStatusUntouched(t *testing.T) {
	store := &fakeSuiteStore{suites: []domain.QATestSuite{passingSuite(domain.QASuiteRunPassed)}}
	val := &fakeValidator{err: errors.New("sandbox down")}
	l := &QALoop{Suites: store, Validator: val}

	if err := l.Run(context.Background()); err != nil {
		t.Fatalf("transient validator error must not fail the tick, got %v", err)
	}
	if len(store.runs) != 0 {
		t.Fatalf("a sandbox hiccup must not flip last_status, got %+v", store.runs)
	}
}

func TestQALoop_ListErrorSurfacesToCron(t *testing.T) {
	store := &fakeSuiteStore{listErr: errors.New("pg down")}
	l := &QALoop{Suites: store, Validator: &fakeValidator{}}
	if err := l.Run(context.Background()); err == nil {
		t.Fatalf("list failure must surface so the cron logs it")
	}
}
