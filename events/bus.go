package events

import (
	"log"
	"reflect"
	"sync"
	"sync/atomic"
)

// subEntry is a single subscriber. It is a pointer shared between the Bus's
// subscription slice and the returned unsubscribe closure, so that a subscriber
// can be invalidated lazily (without touching the slice) by flipping active.
type subEntry struct {
	h      func(any)
	active atomic.Bool
}

// Bus dispatches typed events to subscribers. Events are keyed by their concrete
// Go type: On subscribes to events of type T, Emit delivers to those
// subscribers. Delivery is synchronous, in the Emit caller's goroutine, in
// registration order. A panicking subscriber is recovered and logged so it does
// not prevent other subscribers from receiving the event.
//
// Subscription helpers are package-level generic functions (On / Emit) because
// Go does not permit type parameters on methods.
type Bus struct {
	mu   sync.RWMutex
	subs map[reflect.Type][]*subEntry
}

// NewBus returns an empty Bus.
func NewBus() *Bus {
	return &Bus{subs: make(map[reflect.Type][]*subEntry)}
}

// On subscribes h to events of type T on b and returns an unsubscribe function.
// Handlers are invoked in registration order. The returned function is safe to
// call multiple times and after the event has been delivered.
func On[T any](b *Bus, h func(T)) func() {
	b.mu.Lock()
	defer b.mu.Unlock()

	t := reflect.TypeOf(h).In(0)
	e := &subEntry{h: func(v any) { h(v.(T)) }}
	e.active.Store(true)
	b.subs[t] = append(b.subs[t], e)

	return func() { e.active.Store(false) }
}

// Emit delivers ev to every subscriber of its concrete type on b, synchronously,
// in registration order. Subscribers unsubscribed before delivery are skipped. A
// panicking subscriber is recovered and logged without affecting the others.
func Emit[T any](b *Bus, ev T) {
	t := reflect.TypeOf(ev)

	b.mu.RLock()
	entries := append([]*subEntry(nil), b.subs[t]...)
	b.mu.RUnlock()

	for _, e := range entries {
		if !e.active.Load() {
			continue
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("events: subscriber panic: %v", r)
				}
			}()
			e.h(ev)
		}()
	}
}
