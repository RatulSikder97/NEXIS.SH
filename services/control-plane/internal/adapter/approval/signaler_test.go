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

type signalerFakeRepo struct {
	mu      sync.Mutex
	row     domain.ApprovalDecision
	rowErr  error
	updates int
}

func (f *signalerFakeRepo) Create(_ context.Context, _ domain.ApprovalDecision) (string, error) {
	return "id-1", nil
}
func (f *signalerFakeRepo) GetByRun(_ context.Context, _ string) (domain.ApprovalDecision, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.row, f.rowErr
}
func (f *signalerFakeRepo) UpdateDecision(_ context.Context, _ string, _ domain.ApprovalDecisionState, _, _ string, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates++
	return nil
}
func (f *signalerFakeRepo) ListPending(_ context.Context, _ string, _ int) ([]domain.ApprovalDecision, error) {
	return nil, nil
}

type recordingTemSig struct {
	mu      sync.Mutex
	calls   []signalCall
	failErr error
}

type signalCall struct {
	workflowID string
	signal     domain.ApprovalSignal
}

func (r *recordingTemSig) SignalApproval(_ context.Context, wf string, sig domain.ApprovalSignal) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failErr != nil {
		return r.failErr
	}
	r.calls = append(r.calls, signalCall{wf, sig})
	return nil
}

type silentAudit struct{}

func (silentAudit) Write(_ context.Context, _ domain.Principal, _, _ string, _ map[string]any) error {
	return nil
}

func TestSignaler_Decide_HappyPath(t *testing.T) {
	repo := &signalerFakeRepo{row: domain.ApprovalDecision{
		WorkspaceID: "ws-1", Decision: domain.ApprovalPending,
	}}
	tem := &recordingTemSig{}
	svc := approval.New(repo, nil, silentAudit{})
	sig := approval.NewSignalerService(repo, tem, svc)

	err := sig.Decide(context.Background(), approval.DecideInput{
		Principal:     domain.Principal{UserID: "user-1", OrgID: "org-1"},
		WorkspaceID:   "ws-1",
		WorkflowRunID: "run-1",
		Decision:      domain.ApprovalApproved,
		Notes:         "lgtm",
	})
	require.NoError(t, err)
	require.Len(t, tem.calls, 1)
	require.Equal(t, domain.ApprovalApproved, tem.calls[0].signal.Decision)
	require.Equal(t, "user-1", tem.calls[0].signal.DecidedBy)
	require.Equal(t, "lgtm", tem.calls[0].signal.Notes)
	require.Equal(t, 1, repo.updates)
}

func TestSignaler_Decide_RejectsAlreadyDecided(t *testing.T) {
	repo := &signalerFakeRepo{row: domain.ApprovalDecision{
		WorkspaceID: "ws-1", Decision: domain.ApprovalApproved,
	}}
	tem := &recordingTemSig{}
	svc := approval.New(repo, nil, silentAudit{})
	sig := approval.NewSignalerService(repo, tem, svc)

	err := sig.Decide(context.Background(), approval.DecideInput{
		Principal:     domain.Principal{UserID: "user-1", OrgID: "org-1"},
		WorkspaceID:   "ws-1",
		WorkflowRunID: "run-1",
		Decision:      domain.ApprovalApproved,
	})
	require.ErrorIs(t, err, domain.ErrConflict)
	require.Empty(t, tem.calls)
}

func TestSignaler_Decide_RejectsForeignWorkspace(t *testing.T) {
	repo := &signalerFakeRepo{row: domain.ApprovalDecision{
		WorkspaceID: "ws-other", Decision: domain.ApprovalPending,
	}}
	tem := &recordingTemSig{}
	svc := approval.New(repo, nil, silentAudit{})
	sig := approval.NewSignalerService(repo, tem, svc)

	err := sig.Decide(context.Background(), approval.DecideInput{
		Principal:     domain.Principal{UserID: "user-1", OrgID: "org-1"},
		WorkspaceID:   "ws-1",
		WorkflowRunID: "run-1",
		Decision:      domain.ApprovalApproved,
	})
	require.ErrorIs(t, err, domain.ErrForbidden)
	require.Empty(t, tem.calls)
}

func TestSignaler_Decide_RejectsInvalidDecision(t *testing.T) {
	repo := &signalerFakeRepo{}
	tem := &recordingTemSig{}
	svc := approval.New(repo, nil, silentAudit{})
	sig := approval.NewSignalerService(repo, tem, svc)

	err := sig.Decide(context.Background(), approval.DecideInput{
		Principal:     domain.Principal{UserID: "user-1", OrgID: "org-1"},
		WorkspaceID:   "ws-1",
		WorkflowRunID: "run-1",
		Decision:      domain.ApprovalPending, // must be approved|rejected
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "approved|rejected")
}

func TestSignaler_Decide_PropagatesRepoNotFound(t *testing.T) {
	repo := &signalerFakeRepo{rowErr: domain.ErrNotFound}
	tem := &recordingTemSig{}
	svc := approval.New(repo, nil, silentAudit{})
	sig := approval.NewSignalerService(repo, tem, svc)

	err := sig.Decide(context.Background(), approval.DecideInput{
		Principal:     domain.Principal{UserID: "user-1", OrgID: "org-1"},
		WorkspaceID:   "ws-1",
		WorkflowRunID: "run-1",
		Decision:      domain.ApprovalApproved,
	})
	require.ErrorIs(t, err, domain.ErrNotFound)
}

func TestSignaler_Decide_PropagatesSignalFailure(t *testing.T) {
	repo := &signalerFakeRepo{row: domain.ApprovalDecision{
		WorkspaceID: "ws-1", Decision: domain.ApprovalPending,
	}}
	tem := &recordingTemSig{failErr: errors.New("temporal down")}
	svc := approval.New(repo, nil, silentAudit{})
	sig := approval.NewSignalerService(repo, tem, svc)

	err := sig.Decide(context.Background(), approval.DecideInput{
		Principal:     domain.Principal{UserID: "user-1", OrgID: "org-1"},
		WorkspaceID:   "ws-1",
		WorkflowRunID: "run-1",
		Decision:      domain.ApprovalApproved,
	})
	require.ErrorContains(t, err, "temporal down")
	require.Equal(t, 0, repo.updates, "no row update when signal failed")
}
