package runner

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"path/filepath"
	"testing"
	"time"
)

// fakeSidecar boots a Unix-socket listener that mimics the Python
// hypothesis sidecar's framed-JSON protocol. The test cases below swap
// in a handler that returns canned bodies.
func fakeSidecar(t *testing.T, handle func(req HypothesisRequest) HypothesisResponse) string {
	t.Helper()
	sockPath := filepath.Join(t.TempDir(), "hyp.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				hdr := make([]byte, 4)
				if _, err := io.ReadFull(c, hdr); err != nil {
					return
				}
				n := binary.BigEndian.Uint32(hdr)
				body := make([]byte, n)
				if _, err := io.ReadFull(c, body); err != nil {
					return
				}
				var req HypothesisRequest
				if err := json.Unmarshal(body, &req); err != nil {
					return
				}
				resp := handle(req)
				out, _ := json.Marshal(resp)
				outHdr := make([]byte, 4)
				binary.BigEndian.PutUint32(outHdr, uint32(len(out)))
				_, _ = c.Write(append(outHdr, out...))
			}(conn)
		}
	}()
	return sockPath
}

func TestHypothesisClient_PassingRun(t *testing.T) {
	sock := fakeSidecar(t, func(req HypothesisRequest) HypothesisResponse {
		if req.MaxRuntimeSeconds == 0 || req.MaxExamples == 0 {
			t.Errorf("expected defaults injected; got %+v", req)
		}
		return HypothesisResponse{
			TestsPassed: true,
			Failures:    []HypothesisFailure{},
			DurationMs:  42,
		}
	})

	c := NewHypothesisClientAt(sock)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := c.Run(ctx, HypothesisRequest{PatchDiff: "", RepoPath: "/srv/fixture"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !resp.TestsPassed {
		t.Fatalf("expected TestsPassed=true; got %+v", resp)
	}
	if resp.DurationMs != 42 {
		t.Fatalf("expected DurationMs=42; got %d", resp.DurationMs)
	}
}

func TestHypothesisClient_FailingRun(t *testing.T) {
	sock := fakeSidecar(t, func(_ HypothesisRequest) HypothesisResponse {
		return HypothesisResponse{
			TestsPassed: false,
			Failures: []HypothesisFailure{
				{Test: "test_safe_div_zero_always_raises", Counterexample: "a=0", Shrunk: true},
			},
			DurationMs: 1200,
		}
	})
	c := NewHypothesisClientAt(sock)
	resp, err := c.Run(context.Background(), HypothesisRequest{
		PatchDiff:         "diff",
		RepoPath:          "/srv/fixture",
		MaxExamples:       5,
		MaxRuntimeSeconds: 10,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if resp.TestsPassed {
		t.Fatalf("expected TestsPassed=false")
	}
	if len(resp.Failures) != 1 || resp.Failures[0].Test != "test_safe_div_zero_always_raises" {
		t.Fatalf("unexpected failures: %+v", resp.Failures)
	}
	if !resp.Failures[0].Shrunk {
		t.Fatalf("expected Shrunk=true")
	}
}

func TestHypothesisClient_DialError(t *testing.T) {
	c := NewHypothesisClientAt(filepath.Join(t.TempDir(), "missing.sock"))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := c.Run(ctx, HypothesisRequest{}); err == nil {
		t.Fatalf("expected dial error")
	}
}
