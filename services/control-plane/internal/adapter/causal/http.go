package causal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// Client is the HTTP implementation of domain.CausalEngine. It talks to the
// FastAPI sidecar in services/causal-inference (POST /infer).
//
// The sidecar is HTTP, not gRPC — the CAUSAL_GRPC_ENDPOINT env var keeps its
// Phase 6 name for compatibility but carries a host:port that we dial over
// plain HTTP. Endpoint values with no scheme get http:// prepended.
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

// New returns a Client for the given host:port (or full URL). Timeout bounds
// a single /infer call; the Pathfinder tolerates an error by degrading to
// evidence-only output, so a short timeout is preferable to a stalled
// workflow activity.
func New(endpoint string, timeout time.Duration) *Client {
	base := strings.TrimSuffix(endpoint, "/")
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		base = "http://" + base
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &Client{BaseURL: base, HTTP: &http.Client{Timeout: timeout}}
}

// wire shapes — mirror services/causal-inference/src/app.py.

type inferCandidate struct {
	Node                string   `json:"node"`
	Evidence            []string `json:"evidence"`
	InDegree            int      `json:"in_degree"`
	OutDegree           int      `json:"out_degree"`
	DistanceFromSymptom int      `json:"distance_from_symptom"`
}

type inferRequest struct {
	RootCauseCandidates []inferCandidate `json:"root_cause_candidates"`
	Scenario            string           `json:"scenario"`
	IncidentText        string           `json:"incident_text"`
}

type inferRootCause struct {
	Node          string   `json:"node"`
	Confidence    float32  `json:"confidence"`
	EvidenceChain []string `json:"evidence_chain"`
}

type inferResponse struct {
	RootCause       inferRootCause `json:"root_cause"`
	DurationMs      int64          `json:"duration_ms"`
	ScenarioMatched bool           `json:"scenario_matched"`
	Method          string         `json:"method"`
}

// Infer implements domain.CausalEngine.
//
// The sidecar ranks q.Candidates on their own evidence; Scenario is only a
// weak tiebreaker there, and CausalQuery carries none, so we send the empty
// string. RootCauseNode is folded in as an extra candidate when the graph
// produced no traversal hits, so a graph-less deployment still submits the
// crashing symbol rather than falling through to "no_signal".
func (c *Client) Infer(ctx context.Context, q domain.CausalQuery) (domain.CausalResult, error) {
	cands := make([]inferCandidate, 0, len(q.Candidates)+1)
	for _, cd := range q.Candidates {
		ev := cd.Evidence
		if ev == nil {
			ev = []string{}
		}
		cands = append(cands, inferCandidate{
			Node:                cd.Node,
			Evidence:            ev,
			InDegree:            cd.InDegree,
			OutDegree:           cd.OutDegree,
			DistanceFromSymptom: cd.DistanceFromSymptom,
		})
	}
	if len(cands) == 0 && q.RootCauseNode != "" {
		ev := q.Features
		if ev == nil {
			ev = []string{}
		}
		cands = append(cands, inferCandidate{
			Node: q.RootCauseNode, Evidence: ev, DistanceFromSymptom: 0,
		})
	}

	body, err := json.Marshal(inferRequest{
		RootCauseCandidates: cands,
		Scenario:            "",
		IncidentText:        q.Stacktrace,
	})
	if err != nil {
		return domain.CausalResult{}, fmt.Errorf("causal: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/infer", bytes.NewReader(body))
	if err != nil {
		return domain.CausalResult{}, fmt.Errorf("causal: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return domain.CausalResult{}, fmt.Errorf("causal: post /infer: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return domain.CausalResult{}, fmt.Errorf("causal: http %d", resp.StatusCode)
	}

	var out inferResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return domain.CausalResult{}, fmt.Errorf("causal: decode: %w", err)
	}

	// method (graph_evidence_ranking | scenario_prior_fallback | no_signal) is
	// the closest analogue to the estimand name the UI renders under the
	// Pathfinder frame, so it maps onto EstimandName.
	return domain.CausalResult{
		Hypothesis:   out.RootCause.Node,
		Confidence:   out.RootCause.Confidence,
		Evidence:     out.RootCause.EvidenceChain,
		EstimandName: out.Method,
		DurationMs:   out.DurationMs,
	}, nil
}
