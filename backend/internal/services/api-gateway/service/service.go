// Package service hosts api-gateway business logic — currently a thin BFF
// returning representative fixtures so the dashboard renders end-to-end during
// the foundation phase. Recovery-engine integration replaces these in Phase 2.
package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	platerrors "nexis/backend/internal/platform/errors"
)

type Service struct{}

func New() *Service { return &Service{} }

type IncidentSummary struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	Status     string    `json:"status"`
	Severity   string    `json:"severity"`
	Source     string    `json:"source"`
	ServiceID  string    `json:"service_id"`
	IsDemo     bool      `json:"is_demo"`
	DetectedAt time.Time `json:"detected_at"`
}

type IncidentDetail struct {
	IncidentSummary
	WorkflowID string         `json:"workflow_id"`
	Steps      []StepStatus   `json:"steps"`
}

type StepStatus struct {
	Step    string    `json:"step"`
	Agent   string    `json:"agent"`
	Status  string    `json:"status"`
	Started time.Time `json:"started_at"`
}

type AgentSummary struct {
	Name           string  `json:"name"`
	Layer          string  `json:"layer"`
	Status         string  `json:"status"`
	TaskVolume24h  int     `json:"task_volume_24h"`
	SuccessRatePct float64 `json:"success_rate_pct"`
	P95LatencyMs   int     `json:"p95_latency_ms"`
	LastTaskAt     time.Time `json:"last_task_at"`
}

type DemoRun struct {
	RunID      string `json:"run_id"`
	IncidentID string `json:"incident_id"`
}

func (s *Service) ListIncidents(ctx context.Context) ([]IncidentSummary, error) {
	now := time.Now().UTC()
	return []IncidentSummary{
		{ID: "INC-1428", Title: "Schema drift on etl_orders.total_amount", Status: "awaiting_approval", Severity: "high", Source: "sentry", ServiceID: "etl-orders", DetectedAt: now.Add(-2 * time.Minute)},
		{ID: "INC-1427", Title: "checkout-svc rollout failure (bad image tag)", Status: "deployed", Severity: "medium", Source: "argocd", ServiceID: "checkout-svc", DetectedAt: now.Add(-14 * time.Minute)},
		{ID: "INC-1426", Title: "etl_orders pipeline anomaly", Status: "diagnosing", Severity: "high", Source: "sentinel", ServiceID: "etl-orders", DetectedAt: now.Add(-32 * time.Minute)},
		{ID: "INC-1425", Title: "payments-svc auto-rollback", Status: "rolled_back", Severity: "critical", Source: "sentry", ServiceID: "payments-svc", DetectedAt: now.Add(-1 * time.Hour)},
	}, nil
}

func (s *Service) GetIncident(ctx context.Context, id string) (IncidentDetail, error) {
	if id == "" {
		return IncidentDetail{}, platerrors.New(platerrors.KindBadRequest, "id required")
	}
	now := time.Now().UTC()
	return IncidentDetail{
		IncidentSummary: IncidentSummary{
			ID: id, Title: "Schema drift on etl_orders.total_amount",
			Status: "awaiting_approval", Severity: "high", Source: "sentry",
			ServiceID: "etl-orders", DetectedAt: now.Add(-2 * time.Minute),
		},
		WorkflowID: "rec-" + uuid.NewString(),
		Steps: []StepStatus{
			{Step: "detect", Agent: "sentinel", Status: "completed", Started: now.Add(-2 * time.Minute)},
			{Step: "diagnose", Agent: "pathfinder", Status: "completed", Started: now.Add(-110 * time.Second)},
			{Step: "synthesise", Agent: "synthesiser", Status: "completed", Started: now.Add(-90 * time.Second)},
			{Step: "validate", Agent: "validator", Status: "completed", Started: now.Add(-60 * time.Second)},
			{Step: "approve", Agent: "approval_gate", Status: "in_progress", Started: now.Add(-30 * time.Second)},
		},
	}, nil
}

func (s *Service) ListAgents(ctx context.Context) ([]AgentSummary, error) {
	now := time.Now().UTC()
	mk := func(name, layer string, vol int, success float64, p95 int, ago time.Duration) AgentSummary {
		return AgentSummary{Name: name, Layer: layer, Status: "active", TaskVolume24h: vol, SuccessRatePct: success, P95LatencyMs: p95, LastTaskAt: now.Add(-ago)}
	}
	return []AgentSummary{
		// L2 — Self-Healing Loop
		mk("sentinel", "L2", 412, 99.2, 240, time.Minute),
		mk("pathfinder", "L2", 38, 92.1, 1850, 2*time.Minute),
		mk("synthesiser", "L2", 38, 88.4, 6200, 3*time.Minute),
		mk("validator", "L2", 41, 91.2, 84_000, 5*time.Minute),
		// L1 — Execution Team
		mk("architect", "L1", 28, 100.0, 110, 4*time.Minute),
		mk("backend", "L1", 12, 86.5, 4400, 12*time.Minute),
		mk("qa", "L1", 22, 96.0, 2200, 6*time.Minute),
		mk("devops", "L1", 17, 94.1, 9800, 8*time.Minute),
		mk("data_engineer", "L1", 9, 88.8, 7100, 18*time.Minute),
	}, nil
}

func (s *Service) ApproveIncident(ctx context.Context, id, reason string) error {
	if id == "" {
		return platerrors.New(platerrors.KindBadRequest, "id required")
	}
	// stub — in Phase 2 this signals the Temporal workflow.
	return nil
}

func (s *Service) RejectIncident(ctx context.Context, id, reason string) error {
	if id == "" {
		return platerrors.New(platerrors.KindBadRequest, "id required")
	}
	return nil
}

func (s *Service) RunDemoScenario(ctx context.Context, scenarioID string) (DemoRun, error) {
	if scenarioID == "" {
		return DemoRun{}, platerrors.New(platerrors.KindBadRequest, "scenario id required")
	}
	return DemoRun{
		RunID:      uuid.NewString(),
		IncidentID: "DEMO-" + uuid.NewString()[:8],
	}, nil
}
