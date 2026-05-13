package local

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// memPersistence is an in-memory Persistence used by every test here. It
// records the raw shape received via UpsertPaymentMethod so tests can assert
// no PAN ever leaks past the provider boundary.
type memPersistence struct {
	mu   sync.Mutex
	rows map[string]domain.PaymentMethod
	// captured is the slice of PaymentMethods ever upserted, in order. Used by
	// TestLocal_NeverPersistsPAN to scan all field values.
	captured []domain.PaymentMethod
}

func newMemPersistence() *memPersistence {
	return &memPersistence{rows: map[string]domain.PaymentMethod{}}
}

func (m *memPersistence) UpsertPaymentMethod(_ context.Context, pm domain.PaymentMethod) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	pm.ID = "pm-test"
	pm.CreatedAt = time.Now()
	m.rows[pm.OrgID] = pm
	m.captured = append(m.captured, pm)
	return nil
}

func (m *memPersistence) GetPaymentMethod(_ context.Context, orgID string) (*domain.PaymentMethod, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	pm, ok := m.rows[orgID]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return &pm, nil
}

func (m *memPersistence) DeletePaymentMethod(_ context.Context, orgID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.rows, orgID)
	return nil
}

func TestLocal_AttachVisa_4242(t *testing.T) {
	t.Parallel()
	store := newMemPersistence()
	p := New(store)
	princ := domain.Principal{OrgID: "orgA"}
	in := domain.AddCardInput{
		CardNumber:   "4242 4242 4242 4242",
		ExpMonth:     12,
		ExpYear:      time.Now().Year() + 5,
		CVC:          "123",
		PostalCode:   "10001",
		BillingEmail: "t@example.com",
	}
	pm, err := p.AttachCard(context.Background(), princ, in)
	if err != nil {
		t.Fatalf("AttachCard: %v", err)
	}
	if pm.Brand != "visa" {
		t.Errorf("brand = %q, want visa", pm.Brand)
	}
	if pm.Last4 != "4242" {
		t.Errorf("last4 = %q, want 4242", pm.Last4)
	}
	if pm.Provider != "local" {
		t.Errorf("provider = %q, want local", pm.Provider)
	}
	if !strings.HasPrefix(pm.ExternalCustomerID, "cus_local_") {
		t.Errorf("external_customer_id = %q, want cus_local_ prefix", pm.ExternalCustomerID)
	}
	if !strings.HasPrefix(pm.ExternalPaymentMethodID, "pm_local_") {
		t.Errorf("external_payment_method_id = %q, want pm_local_ prefix", pm.ExternalPaymentMethodID)
	}
}

func TestLocal_RejectsExpired(t *testing.T) {
	t.Parallel()
	store := newMemPersistence()
	fixed := time.Date(2026, time.January, 15, 0, 0, 0, 0, time.UTC)
	p := WithClock(store, func() time.Time { return fixed })
	princ := domain.Principal{OrgID: "orgB"}
	cases := []domain.AddCardInput{
		{CardNumber: "4242424242424242", ExpMonth: 12, ExpYear: 2020, CVC: "123"},
		{CardNumber: "4242424242424242", ExpMonth: 1, ExpYear: 2025, CVC: "123"},
		// same year, earlier month
		{CardNumber: "4242424242424242", ExpMonth: 1, ExpYear: 2026, CVC: "123"}, // fixed = Jan -> month 1 == current, OK? No, expect rejection only when ExpMonth < current
	}
	if _, err := p.AttachCard(context.Background(), princ, cases[0]); err == nil {
		t.Error("year=2020 should be rejected")
	}
	if _, err := p.AttachCard(context.Background(), princ, cases[1]); err == nil {
		t.Error("year=2025 should be rejected")
	}
	// Jan 2026 with clock at Jan 15 2026 — month == current month, not strictly less, so accepted.
	if _, err := p.AttachCard(context.Background(), princ, cases[2]); err != nil {
		t.Errorf("Jan 2026 with current Jan 15 2026 should accept: %v", err)
	}
}

func TestLocal_RejectsShortCVC(t *testing.T) {
	t.Parallel()
	store := newMemPersistence()
	p := New(store)
	princ := domain.Principal{OrgID: "orgC"}
	cases := []string{"", "1", "12", "12345", "abc"}
	for _, cvc := range cases {
		in := domain.AddCardInput{
			CardNumber: "4242424242424242",
			ExpMonth:   12, ExpYear: time.Now().Year() + 1,
			CVC: cvc,
		}
		if _, err := p.AttachCard(context.Background(), princ, in); err == nil {
			t.Errorf("expected rejection for cvc=%q", cvc)
		}
	}
}

func TestLocal_DerivesBrandByBIN(t *testing.T) {
	t.Parallel()
	store := newMemPersistence()
	p := New(store)
	cases := []struct{ number, want string }{
		{"4111111111111111", "visa"},
		{"5454545454545454", "mastercard"},
		{"378282246310005", "amex"},
		{"6011111111111117", "card"},
	}
	for i, c := range cases {
		princ := domain.Principal{OrgID: "brand-" + c.want + "-" + itoa(i)}
		in := domain.AddCardInput{
			CardNumber: c.number,
			ExpMonth:   12, ExpYear: time.Now().Year() + 5,
			CVC: "123",
		}
		pm, err := p.AttachCard(context.Background(), princ, in)
		if err != nil {
			t.Fatalf("%s: AttachCard: %v", c.number, err)
		}
		if pm.Brand != c.want {
			t.Errorf("number=%s brand=%q, want %q", c.number, pm.Brand, c.want)
		}
	}
}

func TestLocal_NeverPersistsPAN(t *testing.T) {
	t.Parallel()
	store := newMemPersistence()
	p := New(store)
	princ := domain.Principal{OrgID: "orgPan"}
	in := domain.AddCardInput{
		CardNumber:   "4111 1111 1111 1111",
		ExpMonth:     12, ExpYear: time.Now().Year() + 5,
		CVC:          "9876",
		PostalCode:   "10001",
		BillingEmail: "leak@example.com",
	}
	if _, err := p.AttachCard(context.Background(), princ, in); err != nil {
		t.Fatalf("AttachCard: %v", err)
	}
	// Scan every persisted field for the PAN or the CVC.
	for _, row := range store.captured {
		fields := []string{
			row.ID, row.OrgID, row.Provider, row.ExternalCustomerID,
			row.ExternalPaymentMethodID, row.Brand, row.Last4, row.BillingEmail,
		}
		for _, f := range fields {
			if strings.Contains(f, "4111111111111111") {
				t.Errorf("PAN leaked in persisted field: %q", f)
			}
			if strings.Contains(f, "9876") {
				t.Errorf("CVC leaked in persisted field: %q", f)
			}
		}
	}
}

// itoa is a tiny strconv.Itoa to avoid pulling strconv in the test file just
// for the brand-test loop's distinct-org-id seeds.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
