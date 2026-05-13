// Package workspace implements domain.WorkspaceService — a state machine that
// pretends to provision a compute environment over a few seconds, persisting
// progress to the workspaces row and publishing each step to an SSE broker
// keyed by workspace id.
//
// The service is constructed with a Store (production: *repo.WorkspacesRepo)
// and a generic SSE broker. Splitting Store as a package-local interface keeps
// unit tests off Postgres: see service_test.go's memStore.
package workspace

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/repo"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/sse"
)

// Store is the persistence surface this service needs. *repo.WorkspacesRepo
// satisfies it via structural typing. The interface lives here (not in
// domain/) because it's an implementation detail of the workspace adapter; no
// other layer depends on it.
type Store interface {
	Insert(ctx context.Context, w *domain.Workspace) error
	UpdateStatus(ctx context.Context, id string, status domain.WorkspaceStatus, step, message string) error
	Get(ctx context.Context, orgID, id string) (*domain.Workspace, error)
	List(ctx context.Context, orgID string) ([]domain.Workspace, error)
}

// Config bundles every dependency the service needs. StepBudget is the total
// wall-clock time the provisioning state machine should take; tests pass a
// short budget (e.g. 50ms) to keep the suite fast. FailRate is the synthetic
// failure probability (0.0 == never, 1.0 == always) used to exercise the
// error path during demos.
type Config struct {
	Repo       Store
	Broker     *sse.Broker[domain.ProvisioningStep]
	FailRate   float64
	StepBudget time.Duration
}

// Service is the WorkspaceService implementation. Zero value is unusable —
// construct via New.
type Service struct {
	cfg Config
}

// New returns a configured Service. A zero StepBudget defaults to 5s — short
// enough to feel snappy in the onboarding wizard, long enough to land all 5
// SSE frames in distinct render ticks.
func New(cfg Config) *Service {
	if cfg.StepBudget == 0 {
		cfg.StepBudget = 5 * time.Second
	}
	return &Service{cfg: cfg}
}

// compile-time conformance
var _ domain.WorkspaceService = (*Service)(nil)

// phases is the static script for the provisioning state machine. Sum of
// fractions == 1.0 so cumulative progress lands cleanly on 1.0 at the final
// in_progress step before the ready frame.
var phases = []struct {
	step, label string
	fraction    float64
}{
	{"creating_organization", "Creating organization…", 0.15},
	{"allocating_host", "Allocating host…", 0.25},
	{"provisioning_datacenter", "Provisioning datacenter…", 0.30},
	{"deploying", "Deploying workspace…", 0.30},
}

// slugRetryLimit caps the number of times Create will try -2, -3, … suffixes
// on slug collision. The cap exists as defence-in-depth against an
// indefinite loop if something pathological is going on.
const slugRetryLimit = 50

// Create persists a new workspace row, fires off the provisioning state
// machine on a background goroutine, and returns the just-inserted Workspace
// to the caller. On slug collision the service retries with -2, -3, …
// suffixes up to slugRetryLimit.
func (s *Service) Create(ctx context.Context, p domain.Principal, name, region string) (domain.Workspace, error) {
	if !regionAllowed(region) {
		return domain.Workspace{}, fmt.Errorf("unknown region %q", region)
	}
	baseSlug := slugify(name)
	if baseSlug == "" {
		baseSlug = "workspace"
	}

	var inserted *domain.Workspace
	for i := 0; i < slugRetryLimit; i++ {
		slug := baseSlug
		if i > 0 {
			slug = fmt.Sprintf("%s-%d", baseSlug, i+1)
		}
		w := &domain.Workspace{
			OrgID:            p.OrgID,
			Name:             name,
			Slug:             slug,
			Region:           region,
			Status:           domain.WSProvisioning,
			ProvisioningStep: phases[0].step,
		}
		err := s.cfg.Repo.Insert(ctx, w)
		if err == nil {
			inserted = w
			break
		}
		if !repo.IsUniqueViolation(err) {
			return domain.Workspace{}, err
		}
		// fall through and retry with the next suffix
	}
	if inserted == nil {
		return domain.Workspace{}, fmt.Errorf("could not allocate unique slug for %q after %d attempts", name, slugRetryLimit)
	}

	// Drive the state machine on a background goroutine — keep the
	// HTTP-request-scoped ctx out of it so client disconnects don't abort
	// provisioning.
	go s.run(*inserted)
	return *inserted, nil
}

// run executes the provisioning script. Sleeps between phases, optionally
// emits a synthetic failure on the middle phase when willFail is true, and
// fires the final "ready" frame at the end.
func (s *Service) run(w domain.Workspace) {
	ctx := context.Background()
	cumulative := 0.0
	willFail := s.cfg.FailRate > 0 && rand.Float64() < s.cfg.FailRate

	for i, phase := range phases {
		dur := time.Duration(float64(s.cfg.StepBudget) * phase.fraction)
		time.Sleep(dur)
		cumulative += phase.fraction
		if willFail && i == len(phases)/2 {
			msg := "synthetic failure (demo)"
			_ = s.cfg.Repo.UpdateStatus(ctx, w.ID, domain.WSError, phase.step, msg)
			s.cfg.Broker.Publish(w.ID, domain.ProvisioningStep{
				Step: phase.step, Label: phase.label, Progress: cumulative,
				Status: "error", Message: msg, TS: time.Now(),
			})
			return
		}
		_ = s.cfg.Repo.UpdateStatus(ctx, w.ID, domain.WSProvisioning, phase.step, "")
		s.cfg.Broker.Publish(w.ID, domain.ProvisioningStep{
			Step: phase.step, Label: phase.label, Progress: cumulative, Status: "in_progress", TS: time.Now(),
		})
	}
	_ = s.cfg.Repo.UpdateStatus(ctx, w.ID, domain.WSReady, "ready", "")
	s.cfg.Broker.Publish(w.ID, domain.ProvisioningStep{
		Step: "ready", Label: "Workspace ready", Progress: 1.0, Status: "ready", TS: time.Now(),
	})
}

// Get returns one workspace owned by the calling principal's org. ErrNotFound
// propagates as-is to the handler.
func (s *Service) Get(ctx context.Context, p domain.Principal, id string) (domain.Workspace, error) {
	w, err := s.cfg.Repo.Get(ctx, p.OrgID, id)
	if err != nil {
		return domain.Workspace{}, err
	}
	return *w, nil
}

// List returns all workspaces in the caller's org.
func (s *Service) List(ctx context.Context, p domain.Principal) ([]domain.Workspace, error) {
	return s.cfg.Repo.List(ctx, p.OrgID)
}

// Suspend marks the workspace as suspended. The state machine itself is not
// cancelled — if the workspace is mid-provisioning the next UpdateStatus call
// from run() will race with this one; last write wins, which is acceptable
// because the wire status is what the user sees.
func (s *Service) Suspend(ctx context.Context, p domain.Principal, id string) error {
	return s.cfg.Repo.UpdateStatus(ctx, id, domain.WSSuspended, "suspended", "")
}

// Events returns a channel of provisioning frames for the given workspace.
// The caller (SSE handler) blocks on ctx.Done to unsubscribe and close the
// channel.
func (s *Service) Events(ctx context.Context, workspaceID string) <-chan domain.ProvisioningStep {
	ch, unsub := s.cfg.Broker.Subscribe(workspaceID, 16)
	go func() {
		<-ctx.Done()
		unsub()
	}()
	return ch
}

// regionAllowed reports whether r appears in the static catalog. Region
// allowlisting is enforced at Create time only — the DB stores whatever we
// inserted so a stale row with a since-removed region survives.
func regionAllowed(r string) bool {
	for _, x := range domain.Regions {
		if x.ID == r {
			return true
		}
	}
	return false
}

// slugify reduces a free-form workspace name to a URL-safe slug. Lowercase,
// alphanumeric runs separated by a single dash, no leading/trailing dashes.
// Empty input returns an empty string; the caller substitutes a fallback.
func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	prevDash := false
	for _, c := range s {
		switch {
		case (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9'):
			b.WriteRune(c)
			prevDash = false
		default:
			if !prevDash && b.Len() > 0 {
				b.WriteRune('-')
				prevDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}
