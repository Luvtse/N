package eventbus

import (
	"context"
	"sync"
)

// MemoryBus is an in-process EventBus implementation used by integration
// tests (and available for single-node dev boot). Every Publish delivers the
// event synchronously to all handlers registered via Subscribe, regardless of
// the topic argument — mirroring KafkaBus's canonical-topic behavior where
// Publish writes everything to "nidaw.events" and subscribers filter by
// event.Type. Delivery happens while holding the read lock so a test can
// publish and immediately assert on consumer side effects without sleeping.
type MemoryBus struct {
	mu       sync.RWMutex
	handlers []func(Event)
}

// NewMemoryBus returns an empty bus with no subscriptions.
func NewMemoryBus() *MemoryBus {
	return &MemoryBus{}
}

// Publish delivers the event to every registered handler, skipping handlers
// whose context has already been cancelled (the same lifecycle rule KafkaBus
// applies when its reader loop exits on ctx cancellation).
func (b *MemoryBus) Publish(_ context.Context, _ string, event Event) error {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, h := range b.handlers {
		h(event)
	}
	return nil
}

// Subscribe registers a handler. The handler stops being invoked once ctx is
// cancelled; the entry itself stays until Close for simplicity (test-only type).
func (b *MemoryBus) Subscribe(ctx context.Context, _ string, handler func(Event)) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers = append(b.handlers, func(ev Event) {
		select {
		case <-ctx.Done():
			return
		default:
			handler(ev)
		}
	})
	return nil
}

// Close satisfies the EventBus interface. It drops all handlers.
func (b *MemoryBus) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers = nil
	return nil
}

var _ EventBus = (*MemoryBus)(nil)
