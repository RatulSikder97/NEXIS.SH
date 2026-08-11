package approval_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/approval"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// ---- severity matrix -------------------------------------------------------

func TestClassify_SchemaDriftScenarioForcesHigh(t *testing.T) {
	sev, risk := approval.Classify("schema_drift", "diff --git a/x.go b/x.go\n+1")
	require.Equal(t, domain.SeverityHigh, sev)
	require.Equal(t, approval.RiskScoreHigh, risk)
}

func TestClassify_UnknownScenarioIsHigh(t *testing.T) {
	sev, _ := approval.Classify("unknown", "diff --git a/x.go b/x.go\n+1\n+2")
	require.Equal(t, domain.SeverityHigh, sev)
}

func TestClassify_SQLPatchIsHighEvenForBenignScenario(t *testing.T) {
	sev, _ := approval.Classify("null_deref", "diff --git a/db/m.sql b/db/m.sql\n+CREATE TABLE x();")
	require.Equal(t, domain.SeverityHigh, sev)
}

func TestClassify_AuthDirectoryIsHigh(t *testing.T) {
	sev, _ := approval.Classify("null_deref", "diff --git a/auth/jwt.go b/auth/jwt.go\n+x")
	require.Equal(t, domain.SeverityHigh, sev)
}

func TestClassify_MigrationsDirectoryIsHigh(t *testing.T) {
	sev, _ := approval.Classify("null_deref", "diff --git a/services/control-plane/migrations/0099_x.up.sql b/services/control-plane/migrations/0099_x.up.sql\n+SELECT 1")
	require.Equal(t, domain.SeverityHigh, sev)
}

func TestClassify_OOMScenarioIsMedium(t *testing.T) {
	sev, risk := approval.Classify("oom", "diff --git a/x.go b/x.go\n+1\n+2")
	require.Equal(t, domain.SeverityMedium, sev)
	require.Equal(t, approval.RiskScoreMedium, risk)
}

func TestClassify_TrivialUIIsLow(t *testing.T) {
	diff := "diff --git a/apps/web/components/Btn.tsx b/apps/web/components/Btn.tsx\n+const x = 1\n-const y = 2"
	sev, risk := approval.Classify("null_deref", diff)
	require.Equal(t, domain.SeverityLow, sev)
	require.Equal(t, approval.RiskScoreLow, risk)
}

func TestClassify_LargeUIIsMedium(t *testing.T) {
	// > 10 changed lines bumps out of LOW.
	body := "diff --git a/apps/web/components/Btn.tsx b/apps/web/components/Btn.tsx\n"
	for i := 0; i < 12; i++ {
		body += "+line\n"
	}
	sev, _ := approval.Classify("null_deref", body)
	require.Equal(t, domain.SeverityMedium, sev)
}

func TestClassify_MixedPathsAreMedium(t *testing.T) {
	diff := "diff --git a/apps/web/components/Btn.tsx b/apps/web/components/Btn.tsx\n+x\n" +
		"diff --git a/services/control-plane/internal/foo.go b/services/control-plane/internal/foo.go\n+y"
	sev, _ := approval.Classify("null_deref", diff)
	require.Equal(t, domain.SeverityMedium, sev)
}

func TestClassify_EmptyPatchEmptyScenarioIsMedium(t *testing.T) {
	sev, _ := approval.Classify("", "")
	require.Equal(t, domain.SeverityMedium, sev)
}

// ---- WaitForDecision race --------------------------------------------------

func TestWaitForDecision_LowAutoApprovesImmediately(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	ch := make(chan domain.ApprovalSignal, 1)
	got, err := approval.WaitForDecision(ctx, domain.SeverityLow, ch, time.Second)
	require.NoError(t, err)
	require.Equal(t, domain.ApprovalAutoApproved, got.Decision)
}

func TestWaitForDecision_MediumSignalBeatsTimer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ch := make(chan domain.ApprovalSignal, 1)
	go func() {
		time.Sleep(20 * time.Millisecond)
		ch <- domain.ApprovalSignal{Decision: domain.ApprovalApproved, DecidedBy: "user-1"}
	}()
	got, err := approval.WaitForDecision(ctx, domain.SeverityMedium, ch, 500*time.Millisecond)
	require.NoError(t, err)
	require.Equal(t, domain.ApprovalApproved, got.Decision)
	require.Equal(t, "user-1", got.DecidedBy)
}

func TestWaitForDecision_MediumTimerWinsWhenNoSignal(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ch := make(chan domain.ApprovalSignal, 1)
	start := time.Now()
	got, err := approval.WaitForDecision(ctx, domain.SeverityMedium, ch, 30*time.Millisecond)
	elapsed := time.Since(start)
	require.NoError(t, err)
	require.Equal(t, domain.ApprovalTimeoutRejected, got.Decision)
	require.GreaterOrEqual(t, elapsed, 25*time.Millisecond)
	require.Less(t, elapsed, 500*time.Millisecond, "timer should fire promptly after the deadline")
}

func TestWaitForDecision_HighBlocksUntilSignal(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ch := make(chan domain.ApprovalSignal, 1)
	go func() {
		time.Sleep(40 * time.Millisecond)
		ch <- domain.ApprovalSignal{Decision: domain.ApprovalRejected, DecidedBy: "user-2", Notes: "no"}
	}()
	got, err := approval.WaitForDecision(ctx, domain.SeverityHigh, ch, 10*time.Millisecond)
	require.NoError(t, err)
	require.Equal(t, domain.ApprovalRejected, got.Decision)
	require.Equal(t, "user-2", got.DecidedBy)
}

func TestWaitForDecision_HighCancelsOnCtx(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	ch := make(chan domain.ApprovalSignal, 1)
	_, err := approval.WaitForDecision(ctx, domain.SeverityHigh, ch, 0)
	require.Error(t, err)
	require.True(t, errors.Is(err, context.DeadlineExceeded))
}

// ---- CreatePending + RecordDecision: fake repo + audit --------------------

type fakeRepo struct {
	mu      sync.Mutex
	created []domain.ApprovalDecision
	updates []update
}

type update struct {
	runID string
	state domain.ApprovalDecisionState
	by    string
	notes string
	at    time.Time
}

func (f *fakeRepo) Create(_ context.Context, d domain.ApprovalDecision) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d.ID = "decision-1"
	f.created = append(f.created, d)
	return d.ID, nil
}

func (f *fakeRepo) GetByRun(_ context.Context, _ string) (domain.ApprovalDecision, error) {
	return domain.ApprovalDecision{}, nil
}

func (f *fakeRepo) UpdateDecision(_ context.Context, runID string, state domain.ApprovalDecisionState, by, notes string, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates = append(f.updates, update{runID: runID, state: state, by: by, notes: notes, at: at})
	return nil
}

func (f *fakeRepo) ListPending(_ context.Context, _ string, _ int) ([]domain.ApprovalDecision, error) {
	return nil, nil
}

type fakeAudit struct {
	mu      sync.Mutex
	written []auditCall
}

type auditCall struct {
	princ    domain.Principal
	action   string
	target   string
	metadata map[string]any
}

func (f *fakeAudit) Write(_ context.Context, p domain.Principal, action, target string, metadata map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.written = append(f.written, auditCall{princ: p, action: action, target: target, metadata: metadata})
	return nil
}

type fakeNotifier struct {
	mu   sync.Mutex
	sent []domain.Notification
	fail bool
}

func (f *fakeNotifier) Channel() string { return "fake" }
func (f *fakeNotifier) Send(_ context.Context, n domain.Notification) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, n)
	if f.fail {
		return errors.New("notifier fail")
	}
	return nil
}

func TestService_CreatePending_WritesRowAndAudit(t *testing.T) {
	repo := &fakeRepo{}
	aud := &fakeAudit{}
	svc := approval.New(repo, &fakeNotifier{}, aud)

	id, err := svc.CreatePending(context.Background(), approval.CreateInput{
		OrgID: "org-1", WorkspaceID: "ws-1", WorkflowRunID: "run-1",
		Severity: domain.SeverityHigh, Scenario: "schema_drift", RiskScore: approval.RiskScoreHigh,
	})
	require.NoError(t, err)
	require.Equal(t, "decision-1", id)
	require.Len(t, repo.created, 1)
	require.Equal(t, domain.ApprovalPending, repo.created[0].Decision)
	require.Len(t, aud.written, 1)
	require.Equal(t, "approval.requested", aud.written[0].action)
	require.Equal(t, "run-1", aud.written[0].target)
	require.Equal(t, "high", aud.written[0].metadata["severity"])
}

func TestService_AutoApprove_UpdatesAndAudits(t *testing.T) {
	repo := &fakeRepo{}
	aud := &fakeAudit{}
	svc := approval.New(repo, nil, aud)

	require.NoError(t, svc.AutoApprove(context.Background(), "run-1", "org-1"))
	require.Len(t, repo.updates, 1)
	require.Equal(t, domain.ApprovalAutoApproved, repo.updates[0].state)
	require.Empty(t, repo.updates[0].by)
	require.Len(t, aud.written, 1)
	require.Equal(t, "approval.decided", aud.written[0].action)
	require.Equal(t, true, aud.written[0].metadata["auto"])
}

func TestService_TimeoutReject_UpdatesAndAudits(t *testing.T) {
	repo := &fakeRepo{}
	aud := &fakeAudit{}
	svc := approval.New(repo, nil, aud)

	require.NoError(t, svc.TimeoutReject(context.Background(), "run-1", "org-1"))
	require.Len(t, repo.updates, 1)
	require.Equal(t, domain.ApprovalTimeoutRejected, repo.updates[0].state)
	require.Len(t, aud.written, 1)
	require.Equal(t, true, aud.written[0].metadata["auto"])
}

func TestService_RecordDecision_FlipsPendingAndAudits(t *testing.T) {
	repo := &fakeRepo{}
	aud := &fakeAudit{}
	svc := approval.New(repo, nil, aud)

	require.NoError(t, svc.RecordDecision(context.Background(), "run-1", "org-1", domain.ApprovalSignal{
		Decision: domain.ApprovalApproved, DecidedBy: "user-3", Notes: "lgtm",
	}))
	require.Len(t, repo.updates, 1)
	require.Equal(t, domain.ApprovalApproved, repo.updates[0].state)
	require.Equal(t, "user-3", repo.updates[0].by)
	require.Equal(t, "lgtm", repo.updates[0].notes)
	require.Len(t, aud.written, 1)
	require.Equal(t, false, aud.written[0].metadata["auto"])
}

func TestService_Notify_NoopWhenNotifierNil(t *testing.T) {
	svc := approval.New(&fakeRepo{}, nil, &fakeAudit{})
	// Should not panic.
	svc.Notify(context.Background(), domain.Notification{Kind: domain.NotifApprovalRequested})
}

func TestService_Notify_DelegatesToInjectedNotifier(t *testing.T) {
	notif := &fakeNotifier{}
	svc := approval.New(&fakeRepo{}, notif, &fakeAudit{})
	svc.Notify(context.Background(), domain.Notification{OrgID: "org-1", Kind: domain.NotifApprovalRequested})
	require.Len(t, notif.sent, 1)
	require.Equal(t, "org-1", notif.sent[0].OrgID)
}
