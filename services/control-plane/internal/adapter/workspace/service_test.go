package workspace

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/sse"
)

// memStore is an in-memory Store implementation used by every test in this
// file. It satisfies the package-local Store interface — no Postgres needed.
// Concurrent-safe via mu so the state machine goroutine can race with the
// main test goroutine without panicking the race detector.
type memStore struct {
	mu   sync.Mutex
	rows map[string]*domain.Workspace
	seq  int
}

func newMemStore() *memStore { return &memStore{rows: map[string]*domain.Workspace{}} }

func (m *memStore) Insert(_ context.Context, w *domain.Workspace) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	// emulate UNIQUE (org_id, slug)
	for _, existing := range m.rows {
		if existing.OrgID == w.OrgID && existing.Slug == w.Slug {
			// mimic a unique violation: surface a sentinel that the service
			// wouldn't classify as IsUniqueViolation, then test setups that
			// expect the retry path use distinct slugs anyway. We only use
			// memStore for tests that don't depend on slug collision.
			return errFake("duplicate slug")
		}
	}
	m.seq++
	now := time.Now()
	w.ID = fakeID(m.seq)
	w.CreatedAt = now
	w.UpdatedAt = now
	clone := *w
	m.rows[w.ID] = &clone
	return nil
}

func (m *memStore) UpdateStatus(_ context.Context, id string, status domain.WorkspaceStatus, step, message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rows[id]
	if !ok {
		return domain.ErrNotFound
	}
	r.Status = status
	r.ProvisioningStep = step
	r.StatusMessage = message
	r.UpdatedAt = time.Now()
	if status == domain.WSReady {
		n := time.Now()
		r.ReadyAt = &n
	}
	return nil
}

func (m *memStore) Get(_ context.Context, orgID, id string) (*domain.Workspace, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rows[id]
	if !ok || r.OrgID != orgID {
		return nil, domain.ErrNotFound
	}
	clone := *r
	return &clone, nil
}

func (m *memStore) List(_ context.Context, orgID string) ([]domain.Workspace, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []domain.Workspace{}
	for _, r := range m.rows {
		if r.OrgID == orgID {
			out = append(out, *r)
		}
	}
	return out, nil
}

type errFake string

func (e errFake) Error() string { return string(e) }

func fakeID(n int) string {
	const tpl = "00000000-0000-0000-0000-000000000000"
	suffix := []byte("000000000000")
	for i := len(suffix) - 1; i >= 0 && n > 0; i-- {
		suffix[i] = byte('0' + n%10)
		n /= 10
	}
	return tpl[:24] + string(suffix)
}

func newSvc(failRate float64, budget time.Duration) (*Service, *memStore, *sse.Broker[domain.ProvisioningStep]) {
	store := newMemStore()
	broker := sse.New[domain.ProvisioningStep]()
	svc := New(Config{Repo: store, Broker: broker, FailRate: failRate, StepBudget: budget})
	return svc, store, broker
}

func TestService_Create_RegionWhitelist(t *testing.T) {
	t.Parallel()
	svc, _, _ := newSvc(0, 10*time.Millisecond)
	p := domain.Principal{OrgID: "org1", Role: domain.RoleOwner}
	if _, err := svc.Create(context.Background(), p, "demo", "atlantis-1"); err == nil {
		t.Fatal("expected error for unknown region")
	}
	for _, r := range domain.Regions {
		if _, err := svc.Create(context.Background(), p, "demo-"+r.ID, r.ID); err != nil {
			t.Fatalf("Create on whitelisted region %s: %v", r.ID, err)
		}
	}
}

func TestService_Create_PublishesEvents(t *testing.T) {
	t.Parallel()
	svc, _, broker := newSvc(0, 50*time.Millisecond)
	p := domain.Principal{OrgID: "orgPublish", Role: domain.RoleOwner}

	// Subscribe BEFORE Create so we don't miss the first frames. The service
	// re-uses the workspace.ID as the topic — we don't know the id until
	// after Insert, so we tunnel through a second subscription post-Create.
	w, err := svc.Create(context.Background(), p, "Prod 1", "us-east-1")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	ch, unsub := broker.Subscribe(w.ID, 32)
	defer unsub()

	timeout := time.After(2 * time.Second)
	statuses := []string{}
	terminal := ""
	for terminal == "" {
		select {
		case ev := <-ch:
			statuses = append(statuses, ev.Status)
			if ev.Status == "ready" || ev.Status == "error" {
				terminal = ev.Status
			}
		case <-timeout:
			t.Fatalf("timed out waiting for terminal event; saw: %v", statuses)
		}
	}
	if terminal != "ready" {
		t.Fatalf("expected ready terminal status, got %q (sequence: %v)", terminal, statuses)
	}
	// Subscribed AFTER Insert — we may have missed the very first frames.
	// Still expect at least the trailing few + ready.
	if len(statuses) < 2 {
		t.Fatalf("expected at least 2 events post-subscribe, got %d (%v)", len(statuses), statuses)
	}
}

func TestService_Create_FailureMode(t *testing.T) {
	t.Parallel()
	svc, store, broker := newSvc(1.0, 30*time.Millisecond)
	p := domain.Principal{OrgID: "orgFail", Role: domain.RoleOwner}
	w, err := svc.Create(context.Background(), p, "fail-mode", "us-east-1")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	ch, unsub := broker.Subscribe(w.ID, 32)
	defer unsub()
	timeout := time.After(2 * time.Second)
	sawError := false
loop:
	for {
		select {
		case ev := <-ch:
			if ev.Status == "error" {
				sawError = true
				break loop
			}
			if ev.Status == "ready" {
				t.Fatalf("expected error frame, got ready")
			}
		case <-timeout:
			t.Fatalf("timed out without seeing an error frame")
		}
	}
	if !sawError {
		t.Fatal("expected error frame on FailRate=1.0")
	}
	// Allow the state machine goroutine to finalise the row.
	time.Sleep(50 * time.Millisecond)
	row, err := store.Get(context.Background(), p.OrgID, w.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if row.Status != domain.WSError {
		t.Fatalf("expected row.Status=error, got %q", row.Status)
	}
}

func TestService_Slugify(t *testing.T) {
	t.Parallel()
	cases := []struct{ in, want string }{
		{"", ""},
		{"Prod 1", "prod-1"},
		{"A!!!B", "a-b"},
		{"  Trim  Me  ", "trim-me"},
		{"Hello, World!", "hello-world"},
		{"under_score", "under-score"},
		{"123-abc", "123-abc"},
		{"a---b", "a-b"},
	}
	for _, c := range cases {
		got := slugify(c.in)
		if got != c.want {
			t.Errorf("slugify(%q) = %q, want %q", c.in, got, c.want)
		}
		if strings.HasPrefix(got, "-") || strings.HasSuffix(got, "-") {
			t.Errorf("slugify(%q) = %q has leading/trailing dash", c.in, got)
		}
	}
}
