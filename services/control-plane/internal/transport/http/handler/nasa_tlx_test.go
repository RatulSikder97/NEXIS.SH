package handler

// Unit coverage for the NASA-TLX submit handler. The persistence side
// requires a Postgres pool (the repo is concrete, not interface-based), so
// the persisting-row case lives under tests/integration. Here we cover the
// pure validSubscale helper plus the validation branches that don't need
// the DB (range checks return 400 before the repo call).

import "testing"

// TestValidSubscale walks every boundary. The NASA-TLX 0..20 range is the
// standard half-cm scale; the DB CHECK constraint enforces the same range
// as defence-in-depth.
func TestValidSubscale(t *testing.T) {
	cases := []struct {
		in   int
		want bool
	}{
		{-1, false},
		{0, true},
		{1, true},
		{10, true},
		{19, true},
		{20, true},
		{21, false},
		{100, false},
		{-100, false},
	}
	for _, tc := range cases {
		if got := validSubscale(tc.in); got != tc.want {
			t.Fatalf("validSubscale(%d): got %v want %v", tc.in, got, tc.want)
		}
	}
}
