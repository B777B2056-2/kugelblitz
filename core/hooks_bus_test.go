package core

import (
	"errors"
	"testing"

	"github.com/B777B2056-2/kugelblitz/constants"
	types "github.com/B777B2056-2/kugelblitz/core/types"
	"github.com/B777B2056-2/kugelblitz/events"
	"github.com/stretchr/testify/assert"
)

func TestAgentEventHooks_Subscribe(t *testing.T) {
	bus := events.NewBus()
	var (
		gotChunk   string
		gotPlanID  string
		gotVersion int
		gotResult  types.ToolCallResult
		gotErr     error
	)

	hooks := AgentEventHooks{
		OnReplyChunk: func(id constants.AgentIdentity, chunk string) {
			assert.Equal(t, constants.AgentMain, id)
			gotChunk = chunk
		},
		OnPlanRollback: func(id constants.AgentIdentity, planID string, v int, name string) {
			gotPlanID, gotVersion = planID, v
		},
		OnToolCallEnd: func(id constants.AgentIdentity, r types.ToolCallResult) {
			gotResult = r
		},
		OnError: func(id constants.AgentIdentity, err error) {
			gotErr = err
		},
	}
	unsub := hooks.Subscribe(bus)

	// Identity is passed through; Instance is dropped (callback still fires).
	events.Emit(bus, events.ReplyChunk{Identity: constants.AgentMain, Instance: "w1", Chunk: "hi"})
	events.Emit(bus, events.PlanRollback{Identity: constants.AgentMain, PlanID: "p1", TargetVersion: 2, PlanName: "n"})
	events.Emit(bus, events.ToolCallEnd{Identity: constants.AgentMain, Result: types.ToolCallResult{ToolName: "t"}})
	events.Emit(bus, events.AgentError{Identity: constants.AgentMain, Err: errors.New("boom")})

	assert.Equal(t, "hi", gotChunk)
	assert.Equal(t, "p1", gotPlanID)
	assert.Equal(t, 2, gotVersion)
	assert.Equal(t, "t", gotResult.ToolName)
	assert.Equal(t, "boom", gotErr.Error())

	unsub()
	events.Emit(bus, events.ReplyChunk{Identity: constants.AgentMain, Chunk: "after"})
	assert.Equal(t, "hi", gotChunk, "unsubscribe must stop delivery")
}

func TestNewBusModelHandler(t *testing.T) {
	bus := events.NewBus()
	var (
		gotChunk  string
		gotDetail types.ToolCallDetail
		gotUsage  types.Usage
		gotErr    error
	)

	events.On(bus, func(ev events.ReplyChunk) {
		assert.Equal(t, constants.AgentReviewer, ev.Identity)
		assert.Equal(t, "inst", ev.Instance)
		gotChunk = ev.Chunk
	})
	events.On(bus, func(ev events.FunctionCall) { gotDetail = ev.Detail })
	events.On(bus, func(ev events.UsageUpdated) { gotUsage = ev.Usage })
	events.On(bus, func(ev events.AgentError) { gotErr = ev.Err })

	h := NewBusModelHandler(bus, constants.AgentReviewer, "inst")
	h.OnReplyChunk("chunk")
	h.OnFunctionCall(types.ToolCallDetail{ID: "1", ToolName: "fn"})
	h.OnUsageUpdated(types.Usage{TotalTokens: 9})
	h.OnError(errors.New("oops"))

	assert.Equal(t, "chunk", gotChunk)
	assert.Equal(t, "fn", gotDetail.ToolName)
	assert.Equal(t, int64(9), gotUsage.TotalTokens)
	assert.Equal(t, "oops", gotErr.Error())
}

func TestAgentEventHooks_Subscribe_RepeatedDoesNotDuplicate(t *testing.T) {
	bus := events.NewBus()
	calls := 0
	hooks := AgentEventHooks{
		OnReplyChunk: func(_ constants.AgentIdentity, _ string) { calls++ },
	}

	_ = hooks.Subscribe(bus) // first subscription
	_ = hooks.Subscribe(bus) // second must replace the first, not accumulate

	events.Emit(bus, events.ReplyChunk{Identity: constants.AgentMain, Chunk: "x"})

	assert.Equal(t, 1, calls, "repeated Subscribe on the same value must not deliver twice")
}
