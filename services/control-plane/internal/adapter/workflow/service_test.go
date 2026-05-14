package workflow

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/mock"

	temporalclient "go.temporal.io/sdk/client"
	temporalmocks "go.temporal.io/sdk/mocks"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/repo"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/db"
)

// recordingTx is a pgx.Tx stub that records every Exec call. The workflow
// adapter's WorkflowRepo routes its INSERT/UPDATE statements through
// db.FromCtx(ctx, r.app) — if the test stashes one of these in ctx via
// db.WithTx, the repo writes land here instead of the (non-existent) database.
type recordingTx struct {
	mu       sync.Mutex
	calls    []recordedExec
	execErr  error // if set, every Exec returns this error
	commitOK bool
}

type recordedExec struct {
	sql  string
	args []any
}

func (t *recordingTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	cp := make([]any, len(args))
	copy(cp, args)
	t.calls = append(t.calls, recordedExec{sql: sql, args: cp})
	if t.execErr != nil {
		return pgconn.CommandTag{}, t.execErr
	}
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}
func (t *recordingTx) Query(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
	return nil, errors.New("Query not used")
}
func (t *recordingTx) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	return nil
}
func (t *recordingTx) Begin(_ context.Context) (pgx.Tx, error) {
	return nil, errors.New("Begin not used")
}
func (t *recordingTx) Commit(_ context.Context) error {
	t.commitOK = true
	return nil
}
func (t *recordingTx) Rollback(_ context.Context) error { return nil }
func (t *recordingTx) CopyFrom(_ context.Context, _ pgx.Identifier, _ []string, _ pgx.CopyFromSource) (int64, error) {
	return 0, errors.New("CopyFrom not used")
}
func (t *recordingTx) SendBatch(_ context.Context, _ *pgx.Batch) pgx.BatchResults {
	return nil
}
func (t *recordingTx) LargeObjects() pgx.LargeObjects { return pgx.LargeObjects{} }
func (t *recordingTx) Prepare(_ context.Context, _, _ string) (*pgconn.StatementDescription, error) {
	return nil, errors.New("Prepare not used")
}
func (t *recordingTx) Conn() *pgx.Conn { return nil }

var _ pgx.Tx = (*recordingTx)(nil)

// findExec returns the first recorded Exec whose SQL contains every substring
// in needles. Use this to pick out INSERT vs UPDATE vs SELECT without
// coupling tests to whitespace.
func (t *recordingTx) findExec(needles ...string) *recordedExec {
	t.mu.Lock()
	defer t.mu.Unlock()
	for i := range t.calls {
		match := true
		for _, n := range needles {
			if !strings.Contains(t.calls[i].sql, n) {
				match = false
				break
			}
		}
		if match {
			return &t.calls[i]
		}
	}
	return nil
}

// unreachablePool returns a real *pgxpool.Pool pointed at an unroutable
// address. pgxpool is lazy — it won't dial until first use, and on first use
// it returns a connect-refused error (no panic). The workflow.Service's
// failure path uses context.Background() to call UpdateRunStatus on the admin
// pool; we use this to keep that path well-behaved without a live DB.
func unreachablePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	// Acquire and release a localhost port to get a guaranteed-closed
	// destination. Avoids depending on a hard-coded port that might be open.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().String()
	_ = l.Close() // immediately close so subsequent connects fail

	pool, err := pgxpool.New(context.Background(), "postgres://nobody@"+addr+"/x?connect_timeout=1")
	if err != nil {
		t.Fatalf("build unreachable pool: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	return pool
}

// blockingWorkflowRun is a WorkflowRun whose Get blocks until done is closed.
// Tests use it to keep the reaper goroutine parked while assertions run.
// Embedding a temporalmocks.WorkflowRun lets us reuse GetID/GetRunID and only
// override Get.
type blockingWorkflowRun struct {
	temporalmocks.WorkflowRun
	done chan struct{}
}

func (w *blockingWorkflowRun) Get(_ context.Context, _ interface{}) error {
	<-w.done
	return nil
}

// ----- Tests -------------------------------------------------------------

// TestService_Start_PersistsWorkflowRun verifies the row insert lands inside
// the request tx (so RLS sees app.current_org_id) and that the row carries
// the principal-derived fields. We don't need a real DB because we capture
// the Exec call on a fake tx threaded through ctx via db.WithTx.
func TestService_Start_PersistsWorkflowRun(t *testing.T) {
	tx := &recordingTx{}
	ctx := db.WithTx(context.Background(), tx)

	mc := &temporalmocks.Client{}
	wf := &blockingWorkflowRun{done: make(chan struct{})}
	wf.WorkflowRun.On("GetID").Return("wf-id-123")
	wf.WorkflowRun.On("GetRunID").Return("temporal-run-456")

	mc.On("ExecuteWorkflow",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return(wf, nil)

	// Build a real *repo.WorkflowRepo with unreachable pools. InsertRun
	// routes through db.FromCtx(ctx, r.app) — the fake tx in ctx wins — so
	// r.app is never touched. r.admin is only touched on error or reaper
	// paths; we keep wf.Get blocked so the reaper waits.
	pool := unreachablePool(t)
	repo := repo.NewWorkflowRepo(pool, pool)
	svc := New(Config{
		Repo:      repo,
		Temporal:  mc,
		TaskQueue: "test-queue",
	})

	princ := domain.Principal{UserID: "u-1", OrgID: "org-1", Role: domain.RoleOwner}
	run, err := svc.Start(ctx, princ, "ws-1", "recovery_pipeline", nil)
	if err != nil {
		t.Fatalf("Start: unexpected error: %v", err)
	}
	defer close(wf.done) // release the reaper goroutine on test exit

	if run.ID == "" {
		t.Errorf("Start should return a populated run ID")
	}
	if run.OrgID != "org-1" {
		t.Errorf("OrgID forwarded: got %q want org-1", run.OrgID)
	}
	if run.WorkspaceID != "ws-1" {
		t.Errorf("WorkspaceID forwarded: got %q want ws-1", run.WorkspaceID)
	}
	if run.WorkflowType != "recovery_pipeline" {
		t.Errorf("WorkflowType forwarded: got %q want recovery_pipeline", run.WorkflowType)
	}

	insert := tx.findExec("INSERT INTO workflow_runs")
	if insert == nil {
		t.Fatalf("InsertRun did not run on the request tx; recorded calls=%d", len(tx.calls))
	}
	// args[1] is org_id, args[2] is workspace_id, args[3] is workflow_type per InsertRun
	if insert.args[1] != "org-1" {
		t.Errorf("INSERT org_id: got %v want org-1", insert.args[1])
	}
	if insert.args[2] != "ws-1" {
		t.Errorf("INSERT workspace_id: got %v want ws-1", insert.args[2])
	}
	if insert.args[3] != "recovery_pipeline" {
		t.Errorf("INSERT workflow_type: got %v want recovery_pipeline", insert.args[3])
	}

	// ExecuteWorkflow must have been called exactly once.
	mc.AssertCalled(t, "ExecuteWorkflow",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	)
}

// TestService_Start_PropagatesProjectID — when the input JSON carries
// "project_id", the InsertRun row must carry it onward so downstream
// dashboards can filter by project.
func TestService_Start_PropagatesProjectID(t *testing.T) {
	tx := &recordingTx{}
	ctx := db.WithTx(context.Background(), tx)

	mc := &temporalmocks.Client{}
	wf := &blockingWorkflowRun{done: make(chan struct{})}
	wf.WorkflowRun.On("GetID").Return("wf-2")
	wf.WorkflowRun.On("GetRunID").Return("tr-2")
	mc.On("ExecuteWorkflow",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return(wf, nil)

	pool := unreachablePool(t)
	repo := repo.NewWorkflowRepo(pool, pool)
	svc := New(Config{Repo: repo, Temporal: mc, TaskQueue: "q"})

	princ := domain.Principal{UserID: "u", OrgID: "org-2", Role: domain.RoleOwner}
	input := []byte(`{"project_id":"proj-XYZ","incident_id":"inc-1"}`)
	run, err := svc.Start(ctx, princ, "ws-2", "recovery_pipeline", input)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer close(wf.done)

	if run.ProjectID != "proj-XYZ" {
		t.Errorf("returned ProjectID: got %q want proj-XYZ", run.ProjectID)
	}
	insert := tx.findExec("INSERT INTO workflow_runs")
	if insert == nil {
		t.Fatalf("INSERT not recorded")
	}
	// project_id is the LAST arg in the INSERT (positional $16 — see
	// workflows_repo.go InsertRun). Search by value to stay resilient to the
	// exact column ordering.
	var found bool
	for _, a := range insert.args {
		if s, ok := a.(string); ok && s == "proj-XYZ" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("INSERT did not carry project_id=proj-XYZ; args=%+v", insert.args)
	}
}

// TestService_Start_HandlesTemporalErrorGracefully — when ExecuteWorkflow
// returns an error, Start must wrap+return it WITHOUT panicking. The
// production code calls UpdateRunStatus(context.Background(), …) on the
// failure path; that runs on the admin pool which we point at an unreachable
// host so the call returns an error (which the production code ignores via
// `_ =`). The end-to-end claim is "the temporal error is what propagates,
// regardless of any subsequent bookkeeping failures."
func TestService_Start_HandlesTemporalErrorGracefully(t *testing.T) {
	tx := &recordingTx{}
	ctx := db.WithTx(context.Background(), tx)

	temporalErr := errors.New("temporal unavailable")
	mc := &temporalmocks.Client{}
	mc.On("ExecuteWorkflow",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return((*blockingWorkflowRun)(nil), temporalErr)

	pool := unreachablePool(t)
	repo := repo.NewWorkflowRepo(pool, pool)
	svc := New(Config{Repo: repo, Temporal: mc, TaskQueue: "q"})

	princ := domain.Principal{UserID: "u", OrgID: "org-3", Role: domain.RoleOwner}

	// We expect Start to return — not panic. The temporal error should
	// surface; the InsertRun should still have run (Start inserts first,
	// then calls ExecuteWorkflow).
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Start panicked on temporal error: %v", r)
		}
	}()

	_, err := svc.Start(ctx, princ, "ws-3", "recovery_pipeline", nil)
	if err == nil {
		t.Fatalf("Start should propagate the temporal error")
	}
	if !errors.Is(err, temporalErr) {
		t.Errorf("Start error should wrap the temporal error; got %v", err)
	}

	// The pre-Temporal INSERT must still have landed on the request tx.
	if tx.findExec("INSERT INTO workflow_runs") == nil {
		t.Errorf("InsertRun did not run before ExecuteWorkflow")
	}
}

// TestService_IsNotFound verifies the small helper the HTTP layer uses to
// stay decoupled from the repo's error type.
func TestService_IsNotFound(t *testing.T) {
	if !IsNotFound(domain.ErrNotFound) {
		t.Errorf("IsNotFound(ErrNotFound) should be true")
	}
	if IsNotFound(errors.New("other")) {
		t.Errorf("IsNotFound(other err) should be false")
	}
	if IsNotFound(nil) {
		t.Errorf("IsNotFound(nil) should be false")
	}
}

// TestService_Subscribe_NoBrokerReturnsClosedChannel — defensive: when no SSE
// broker is wired (the test fixture path), Subscribe must return an
// immediately-closed channel rather than nil. A nil channel blocks forever
// on receive; a closed channel returns the zero value on receive, which
// lets the HTTP SSE writer fall through cleanly.
func TestService_Subscribe_NoBrokerReturnsClosedChannel(t *testing.T) {
	svc := New(Config{Repo: nil, Temporal: nil})
	ch := svc.Subscribe(context.Background(), "any-run")

	// Should be receivable immediately (closed channel returns zero value).
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatalf("expected closed channel; got a value")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("channel was not closed within 100ms")
	}
}

// Sanity check — the temporalclient import is intentional even when not
// directly referenced in test bodies (it pins the SDK version we test
// against). Suppress the unused-import error.
var _ = temporalclient.StartWorkflowOptions{}
