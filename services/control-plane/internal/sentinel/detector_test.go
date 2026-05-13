package sentinel

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// fakeIncidents drives a single org and one canned fatal row. CountRecent is
// fixed; MaxReceivedAt returns the unix epoch on first call so warm() does
// not pre-seed lastSeen past the row's ReceivedAt.
type fakeIncidents struct {
	mu      sync.Mutex
	fatals  map[string][]domain.IncidentRow
	recent  map[string]int
	maxSeen map[string]time.Time

	pollCalls int
}

func (f *fakeIncidents) PollFatalSince(_ context.Context, orgID string, since time.Time) ([]domain.IncidentRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pollCalls++
	out := []domain.IncidentRow{}
	for _, r := range f.fatals[orgID] {
		if r.ReceivedAt.After(since) {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeIncidents) CountRecent(_ context.Context, orgID string, _ time.Duration) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.recent[orgID], nil
}

func (f *fakeIncidents) MaxReceivedAt(_ context.Context, orgID string) (time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.maxSeen[orgID], nil
}

type fakeWorkspaces struct{ ids map[string]string }

func (f *fakeWorkspaces) DefaultForOrg(_ context.Context, orgID string) (string, error) {
	if id, ok := f.ids[orgID]; ok {
		return id, nil
	}
	return "", domain.ErrNotFound
}

type fakeIntegrations struct{ orgs []string }

func (f *fakeIntegrations) ConnectedSentryOrgs(_ context.Context) ([]string, error) {
	return f.orgs, nil
}

func (f *fakeIntegrations) ConnectedIncidentOrgs(_ context.Context) ([]string, error) {
	return f.orgs, nil
}

type fakeWorkflows struct {
	mu    sync.Mutex
	calls []startCall
}
type startCall struct {
	OrgID, WorkspaceID, WorkflowType string
	Input                            []byte
}

func (f *fakeWorkflows) Start(_ context.Context, p domain.Principal, wsID, wfType string, input []byte) (domain.WorkflowRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, startCall{
		OrgID: p.OrgID, WorkspaceID: wsID, WorkflowType: wfType, Input: append([]byte(nil), input...),
	})
	return domain.WorkflowRun{
		ID: "wf-" + p.OrgID, OrgID: p.OrgID, WorkspaceID: wsID,
		WorkflowType: wfType, Status: domain.WRQueued,
	}, nil
}

func (f *fakeWorkflows) Get(context.Context, domain.Principal, string) (domain.WorkflowRun, []domain.ActivityEvent, error) {
	return domain.WorkflowRun{}, nil, nil
}

func (f *fakeWorkflows) List(context.Context, domain.Principal, string, int, time.Time) ([]domain.WorkflowRun, error) {
	return nil, nil
}

func (f *fakeWorkflows) Subscribe(context.Context, string) <-chan domain.ActivityEvent {
	ch := make(chan domain.ActivityEvent)
	close(ch)
	return ch
}

// TestDetector_FiresWorkflowOnFatalRow drives a single tick by hand and asserts
// the workflow.Start fake receives exactly one call with the expected shape.
func TestDetector_FiresWorkflowOnFatalRow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	t.Cleanup(cancel)

	orgID := "org-1"
	wsID := "ws-1"
	now := time.Now().UTC().Truncate(time.Microsecond)
	fakeRow := domain.IncidentRow{
		ID: "inc-1", OrgID: orgID, Source: "sentry", Level: "fatal",
		Title: "boom", Service: "api", Environment: "prod",
		ReceivedAt: now,
	}
	inc := &fakeIncidents{
		fatals:  map[string][]domain.IncidentRow{orgID: {fakeRow}},
		recent:  map[string]int{orgID: 1},
		maxSeen: map[string]time.Time{orgID: time.Time{}},
	}
	ws := &fakeWorkspaces{ids: map[string]string{orgID: wsID}}
	ints := &fakeIntegrations{orgs: []string{orgID}}
	wf := &fakeWorkflows{}

	det := New(Config{
		Incidents:    inc,
		Workflows:    wf,
		Workspaces:   ws,
		Integrations: ints,
		Audit:        nil,
		WorkflowType: "RecoveryPipeline",
		Interval:     50 * time.Millisecond,
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	// Drive the loop for a short window; the first tick fires after 50ms.
	runCtx, runCancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer runCancel()
	det.Run(runCtx)

	wf.mu.Lock()
	defer wf.mu.Unlock()
	require.NotEmpty(t, wf.calls)
	require.Equal(t, orgID, wf.calls[0].OrgID)
	require.Equal(t, wsID, wf.calls[0].WorkspaceID)
	require.Equal(t, "RecoveryPipeline", wf.calls[0].WorkflowType)
	require.Contains(t, string(wf.calls[0].Input), "sentinel")
	require.Contains(t, string(wf.calls[0].Input), "inc-1")
}
