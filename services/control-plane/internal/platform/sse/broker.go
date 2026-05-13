// Package sse is a tiny in-process pub/sub for Server-Sent Events. Topics are
// arbitrary strings (we use workspace id). Subscribers get a buffered channel;
// non-blocking Publish drops events on slow consumers rather than blocking the
// producer — the SSE handler is expected to drain fast and the provisioning
// state machine is the only producer.
//
// The broker is generic over T so future surfaces (e.g. live job-status feeds)
// can reuse it without unsafe-typed map values.
package sse

import "sync"

// Broker is the in-memory hub. Zero value is unusable — construct via New.
type Broker[T any] struct {
	mu   sync.RWMutex
	subs map[string]map[chan T]struct{}
}

// New returns a ready-to-use Broker. Safe for concurrent Publish/Subscribe.
func New[T any]() *Broker[T] {
	return &Broker[T]{subs: map[string]map[chan T]struct{}{}}
}

// Subscribe registers a new subscriber on the given topic and returns the read
// channel plus an unsubscribe function. The channel is buffered to `buf`
// events; if the consumer falls behind, Publish drops events for that
// subscriber rather than blocking. The unsubscribe function is idempotent in
// practice — calling it twice will close an already-closed channel and panic,
// so callers must ensure exactly-once invocation (the workspace service does
// this via a goroutine bound to ctx.Done).
func (b *Broker[T]) Subscribe(topic string, buf int) (<-chan T, func()) {
	ch := make(chan T, buf)
	b.mu.Lock()
	if _, ok := b.subs[topic]; !ok {
		b.subs[topic] = map[chan T]struct{}{}
	}
	b.subs[topic][ch] = struct{}{}
	b.mu.Unlock()
	return ch, func() {
		b.mu.Lock()
		if subs, ok := b.subs[topic]; ok {
			delete(subs, ch)
			if len(subs) == 0 {
				delete(b.subs, topic)
			}
		}
		b.mu.Unlock()
		close(ch)
	}
}

// Publish delivers ev to every subscriber of topic. Best-effort: a slow
// consumer that has filled its buffer simply misses the event. Callers must
// not assume delivery; SSE is a hint, not a durable queue.
func (b *Broker[T]) Publish(topic string, ev T) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.subs[topic] {
		select {
		case ch <- ev:
		default:
			// drop on slow consumer
		}
	}
}
