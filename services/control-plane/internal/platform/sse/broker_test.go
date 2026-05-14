package sse

import (
	"sync"
	"testing"
	"time"
)

// TestBroker_PublishDeliversToSubscriber — happy path. One subscriber, one
// publish, expect to receive the event.
func TestBroker_PublishDeliversToSubscriber(t *testing.T) {
	b := New[int]()
	ch, unsub := b.Subscribe("topic-1", 4)
	t.Cleanup(unsub)

	b.Publish("topic-1", 42)

	select {
	case got := <-ch:
		if got != 42 {
			t.Fatalf("got %d want 42", got)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("timed out waiting for event")
	}
}

// TestBroker_PublishOtherTopicNotDelivered — publishing on topic-A must
// NOT reach subscribers of topic-B.
func TestBroker_PublishOtherTopicNotDelivered(t *testing.T) {
	b := New[int]()
	chA, unsubA := b.Subscribe("a", 4)
	t.Cleanup(unsubA)

	b.Publish("b", 99)

	select {
	case got := <-chA:
		t.Fatalf("subscriber on a received cross-topic event: %d", got)
	case <-time.After(50 * time.Millisecond):
		// expected — no event
	}
}

// TestBroker_PublishDropsOnSlowConsumer — small buffer + a publisher that
// outpaces the consumer should not block. The fourth event must be
// dropped silently.
func TestBroker_PublishDropsOnSlowConsumer(t *testing.T) {
	b := New[int]()
	ch, unsub := b.Subscribe("topic", 2)
	t.Cleanup(unsub)

	// Fire 5 events without draining; the 2-event buffer absorbs the first
	// two, the rest are dropped on the floor.
	for i := 0; i < 5; i++ {
		b.Publish("topic", i)
	}

	// Drain — expect at most 2 events.
	count := 0
loop:
	for {
		select {
		case <-ch:
			count++
			if count > 2 {
				t.Fatalf("expected at most 2 events, got %d", count)
			}
		case <-time.After(50 * time.Millisecond):
			break loop
		}
	}
	if count != 2 {
		t.Fatalf("expected exactly 2 events (buffer size), got %d", count)
	}
}

// TestBroker_UnsubscribeStopsDelivery — after unsubscribe the channel
// closes and no further events arrive.
func TestBroker_UnsubscribeStopsDelivery(t *testing.T) {
	b := New[int]()
	ch, unsub := b.Subscribe("topic", 4)
	unsub()

	b.Publish("topic", 1)

	// Read from a closed channel — should return immediately with the
	// zero value.
	got, ok := <-ch
	if ok {
		t.Fatalf("channel should be closed, got %d", got)
	}
}

// TestBroker_MultipleSubscribersAllReceive — every subscriber on a topic
// gets the event.
func TestBroker_MultipleSubscribersAllReceive(t *testing.T) {
	b := New[int]()
	var wg sync.WaitGroup
	var received [3]int

	for i := 0; i < 3; i++ {
		ch, unsub := b.Subscribe("multi", 4)
		t.Cleanup(unsub)
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			select {
			case got := <-ch:
				received[idx] = got
			case <-time.After(500 * time.Millisecond):
				received[idx] = -1
			}
		}(i)
	}

	// Give the goroutines a moment to subscribe.
	time.Sleep(10 * time.Millisecond)
	b.Publish("multi", 7)

	wg.Wait()
	for i, v := range received {
		if v != 7 {
			t.Fatalf("subscriber %d: got %d want 7", i, v)
		}
	}
}

// TestBroker_UnsubscribeCleansEmptyTopic — when the last subscriber leaves,
// the topic entry is removed (verified indirectly by re-subscribing and
// confirming delivery still works).
func TestBroker_UnsubscribeCleansEmptyTopic(t *testing.T) {
	b := New[int]()
	_, unsub := b.Subscribe("t", 1)
	unsub()
	// Verify resubscribe still works.
	ch2, unsub2 := b.Subscribe("t", 1)
	t.Cleanup(unsub2)
	b.Publish("t", 100)
	select {
	case got := <-ch2:
		if got != 100 {
			t.Fatalf("got %d want 100", got)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("timed out")
	}
}
