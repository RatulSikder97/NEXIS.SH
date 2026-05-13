// Package httpx provides a shared *http.Client wrapper for the integration
// adapters (github, sentry, argocd, slack, datadog, pagerduty). It layers four
// concerns on top of net/http:
//
//  1. Per-host token-bucket rate limiting (golang.org/x/time/rate).
//  2. Per-host circuit breaker (closed / open / half-open) that trips on
//     consecutive 5xx responses inside an observation window.
//  3. Bounded retry with exponential backoff + jitter on 429 + 5xx; the
//     Retry-After header is honoured on 429 when present.
//  4. Structured slog debug logging of every attempt with the Authorization
//     header redacted so installation tokens never leak into log sinks.
//
// The package is internal to internal/adapter/integration so only the
// integration adapters can depend on it; nothing else in the control-plane
// should be reaching across to it.
package httpx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/rand/v2"
	"net/http"
	"strconv"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Config tunes the wrapper. Zero values are filled with the defaults documented
// on each field; callers can pass an empty Config and get sensible behaviour.
type Config struct {
	// RatePerSec is the token-bucket steady-state rate per host (default 10).
	RatePerSec float64
	// Burst is the token-bucket burst per host (default 20).
	Burst int
	// MaxAttempts caps total attempts including the first (default 3).
	MaxAttempts int
	// BreakerThreshold opens the breaker after N consecutive 5xx (default 5).
	BreakerThreshold int
	// BreakerOpenWindow is how long the breaker stays fully open before it
	// half-opens to probe the upstream (default 60s).
	BreakerOpenWindow time.Duration
	// BreakerObserveWindow is the rolling window inside which BreakerThreshold
	// consecutive failures must accumulate to trip the breaker (default 30s).
	BreakerObserveWindow time.Duration
	// Logger is the slog logger used for per-attempt debug records. Defaults
	// to slog.Default().
	Logger *slog.Logger

	// nowFunc / sleepFunc are injection seams for tests; nil = real time.
	nowFunc   func() time.Time
	sleepFunc func(context.Context, time.Duration) error
}

// ErrBreakerOpen is returned by Do when the per-host circuit breaker is open
// and not yet ready to half-open. Callers can errors.Is against this to short
// circuit retries at a higher layer.
var ErrBreakerOpen = errors.New("httpx: breaker open")

// breakerState describes the three positions in the circuit-breaker state
// machine. Transitions are guarded by Client.mu.
type breakerState int

const (
	breakerClosed breakerState = iota
	breakerOpen
	breakerHalfOpen
)

// hostState holds the limiter + breaker bookkeeping for a single host. We key
// by req.URL.Host so the same Client instance can fan out across many
// upstreams (api.github.com, sentry.io, ...) with independent rate budgets.
type hostState struct {
	limiter *rate.Limiter

	breaker          breakerState
	failures         int
	firstFailureAt   time.Time
	openedAt         time.Time
	halfOpenInFlight bool
}

// Client is the wrapper. Construct with New; safe for concurrent use.
type Client struct {
	inner *http.Client
	cfg   Config
	log   *slog.Logger

	mu    sync.Mutex
	hosts map[string]*hostState
}

// New builds a Client wrapping the supplied *http.Client. Pass nil for inner
// to use a fresh client with a 30 second per-request timeout. Zero-valued
// Config fields are replaced with package defaults.
func New(inner *http.Client, cfg Config) *Client {
	if inner == nil {
		inner = &http.Client{Timeout: 30 * time.Second}
	}
	if cfg.RatePerSec <= 0 {
		cfg.RatePerSec = 10
	}
	if cfg.Burst <= 0 {
		cfg.Burst = 20
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 3
	}
	if cfg.BreakerThreshold <= 0 {
		cfg.BreakerThreshold = 5
	}
	if cfg.BreakerOpenWindow <= 0 {
		cfg.BreakerOpenWindow = 60 * time.Second
	}
	if cfg.BreakerObserveWindow <= 0 {
		cfg.BreakerObserveWindow = 30 * time.Second
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	if cfg.nowFunc == nil {
		cfg.nowFunc = time.Now
	}
	if cfg.sleepFunc == nil {
		cfg.sleepFunc = ctxSleep
	}
	return &Client{
		inner: inner,
		cfg:   cfg,
		log:   log,
		hosts: map[string]*hostState{},
	}
}

// Do executes req, honouring the wrapper's rate limit, breaker, and retry
// policy. Each attempt is logged at slog.Debug; failed attempts close their
// response body before retrying so callers do not leak connections.
//
// The returned response is the final attempt's response (which may be a 4xx
// other than 429 — those are not retried). On non-recoverable failure or once
// MaxAttempts is reached, Do returns nil + a wrapped error.
func (c *Client) Do(req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, errors.New("httpx: nil request")
	}
	ctx := req.Context()
	host := req.URL.Host

	var (
		resp    *http.Response
		lastErr error
	)

	for attempt := 1; attempt <= c.cfg.MaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		// Breaker decision is taken before we spend a rate-limit token: an
		// open breaker should refuse instantly without consuming budget.
		probe, err := c.beforeAttempt(host)
		if err != nil {
			return nil, err
		}

		// Per-host rate limit. Uses Wait so the caller back-pressures rather
		// than burning attempts on rate-limited responses we generate
		// ourselves.
		if err := c.limiterFor(host).Wait(ctx); err != nil {
			return nil, fmt.Errorf("httpx: rate limit wait: %w", err)
		}

		start := c.cfg.nowFunc()
		// Clone so retries get a fresh Body reader if Body is not nil. For
		// integrations we typically POST short JSON bodies; we rely on the
		// caller setting req.GetBody (http.NewRequest does this for
		// bytes.Reader / strings.Reader).
		attemptReq := req.Clone(ctx)
		if attemptReq.Body != nil && req.GetBody != nil {
			body, err := req.GetBody()
			if err != nil {
				return nil, fmt.Errorf("httpx: rebuild body: %w", err)
			}
			attemptReq.Body = body
		}

		resp, lastErr = c.inner.Do(attemptReq)
		latency := c.cfg.nowFunc().Sub(start)

		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		c.logAttempt(req, attempt, status, latency, lastErr)

		// Transport-level error (network, timeout, ctx). Treated as 5xx for
		// breaker purposes and retried with backoff.
		if lastErr != nil {
			c.afterAttempt(host, true, probe)
			if attempt == c.cfg.MaxAttempts {
				return nil, fmt.Errorf("httpx: max attempts (%d) reached: %w", c.cfg.MaxAttempts, lastErr)
			}
			if err := c.cfg.sleepFunc(ctx, backoff(attempt)); err != nil {
				return nil, err
			}
			continue
		}

		// 2xx-4xx (excluding 429) → terminal success path. Breaker resets.
		if !isRetryable(status) {
			c.afterAttempt(host, false, probe)
			return resp, nil
		}

		// 429 / 5xx → retry path. Record failure (only 5xx feeds the breaker;
		// 429 means the upstream is healthy but throttling us).
		retryAfter := parseRetryAfter(resp, c.cfg.nowFunc())
		c.drainAndClose(resp)
		c.afterAttempt(host, status >= 500, probe)

		if attempt == c.cfg.MaxAttempts {
			return nil, fmt.Errorf("httpx: max attempts (%d) reached: last status %d", c.cfg.MaxAttempts, status)
		}

		wait := retryAfter
		if wait <= 0 {
			wait = backoff(attempt)
		}
		if err := c.cfg.sleepFunc(ctx, wait); err != nil {
			return nil, err
		}
	}

	// Loop falls through only if MaxAttempts is zero, which New normalises
	// away; keep an explicit error just in case.
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, errors.New("httpx: no attempts made")
}

// limiterFor returns the per-host token-bucket limiter, lazily constructing it
// on first reference. We never garbage-collect entries — the set of upstream
// hosts an adapter talks to is small and bounded.
func (c *Client) limiterFor(host string) *rate.Limiter {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := c.hostStateLocked(host)
	return st.limiter
}

func (c *Client) hostStateLocked(host string) *hostState {
	st, ok := c.hosts[host]
	if !ok {
		st = &hostState{
			limiter: rate.NewLimiter(rate.Limit(c.cfg.RatePerSec), c.cfg.Burst),
		}
		c.hosts[host] = st
	}
	return st
}

// beforeAttempt consults the breaker. The bool return is true when this
// attempt is the half-open probe; the caller passes it back into afterAttempt
// so we can correctly close the breaker on a 2xx probe vs. re-open it on a
// 5xx probe.
func (c *Client) beforeAttempt(host string) (probe bool, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := c.hostStateLocked(host)
	now := c.cfg.nowFunc()

	switch st.breaker {
	case breakerClosed:
		return false, nil
	case breakerOpen:
		if now.Sub(st.openedAt) >= c.cfg.BreakerOpenWindow {
			// Window elapsed — move to half-open and let this caller probe.
			st.breaker = breakerHalfOpen
			st.halfOpenInFlight = true
			return true, nil
		}
		return false, ErrBreakerOpen
	case breakerHalfOpen:
		// Another caller is already probing. Reject to keep the probe single.
		if st.halfOpenInFlight {
			return false, ErrBreakerOpen
		}
		st.halfOpenInFlight = true
		return true, nil
	}
	return false, nil
}

// afterAttempt records the result of an attempt against the breaker.
func (c *Client) afterAttempt(host string, failure, probe bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := c.hostStateLocked(host)
	now := c.cfg.nowFunc()

	if probe {
		// Resolution of a half-open probe.
		st.halfOpenInFlight = false
		if failure {
			// Probe failed → re-open immediately, reset the count window.
			st.breaker = breakerOpen
			st.openedAt = now
			st.failures = 0
			st.firstFailureAt = time.Time{}
		} else {
			// Probe succeeded → close the breaker.
			st.breaker = breakerClosed
			st.failures = 0
			st.firstFailureAt = time.Time{}
		}
		return
	}

	if !failure {
		// Any non-5xx success resets the consecutive-failure counter.
		st.failures = 0
		st.firstFailureAt = time.Time{}
		return
	}

	// Consecutive-failure accounting. If the window has elapsed since the
	// first failure, restart the count from this attempt.
	if st.failures == 0 || now.Sub(st.firstFailureAt) > c.cfg.BreakerObserveWindow {
		st.failures = 1
		st.firstFailureAt = now
	} else {
		st.failures++
	}
	if st.failures >= c.cfg.BreakerThreshold {
		st.breaker = breakerOpen
		st.openedAt = now
	}
}

// logAttempt emits a slog.Debug record. The Authorization header is replaced
// with the literal string "REDACTED" so installation tokens, bearer tokens,
// and basic-auth credentials never reach the log sink.
func (c *Client) logAttempt(req *http.Request, attempt, status int, latency time.Duration, err error) {
	if !c.log.Enabled(req.Context(), slog.LevelDebug) {
		return
	}
	hdr := make(http.Header, len(req.Header))
	for k, v := range req.Header {
		hdr[k] = v
	}
	if _, ok := hdr["Authorization"]; ok {
		hdr["Authorization"] = []string{"REDACTED"}
	}
	attrs := []any{
		slog.String("method", req.Method),
		slog.String("host", req.URL.Host),
		slog.String("path", req.URL.Path),
		slog.Int("attempt", attempt),
		slog.Int("status", status),
		slog.Int64("latency_ms", latency.Milliseconds()),
		slog.Any("headers", hdr),
	}
	if err != nil {
		attrs = append(attrs, slog.String("error", err.Error()))
	}
	c.log.LogAttrs(req.Context(), slog.LevelDebug, "httpx attempt", toAttrs(attrs)...)
}

// drainAndClose drains and closes the response body so the underlying
// connection can be reused on retry. We bound the drain to 64 KiB so a
// pathological server can not stall the retry path.
func (c *Client) drainAndClose(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_, _ = io.CopyN(io.Discard, resp.Body, 64<<10)
	_ = resp.Body.Close()
}

// isRetryable reports whether a status code should trigger a retry. 429 + any
// 5xx are retried; everything else (including 4xx other than 429) is terminal.
func isRetryable(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500
}

// backoff returns the wait before attempt N+1 given that attempt N just
// failed. Curve: 200ms * 2^(attempt-1) plus 0–100ms uniform jitter.
func backoff(attempt int) time.Duration {
	base := 200 * time.Millisecond * time.Duration(math.Pow(2, float64(attempt-1)))
	jitter := time.Duration(rand.Float64() * float64(100*time.Millisecond))
	return base + jitter
}

// parseRetryAfter pulls the Retry-After header off a 429 response. Only the
// integer-seconds form is supported (the HTTP-date form is rare from the APIs
// we hit). Returns 0 when absent or unparseable so the caller falls back to
// the standard backoff curve.
func parseRetryAfter(resp *http.Response, _ time.Time) time.Duration {
	if resp == nil {
		return 0
	}
	v := resp.Header.Get("Retry-After")
	if v == "" {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0
	}
	return time.Duration(n) * time.Second
}

// ctxSleep is the default sleepFunc. It blocks for d or until ctx is done,
// whichever comes first, returning ctx.Err() in the latter case.
func ctxSleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// toAttrs converts the []any slice we built into a []slog.Attr — slog.LogAttrs
// requires the strongly-typed form.
func toAttrs(args []any) []slog.Attr {
	out := make([]slog.Attr, 0, len(args))
	for _, a := range args {
		if at, ok := a.(slog.Attr); ok {
			out = append(out, at)
		}
	}
	return out
}
