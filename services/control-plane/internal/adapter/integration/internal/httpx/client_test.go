package httpx

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newTestServer returns an *httptest.Server whose handler is driven by the
// supplied function. Each invocation gets the running attempt count (1-based)
// so handlers can vary behaviour by attempt without their own state.
func newTestServer(t *testing.T, handler func(attempt int, w http.ResponseWriter, r *http.Request)) (*httptest.Server, *int64) {
	t.Helper()
	var count int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt64(&count, 1)
		handler(int(n), w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, &count
}

// fastBackoff replaces the production sleep with an immediate return so retry
// tests do not actually wait. We still call the limiter for realism (which is
// near-instant given the default 10 RPS, burst 20).
func fastBackoff() func(context.Context, time.Duration) error {
	return func(ctx context.Context, _ time.Duration) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return nil
	}
}

func TestClient_RetriesOn429(t *testing.T) {
	srv, count := newTestServer(t, func(attempt int, w http.ResponseWriter, _ *http.Request) {
		if attempt < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`ok`))
	})

	c := New(srv.Client(), Config{
		RatePerSec:  100,
		Burst:       100,
		MaxAttempts: 3,
		sleepFunc:   fastBackoff(),
	})
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)

	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := atomic.LoadInt64(count); got != 3 {
		t.Fatalf("attempts = %d, want 3", got)
	}
}

func TestClient_RetriesOn5xx(t *testing.T) {
	srv, count := newTestServer(t, func(attempt int, w http.ResponseWriter, _ *http.Request) {
		if attempt < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	c := New(srv.Client(), Config{
		RatePerSec:  100,
		Burst:       100,
		MaxAttempts: 3,
		// Threshold > 2 so the two 5xx attempts before success do not trip
		// the breaker during this test.
		BreakerThreshold: 5,
		sleepFunc:        fastBackoff(),
	})
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)

	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := atomic.LoadInt64(count); got != 3 {
		t.Fatalf("attempts = %d, want 3", got)
	}
}

func TestClient_GivesUpAfterMaxAttempts(t *testing.T) {
	srv, count := newTestServer(t, func(_ int, w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})

	c := New(srv.Client(), Config{
		RatePerSec:       100,
		Burst:            100,
		MaxAttempts:      3,
		BreakerThreshold: 10, // do not trip during this test
		sleepFunc:        fastBackoff(),
	})
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)

	resp, err := c.Do(req)
	if err == nil {
		t.Fatal("Do: expected error after max attempts")
	}
	if resp != nil {
		t.Fatalf("Do: expected nil response, got %d", resp.StatusCode)
	}
	if !strings.Contains(err.Error(), "max attempts") {
		t.Fatalf("error %q does not mention 'max attempts'", err.Error())
	}
	if got := atomic.LoadInt64(count); got != 3 {
		t.Fatalf("attempts = %d, want 3", got)
	}
}

func TestClient_OpensBreakerOnConsecutive5xx(t *testing.T) {
	srv, count := newTestServer(t, func(_ int, w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	// MaxAttempts = 1 keeps each Do single-shot so we can drive the breaker
	// exactly 5 failures → open. Threshold 5 inside a 1 min observation
	// window.
	c := New(srv.Client(), Config{
		RatePerSec:           100,
		Burst:                100,
		MaxAttempts:          1,
		BreakerThreshold:     5,
		BreakerObserveWindow: time.Minute,
		BreakerOpenWindow:    time.Minute,
		sleepFunc:            fastBackoff(),
	})

	for i := 0; i < 5; i++ {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
		_, err := c.Do(req)
		if err == nil {
			t.Fatalf("attempt %d: expected error", i+1)
		}
		if errors.Is(err, ErrBreakerOpen) {
			t.Fatalf("attempt %d: breaker opened too soon", i+1)
		}
	}

	got := atomic.LoadInt64(count)
	if got != 5 {
		t.Fatalf("after 5 failures: server saw %d requests, want 5", got)
	}

	// 6th call must not hit the server.
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	_, err := c.Do(req)
	if !errors.Is(err, ErrBreakerOpen) {
		t.Fatalf("6th call: err = %v, want ErrBreakerOpen", err)
	}
	if got := atomic.LoadInt64(count); got != 5 {
		t.Fatalf("after breaker open: server saw %d requests, want 5", got)
	}
}

// fakeClock is a manually-advanced clock for tests that need to step through
// the breaker timers without sleeping wall-clock.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (f *fakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *fakeClock) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}

func TestClient_BreakerHalfOpensAfterWindow(t *testing.T) {
	// First trip the breaker with 5 consecutive 5xx, then succeed on the
	// probe and confirm a subsequent call is allowed (breaker is closed).
	var failingPhase atomic.Bool
	failingPhase.Store(true)

	srv, count := newTestServer(t, func(_ int, w http.ResponseWriter, _ *http.Request) {
		if failingPhase.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	clock := &fakeClock{now: time.Unix(1_700_000_000, 0)}

	c := New(srv.Client(), Config{
		RatePerSec:           100,
		Burst:                100,
		MaxAttempts:          1,
		BreakerThreshold:     5,
		BreakerObserveWindow: time.Minute,
		BreakerOpenWindow:    30 * time.Second,
		sleepFunc:            fastBackoff(),
		nowFunc:              clock.Now,
	})

	// Trip the breaker.
	for i := 0; i < 5; i++ {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
		_, _ = c.Do(req)
	}
	if got := atomic.LoadInt64(count); got != 5 {
		t.Fatalf("trip phase: server saw %d requests, want 5", got)
	}

	// While still inside the open window the breaker rejects.
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	_, err := c.Do(req)
	if !errors.Is(err, ErrBreakerOpen) {
		t.Fatalf("inside open window: err = %v, want ErrBreakerOpen", err)
	}

	// Advance past the open window and flip the server to success.
	clock.Advance(31 * time.Second)
	failingPhase.Store(false)

	// Probe call — should reach the server and succeed, closing the breaker.
	req, _ = http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("half-open probe: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("half-open probe status = %d, want 200", resp.StatusCode)
	}

	// And a subsequent call is also allowed through (breaker closed).
	req, _ = http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	resp, err = c.Do(req)
	if err != nil {
		t.Fatalf("post-probe call: %v", err)
	}
	_ = resp.Body.Close()
	if got := atomic.LoadInt64(count); got != 7 {
		t.Fatalf("after probe + 1: server saw %d requests, want 7", got)
	}
}

func TestClient_RespectsContextCancellation(t *testing.T) {
	srv, _ := newTestServer(t, func(_ int, w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
		w.WriteHeader(http.StatusOK)
	})

	c := New(srv.Client(), Config{
		RatePerSec:  100,
		Burst:       100,
		MaxAttempts: 3,
		sleepFunc:   fastBackoff(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	_, err := c.Do(req)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want wrapping context.Canceled", err)
	}
}

func TestClient_RedactsAuthHeader(t *testing.T) {
	srv, _ := newTestServer(t, func(_ int, w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	var buf syncBuffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	c := New(srv.Client(), Config{
		RatePerSec:  100,
		Burst:       100,
		MaxAttempts: 1,
		Logger:      logger,
		sleepFunc:   fastBackoff(),
	})

	const token = "ghs_super_secret_installation_token"
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	_ = resp.Body.Close()

	log := buf.String()
	if strings.Contains(log, token) {
		t.Fatalf("log leaked Authorization token: %s", log)
	}
	if !strings.Contains(log, "REDACTED") {
		t.Fatalf("log missing REDACTED marker:\n%s", log)
	}
	// And confirm the header itself is still on the actual request (we only
	// redact for logging, not on the wire).
	if got := req.Header.Get("Authorization"); !strings.Contains(got, token) {
		t.Fatalf("Authorization header mutated: %q", got)
	}
}

func TestClient_RateLimitsPerHost(t *testing.T) {
	srv, _ := newTestServer(t, func(_ int, w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// 10 RPS, burst 10 — first 10 fire instantly, next 20 take ~2s at 10 rps.
	// To keep the test fast we drive 15 requests: 10 burst + 5 = ~500ms.
	c := New(srv.Client(), Config{
		RatePerSec:  10,
		Burst:       10,
		MaxAttempts: 1,
		sleepFunc:   fastBackoff(),
	})

	const total = 15
	start := time.Now()
	for i := 0; i < total; i++ {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
		resp, err := c.Do(req)
		if err != nil {
			t.Fatalf("req %d: %v", i, err)
		}
		_ = resp.Body.Close()
	}
	elapsed := time.Since(start)

	// 5 requests beyond the burst at 10 rps ≈ 500ms. Allow a 30% margin to
	// absorb scheduler jitter on busy CI.
	if elapsed < 350*time.Millisecond {
		t.Fatalf("elapsed = %s, expected ≥ 350ms (rate limit not kicking in)", elapsed)
	}
}

// syncBuffer is a tiny thread-safe bytes.Buffer wrapper so the slog test
// handler can be written to from any goroutine without racing the reader.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

