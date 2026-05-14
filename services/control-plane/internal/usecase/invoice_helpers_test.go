package usecase

// Coverage for the InvoiceRoller's pure helpers. The Run path needs a
// BillingStore — that's exercised under tests/integration with the local
// provider.

import (
	"context"
	"testing"
	"time"
)

// TestMonthBounds_RoundsToFirstAndNextMonth confirms the helper returns the
// inclusive start + exclusive end of the calendar month for various inputs.
func TestMonthBounds_RoundsToFirstAndNextMonth(t *testing.T) {
	cases := []struct {
		name      string
		in        time.Time
		wantStart time.Time
		wantEnd   time.Time
	}{
		{
			name:      "mid_month",
			in:        time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC),
			wantStart: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
			wantEnd:   time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:      "first_of_month",
			in:        time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			wantStart: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			wantEnd:   time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:      "december_rolls_to_january",
			in:        time.Date(2025, 12, 31, 23, 59, 59, 0, time.UTC),
			wantStart: time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC),
			wantEnd:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:      "feb_29_leap_year_rolls_to_march",
			in:        time.Date(2024, 2, 29, 12, 0, 0, 0, time.UTC),
			wantStart: time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC),
			wantEnd:   time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:      "non_utc_input_is_converted",
			in:        time.Date(2026, 5, 13, 23, 0, 0, 0, time.FixedZone("PST", -8*3600)),
			wantStart: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
			wantEnd:   time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			start, end := monthBounds(tc.in)
			if !start.Equal(tc.wantStart) {
				t.Fatalf("start: got %v want %v", start, tc.wantStart)
			}
			if !end.Equal(tc.wantEnd) {
				t.Fatalf("end: got %v want %v", end, tc.wantEnd)
			}
		})
	}
}

// TestInvoiceRoller_NilBillingIsNoOp — when the dependency is nil the
// Run path short-circuits without panicking.
func TestInvoiceRoller_NilBillingIsNoOp(t *testing.T) {
	r := &InvoiceRoller{Billing: nil}
	if err := r.Run(context.Background()); err != nil {
		t.Fatalf("nil billing must short-circuit, got %v", err)
	}
}
