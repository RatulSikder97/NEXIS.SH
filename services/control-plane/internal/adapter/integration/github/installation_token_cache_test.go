package github

// Coverage for the installation-token cache helpers. The cache is nil-safe
// + tolerant of backend errors; both branches are exercised by the test
// matrix below using a controllable stub backend.

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// stubRedis captures every Get/Set call so the test can inspect what the
// cache wrote and when.
type stubRedis struct {
	mu      sync.Mutex
	entries map[string]string
	getErr  error
	setErr  error
	gets    int
	sets    int
}

func newStubRedis() *stubRedis {
	return &stubRedis{entries: map[string]string{}}
}

func (s *stubRedis) Get(_ context.Context, key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gets++
	if s.getErr != nil {
		return "", s.getErr
	}
	v, ok := s.entries[key]
	if !ok {
		return "", errors.New("nil")
	}
	return v, nil
}

func (s *stubRedis) Set(_ context.Context, key, value string, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sets++
	if s.setErr != nil {
		return s.setErr
	}
	s.entries[key] = value
	return nil
}

// TestInstallationTokenCache_NilBackendNoOp — both Get and Set short-circuit
// when the backend is nil. No panics, false return on Get.
func TestInstallationTokenCache_NilBackendNoOp(t *testing.T) {
	c := newInstallationTokenCache(nil)
	v, ok := c.Get(context.Background(), 12345)
	if ok || v != "" {
		t.Fatalf("nil backend should miss: %v %q", ok, v)
	}
	c.Set(context.Background(), 12345, "tok", time.Now().Add(time.Hour), time.Now())
	// Should not panic.
}

// TestInstallationTokenCache_NilReceiverIsNoOp — the helper is nil-safe on
// the receiver too (defensive — the Provider may construct it lazily).
func TestInstallationTokenCache_NilReceiverIsNoOp(t *testing.T) {
	var c *installationTokenCache
	v, ok := c.Get(context.Background(), 1)
	if ok || v != "" {
		t.Fatalf("nil receiver: %v %q", ok, v)
	}
	c.Set(context.Background(), 1, "tok", time.Now().Add(time.Hour), time.Now())
}

// TestInstallationTokenCache_RoundTrip — Set then Get returns the token.
func TestInstallationTokenCache_RoundTrip(t *testing.T) {
	r := newStubRedis()
	c := newInstallationTokenCache(r)
	now := time.Now()
	expires := now.Add(time.Hour)
	c.Set(context.Background(), 12345, "ghs_token_xyz", expires, now)
	v, ok := c.Get(context.Background(), 12345)
	if !ok || v != "ghs_token_xyz" {
		t.Fatalf("round-trip: %v %q", ok, v)
	}
}

// TestInstallationTokenCache_SetWithinSafetyMarginSkips — a token whose
// expiry is inside the 5-minute safety margin must NOT be cached.
func TestInstallationTokenCache_SetWithinSafetyMarginSkips(t *testing.T) {
	r := newStubRedis()
	c := newInstallationTokenCache(r)
	now := time.Now()
	expires := now.Add(2 * time.Minute) // less than 5min safety margin
	c.Set(context.Background(), 1, "tok", expires, now)

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sets != 0 {
		t.Fatalf("set must skip when ttl <= 0, got %d sets", r.sets)
	}
}

// TestInstallationTokenCache_GetErrorReturnsMiss — backend Get error
// surfaces as a cache miss, not as an error to the caller.
func TestInstallationTokenCache_GetErrorReturnsMiss(t *testing.T) {
	r := newStubRedis()
	r.getErr = errors.New("redis offline")
	c := newInstallationTokenCache(r)
	v, ok := c.Get(context.Background(), 1)
	if ok || v != "" {
		t.Fatalf("backend err must be a miss: %v %q", ok, v)
	}
}

// TestInstallationTokenCache_SetErrorSwallowed — backend Set error is
// swallowed so the integration path is never blocked by cache failures.
func TestInstallationTokenCache_SetErrorSwallowed(t *testing.T) {
	r := newStubRedis()
	r.setErr = errors.New("redis write fail")
	c := newInstallationTokenCache(r)
	c.Set(context.Background(), 1, "tok", time.Now().Add(time.Hour), time.Now())
	// Should not panic.
}

// TestInstallationTokenCache_KeyPrefix — the key format is gh:inst:<id>.
func TestInstallationTokenCache_KeyPrefix(t *testing.T) {
	c := newInstallationTokenCache(nil)
	if got := c.key(12345); got != "gh:inst:12345" {
		t.Fatalf("key: %q", got)
	}
	if got := c.key(0); got != "gh:inst:0" {
		t.Fatalf("zero key: %q", got)
	}
	if got := c.key(-1); got != "gh:inst:-1" {
		t.Fatalf("neg key: %q", got)
	}
}

// TestItoa walks small + large + negative numbers.
func TestItoa(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0"},
		{1, "1"},
		{12345, "12345"},
		{-1, "-1"},
		{-12345, "-12345"},
		{9223372036854775807, "9223372036854775807"},
	}
	for _, tc := range cases {
		got := itoa(tc.in)
		if got != tc.want {
			t.Fatalf("itoa(%d): got %q want %q", tc.in, got, tc.want)
		}
	}
}
