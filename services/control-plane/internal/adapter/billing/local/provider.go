// Package local implements a synthetic BillingProvider for Phase 3.5. It
// validates a card body, derives brand + last4 from BIN, and persists ONLY
// the safe fields (brand, last4, expiry, billing email, synthetic ids). Real
// charge / customer wiring lands in Phase 7 via the stripe adapter.
package local

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"strings"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// Persistence is the narrow surface this provider needs from the billing
// repo. *repo.BillingRepo satisfies it via structural typing. Splitting the
// interface out lets unit tests use an in-memory fake.
type Persistence interface {
	UpsertPaymentMethod(ctx context.Context, pm domain.PaymentMethod) error
	GetPaymentMethod(ctx context.Context, orgID string) (*domain.PaymentMethod, error)
	DeletePaymentMethod(ctx context.Context, orgID string) error
}

// Provider is the local billing implementation. It depends on the Persistence
// port for persistence; the repo handles RLS-aware writes via db.FromCtx.
type Provider struct {
	repo Persistence
	// now is the clock used for expiry validation. Default time.Now; tests
	// inject a fixed clock to keep TestLocal_RejectsExpired deterministic.
	now func() time.Time
}

// New returns a Provider with time.Now as the default clock.
func New(r Persistence) *Provider {
	return &Provider{repo: r, now: time.Now}
}

// WithClock returns a Provider whose now() returns the supplied time. Test-
// only — production paths use New.
func WithClock(r Persistence, now func() time.Time) *Provider {
	return &Provider{repo: r, now: now}
}

// compile-time conformance check
var _ domain.BillingProvider = (*Provider)(nil)

// Name returns the wire name of this provider.
func (p *Provider) Name() string { return "local" }

// AttachCard validates the supplied AddCardInput, derives safe display fields,
// generates synthetic customer + payment-method ids, and persists the row. The
// raw card number and CVC are NEVER passed to the repo and never logged.
func (p *Provider) AttachCard(ctx context.Context, princ domain.Principal, in domain.AddCardInput) (domain.PaymentMethod, error) {
	digits := stripSpaces(in.CardNumber)
	if !allDigits(digits) {
		return domain.PaymentMethod{}, fmt.Errorf("card number must contain only digits and spaces")
	}
	if len(digits) < 13 || len(digits) > 19 {
		return domain.PaymentMethod{}, fmt.Errorf("card number must be 13-19 digits")
	}
	if !cvcValid(in.CVC) {
		return domain.PaymentMethod{}, fmt.Errorf("cvc must be 3 or 4 digits")
	}
	if in.ExpMonth < 1 || in.ExpMonth > 12 {
		return domain.PaymentMethod{}, fmt.Errorf("exp_month out of range")
	}
	now := p.now().UTC()
	if in.ExpYear < now.Year() || (in.ExpYear == now.Year() && in.ExpMonth < int(now.Month())) {
		return domain.PaymentMethod{}, fmt.Errorf("card expired")
	}

	brand := brandFromBIN(digits)
	last4 := digits[len(digits)-4:]

	custID, err := genSyntheticID("cus_local_")
	if err != nil {
		return domain.PaymentMethod{}, err
	}
	pmID, err := genSyntheticID("pm_local_")
	if err != nil {
		return domain.PaymentMethod{}, err
	}

	pm := domain.PaymentMethod{
		OrgID:                   princ.OrgID,
		Provider:                "local",
		ExternalCustomerID:      custID,
		ExternalPaymentMethodID: pmID,
		Brand:                   brand,
		Last4:                   last4,
		ExpMonth:                in.ExpMonth,
		ExpYear:                 in.ExpYear,
		BillingEmail:            in.BillingEmail,
	}
	if err := p.repo.UpsertPaymentMethod(ctx, pm); err != nil {
		return domain.PaymentMethod{}, err
	}
	// Re-fetch so the caller gets the persisted ID + CreatedAt.
	out, err := p.repo.GetPaymentMethod(ctx, princ.OrgID)
	if err != nil {
		return domain.PaymentMethod{}, err
	}
	return *out, nil
}

// GetPaymentMethod returns the org's payment method or domain.ErrNotFound.
func (p *Provider) GetPaymentMethod(ctx context.Context, princ domain.Principal) (domain.PaymentMethod, error) {
	pm, err := p.repo.GetPaymentMethod(ctx, princ.OrgID)
	if err != nil {
		return domain.PaymentMethod{}, err
	}
	return *pm, nil
}

// DetachCard removes the org's payment method. Idempotent.
func (p *Provider) DetachCard(ctx context.Context, princ domain.Principal) error {
	return p.repo.DeletePaymentMethod(ctx, princ.OrgID)
}

// stripSpaces removes ASCII spaces from s.
func stripSpaces(s string) string {
	return strings.ReplaceAll(s, " ", "")
}

// allDigits reports whether s consists of only ASCII 0-9.
func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// cvcValid reports whether s is a 3- or 4-digit string.
func cvcValid(s string) bool {
	if len(s) != 3 && len(s) != 4 {
		return false
	}
	return allDigits(s)
}

// brandFromBIN derives a card brand from the leading digits per the published
// IIN ranges. Phase 3.5 keeps this small — Visa / Mastercard / Amex / generic.
func brandFromBIN(digits string) string {
	if len(digits) < 2 {
		return "card"
	}
	if digits[0] == '4' {
		return "visa"
	}
	// Mastercard: 51-55 OR 2221-2720. We only check the legacy 51-55 prefix
	// because the synthetic mock covers it and the new range isn't required
	// for Phase 3.5 — Phase 7 swaps in real Stripe brand detection anyway.
	if digits[0] == '5' && digits[1] >= '1' && digits[1] <= '5' {
		return "mastercard"
	}
	if digits[0] == '3' && (digits[1] == '4' || digits[1] == '7') {
		return "amex"
	}
	return "card"
}

// genSyntheticID returns prefix + base32(rand 8 bytes) lower-cased and
// stripped of padding. Used for ExternalCustomerID / ExternalPaymentMethodID
// so the local provider rows look Stripe-shaped without requiring real
// credentials.
func genSyntheticID(prefix string) (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	enc := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b[:])
	return prefix + strings.ToLower(enc), nil
}
