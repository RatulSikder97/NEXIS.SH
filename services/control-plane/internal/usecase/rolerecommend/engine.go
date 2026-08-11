// Package rolerecommend implements the intelligent role-recommendation
// analyser (FYP: "Role Management & Access Control (Intelligent)").
//
// The engine half (this file) is pure: it evaluates a small, explainable rule
// set over per-member Signals already captured by the platform — audit_log
// rows (who did what, when) and sessions rows (login recency). Every
// recommendation carries a plain-language rationale so admins see the WHY,
// not just a suggested role. The store half (store.go) collects Signals via
// SQL and persists the results to role_recommendations (migration 0030).
//
// Deliberately NOT here: a synthetic "behaviour score". The schema captures
// no login IP / device / geo metadata today, so the location-anomaly rule
// from the spec is dropped rather than faked (see task follow-ups).
package rolerecommend

import (
	"fmt"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// AdminAuditActions is the set of audit_log action strings that are only
// emitted by owner|admin- or owner-gated endpoints (the two RequireRole
// sub-groups in transport/http/server.go). A member's actor row carrying one
// of these means they exercised privileged capability at some point — the
// strongest real signal we have for a promotion recommendation.
//
// Keep in sync with the aud.Write callsites in the role-gated handlers;
// ambiguous actions reachable by plain members (apikey.revoked, user.*,
// approval.requested) are intentionally excluded.
var AdminAuditActions = []string{
	"approval.decided",
	"apikey.created",
	"integration.connected",
	"integration.disconnected",
	"integration.probed",
	"invite.issued",
	"invite.revoked",
	"workspace.created",
	"workspace.suspended",
	"project.created",
	"project.updated",
	"project.archived",
	"project.policy_updated",
	"eval.run_created",
	"eval.run_started",
	"billing.payment_method_attached",
	"billing.payment_method_detached",
	"billing.setup_intent_created",
	"incident.sentinel_admin_triggered",
}

// Rule identifiers — persisted to role_recommendations.rule and used for the
// per-rule dismissal cool-down.
const (
	RulePromoteActiveMember = "promote_active_member"
	RuleDemoteInactiveAdmin = "demote_inactive_admin"
	RuleDemoteUnusedAdmin   = "demote_unused_admin"
)

// Signals is one org member's observed behaviour, computed by the store from
// org_members + audit_log + sessions. All counts are real row counts — no
// derived scores.
type Signals struct {
	UserID string
	Email  string
	Role   domain.Role
	// MemberSince is org_members.created_at — rules that punish inactivity
	// only fire once the membership is older than the window they measure,
	// so a freshly-invited admin is never flagged.
	MemberSince time.Time
	// LastLoginAt is max(sessions.created_at) for this user in this org;
	// nil when no session has ever been recorded.
	LastLoginAt *time.Time
	// AdminActions30d counts audit_log rows by this actor in the last 30
	// days whose action is in AdminAuditActions.
	AdminActions30d int
	// TotalActions60d / AdminActions60d count all vs admin-gated audit_log
	// rows by this actor in the last 60 days.
	TotalActions60d int
	AdminActions60d int
}

// Candidate is one computed recommendation, pre-persistence.
type Candidate struct {
	UserID          string
	Email           string
	CurrentRole     domain.Role
	RecommendedRole domain.Role
	Rule            string
	Rationale       string
}

// Config holds the rule thresholds. Exported so the orchestrator can tune
// them without a code change; DefaultConfig matches the FYP prose.
type Config struct {
	// PromoteAdminActionMin — minimum admin-gated actions in the last 30
	// days for a member to be recommended for admin.
	PromoteAdminActionMin int
	// InactivityDays — an admin who hasn't logged in for this many days is
	// recommended for downgrade to member.
	InactivityDays int
	// UnusedAdminWindowDays / UnusedAdminMinActions — an admin with at
	// least MinActions total actions in the window but ZERO admin-gated
	// ones is recommended for downgrade (privileges held but unused).
	UnusedAdminWindowDays int
	UnusedAdminMinActions int
}

// DefaultConfig returns the thresholds used in production.
func DefaultConfig() Config {
	return Config{
		PromoteAdminActionMin: 5,
		InactivityDays:        90,
		UnusedAdminWindowDays: 60,
		UnusedAdminMinActions: 10,
	}
}

// Evaluate runs the rule set over one member's signals and returns at most
// one Candidate (first matching rule wins, in priority order), or nil when no
// rule fires. Owners are never evaluated — ownership transfer is an explicit
// human decision, not a heuristic one — and no rule ever recommends owner.
func Evaluate(now time.Time, s Signals, cfg Config) *Candidate {
	if s.Role == domain.RoleOwner {
		return nil
	}

	// Rule 1 — promote_active_member. A member whose audit trail shows
	// privileged actions (taken while they previously held a higher role,
	// or via delegated flows) is operating like an admin; recommend
	// formalising it.
	if s.Role == domain.RoleMember && s.AdminActions30d >= cfg.PromoteAdminActionMin {
		return &Candidate{
			UserID:          s.UserID,
			Email:           s.Email,
			CurrentRole:     s.Role,
			RecommendedRole: domain.RoleAdmin,
			Rule:            RulePromoteActiveMember,
			Rationale: fmt.Sprintf(
				"%s performed %d owner/admin-gated actions (approvals, integrations, projects, invites) in the last 30 days while holding the member role. Promoting to admin matches their observed responsibilities.",
				s.Email, s.AdminActions30d),
		}
	}

	if s.Role != domain.RoleAdmin {
		return nil
	}

	// Rule 2 — demote_inactive_admin. Dormant privileged accounts are the
	// classic lateral-movement target; shrink the surface until they return.
	inactivityCutoff := now.AddDate(0, 0, -cfg.InactivityDays)
	if s.MemberSince.Before(inactivityCutoff) &&
		(s.LastLoginAt == nil || s.LastLoginAt.Before(inactivityCutoff)) {
		lastLogin := "never"
		if s.LastLoginAt != nil {
			lastLogin = s.LastLoginAt.UTC().Format("2006-01-02")
		}
		return &Candidate{
			UserID:          s.UserID,
			Email:           s.Email,
			CurrentRole:     s.Role,
			RecommendedRole: domain.RoleMember,
			Rule:            RuleDemoteInactiveAdmin,
			Rationale: fmt.Sprintf(
				"%s holds the admin role but has not logged in for %d+ days (last login: %s). Downgrading to member shrinks the privileged surface until they return.",
				s.Email, cfg.InactivityDays, lastLogin),
		}
	}

	// Rule 3 — demote_unused_admin. The admin logs in and is active, but
	// their entire recent audit trail contains zero admin-gated actions:
	// their usage pattern is a member's. Requires a minimum activity sample
	// so a quiet-but-legitimate admin isn't flagged off two rows.
	unusedCutoff := now.AddDate(0, 0, -cfg.UnusedAdminWindowDays)
	if s.MemberSince.Before(unusedCutoff) &&
		s.TotalActions60d >= cfg.UnusedAdminMinActions &&
		s.AdminActions60d == 0 {
		return &Candidate{
			UserID:          s.UserID,
			Email:           s.Email,
			CurrentRole:     s.Role,
			RecommendedRole: domain.RoleMember,
			Rule:            RuleDemoteUnusedAdmin,
			Rationale: fmt.Sprintf(
				"%s holds the admin role and was active (%d actions in the last %d days) but used zero admin-gated capabilities in that window. Their usage pattern matches the member role.",
				s.Email, s.TotalActions60d, cfg.UnusedAdminWindowDays),
		}
	}

	return nil
}

// EvaluateAll maps Evaluate over every member and returns the fired
// candidates. Order follows the input order (the store feeds members sorted
// by join date, so output is deterministic for tests and idempotent inserts).
func EvaluateAll(now time.Time, members []Signals, cfg Config) []Candidate {
	out := []Candidate{}
	for _, m := range members {
		if c := Evaluate(now, m, cfg); c != nil {
			out = append(out, *c)
		}
	}
	return out
}
