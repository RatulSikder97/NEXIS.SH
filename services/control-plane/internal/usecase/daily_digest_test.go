package usecase

// Coverage for the DailyDigest cron: per-org aggregation → report insert →
// notifier ping, the once-per-day dedupe (a duplicate insert suppresses the
// email), and the DigestInsights heuristics that turn raw metrics into the
// "actionable insights" strip.

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// ---- fakes -----------------------------------------------------------------

type fakeDigestStore struct {
	mu        sync.Mutex
	orgs      []string
	orgsErr   error
	metrics   map[string]domain.DigestMetrics
	aggErr    error
	inserted  []domain.DigestReport
	insertRet bool // value AdminInsertReport returns for `inserted`
	insertErr error
}

func (f *fakeDigestStore) AdminListActiveOrgs(_ context.Context, _, _ time.Time) ([]string, error) {
	return f.orgs, f.orgsErr
}

func (f *fakeDigestStore) AdminAggregate(_ context.Context, orgID string, _, _ time.Time) (domain.DigestMetrics, error) {
	if f.aggErr != nil {
		return domain.DigestMetrics{}, f.aggErr
	}
	return f.metrics[orgID], nil
}

func (f *fakeDigestStore) AdminInsertReport(_ context.Context, rep domain.DigestReport) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.insertErr != nil {
		return false, f.insertErr
	}
	f.inserted = append(f.inserted, rep)
	return f.insertRet, nil
}

type recordingNotifier struct {
	mu   sync.Mutex
	sent []domain.Notification
}

func (f *recordingNotifier) Channel() string { return "fake" }
func (f *recordingNotifier) Send(_ context.Context, n domain.Notification) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, n)
	return nil
}

// ---- Run -------------------------------------------------------------------

func TestDailyDigest_NilStoreNoOp(t *testing.T) {
	d := &DailyDigest{}
	if err := d.Run(context.Background()); err != nil {
		t.Fatalf("nil store must short-circuit, got %v", err)
	}
}

func TestDailyDigest_GeneratesReportAndNotifies(t *testing.T) {
	store := &fakeDigestStore{
		orgs:      []string{"org-1"},
		insertRet: true,
		metrics: map[string]domain.DigestMetrics{
			"org-1": {
				IncidentsDetected: 3, RunsStarted: 3,
				RepairsSucceeded: 2, RepairsFailed: 1,
				Approved: 1, AutoApproved: 1,
				MTTRMs:        90_000,
				AgentActivity: map[string]int{"backend": 4, "qa": 2},
			},
		},
	}
	notif := &recordingNotifier{}
	d := &DailyDigest{Store: store, Notifier: notif, ConsoleLinkURL: "https://app.nexis.dev/console"}

	if err := d.Run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(store.inserted) != 1 {
		t.Fatalf("want one persisted report, got %d", len(store.inserted))
	}
	rep := store.inserted[0]
	if rep.OrgID != "org-1" || rep.Metrics.IncidentsDetected != 3 {
		t.Fatalf("report must carry the aggregated metrics, got %+v", rep)
	}
	if len(rep.Insights) == 0 {
		t.Fatalf("report must carry derived insights")
	}
	// 24h window.
	if got := rep.PeriodEnd.Sub(rep.PeriodStart); got != 24*time.Hour {
		t.Fatalf("period must span 24h, got %s", got)
	}
	// Email ping with the digest kind + deep link.
	if len(notif.sent) != 1 {
		t.Fatalf("want one notification, got %d", len(notif.sent))
	}
	n := notif.sent[0]
	if n.Kind != domain.NotifDailyDigest || n.OrgID != "org-1" {
		t.Fatalf("notification kind/org wrong: %+v", n)
	}
	if n.LinkURL != "https://app.nexis.dev/console" {
		t.Fatalf("notification must deep-link to the console, got %q", n.LinkURL)
	}
	if !strings.Contains(n.Title, "3 incidents") {
		t.Fatalf("title must summarise the day, got %q", n.Title)
	}
}

func TestDailyDigest_DuplicateDaySuppressesEmail(t *testing.T) {
	store := &fakeDigestStore{
		orgs:      []string{"org-1"},
		insertRet: false, // unique index rejected — already digested today
		metrics:   map[string]domain.DigestMetrics{"org-1": {IncidentsDetected: 1}},
	}
	notif := &recordingNotifier{}
	d := &DailyDigest{Store: store, Notifier: notif}

	if err := d.Run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(notif.sent) != 0 {
		t.Fatalf("duplicate-day insert must suppress the email, got %d sends", len(notif.sent))
	}
}

func TestDailyDigest_OrgFailureDoesNotStarveOthers(t *testing.T) {
	store := &fakeDigestStore{
		orgs:      []string{"org-bad", "org-good"},
		insertRet: true,
		metrics:   map[string]domain.DigestMetrics{"org-good": {IncidentsDetected: 1}},
		// org-bad fails at insert time via a metrics-independent error:
	}
	// Wrap: fail only the first org by erroring aggregation once.
	first := true
	failing := &conditionalDigestStore{inner: store, failFirst: &first}
	notif := &recordingNotifier{}
	d := &DailyDigest{Store: failing, Notifier: notif}

	err := d.Run(context.Background())
	if err == nil {
		t.Fatalf("first org's failure must surface as the tick error")
	}
	if len(store.inserted) != 1 || store.inserted[0].OrgID != "org-good" {
		t.Fatalf("second org must still be digested, got %+v", store.inserted)
	}
}

// conditionalDigestStore fails AdminAggregate for the first org only —
// proves the per-org loop continues past a failure.
type conditionalDigestStore struct {
	inner     *fakeDigestStore
	failFirst *bool
}

func (c *conditionalDigestStore) AdminListActiveOrgs(ctx context.Context, since, until time.Time) ([]string, error) {
	return c.inner.AdminListActiveOrgs(ctx, since, until)
}

func (c *conditionalDigestStore) AdminAggregate(ctx context.Context, orgID string, since, until time.Time) (domain.DigestMetrics, error) {
	if *c.failFirst {
		*c.failFirst = false
		return domain.DigestMetrics{}, errors.New("aggregate boom")
	}
	return c.inner.AdminAggregate(ctx, orgID, since, until)
}

func (c *conditionalDigestStore) AdminInsertReport(ctx context.Context, rep domain.DigestReport) (bool, error) {
	return c.inner.AdminInsertReport(ctx, rep)
}

// ---- DigestInsights --------------------------------------------------------

func TestDigestInsights_QuietDay(t *testing.T) {
	got := DigestInsights(domain.DigestMetrics{})
	if len(got) != 1 || !strings.Contains(got[0], "No incidents") {
		t.Fatalf("quiet day must produce exactly the no-incidents line, got %v", got)
	}
}

func TestDigestInsights_ActionLines(t *testing.T) {
	got := strings.Join(DigestInsights(domain.DigestMetrics{
		IncidentsDetected: 5, RunsStarted: 5,
		RepairsSucceeded: 1, RepairsFailed: 3,
		PendingApprovals: 2, TimeoutRejected: 1,
		MTTRMs:        150_000,
		AgentActivity: map[string]int{"backend": 7, "qa": 7},
	}), "\n")

	for _, want := range []string{
		"5 incident(s) detected",
		"1 succeeded, 3 failed",
		"failed repairs outnumber successes",
		"Mean time to recover: 2m30s",
		"2 approval(s) still pending",
		"timed out unattended",
		// tie on count → alphabetical winner for determinism
		"busiest agent: backend (7)",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("insights missing %q:\n%s", want, got)
		}
	}
}
