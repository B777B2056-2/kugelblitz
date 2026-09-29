// Package events defines the typed events and the Bus that carries them through
// the agent execution pipeline. It is a leaf package: it depends only on
// constants and core/types so that core (and every higher layer) may import it
// without introducing an import cycle.
package events

import (
	"github.com/B777B2056-2/kugelblitz/constants"
	coretypes "github.com/B777B2056-2/kugelblitz/core/types"
)

// Every event carries two routing fields:
//
//	Identity — the agent role that produced the event (main / worker / reviewer).
//	Instance — a unique ID for the producing agent instance. The main agent and
//	           reviewer use ""; DAG workers use their task ID so concurrent
//	           workers on a shared bus can be told apart.
//
// ── Model events (emitted from provider callbacks via ModelEventHandler) ──

// ThinkingChunk carries one streaming reasoning chunk.
type ThinkingChunk struct {
	Identity constants.AgentIdentity
	Instance string
	Chunk    string
}

// ReplyChunk carries one streaming reply chunk.
type ReplyChunk struct {
	Identity constants.AgentIdentity
	Instance string
	Chunk    string
}

// BlockThinking carries a complete reasoning block (non-streaming mode).
type BlockThinking struct {
	Identity  constants.AgentIdentity
	Instance  string
	Reasoning string
}

// BlockReply carries a complete reply block (non-streaming mode).
type BlockReply struct {
	Identity constants.AgentIdentity
	Instance string
	Text     string
}

// FunctionCall carries a tool call requested by the LLM.
type FunctionCall struct {
	Identity constants.AgentIdentity
	Instance string
	Detail   coretypes.ToolCallDetail
}

// ModelFinished carries the finish reason of a completed generation.
type ModelFinished struct {
	Identity constants.AgentIdentity
	Instance string
	Reason   string
}

// UsageUpdated carries token usage for a generation.
type UsageUpdated struct {
	Identity constants.AgentIdentity
	Instance string
	Usage    coretypes.Usage
}

// AgentError carries an error produced by the model or the agent. It merges the
// former ModelEventHandler.OnError and AgentEventHooks.OnError into one event.
type AgentError struct {
	Identity constants.AgentIdentity
	Instance string
	Err      error
}

// ── Agent events (emitted by ReAct / DAG / FSM) ──

// ToolCallEnd carries the result of an executed tool call.
type ToolCallEnd struct {
	Identity constants.AgentIdentity
	Instance string
	Result   coretypes.ToolCallResult
}

// WaitForHumanAction signals that the agent is blocked waiting for a human.
type WaitForHumanAction struct {
	Identity constants.AgentIdentity
	Instance string
	Reason   string
	Prompt   string
}

// PlanRollback signals a plan rollback triggered by goal drift.
type PlanRollback struct {
	Identity      constants.AgentIdentity
	Instance      string
	PlanID        string
	TargetVersion int
	PlanName      string
}

// TaskUpdated signals a change to a DAG task's status.
type TaskUpdated struct {
	Identity constants.AgentIdentity
	Instance string
	TaskID   string
	Goal     string
	Status   string
	Output   string
}

// BeforeCompress signals an imminent context compression.
type BeforeCompress struct {
	Identity constants.AgentIdentity
	Instance string
}

// ── Cross-component signals ──

// AgentActivity signals that the agent handled a request, resetting idle timers
// (e.g. the auto-dream scheduler).
type AgentActivity struct {
	Identity constants.AgentIdentity
	Instance string
}

// RunStarted signals the start of an AgentLoop execution.
type RunStarted struct {
	SessionID string
}

// RunEnded signals the end of an AgentLoop execution, carrying any error.
type RunEnded struct {
	SessionID string
	Err       error
}
