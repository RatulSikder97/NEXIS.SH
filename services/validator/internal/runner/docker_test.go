package runner

import "testing"

func TestExtractReport_Found(t *testing.T) {
	out := []byte(`============ test session starts ============
collected 12 items
12 passed in 0.05s
{"summary":{"passed":12,"failed":0,"total":12}}`)
	rep := extractReport(out)
	if rep == nil {
		t.Fatalf("expected report, got nil")
	}
	if rep.Summary.Total != 12 || rep.Summary.Passed != 12 {
		t.Fatalf("unexpected summary: %+v", rep.Summary)
	}
}

func TestExtractReport_Missing(t *testing.T) {
	out := []byte("no json here")
	if got := extractReport(out); got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
}

func TestExtractReport_NoiseBefore(t *testing.T) {
	out := []byte(`some logs {invalid json
============================
{"summary":{"passed":3,"failed":1,"total":4}}`)
	rep := extractReport(out)
	if rep == nil {
		t.Fatalf("expected report")
	}
	if rep.Summary.Failed != 1 || rep.Summary.Total != 4 {
		t.Fatalf("unexpected: %+v", rep.Summary)
	}
}
