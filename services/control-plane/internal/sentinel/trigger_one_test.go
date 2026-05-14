package sentinel

// Coverage for Detector.TriggerOne — the admin-escape-hatch path used by
// POST /v1/admin/sentinel/trigger. It bypasses the polling loop entirely and
// builds a synthetic IncidentTrigger directly, so it can be exercised with
// the same fakeWorkspaces + fakeWorkflows shape detector_test.go uses.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// recordingAudit captures Write calls so tests can assert the audit row
// emission matches the expected shape.
type recordingAudit struct {
	mu    sync.Mutex
	calls []recAuditCall
}

type recAuditCall struct {
	Principal domain.Principal
	Action    string
	Subject   string
	Meta      map[string]any
}

func (r *recordingAudit) Write(_ context.Context, p domain.Principal, action, subject string, meta map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, recAuditCall{
		Principal: p, Action: action, Subject: subject, Meta: meta,
	})
	return nil
}

// TestDetector_TriggerOne_HappyPath fires a synthetic trigger and confirms:
//   1. Workflow.Start is called once with WorkflowType + a WorkspaceID
//      resolved from the workspaces port.
//   2. The principal is the system principal with the right OrgID.
//   3. The input JSON carries the manual-trigger marker.
//   4. The audit writer receives one row with action="incident.sentinel_admin_triggered".
func TestDetector_TriggerOne_HappyPath(t *testing.T) {
	const orgID = "org-T"
	const wsID = "ws-T"
	const incID = "inc-T-1"

	wf := &fakeWorkflows{}
	ws := &fakeWorkspaces{ids: map[string]string{orgID: wsID}}
	ints := &fakeIntegrations{orgs: []string{orgID}}
	inc := &fakeIncidents{
		fatals:  map[string][]domain.IncidentRow{},
		recent:  map[string]int{},
		maxSeen: map[string]time.Time{},
	}
	aud := &recordingAudit{}

	det := New(Config{
		Incidents:    inc,
		Workflows:    wf,
		Workspaces:   ws,
		Integrations: ints,
		Audit:        aud,
		WorkflowType: "RecoveryPipeline",
		Interval:     50 * time.Millisecond,
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	run, err := det.TriggerOne(context.Background(), orgID, incID)
	require.NoError(t, err)
	require.NotEmpty(t, run.ID, "returned run must have an id")
	require.Equal(t, orgID, run.OrgID)
	require.Equal(t, wsID, run.WorkspaceID)

	// fakeWorkflows captured the call.
	wf.mu.Lock()
	defer wf.mu.Unlock()
	require.Len(t, wf.calls, 1)
	require.Equal(t, orgID, wf.calls[0].OrgID)
	require.Equal(t, wsID, wf.calls[0].WorkspaceID)
	require.Equal(t, "RecoveryPipeline", wf.calls[0].WorkflowType)

	// Input contains the canonical marker fields.
	var input map[string]any
	require.NoError(t, json.Unmarshal(wf.calls[0].Input, &input))
	require.Equal(t, "sentinel_admin", input["triggered_by"])
	require.Equal(t, incID, input["incident_id"])
	require.Equal(t, "manual", input["rule"])

	// Audit row landed.
	aud.mu.Lock()
	defer aud.mu.Unlock()
	require.Len(t, aud.calls, 1)
	require.Equal(t, "incident.sentinel_admin_triggered", aud.calls[0].Action)
	require.Equal(t, run.ID, aud.calls[0].Subject)
	require.Equal(t, orgID, aud.calls[0].Principal.OrgID)
}

// TestDetector_TriggerOne_NoWorkspaceReturnsErrNotFound — the workspaces
// port returns ErrNotFound when the org has no default workspace; the
// admin endpoint maps this to 404 (verified separately in the handler test).
func TestDetector_TriggerOne_NoWorkspaceReturnsErrNotFound(t *testing.T) {
	wf := &fakeWorkflows{}
	ws := &fakeWorkspaces{ids: map[string]string{}} // no entry → ErrNotFound
	ints := &fakeIntegrations{orgs: []string{}}

	det := New(Config{
		Incidents: &fakeIncidents{fatals: map[string][]domain.IncidentRow{}, recent: map[string]int{}, maxSeen: map[string]time.Time{}},
		Workflows: wf, Workspaces: ws, Integrations: ints,
		WorkflowType: "RecoveryPipeline",
		Interval:     50 * time.Millisecond,
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	_, err := det.TriggerOne(context.Background(), "org-missing", "inc-1")
	require.Error(t, err)
	require.ErrorIs(t, err, domain.ErrNotFound)
	// No workflow start happened.
	require.Len(t, wf.calls, 0)
}

// TestDetector_TriggerOne_NilDetectorIsHandled — calling TriggerOne on a
// nil receiver returns a sentinel error rather than panicking. This is the
// defence-in-depth path the handler relies on when the detector wasn't
// wired (boot path before sentinel was enabled).
func TestDetector_TriggerOne_NilDetectorIsHandled(t *testing.T) {
	var d *Detector
	_, err := d.TriggerOne(context.Background(), "org-1", "inc-1")
	require.Error(t, err)
}

// TestDetector_TriggerOne_WorkflowStartFailsSurfacesError — when the
// workflow service returns an error (e.g. Temporal unreachable), the call
// surfaces it without panicking. The audit writer must NOT fire on error.
func TestDetector_TriggerOne_WorkflowStartFailsSurfacesError(t *testing.T) {
	wf := &failingWorkflows{err: errors.New("temporal: unreachable")}
	ws := &fakeWorkspaces{ids: map[string]string{"org-1": "ws-1"}}
	ints := &fakeIntegrations{orgs: []string{"org-1"}}
	aud := &recordingAudit{}

	det := New(Config{
		Incidents: &fakeIncidents{fatals: map[string][]domain.IncidentRow{}, recent: map[string]int{}, maxSeen: map[string]time.Time{}},
		Workflows: wf, Workspaces: ws, Integrations: ints, Audit: aud,
		WorkflowType: "RecoveryPipeline",
		Interval:     50 * time.Millisecond,
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	_, err := det.TriggerOne(context.Background(), "org-1", "inc-1")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unreachable")
	// Audit must not fire — the workflow never started.
	aud.mu.Lock()
	defer aud.mu.Unlock()
	require.Len(t, aud.calls, 0)
}

// failingWorkflows is a tiny shim around the WorkflowService port that
// always errors on Start. Used to exercise the error surface in TriggerOne.
type failingWorkflows struct {
	err error
}

func (f *failingWorkflows) Start(context.Context, domain.Principal, string, string, []byte) (domain.WorkflowRun, error) {
	return domain.WorkflowRun{}, f.err
}

func (f *failingWorkflows) Get(context.Context, domain.Principal, string) (domain.WorkflowRun, []domain.ActivityEvent, error) {
	return domain.WorkflowRun{}, nil, nil
}

func (f *failingWorkflows) List(context.Context, domain.Principal, string, string, int, time.Time) ([]domain.WorkflowRun, error) {
	return nil, nil
}

func (f *failingWorkflows) Subscribe(context.Context, string) <-chan domain.ActivityEvent {
	ch := make(chan domain.ActivityEvent)
	close(ch)
	return ch
}
