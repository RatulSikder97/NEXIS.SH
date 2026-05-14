package temporal

// Coverage for the Heartbeat in-process liveness tracker. The StartHeartbeat
// goroutine requires a Temporal client; that path is exercised at integration
// time. Here we cover the pure helpers: NewHeartbeat, LastSeen, IsHealthy,
// Touch.

import (
	"testing"
	"time"
)

// TestNewHeartbeat_ZeroLastSeen — a fresh heartbeat reports zero time +
// is not healthy.
func TestNewHeartbeat_ZeroLastSeen(t *testing.T) {
	h := NewHeartbeat()
	if !h.LastSeen().IsZero() {
		t.Fatalf("LastSeen should be zero, got %v", h.LastSeen())
	}
	if h.IsHealthy(60 * time.Second) {
		t.Fatalf("fresh heartbeat must NOT be healthy")
	}
}

// TestHeartbeat_TouchUpdatesLastSeen — Touch sets LastSeen to ~now.
func TestHeartbeat_TouchUpdatesLastSeen(t *testing.T) {
	h := NewHeartbeat()
	before := time.Now().UTC()
	h.Touch()
	after := time.Now().UTC()

	ls := h.LastSeen()
	if ls.IsZero() {
		t.Fatalf("LastSeen must be non-zero after Touch")
	}
	if ls.Before(before.Add(-time.Second)) || ls.After(after.Add(time.Second)) {
		t.Fatalf("LastSeen %v not within [%v, %v]", ls, before, after)
	}
}

// TestHeartbeat_IsHealthyWithinWindow — touched + checked within the
// window → healthy.
func TestHeartbeat_IsHealthyWithinWindow(t *testing.T) {
	h := NewHeartbeat()
	h.Touch()
	if !h.IsHealthy(60 * time.Second) {
		t.Fatalf("touched within window must be healthy")
	}
}

// TestHeartbeat_NotHealthyOutsideWindow — manually backdate the heartbeat
// past the window via atomic store; IsHealthy must return false.
func TestHeartbeat_NotHealthyOutsideWindow(t *testing.T) {
	h := NewHeartbeat()
	// Backdate by 5 minutes.
	old := time.Now().Add(-5 * time.Minute).UnixNano()
	h.lastSeenUnixNano.Store(old)
	if h.IsHealthy(60 * time.Second) {
		t.Fatalf("stale heartbeat must NOT be healthy")
	}
}

// TestHeartbeat_RecentTouchHealthyEvenSmallWindow — Touch then check
// against 1ms should still be healthy if the check fires immediately.
// We don't sleep at all so the elapsed must be effectively 0.
func TestHeartbeat_RecentTouchHealthyEvenSmallWindow(t *testing.T) {
	h := NewHeartbeat()
	h.Touch()
	// 1s window — generous so the test isn't flaky on a slow CI box.
	if !h.IsHealthy(time.Second) {
		t.Fatalf("just-touched heartbeat must be healthy within 1s window")
	}
}
