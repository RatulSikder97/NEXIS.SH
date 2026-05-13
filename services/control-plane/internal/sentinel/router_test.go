package sentinel

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// fakeProjectMatcher is a table-of-fingerprints driver. Each test composes
// the relevant key family and asserts the trigger comes out stamped (or
// not).
type fakeProjectMatcher struct {
	mu      sync.Mutex
	bySent  map[sentryKey]string
	byTag   map[string]string // datadog
	byPSvc  map[string]string // pagerduty
	byRepo  map[string]string // github
	failErr error
	calls   int
}

type sentryKey struct {
	org, project string
}

func newFakeMatcher() *fakeProjectMatcher {
	return &fakeProjectMatcher{
		bySent: map[sentryKey]string{},
		byTag:  map[string]string{},
		byPSvc: map[string]string{},
		byRepo: map[string]string{},
	}
}

func (f *fakeProjectMatcher) MatchByFingerprint(_ context.Context, _ string, fp domain.IncidentFingerprint) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.failErr != nil {
		return "", false, f.failErr
	}
	switch fp.Source {
	case "sentry":
		if id, ok := f.bySent[sentryKey{org: fp.SentryOrganizationSlug, project: fp.SentryProjectSlug}]; ok {
			return id, true, nil
		}
	case "datadog":
		if id, ok := f.byTag[fp.DatadogServiceTag]; ok {
			return id, true, nil
		}
	case "pagerduty":
		if id, ok := f.byPSvc[fp.PagerDutyServiceID]; ok {
			return id, true, nil
		}
	case "github":
		if id, ok := f.byRepo[fp.GitHubRepo]; ok {
			return id, true, nil
		}
	}
	return "", false, nil
}

// fakeIncidentUpdater records each UpdateProjectID call so the test can
// assert the persistence side-effect fired with the right ids.
type fakeIncidentUpdater struct {
	mu      sync.Mutex
	calls   []updateCall
	failErr error
}

type updateCall struct {
	IncidentRawID string
	ProjectID     string
}

func (f *fakeIncidentUpdater) UpdateProjectID(_ context.Context, incidentRawID, projectID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failErr != nil {
		return f.failErr
	}
	f.calls = append(f.calls, updateCall{IncidentRawID: incidentRawID, ProjectID: projectID})
	return nil
}

func (f *fakeIncidentUpdater) snapshot() []updateCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]updateCall, len(f.calls))
	copy(out, f.calls)
	return out
}

func newTestRouter(matcher ProjectMatcher, updater IncidentUpdater) *Router {
	return NewRouter(RouterConfig{
		Matcher:  matcher,
		Incident: updater,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

func TestRouter_MatchesSentryEventToProject(t *testing.T) {
	matcher := newFakeMatcher()
	matcher.bySent[sentryKey{org: "acme-eng", project: "orders-api"}] = "proj-sentry-1"
	updater := &fakeIncidentUpdater{}
	r := newTestRouter(matcher, updater)

	trig := &domain.IncidentTrigger{
		OrgID:                  "org-1",
		IncidentRawID:          "inc-1",
		Source:                 "sentry",
		SentryOrganizationSlug: "acme-eng",
		SentryProjectSlug:      "orders-api",
	}
	require.NoError(t, r.Route(context.Background(), trig))
	require.Equal(t, "proj-sentry-1", trig.ProjectID)
	require.Equal(t, 1, matcher.calls)
}

func TestRouter_MatchesDatadogServiceTag(t *testing.T) {
	matcher := newFakeMatcher()
	matcher.byTag["service:orders-api"] = "proj-dd-1"
	r := newTestRouter(matcher, &fakeIncidentUpdater{})

	trig := &domain.IncidentTrigger{
		OrgID:             "org-1",
		IncidentRawID:     "inc-dd",
		Source:            "datadog",
		DatadogServiceTag: "service:orders-api",
	}
	require.NoError(t, r.Route(context.Background(), trig))
	require.Equal(t, "proj-dd-1", trig.ProjectID)
}

func TestRouter_MatchesPagerDutyServiceID(t *testing.T) {
	matcher := newFakeMatcher()
	matcher.byPSvc["P1234567"] = "proj-pd-1"
	r := newTestRouter(matcher, &fakeIncidentUpdater{})

	trig := &domain.IncidentTrigger{
		OrgID:              "org-1",
		IncidentRawID:      "inc-pd",
		Source:             "pagerduty",
		PagerDutyServiceID: "P1234567",
	}
	require.NoError(t, r.Route(context.Background(), trig))
	require.Equal(t, "proj-pd-1", trig.ProjectID)
}

func TestRouter_MatchesGitHubRepo(t *testing.T) {
	matcher := newFakeMatcher()
	matcher.byRepo["acme/orders-api"] = "proj-gh-1"
	r := newTestRouter(matcher, &fakeIncidentUpdater{})

	trig := &domain.IncidentTrigger{
		OrgID:         "org-1",
		IncidentRawID: "inc-gh",
		Source:        "github",
		GitHubRepo:    "acme/orders-api",
	}
	require.NoError(t, r.Route(context.Background(), trig))
	require.Equal(t, "proj-gh-1", trig.ProjectID)
}

func TestRouter_NoMatch_LeavesProjectIDEmpty(t *testing.T) {
	matcher := newFakeMatcher() // empty table → every lookup misses
	updater := &fakeIncidentUpdater{}
	r := newTestRouter(matcher, updater)

	trig := &domain.IncidentTrigger{
		OrgID:                  "org-1",
		IncidentRawID:          "inc-2",
		Source:                 "sentry",
		SentryOrganizationSlug: "acme-eng",
		SentryProjectSlug:      "billing",
	}
	require.NoError(t, r.Route(context.Background(), trig))
	require.Empty(t, trig.ProjectID, "no match must leave project id empty")
	// No persistence call on miss.
	require.Empty(t, updater.snapshot(), "updater must not be invoked on no-match")
}

func TestRouter_PersistsProjectIDOnIncidentRaw(t *testing.T) {
	matcher := newFakeMatcher()
	matcher.bySent[sentryKey{org: "acme-eng", project: "orders-api"}] = "proj-sentry-1"
	updater := &fakeIncidentUpdater{}
	r := newTestRouter(matcher, updater)

	trig := &domain.IncidentTrigger{
		OrgID:                  "org-1",
		IncidentRawID:          "inc-7",
		Source:                 "sentry",
		SentryOrganizationSlug: "acme-eng",
		SentryProjectSlug:      "orders-api",
	}
	require.NoError(t, r.Route(context.Background(), trig))
	calls := updater.snapshot()
	require.Len(t, calls, 1)
	require.Equal(t, updateCall{IncidentRawID: "inc-7", ProjectID: "proj-sentry-1"}, calls[0])
}

// TestRouter_PassThroughOnNilFingerprint exercises the rate-spike path: a
// trigger without any source-specific selector should be left alone (no
// matcher call, no updater call) so the workflow falls back to fixtures.
func TestRouter_PassThroughOnNilFingerprint(t *testing.T) {
	matcher := newFakeMatcher()
	updater := &fakeIncidentUpdater{}
	r := newTestRouter(matcher, updater)

	trig := &domain.IncidentTrigger{
		OrgID: "org-1", IncidentRawID: "",
		Rule: "error_rate_spike",
	}
	require.NoError(t, r.Route(context.Background(), trig))
	require.Empty(t, trig.ProjectID)
	require.Equal(t, 0, matcher.calls)
	require.Empty(t, updater.snapshot())
}

// TestRouter_MatcherErrorIsBestEffort verifies a matcher failure logs a warn
// but doesn't propagate — the trigger still fires with ProjectID="".
func TestRouter_MatcherErrorIsBestEffort(t *testing.T) {
	matcher := newFakeMatcher()
	matcher.failErr = errors.New("postgres timeout")
	updater := &fakeIncidentUpdater{}
	r := newTestRouter(matcher, updater)

	trig := &domain.IncidentTrigger{
		OrgID:                  "org-1",
		IncidentRawID:          "inc-8",
		Source:                 "sentry",
		SentryOrganizationSlug: "acme",
		SentryProjectSlug:      "x",
	}
	require.NoError(t, r.Route(context.Background(), trig))
	require.Empty(t, trig.ProjectID)
	require.Empty(t, updater.snapshot())
}

// TestRouter_PersistErrorDoesNotDropMatch verifies a write-back failure
// still leaves trigger.ProjectID stamped — the in-memory flag is what the
// workflow consumes, the DB write is forensic.
func TestRouter_PersistErrorDoesNotDropMatch(t *testing.T) {
	matcher := newFakeMatcher()
	matcher.bySent[sentryKey{org: "acme", project: "x"}] = "proj-X"
	updater := &fakeIncidentUpdater{failErr: errors.New("rls drift")}
	r := newTestRouter(matcher, updater)

	trig := &domain.IncidentTrigger{
		OrgID:                  "org-1",
		IncidentRawID:          "inc-9",
		Source:                 "sentry",
		SentryOrganizationSlug: "acme",
		SentryProjectSlug:      "x",
	}
	require.NoError(t, r.Route(context.Background(), trig))
	require.Equal(t, "proj-X", trig.ProjectID, "match must survive a persistence failure")
}
