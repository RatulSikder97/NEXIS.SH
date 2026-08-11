package rolerecommend

// Pure-function coverage for the heuristic engine: each rule's fire/no-fire
// boundary, owner exemption, rule priority, and rationale content — all over
// synthetic Signals rows (no DB). The store's SQL paths are exercised against
// a real Postgres in tests/integration alongside the other repo adapters.

import (
	"strings"
	"testing"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// fixed "now" so day-window arithmetic in cases is deterministic.
var now = time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)

func daysAgo(n int) time.Time { return now.AddDate(0, 0, -n) }

func daysAgoPtr(n int) *time.Time {
	t := daysAgo(n)
	return &t
}

func TestEvaluate(t *testing.T) {
	cfg := DefaultConfig()

	cases := []struct {
		name     string
		s        Signals
		wantRule string      // "" → expect nil
		wantRole domain.Role // checked only when wantRule != ""
	}{
		{
			name: "owner is never evaluated even when dormant",
			s: Signals{
				UserID: "u1", Email: "owner@x.io", Role: domain.RoleOwner,
				MemberSince: daysAgo(400), LastLoginAt: daysAgoPtr(200),
			},
			wantRule: "",
		},
		{
			name: "member below admin-action threshold is left alone",
			s: Signals{
				UserID: "u2", Email: "quiet@x.io", Role: domain.RoleMember,
				MemberSince: daysAgo(200), LastLoginAt: daysAgoPtr(1),
				AdminActions30d: cfg.PromoteAdminActionMin - 1,
			},
			wantRule: "",
		},
		{
			name: "member at admin-action threshold is recommended for admin",
			s: Signals{
				UserID: "u3", Email: "busy@x.io", Role: domain.RoleMember,
				MemberSince: daysAgo(200), LastLoginAt: daysAgoPtr(1),
				AdminActions30d: cfg.PromoteAdminActionMin,
			},
			wantRule: RulePromoteActiveMember,
			wantRole: domain.RoleAdmin,
		},
		{
			name: "admin who never logged in is recommended for downgrade",
			s: Signals{
				UserID: "u4", Email: "ghost@x.io", Role: domain.RoleAdmin,
				MemberSince: daysAgo(120), LastLoginAt: nil,
			},
			wantRule: RuleDemoteInactiveAdmin,
			wantRole: domain.RoleMember,
		},
		{
			name: "admin with 90+ day old login is recommended for downgrade",
			s: Signals{
				UserID: "u5", Email: "away@x.io", Role: domain.RoleAdmin,
				MemberSince: daysAgo(300), LastLoginAt: daysAgoPtr(120),
			},
			wantRule: RuleDemoteInactiveAdmin,
			wantRole: domain.RoleMember,
		},
		{
			name: "freshly-invited admin is not flagged for inactivity",
			s: Signals{
				UserID: "u6", Email: "new@x.io", Role: domain.RoleAdmin,
				MemberSince: daysAgo(10), LastLoginAt: nil,
			},
			wantRule: "",
		},
		{
			name: "active admin using zero admin capability is recommended for downgrade",
			s: Signals{
				UserID: "u7", Email: "reader@x.io", Role: domain.RoleAdmin,
				MemberSince: daysAgo(200), LastLoginAt: daysAgoPtr(2),
				TotalActions60d: cfg.UnusedAdminMinActions, AdminActions60d: 0,
			},
			wantRule: RuleDemoteUnusedAdmin,
			wantRole: domain.RoleMember,
		},
		{
			name: "active admin who uses admin capability is left alone",
			s: Signals{
				UserID: "u8", Email: "working@x.io", Role: domain.RoleAdmin,
				MemberSince: daysAgo(200), LastLoginAt: daysAgoPtr(2),
				TotalActions60d: 40, AdminActions60d: 6,
			},
			wantRule: "",
		},
		{
			name: "quiet admin below the activity sample floor is not flagged as unused",
			s: Signals{
				UserID: "u9", Email: "sparse@x.io", Role: domain.RoleAdmin,
				MemberSince: daysAgo(200), LastLoginAt: daysAgoPtr(2),
				TotalActions60d: cfg.UnusedAdminMinActions - 1, AdminActions60d: 0,
			},
			wantRule: "",
		},
		{
			name: "inactivity outranks unused-capability when both would fire",
			s: Signals{
				UserID: "u10", Email: "gone@x.io", Role: domain.RoleAdmin,
				MemberSince: daysAgo(400), LastLoginAt: daysAgoPtr(150),
				TotalActions60d: 20, AdminActions60d: 0,
			},
			wantRule: RuleDemoteInactiveAdmin,
			wantRole: domain.RoleMember,
		},
		{
			name: "active member with zero admin actions is left alone",
			s: Signals{
				UserID: "u11", Email: "normal@x.io", Role: domain.RoleMember,
				MemberSince: daysAgo(200), LastLoginAt: daysAgoPtr(1),
				TotalActions60d: 50, AdminActions60d: 0,
			},
			wantRule: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Evaluate(now, tc.s, cfg)
			if tc.wantRule == "" {
				if got != nil {
					t.Fatalf("expected nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("expected rule %q, got nil", tc.wantRule)
			}
			if got.Rule != tc.wantRule {
				t.Fatalf("rule: got %q want %q", got.Rule, tc.wantRule)
			}
			if got.RecommendedRole != tc.wantRole {
				t.Fatalf("recommended role: got %q want %q", got.RecommendedRole, tc.wantRole)
			}
			if got.CurrentRole != tc.s.Role {
				t.Fatalf("current role: got %q want %q", got.CurrentRole, tc.s.Role)
			}
			if got.UserID != tc.s.UserID || got.Email != tc.s.Email {
				t.Fatalf("identity not carried through: %+v", got)
			}
			if got.Rationale == "" || !strings.Contains(got.Rationale, tc.s.Email) {
				t.Fatalf("rationale must name the member: %q", got.Rationale)
			}
		})
	}
}

// TestEvaluate_RationaleIsExplainable — the FYP language is "recommend role
// adjustments ... with full audit visibility": the rationale must carry the
// concrete numbers the rule computed, not a generic sentence.
func TestEvaluate_RationaleIsExplainable(t *testing.T) {
	cfg := DefaultConfig()

	promo := Evaluate(now, Signals{
		UserID: "u1", Email: "busy@x.io", Role: domain.RoleMember,
		MemberSince: daysAgo(100), AdminActions30d: 7,
	}, cfg)
	if promo == nil || !strings.Contains(promo.Rationale, "7 owner/admin-gated actions") {
		t.Fatalf("promotion rationale must state the action count: %+v", promo)
	}

	neverLoggedIn := Evaluate(now, Signals{
		UserID: "u2", Email: "ghost@x.io", Role: domain.RoleAdmin,
		MemberSince: daysAgo(120), LastLoginAt: nil,
	}, cfg)
	if neverLoggedIn == nil || !strings.Contains(neverLoggedIn.Rationale, "last login: never") {
		t.Fatalf("inactivity rationale must state the never-logged-in case: %+v", neverLoggedIn)
	}

	staleLogin := Evaluate(now, Signals{
		UserID: "u3", Email: "away@x.io", Role: domain.RoleAdmin,
		MemberSince: daysAgo(300), LastLoginAt: daysAgoPtr(120),
	}, cfg)
	wantDate := daysAgo(120).Format("2006-01-02")
	if staleLogin == nil || !strings.Contains(staleLogin.Rationale, wantDate) {
		t.Fatalf("inactivity rationale must state the last-login date %s: %+v", wantDate, staleLogin)
	}

	unused := Evaluate(now, Signals{
		UserID: "u4", Email: "reader@x.io", Role: domain.RoleAdmin,
		MemberSince: daysAgo(200), LastLoginAt: daysAgoPtr(2),
		TotalActions60d: 23, AdminActions60d: 0,
	}, cfg)
	if unused == nil || !strings.Contains(unused.Rationale, "23 actions in the last 60 days") {
		t.Fatalf("unused-admin rationale must state the activity sample: %+v", unused)
	}
}

// TestEvaluateAll — one pass over a mixed org: exactly the firing members
// come back, in input order, with no cross-contamination of fields.
func TestEvaluateAll(t *testing.T) {
	cfg := DefaultConfig()
	members := []Signals{
		{UserID: "owner", Email: "o@x.io", Role: domain.RoleOwner, MemberSince: daysAgo(500)},
		{UserID: "m-busy", Email: "b@x.io", Role: domain.RoleMember, MemberSince: daysAgo(200),
			LastLoginAt: daysAgoPtr(1), AdminActions30d: 9},
		{UserID: "m-quiet", Email: "q@x.io", Role: domain.RoleMember, MemberSince: daysAgo(200),
			LastLoginAt: daysAgoPtr(3)},
		{UserID: "a-ghost", Email: "g@x.io", Role: domain.RoleAdmin, MemberSince: daysAgo(400),
			LastLoginAt: daysAgoPtr(200)},
	}

	got := EvaluateAll(now, members, cfg)
	if len(got) != 2 {
		t.Fatalf("expected 2 candidates, got %d: %+v", len(got), got)
	}
	if got[0].UserID != "m-busy" || got[0].Rule != RulePromoteActiveMember {
		t.Fatalf("candidate 0: %+v", got[0])
	}
	if got[1].UserID != "a-ghost" || got[1].Rule != RuleDemoteInactiveAdmin {
		t.Fatalf("candidate 1: %+v", got[1])
	}
}

// TestEvaluateAll_EmptyInput — always an empty (non-nil) slice so callers can
// range/len without nil checks; mirrors the always-array JSON convention.
func TestEvaluateAll_EmptyInput(t *testing.T) {
	got := EvaluateAll(now, nil, DefaultConfig())
	if got == nil || len(got) != 0 {
		t.Fatalf("expected empty non-nil slice, got %#v", got)
	}
}
