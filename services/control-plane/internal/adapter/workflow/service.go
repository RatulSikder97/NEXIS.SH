// Package workflow implements domain.WorkflowService on top of a Temporal
// client + the workflow repo + an SSE broker. The HTTP transport layer
// depends on the port (domain.WorkflowService); this is the only place that
// knows how to talk to Temporal.
package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"go.temporal.io/sdk/client"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/repo"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/sse"
	recoverywf "github.com/nexis-eco/nexis/services/control-plane/internal/workflow/recovery"
)

// Config bundles every dependency the service needs. The Workspaces repo is
// only used for the OwnsWorkspace check on Start/List — it's lifted out so
// tests can stub it without dragging in the full repo.
type Config struct {
	Repo       *repo.WorkflowRepo
	Workspaces *repo.WorkspacesRepo
	Temporal   client.Client
	Broker     *sse.Broker[domain.ActivityEvent]
	TaskQueue  string
	Logger     *slog.Logger
}

// Service is the WorkflowService implementation. Zero value is unusable —
// construct via New.
type Service struct{ cfg Config }

// New returns a configured Service.
func New(cfg Config) *Service {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Service{cfg: cfg}
}

// compile-time conformance check
var _ domain.WorkflowService = (*Service)(nil)

// Start verifies workspace ownership, inserts a workflow_runs row inside the
// request tx (so RLS sees app.current_org_id), then calls Temporal
// ExecuteWorkflow. On Temporal error the row is marked failed; on success a
// reaper goroutine waits for completion and updates the row to the terminal
// status.
func (s *Service) Start(ctx context.Context, p domain.Principal, workspaceID, workflowType string, inputJSON []byte) (domain.WorkflowRun, error) {
	if s.cfg.Workspaces != nil {
		ok, err := s.cfg.Workspaces.OwnsWorkspace(ctx, p.OrgID, workspaceID)
		if err != nil {
			return domain.WorkflowRun{}, err
		}
		if !ok {
			return domain.WorkflowRun{}, domain.ErrNotFound
		}
	}

	runID := uuid.NewString()
	in := recoverywf.PipelineInput{
		OrgID:       p.OrgID,
		WorkspaceID: workspaceID,
		RunID:       runID,
		IncidentID:  "manual",
		TriggeredBy: "manual",
	}
	if len(inputJSON) > 0 {
		// Best-effort: pull triggered_by + incident payload out of the input
		// payload if the caller supplied one.
		var raw map[string]any
		if err := json.Unmarshal(inputJSON, &raw); err == nil {
			if v, ok := raw["triggered_by"].(string); ok && v != "" {
				in.TriggeredBy = v
			}
			if v, ok := raw["incident_id"].(string); ok && v != "" {
				in.IncidentID = v
			}
			if v, ok := raw["repo_sha"].(string); ok && v != "" {
				in.RepoSHA = v
			}
			if v, ok := raw["project_id"].(string); ok && v != "" {
				in.ProjectID = v
			}
			if inc, ok := raw["incident"].(map[string]any); ok {
				ip := &domain.IncidentPayload{}
				if v, ok := inc["label"].(string); ok {
					ip.Label = v
				}
				if v, ok := inc["title"].(string); ok {
					ip.Title = v
				}
				if v, ok := inc["service"].(string); ok {
					ip.Service = v
				}
				if v, ok := inc["environment"].(string); ok {
					ip.Environment = v
				}
				if v, ok := inc["stacktrace"].(string); ok {
					ip.Stacktrace = v
				}
				if v, ok := inc["logs"].(string); ok {
					ip.Logs = v
				}
				in.Incident = ip
			}
		}
	}

	row := &domain.WorkflowRun{
		ID:            runID,
		OrgID:         p.OrgID,
		WorkspaceID:   workspaceID,
		ProjectID:     in.ProjectID,
		WorkflowType:  workflowType,
		TemporalRunID: "pending", // overwritten after ExecuteWorkflow
		TemporalWfID:  runID,
		Status:        domain.WRQueued,
		Input:         inputJSON,
		StartedAt:     time.Now().UTC().Truncate(time.Microsecond),
		CreatedBy:     p.UserID,
	}
	if err := s.cfg.Repo.InsertRun(ctx, row); err != nil {
		return domain.WorkflowRun{}, err
	}

	// StartWorkflow runs OUTSIDE the request tx (gRPC call, no Postgres) —
	// safe to do after the row exists. Use context.Background so the
	// per-request timeout doesn't abort the call mid-handshake.
	wf, err := s.cfg.Temporal.ExecuteWorkflow(context.Background(), client.StartWorkflowOptions{
		ID:                       runID,
		TaskQueue:                s.cfg.TaskQueue,
		WorkflowExecutionTimeout: 10 * time.Minute,
		WorkflowRunTimeout:       10 * time.Minute,
		WorkflowTaskTimeout:      10 * time.Second,
	}, recoverywf.RecoveryPipeline, in)
	if err != nil {
		now := time.Now().UTC().Truncate(time.Microsecond)
		_ = s.cfg.Repo.UpdateRunStatus(context.Background(), runID, domain.WRFailed, "", err.Error(), nil, &now)
		return domain.WorkflowRun{}, err
	}
	row.TemporalWfID = wf.GetID()
	row.TemporalRunID = wf.GetRunID()
	row.Status = domain.WRRunning

	// UpdateTemporalIDs must run inside the request tx — the InsertRun
	// above is still uncommitted on the admin pool's view. UpdateRunStatus
	// for "running" can land on the admin pool: the row will be visible
	// post-commit and the reaper will overwrite it anyway when the workflow
	// finishes.
	if err := s.cfg.Repo.UpdateTemporalIDs(ctx, runID, wf.GetID(), wf.GetRunID()); err != nil {
		s.cfg.Logger.Warn("update temporal ids", "err", err, "run_id", runID)
	}
	// Status flip to "running" is a nice-to-have; the reaper writes the
	// terminal status next. Skip the explicit transition to avoid a second
	// UPDATE inside the request tx.
	row.Status = domain.WRRunning

	// Reaper goroutine — waits for Temporal-side completion, updates the row.
	go s.reap(runID, wf)
	return *row, nil
}

// reap blocks on wf.Get and updates workflow_runs to the terminal status.
// Runs on context.Background — the workflow can outlive any incoming HTTP
// request. Errors are logged but not surfaced; the next GetRun call will see
// the latest status from the DB.
func (s *Service) reap(runID string, wf client.WorkflowRun) {
	ctx := context.Background()
	var out recoverywf.PipelineOutput
	err := wf.Get(ctx, &out)
	now := time.Now().UTC().Truncate(time.Microsecond)
	status := domain.WRSucceeded
	errStr := ""
	if err != nil {
		status = domain.WRFailed
		errStr = err.Error()
	}
	var outBytes []byte
	if err == nil {
		outBytes, _ = json.Marshal(out)
	}
	if uerr := s.cfg.Repo.UpdateRunStatus(ctx, runID, status, "", errStr, outBytes, &now); uerr != nil {
		s.cfg.Logger.Warn("reap update run", "err", uerr, "run_id", runID)
	}
}

// Get returns the run + every activity event ever recorded for it. The
// events list is ordered by seq ascending.
func (s *Service) Get(ctx context.Context, p domain.Principal, runID string) (domain.WorkflowRun, []domain.ActivityEvent, error) {
	r, err := s.cfg.Repo.GetRun(ctx, p.OrgID, runID)
	if err != nil {
		return domain.WorkflowRun{}, nil, err
	}
	evts, err := s.cfg.Repo.ListEvents(ctx, runID, 0, 500)
	return *r, evts, err
}

// List returns the last N runs for the given workspace, newest first.
func (s *Service) List(ctx context.Context, p domain.Principal, workspaceID string, limit int, before time.Time) ([]domain.WorkflowRun, error) {
	if s.cfg.Workspaces != nil {
		ok, err := s.cfg.Workspaces.OwnsWorkspace(ctx, p.OrgID, workspaceID)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, domain.ErrNotFound
		}
	}
	return s.cfg.Repo.ListRuns(ctx, p.OrgID, workspaceID, limit, before)
}

// Subscribe attaches a tap to the SSE broker for the given run id. The
// returned channel is closed when ctx is cancelled or the broker drops the
// subscription. Phase 4 buffer = 64; sized to absorb a burst from the
// terminal Pipeline.Complete frame.
func (s *Service) Subscribe(ctx context.Context, runID string) <-chan domain.ActivityEvent {
	if s.cfg.Broker == nil {
		// No broker wired (test path) — return an immediately-closed channel.
		ch := make(chan domain.ActivityEvent)
		close(ch)
		return ch
	}
	ch, unsub := s.cfg.Broker.Subscribe(runID, 64)
	go func() {
		<-ctx.Done()
		unsub()
	}()
	return ch
}

// IsNotFound is a small helper so the HTTP layer can stay decoupled from the
// repo. Returns true if err wraps domain.ErrNotFound.
func IsNotFound(err error) bool { return errors.Is(err, domain.ErrNotFound) }
