package usecase

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// DigestStore is the narrow digest surface DailyDigest needs.
// *repo.DigestRepo satisfies it via structural typing. All methods bypass
// RLS via the admin pool — cron has no principal to pin to.
type DigestStore interface {
	AdminListActiveOrgs(ctx context.Context, since, until time.Time) ([]string, error)
	AdminAggregate(ctx context.Context, orgID string, since, until time.Time) (domain.DigestMetrics, error)
	AdminInsertReport(ctx context.Context, rep domain.DigestReport) (inserted bool, err error)
}

// DailyDigest is the 24h admin summary cron (FYP: "Admins receive ... a
// daily intelligent summary with actionable insights", "reports ...
// generated automatically ... no manual compilation required"). Each tick it
// aggregates, per active org, the trailing 24h of incidents / repairs /
// approval decisions / MTTR / agent activity — the same numbers the console
// dashboard widgets derive from workflow_runs — persists the report in
// digest_reports, and pings every org owner/admin through the existing
// email notifier path with a deep link to the console.
//
// Idempotent per (org, UTC day): the digest_reports unique index rejects a
// second insert for the same day, and a rejected insert also suppresses the
// email so a restarted server never double-sends.
type DailyDigest struct {
	Store    DigestStore
	Notifier domain.Notifier // typically notifier.NewEmail(...); nil = persist only
	Logger   *slog.Logger

	// ConsoleLinkURL is the deep link the digest email carries (the console
	// dashboard). Empty falls back to the email notifier's own default.
	ConsoleLinkURL string
}

// Run executes one digest pass over the trailing 24 hours.
func (d *DailyDigest) Run(ctx context.Context) error {
	if d.Store == nil {
		return nil
	}
	until := time.Now().UTC()
	since := until.Add(-24 * time.Hour)

	orgIDs, err := d.Store.AdminListActiveOrgs(ctx, since, until)
	if err != nil {
		return err
	}
	var firstErr error
	for _, orgID := range orgIDs {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := d.runOrg(ctx, orgID, since, until); err != nil {
			if d.Logger != nil {
				d.Logger.Warn("daily_digest: org failed", "org_id", orgID, "err", err)
			}
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// runOrg aggregates + persists + notifies for one org.
func (d *DailyDigest) runOrg(ctx context.Context, orgID string, since, until time.Time) error {
	m, err := d.Store.AdminAggregate(ctx, orgID, since, until)
	if err != nil {
		return err
	}
	insights := DigestInsights(m)
	inserted, err := d.Store.AdminInsertReport(ctx, domain.DigestReport{
		OrgID:       orgID,
		PeriodStart: since,
		PeriodEnd:   until,
		Metrics:     m,
		Insights:    insights,
	})
	if err != nil {
		return err
	}
	if !inserted {
		return nil // already digested today — no duplicate email
	}
	if d.Notifier != nil {
		// Failures are absorbed: the report row is the durable artefact, a
		// mail hiccup shouldn't fail the cron tick (multi-fanout semantics).
		if nerr := d.Notifier.Send(ctx, domain.Notification{
			OrgID:   orgID,
			Kind:    domain.NotifDailyDigest,
			Title:   fmt.Sprintf("Nexis daily digest — %d incidents, %d repairs", m.IncidentsDetected, m.RepairsSucceeded),
			Body:    strings.Join(insights, " "),
			LinkURL: d.ConsoleLinkURL,
		}); nerr != nil && d.Logger != nil {
			d.Logger.Warn("daily_digest: notify failed", "org_id", orgID, "err", nerr)
		}
	}
	if d.Logger != nil {
		d.Logger.Info("daily_digest: report generated",
			"org_id", orgID, "incidents", m.IncidentsDetected,
			"repairs", m.RepairsSucceeded, "mttr_ms", m.MTTRMs)
	}
	return nil
}

// DigestInsights derives the plain-language "actionable insights" strip from
// the raw metrics. Deterministic heuristics — no model call — so the digest
// costs nothing and never hallucinates a number. Exported for unit tests.
func DigestInsights(m domain.DigestMetrics) []string {
	out := []string{}

	switch {
	case m.IncidentsDetected == 0:
		out = append(out, "No incidents detected in the last 24 hours.")
	default:
		out = append(out, fmt.Sprintf("%d incident(s) detected; %d recovery run(s) started.",
			m.IncidentsDetected, m.RunsStarted))
	}

	if m.RepairsSucceeded > 0 || m.RepairsFailed > 0 {
		out = append(out, fmt.Sprintf("Repairs: %d succeeded, %d failed.",
			m.RepairsSucceeded, m.RepairsFailed))
	}
	if m.RepairsFailed > m.RepairsSucceeded && m.RepairsFailed > 0 {
		out = append(out, "Action: failed repairs outnumber successes — review rejected patches and agent transcripts.")
	}

	if m.MTTRMs > 0 {
		out = append(out, fmt.Sprintf("Mean time to recover: %s.",
			(time.Duration(m.MTTRMs)*time.Millisecond).Round(time.Second)))
	}

	decided := m.Approved + m.Rejected + m.Modified + m.AutoApproved + m.TimeoutRejected
	if decided > 0 {
		out = append(out, fmt.Sprintf("Approval gate: %d approved, %d rejected, %d modified, %d auto-approved, %d timed out.",
			m.Approved, m.Rejected, m.Modified, m.AutoApproved, m.TimeoutRejected))
	}
	if m.PendingApprovals > 0 {
		out = append(out, fmt.Sprintf("Action: %d approval(s) still pending — a parked pipeline blocks its recovery.",
			m.PendingApprovals))
	}
	if m.TimeoutRejected > 0 {
		out = append(out, "Action: approvals timed out unattended — consider widening the medium-severity countdown or adding approvers.")
	}

	if len(m.AgentActivity) > 0 {
		type kv struct {
			role string
			n    int
		}
		rows := make([]kv, 0, len(m.AgentActivity))
		total := 0
		for role, n := range m.AgentActivity {
			rows = append(rows, kv{role, n})
			total += n
		}
		sort.Slice(rows, func(i, j int) bool {
			if rows[i].n != rows[j].n {
				return rows[i].n > rows[j].n
			}
			return rows[i].role < rows[j].role
		})
		out = append(out, fmt.Sprintf("Agent activity: %d completed step(s); busiest agent: %s (%d).",
			total, rows[0].role, rows[0].n))
	}
	return out
}
