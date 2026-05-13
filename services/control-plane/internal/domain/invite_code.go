package domain

import "time"

// InviteCode is one entry in the system-wide invite_codes table (Phase 8). It
// is NOT scoped to an org — codes are issued by any owner and consumed at
// signup time, before a tenant principal exists. Atomic redemption (used_count
// increment + cap) lives in InviteCodesRepo.Redeem.
//
// MaxUses is the hard cap on Redeem calls (>=1). UsedCount is monotonically
// incremented by Redeem inside a SELECT ... FOR UPDATE so concurrent signups
// can't race past the cap. ExpiresAt nil means the code never expires;
// revocation sets ExpiresAt = now() rather than deleting the row so the audit
// trail stays intact.
type InviteCode struct {
	Code        string
	MaxUses     int
	UsedCount   int
	ExpiresAt   *time.Time
	CreatedBy   *string // user_id; nil for system-seeded codes
	CreatedAt   time.Time
}

// Active reports whether the code can still be redeemed at the given instant.
// Used by the redemption path + the list endpoint to mark stale rows.
func (c InviteCode) Active(now time.Time) bool {
	if c.UsedCount >= c.MaxUses {
		return false
	}
	if c.ExpiresAt != nil && !c.ExpiresAt.After(now) {
		return false
	}
	return true
}
