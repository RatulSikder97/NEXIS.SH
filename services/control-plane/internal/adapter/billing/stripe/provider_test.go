package stripe

// Coverage for the stripe-billing helper functions + constructor surface.
// The Stripe API roundtrips require the live SDK with a real key; that's
// gated behind STRIPE_SECRET_KEY at integration time.

import (
	"context"
	"errors"
	"testing"

	stripe "github.com/stripe/stripe-go/v82"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// TestNew_RequiresSecretKey — the constructor refuses to boot without
// STRIPE_SECRET_KEY.
func TestNew_RequiresSecretKey(t *testing.T) {
	_, err := New(Config{}, nil)
	if err == nil {
		t.Fatalf("expected error for missing secret key")
	}
}

// TestNew_HappyPath — supplying a non-empty key returns a working Provider.
func TestNew_HappyPath(t *testing.T) {
	p, err := New(Config{SecretKey: "sk_test_dummy"}, nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if p == nil {
		t.Fatalf("nil provider")
	}
	if p.Name() != "stripe" {
		t.Fatalf("name: %q", p.Name())
	}
}

// TestAttachCard_AlwaysRejects — Stripe provider rejects raw card data
// at the boundary; the SetupIntent flow is the only allowed path.
func TestAttachCard_AlwaysRejects(t *testing.T) {
	p, err := New(Config{SecretKey: "sk_test_dummy"}, nil)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	_, attachErr := p.AttachCard(context.Background(), domain.Principal{}, domain.AddCardInput{})
	if attachErr == nil {
		t.Fatalf("AttachCard must reject raw card data")
	}
}

// TestExtractCardDetails — nil pm and nil pm.Card both return zero values.
// Populated card returns its fields verbatim.
func TestExtractCardDetails(t *testing.T) {
	t.Run("nil_pm", func(t *testing.T) {
		brand, last4, m, y := extractCardDetails(nil)
		if brand != "" || last4 != "" || m != 0 || y != 0 {
			t.Fatalf("nil pm: %q %q %d %d", brand, last4, m, y)
		}
	})
	t.Run("nil_card", func(t *testing.T) {
		pm := &stripe.PaymentMethod{Card: nil}
		brand, last4, m, y := extractCardDetails(pm)
		if brand != "" || last4 != "" || m != 0 || y != 0 {
			t.Fatalf("nil card: %q %q %d %d", brand, last4, m, y)
		}
	})
	t.Run("populated", func(t *testing.T) {
		pm := &stripe.PaymentMethod{Card: &stripe.PaymentMethodCard{
			Brand: "visa", Last4: "4242", ExpMonth: 12, ExpYear: 2030,
		}}
		brand, last4, m, y := extractCardDetails(pm)
		if brand != "visa" || last4 != "4242" || m != 12 || y != 2030 {
			t.Fatalf("populated: %q %q %d %d", brand, last4, m, y)
		}
	})
}

// fakePersistence is the minimal Persistence implementation for tests
// that only need to exercise paths that don't reach the Stripe SDK.
type fakePersistence struct {
	pm      *domain.PaymentMethod
	getErr  error
	upErr   error
	delErr  error
	deletes int
}

func (f *fakePersistence) UpsertPaymentMethod(_ context.Context, pm domain.PaymentMethod) error {
	if f.upErr != nil {
		return f.upErr
	}
	f.pm = &pm
	return nil
}

func (f *fakePersistence) GetPaymentMethod(_ context.Context, _ string) (*domain.PaymentMethod, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.pm, nil
}

func (f *fakePersistence) DeletePaymentMethod(_ context.Context, _ string) error {
	f.deletes++
	return f.delErr
}

// TestGetPaymentMethod_PersistenceErrorSurfaces — when the repo errors,
// the provider surfaces the error.
func TestGetPaymentMethod_PersistenceErrorSurfaces(t *testing.T) {
	repo := &fakePersistence{getErr: errors.New("db down")}
	p, _ := New(Config{SecretKey: "sk_test"}, repo)
	_, err := p.GetPaymentMethod(context.Background(), domain.Principal{OrgID: "org-1"})
	if err == nil {
		t.Fatalf("expected error")
	}
}

// TestGetPaymentMethod_HappyPath — happy path dereferences the persisted
// payment method.
func TestGetPaymentMethod_HappyPath(t *testing.T) {
	pm := &domain.PaymentMethod{ID: "pm-1", OrgID: "org-1", Brand: "visa"}
	repo := &fakePersistence{pm: pm}
	p, _ := New(Config{SecretKey: "sk_test"}, repo)
	got, err := p.GetPaymentMethod(context.Background(), domain.Principal{OrgID: "org-1"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.Brand != "visa" {
		t.Fatalf("brand: %q", got.Brand)
	}
}

// TestDetachCard_NoPMReturnsPersistenceDelete — even when no row exists,
// DetachCard still calls DeletePaymentMethod (idempotent contract).
func TestDetachCard_NoPMReturnsPersistenceDelete(t *testing.T) {
	repo := &fakePersistence{pm: nil}
	p, _ := New(Config{SecretKey: "sk_test"}, repo)
	if err := p.DetachCard(context.Background(), domain.Principal{OrgID: "org-1"}); err != nil {
		t.Fatalf("DetachCard: %v", err)
	}
	if repo.deletes != 1 {
		t.Fatalf("expected delete, got %d", repo.deletes)
	}
}
