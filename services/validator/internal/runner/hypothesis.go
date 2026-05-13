// Package runner — Phase 6 Hypothesis sidecar client.
//
// The Hypothesis property-test runner lives inside the validator container
// as a separate Python process listening on a Unix-domain socket
// (default `/tmp/hypothesis.sock`). This file is the Go-side client: it
// frames a JSON request, writes it to the socket, reads the length-prefixed
// JSON response, and surfaces the failure list to the HTTP handler.
//
// Wire format (mirrors hypothesis-sidecar/sidecar.py):
//
//	request:  4-byte BE length prefix + JSON
//	  { "patch_diff": str,
//	    "repo_path": str,
//	    "max_examples": int,
//	    "max_runtime_seconds": int }
//
//	response: 4-byte BE length prefix + JSON
//	  { "tests_passed": bool,
//	    "failures":    [ ... ],
//	    "duration_ms": int,
//	    "error":       str | null }
package runner

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"time"
)

// HypothesisRequest is the request body sent to the sidecar.
type HypothesisRequest struct {
	PatchDiff         string `json:"patch_diff"`
	RepoPath          string `json:"repo_path,omitempty"`
	MaxExamples       int    `json:"max_examples,omitempty"`
	MaxRuntimeSeconds int    `json:"max_runtime_seconds,omitempty"`
}

// HypothesisFailure mirrors the per-test failure record the sidecar emits.
type HypothesisFailure struct {
	Test           string `json:"test"`
	Counterexample string `json:"counterexample"`
	Shrunk         bool   `json:"shrunk"`
}

// HypothesisResponse is the parsed wire response.
type HypothesisResponse struct {
	TestsPassed bool                `json:"tests_passed"`
	Failures    []HypothesisFailure `json:"failures"`
	DurationMs  int64               `json:"duration_ms"`
	Error       string              `json:"error"`
}

// HypothesisRunner is the surface the property-validate HTTP handler depends
// on. Phase 7 will add a Modal implementation that satisfies the same
// interface without touching the handler.
type HypothesisRunner interface {
	Run(ctx context.Context, req HypothesisRequest) (HypothesisResponse, error)
}

// HypothesisClient dials the sidecar over a Unix socket. The default
// HYPOTHESIS_MAX_RUNTIME_SECONDS budget caps each call at 30 s per Phase 6
// constraints.
type HypothesisClient struct {
	socketPath  string
	dialTimeout time.Duration
}

// NewHypothesisClient builds a client. The socket path defaults to
// `/tmp/hypothesis.sock` and can be overridden via HYPOTHESIS_SOCKET.
func NewHypothesisClient() *HypothesisClient {
	p := os.Getenv("HYPOTHESIS_SOCKET")
	if p == "" {
		p = "/tmp/hypothesis.sock"
	}
	return &HypothesisClient{socketPath: p, dialTimeout: 3 * time.Second}
}

// NewHypothesisClientAt is used by tests to point at a temp socket.
func NewHypothesisClientAt(socketPath string) *HypothesisClient {
	return &HypothesisClient{socketPath: socketPath, dialTimeout: 3 * time.Second}
}

// Run sends one request to the sidecar and returns the parsed response.
// Failures returned by the sidecar are surfaced via the (non-nil)
// HypothesisResponse.Error field; transport errors return a wrapped error
// with an empty response.
func (c *HypothesisClient) Run(ctx context.Context, req HypothesisRequest) (HypothesisResponse, error) {
	if req.MaxRuntimeSeconds <= 0 {
		req.MaxRuntimeSeconds = 30
	}
	if req.MaxExamples <= 0 {
		req.MaxExamples = 20
	}

	d := net.Dialer{Timeout: c.dialTimeout}
	conn, err := d.DialContext(ctx, "unix", c.socketPath)
	if err != nil {
		return HypothesisResponse{}, fmt.Errorf("hypothesis dial: %w", err)
	}
	defer conn.Close()

	// Push the context deadline onto the underlying connection so a
	// cancellation propagates to read/write. We add a 5 s grace beyond
	// the runtime budget so a successful run that *just* hits the cap
	// can still flush its response.
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(time.Duration(req.MaxRuntimeSeconds+5) * time.Second)
	}
	_ = conn.SetDeadline(deadline)

	body, err := json.Marshal(req)
	if err != nil {
		return HypothesisResponse{}, fmt.Errorf("hypothesis marshal: %w", err)
	}
	hdr := make([]byte, 4)
	binary.BigEndian.PutUint32(hdr, uint32(len(body)))
	if _, err := conn.Write(append(hdr, body...)); err != nil {
		return HypothesisResponse{}, fmt.Errorf("hypothesis write: %w", err)
	}

	rHdr := make([]byte, 4)
	if _, err := io.ReadFull(conn, rHdr); err != nil {
		return HypothesisResponse{}, fmt.Errorf("hypothesis read header: %w", err)
	}
	rN := binary.BigEndian.Uint32(rHdr)
	if rN == 0 || rN > 8*1024*1024 {
		return HypothesisResponse{}, errors.New("hypothesis: invalid response length")
	}
	rBody := make([]byte, rN)
	if _, err := io.ReadFull(conn, rBody); err != nil {
		return HypothesisResponse{}, fmt.Errorf("hypothesis read body: %w", err)
	}

	var resp HypothesisResponse
	if err := json.Unmarshal(rBody, &resp); err != nil {
		return HypothesisResponse{}, fmt.Errorf("hypothesis unmarshal: %w", err)
	}
	if resp.Failures == nil {
		resp.Failures = []HypothesisFailure{}
	}
	return resp, nil
}
