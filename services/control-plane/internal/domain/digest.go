package domain

import "time"

// DigestMetrics is the per-org 24h aggregate the DailyDigest cron computes.
// Mirrors the numbers the console dashboard derives client-side from
// workflow_runs (DashboardCharts.tsx) so the emailed digest and the on-screen
// KPIs never disagree: incidents detected, recovery outcomes, approval-gate
// decisions, MTTR over succeeded runs, and per-agent activity counts.
type DigestMetrics struct {
	IncidentsDetected int            `json:"incidents_detected"`
	RunsStarted       int            `json:"runs_started"`
	RepairsSucceeded  int            `json:"repairs_succeeded"`
	RepairsFailed     int            `json:"repairs_failed"`
	Approved          int            `json:"approved"`
	Rejected          int            `json:"rejected"`
	Modified          int            `json:"modified"`
	AutoApproved      int            `json:"auto_approved"`
	TimeoutRejected   int            `json:"timeout_rejected"`
	PendingApprovals  int            `json:"pending_approvals"`
	MTTRMs            int64          `json:"mttr_ms"` // mean duration of succeeded runs
	AgentActivity     map[string]int `json:"agent_activity"`
}

// DigestReport is one persisted digest_reports row: the metrics snapshot for
// [PeriodStart, PeriodEnd) plus the plain-language insights derived from it.
type DigestReport struct {
	ID          string
	OrgID       string
	PeriodStart time.Time
	PeriodEnd   time.Time
	Metrics     DigestMetrics
	Insights    []string
	CreatedAt   time.Time
}
