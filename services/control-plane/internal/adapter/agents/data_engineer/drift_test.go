package data_engineer

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

func TestDiffShapes(t *testing.T) {
	base := []ColumnShape{
		{"orders", "id", "uuid"},
		{"orders", "total_amount", "numeric"},
		{"users", "email", "text"},
	}
	cases := []struct {
		name string
		live []ColumnShape
		want []Drift
	}{
		{
			name: "no drift",
			live: []ColumnShape{
				{"orders", "id", "uuid"},
				{"orders", "total_amount", "numeric"},
				{"users", "email", "text"},
			},
			want: []Drift{},
		},
		{
			name: "column removed",
			live: []ColumnShape{
				{"orders", "id", "uuid"},
				{"users", "email", "text"},
			},
			want: []Drift{
				{Kind: DriftColumnRemoved, Table: "orders", Column: "total_amount", BaselineType: "numeric"},
			},
		},
		{
			name: "column added",
			live: []ColumnShape{
				{"orders", "id", "uuid"},
				{"orders", "total_amount", "numeric"},
				{"users", "email", "text"},
				{"users", "middle_name", "text"},
			},
			want: []Drift{
				{Kind: DriftColumnAdded, Table: "users", Column: "middle_name", LiveType: "text"},
			},
		},
		{
			name: "type changed",
			live: []ColumnShape{
				{"orders", "id", "uuid"},
				{"orders", "total_amount", "text"},
				{"users", "email", "text"},
			},
			want: []Drift{
				{Kind: DriftTypeChanged, Table: "orders", Column: "total_amount", BaselineType: "numeric", LiveType: "text"},
			},
		},
		{
			name: "mixed drift is sorted by table then column",
			live: []ColumnShape{
				{"orders", "id", "uuid"},
				{"orders", "total_amount", "text"},
				{"users", "middle_name", "text"},
			},
			want: []Drift{
				{Kind: DriftTypeChanged, Table: "orders", Column: "total_amount", BaselineType: "numeric", LiveType: "text"},
				{Kind: DriftColumnRemoved, Table: "users", Column: "email", BaselineType: "text"},
				{Kind: DriftColumnAdded, Table: "users", Column: "middle_name", LiveType: "text"},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DiffShapes(base, tc.live)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d drifts %v, want %d", len(got), got, len(tc.want))
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("drift[%d] = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestDriftDetail_MatchesSynthesiserFastPath pins the Detail phrasing to the
// Synthesiser's schema-drift fast-path regex (synthesiser/provider.go
// fastPathRules) — the property that makes a REAL detection classify as
// scenario="schema_drift" and ride the severity-HIGH escalation. If either
// side changes, this test is the tripwire.
func TestDriftDetail_MatchesSynthesiserFastPath(t *testing.T) {
	fastPath := regexp.MustCompile(`(?i)(UndefinedColumn|UndefinedTable|OperationalError.*column|column .* does not exist|relation .* does not exist|psycopg2\.errors)`)
	cases := []struct {
		name  string
		drift Drift
	}{
		{"removed", Drift{Kind: DriftColumnRemoved, Table: "orders", Column: "total_amount", BaselineType: "numeric"}},
		{"added", Drift{Kind: DriftColumnAdded, Table: "users", Column: "middle_name", LiveType: "text"}},
		{"type changed", Drift{Kind: DriftTypeChanged, Table: "orders", Column: "total_amount", BaselineType: "numeric", LiveType: "text"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			detail := tc.drift.Detail()
			if !fastPath.MatchString(detail) {
				t.Errorf("Detail %q does not match the synthesiser schema-drift fast path", detail)
			}
			if !strings.Contains(detail, tc.drift.Table) || !strings.Contains(detail, tc.drift.Column) {
				t.Errorf("Detail %q must name table + column", detail)
			}
		})
	}
}

func TestDriftFingerprint_DeterministicAndOrderInsensitive(t *testing.T) {
	a := driftFingerprint([]string{"alpha", "beta"})
	b := driftFingerprint([]string{"beta", "alpha"})
	if a != b {
		t.Errorf("fingerprint is order-sensitive: %q vs %q", a, b)
	}
	if !strings.HasPrefix(a, "drift-") || len(a) != len("drift-")+16 {
		t.Errorf("unexpected fingerprint shape: %q", a)
	}
	if c := driftFingerprint([]string{"alpha", "gamma"}); c == a {
		t.Errorf("different drift sets must not collide: %q", c)
	}
}

// fakeDriftSource returns a canned report/error.
type fakeDriftSource struct {
	report DriftReport
	err    error
}

func (f *fakeDriftSource) Check(context.Context) (DriftReport, error) { return f.report, f.err }

// fakeIncidentSink records Insert calls.
type fakeIncidentSink struct {
	orgID string
	raw   domain.RawIncident
	calls int
	err   error
}

func (f *fakeIncidentSink) Insert(_ context.Context, orgID string, raw domain.RawIncident) error {
	f.calls++
	f.orgID = orgID
	f.raw = raw
	return f.err
}

func TestDriftChecker_Run(t *testing.T) {
	drift := Drift{Kind: DriftTypeChanged, Table: "orders", Column: "total_amount", BaselineType: "numeric", LiveType: "text"}
	cases := []struct {
		name        string
		source      *fakeDriftSource
		sinkErr     error
		wantErr     bool
		wantInserts int
	}{
		{
			name:   "baseline bootstrap does not alert",
			source: &fakeDriftSource{report: DriftReport{BaselineCaptured: true, BaselineColumns: 42}},
		},
		{
			name:   "no drift is a no-op",
			source: &fakeDriftSource{report: DriftReport{BaselineColumns: 42}},
		},
		{
			name:        "drift lands one incident",
			source:      &fakeDriftSource{report: DriftReport{BaselineColumns: 42, Drifts: []Drift{drift}}},
			wantInserts: 1,
		},
		{
			name:    "source error propagates",
			source:  &fakeDriftSource{err: errors.New("db down")},
			wantErr: true,
		},
		{
			name:        "sink error propagates",
			source:      &fakeDriftSource{report: DriftReport{BaselineColumns: 42, Drifts: []Drift{drift}}},
			sinkErr:     errors.New("insert failed"),
			wantErr:     true,
			wantInserts: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sink := &fakeIncidentSink{err: tc.sinkErr}
			c := &DriftChecker{Source: tc.source, Sink: sink, OrgID: "org-1", Interval: time.Minute}
			err := c.Run(context.Background())
			if tc.wantErr != (err != nil) {
				t.Fatalf("err = %v, wantErr = %v", err, tc.wantErr)
			}
			if sink.calls != tc.wantInserts {
				t.Fatalf("sink calls = %d, want %d", sink.calls, tc.wantInserts)
			}
			if tc.wantInserts == 0 || tc.sinkErr != nil {
				return
			}
			if sink.orgID != "org-1" {
				t.Errorf("org = %q, want org-1", sink.orgID)
			}
			raw := sink.raw
			if raw.Source != "schema_drift" || raw.Level != "fatal" {
				t.Errorf("source/level = %q/%q, want schema_drift/fatal", raw.Source, raw.Level)
			}
			if !strings.HasPrefix(raw.SourceEventID, "drift-") {
				t.Errorf("source_event_id = %q, want drift- prefix", raw.SourceEventID)
			}
			st, _ := raw.Payload["stacktrace"].(string)
			if !strings.Contains(st, drift.Detail()) {
				t.Errorf("stacktrace %q must carry the drift detail", st)
			}
		})
	}
}

func TestDriftChecker_Run_Misconfigured(t *testing.T) {
	cases := []struct {
		name    string
		checker *DriftChecker
	}{
		{"nil checker", nil},
		{"nil source", &DriftChecker{Sink: &fakeIncidentSink{}, OrgID: "o"}},
		{"nil sink", &DriftChecker{Source: &fakeDriftSource{}, OrgID: "o"}},
		{"empty org", &DriftChecker{Source: &fakeDriftSource{}, Sink: &fakeIncidentSink{}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.checker.Run(context.Background()); err == nil {
				t.Fatal("expected misconfiguration error, got nil")
			}
		})
	}
}
