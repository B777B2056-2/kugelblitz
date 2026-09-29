package events

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// testEvent is a minimal event type used to exercise Bus mechanics in isolation
// from the real domain events.
type testEvent struct {
	ID int
}

func TestBus_SingleSubscriber(t *testing.T) {
	b := NewBus()
	var got int
	On(b, func(e testEvent) { got = e.ID })
	Emit(b, testEvent{ID: 42})
	assert.Equal(t, 42, got)
}

func TestBus_MultipleSubscribersInOrder(t *testing.T) {
	b := NewBus()
	var order []int
	On(b, func(e testEvent) { order = append(order, 1) })
	On(b, func(e testEvent) { order = append(order, 2) })
	On(b, func(e testEvent) { order = append(order, 3) })
	Emit(b, testEvent{})
	assert.Equal(t, []int{1, 2, 3}, order)
}

func TestBus_Unsubscribe(t *testing.T) {
	b := NewBus()
	calls := 0
	unsub := On(b, func(e testEvent) { calls++ })
	Emit(b, testEvent{})
	unsub()
	Emit(b, testEvent{})
	assert.Equal(t, 1, calls)
}

func TestBus_TypeIsolation(t *testing.T) {
	b := NewBus()
	other := 0
	On(b, func(e testEvent) { other++ })
	Emit(b, 7) // a different concrete type must not reach testEvent subscribers
	assert.Equal(t, 0, other)
}

func TestBus_PanicIsolation(t *testing.T) {
	b := NewBus()
	after := 0
	On(b, func(e testEvent) { panic("boom") })
	On(b, func(e testEvent) { after++ })
	Emit(b, testEvent{})
	assert.Equal(t, 1, after)
}

func TestBus_NoSubscribers(t *testing.T) {
	b := NewBus()
	Emit(b, testEvent{}) // must not panic
}
