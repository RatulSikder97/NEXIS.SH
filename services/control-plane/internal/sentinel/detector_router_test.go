package sentinel

// Coverage for the Detector.tick path with a router wired. The existing
// detector_test.go exercises tick WITHOUT a router; this file adds:
//   1. tick with a router that resolves a project_id → trigger is stamped,
//      and the workflow input carries `project_id`.
//   2. tick with a router that misses → ProjectID stays empty.

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// stubMatcher returns a fixed project_id for any sentry org/project pair.
// Used to stamp the trigger so we can assert on the persisted workflow input.
type stubMatcher struct {
	mu        sync.Mutex
	projectID string
	hit       bool
	calls     int
}

func (m *stubMatcher) MatchByFingerprint(_ context.Context, _ string, _ domain.IncidentFingerprint) (string, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	if !m.hit {
		return "", false, nil
	}
	return m.projectID, true, nil
}

// stubIncidentUpdater records every UpdateProjectID call. Not exercised by
// the tick path directly today (the matcher returns "" or a value, the
// updater path is hit only when both project_id and IncidentRawID are
// populated), but kept here for symmetry with the router test.
type stubIncidentUpdater struct {
	mu    sync.Mutex
	calls []routerUpdateCall
}

type routerUpdateCall struct {
	IncidentRawID string
	ProjectID     string
}

func (u *stubIncidentUpdater) UpdateProjectID(_ context.Context, incidentRawID, projectID string) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.calls = append(u.calls, routerUpdateCall{IncidentRawID: incidentRawID, ProjectID: projectID})
	return nil
}

// buildSentryIncident is a tiny constructor so the table-test rows stay
// compact. Builds an IncidentRow that will pass through Apply as a fatal.
func buildSentryIncident(orgID, id string, ts time.Time) domain.IncidentRow {
	return domain.IncidentRow{
		ID: id, OrgID: orgID, Source: "sentry", Level: "fatal",
		Title:                  "boom",
		Service:                "api",
		Environment:            "prod",
		SentryOrganizationSlug: "acme",
		SentryProjectSlug:      "orders-api",
		ReceivedAt:             ts,
	}
}

// TestDetector_TickWithRouter_StampsProjectIDOnWorkflowInput drives one tick
// with a router that resolves the trigger's fingerprint to "proj-123". The
// resulting workflow input must carry "project_id":"proj-123".
func TestDetector_TickWithRouter_StampsProjectIDOnWorkflowInput(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	t.Cleanup(cancel)

	orgID := "org-router-1"
	wsID := "ws-router-1"
	now := time.Now().UTC().Truncate(time.Microsecond)
	row := buildSentryIncident(orgID, "inc-router-1", now)

	inc := &fakeIncidents{
		fatals:  map[string][]domain.IncidentRow{orgID: {row}},
		recent:  map[string]int{orgID: 1},
		maxSeen: map[string]time.Time{orgID: time.Time{}},
	}
	ws := &fakeWorkspaces{ids: map[string]string{orgID: wsID}}
	ints := &fakeIntegrations{orgs: []string{orgID}}
	wf := &fakeWorkflows{}
	matcher := &stubMatcher{projectID: "proj-router-123", hit: true}
	updater := &stubIncidentUpdater{}

	router := NewRouter(RouterConfig{
		Matcher:  matcher,
		Incident: updater,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	det := New(Config{
		Incidents:    inc,
		Workflows:    wf,
		Workspaces:   ws,
		Integrations: ints,
		Router:       router,
		WorkflowType: "RecoveryPipeline",
		Interval:     50 * time.Millisecond,
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	runCtx, runCancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer runCancel()
	det.Run(runCtx)

	wf.mu.Lock()
	defer wf.mu.Unlock()
	require.NotEmpty(t, wf.calls, "workflow.Start must fire at least once")

	var input map[string]any
	require.NoError(t, json.Unmarshal(wf.calls[0].Input, &input))
	require.Equal(t, "proj-router-123", input["project_id"], "tick must stamp project_id on workflow input")
	require.Greater(t, matcher.calls, 0, "matcher must have been consulted")
}

// TestDetector_TickWithRouter_NoMatchLeavesProjectIDEmpty drives a tick where
// the router's matcher returns no hit. The workflow input must NOT carry a
// project_id key (the production code only stamps it when ProjectID != "").
func TestDetector_TickWithRouter_NoMatchLeavesProjectIDEmpty(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	t.Cleanup(cancel)

	orgID := "org-router-2"
	wsID := "ws-router-2"
	now := time.Now().UTC().Truncate(time.Microsecond)
	row := buildSentryIncident(orgID, "inc-router-2", now)

	inc := &fakeIncidents{
		fatals:  map[string][]domain.IncidentRow{orgID: {row}},
		recent:  map[string]int{orgID: 1},
		maxSeen: map[string]time.Time{orgID: time.Time{}},
	}
	ws := &fakeWorkspaces{ids: map[string]string{orgID: wsID}}
	ints := &fakeIntegrations{orgs: []string{orgID}}
	wf := &fakeWorkflows{}
	matcher := &stubMatcher{} // hit=false → router miss
	updater := &stubIncidentUpdater{}

	router := NewRouter(RouterConfig{
		Matcher:  matcher,
		Incident: updater,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	det := New(Config{
		Incidents:    inc,
		Workflows:    wf,
		Workspaces:   ws,
		Integrations: ints,
		Router:       router,
		WorkflowType: "RecoveryPipeline",
		Interval:     50 * time.Millisecond,
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	runCtx, runCancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer runCancel()
	det.Run(runCtx)

	wf.mu.Lock()
	defer wf.mu.Unlock()
	require.NotEmpty(t, wf.calls)
	var input map[string]any
	require.NoError(t, json.Unmarshal(wf.calls[0].Input, &input))
	_, has := input["project_id"]
	require.False(t, has, "no-match path must omit project_id from input")

	// Updater must NOT be invoked on miss.
	updater.mu.Lock()
	defer updater.mu.Unlock()
	require.Empty(t, updater.calls, "no-match must not invoke incident updater")
}
