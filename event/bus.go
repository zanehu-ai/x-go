package event

import (
	"context"
	"sync"

	"go.uber.org/zap"

	"github.com/zanehu-ai/x-go/logger"
)

// HandlerFunc processes an event payload.
type HandlerFunc func(ctx context.Context, payload any)

// Bus is a lightweight in-process event bus supporting synchronous and asynchronous publishing.
type Bus struct {
	mu       sync.RWMutex
	handlers map[string][]HandlerFunc
}

// New creates an event Bus.
func New() *Bus {
	return &Bus{handlers: make(map[string][]HandlerFunc)}
}

// Subscribe registers a handler for the given event type.
func (b *Bus) Subscribe(eventType string, handler HandlerFunc) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[eventType] = append(b.handlers[eventType], handler)
}

// Publish dispatches the event to all registered handlers synchronously.
func (b *Bus) Publish(ctx context.Context, eventType string, payload any) {
	b.mu.RLock()
	handlers := b.handlers[eventType]
	b.mu.RUnlock()

	for _, h := range handlers {
		h(ctx, payload)
	}
}

// PublishAsync dispatches the event to all registered handlers in separate goroutines.
// Errors in handlers are logged but do not affect the caller.
func (b *Bus) PublishAsync(ctx context.Context, eventType string, payload any) {
	b.mu.RLock()
	handlers := b.handlers[eventType]
	b.mu.RUnlock()

	for _, h := range handlers {
		go func(fn HandlerFunc) {
			defer func() {
				if r := recover(); r != nil {
					logger.Error("event handler panic", zap.String("event", eventType), zap.Any("recover", r))
				}
			}()
			fn(ctx, payload)
		}(h)
	}
}
