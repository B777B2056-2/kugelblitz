package core

import "github.com/B777B2056-2/kugelblitz/constants"

// Chain returns a copy of `original` where every non-nil callback in `sys`
// runs BEFORE the matching callback in `original`. Nil original callbacks
// are simply replaced by the sys callback.
//
// Usage — typical worker wiring:
//
//	hooks := Chain(parentHooks, AgentEventHooks{
//	    OnReplyChunk: func(id constants.AgentIdentity, chunk string) { ... },
//	})
//
// The user's original callbacks fire after the framework's callbacks.
func Chain(original, sys AgentEventHooks) AgentEventHooks {
	result := original

	if sys.OnThinkingChunk != nil {
		prev := result.OnThinkingChunk
		result.OnThinkingChunk = func(id constants.AgentIdentity, chunk string) {
			sys.OnThinkingChunk(id, chunk)
			if prev != nil {
				prev(id, chunk)
			}
		}
	}
	if sys.OnReplyChunk != nil {
		prev := result.OnReplyChunk
		result.OnReplyChunk = func(id constants.AgentIdentity, chunk string) {
			sys.OnReplyChunk(id, chunk)
			if prev != nil {
				prev(id, chunk)
			}
		}
	}
	if sys.OnBlockThinking != nil {
		prev := result.OnBlockThinking
		result.OnBlockThinking = func(id constants.AgentIdentity, reasoning string) {
			sys.OnBlockThinking(id, reasoning)
			if prev != nil {
				prev(id, reasoning)
			}
		}
	}
	if sys.OnBlockReply != nil {
		prev := result.OnBlockReply
		result.OnBlockReply = func(id constants.AgentIdentity, text string) {
			sys.OnBlockReply(id, text)
			if prev != nil {
				prev(id, text)
			}
		}
	}
	if sys.OnFunctionCall != nil {
		prev := result.OnFunctionCall
		result.OnFunctionCall = func(id constants.AgentIdentity, detail ToolCallDetail) {
			sys.OnFunctionCall(id, detail)
			if prev != nil {
				prev(id, detail)
			}
		}
	}
	if sys.OnModelFinished != nil {
		prev := result.OnModelFinished
		result.OnModelFinished = func(id constants.AgentIdentity, reason string) {
			sys.OnModelFinished(id, reason)
			if prev != nil {
				prev(id, reason)
			}
		}
	}
	if sys.OnError != nil {
		prev := result.OnError
		result.OnError = func(id constants.AgentIdentity, err error) {
			sys.OnError(id, err)
			if prev != nil {
				prev(id, err)
			}
		}
	}
	if sys.OnUsageUpdated != nil {
		prev := result.OnUsageUpdated
		result.OnUsageUpdated = func(id constants.AgentIdentity, usage Usage) {
			sys.OnUsageUpdated(id, usage)
			if prev != nil {
				prev(id, usage)
			}
		}
	}
	if sys.OnToolCallEnd != nil {
		prev := result.OnToolCallEnd
		result.OnToolCallEnd = func(id constants.AgentIdentity, r ToolCallResult) {
			sys.OnToolCallEnd(id, r)
			if prev != nil {
				prev(id, r)
			}
		}
	}
	if sys.OnWaitForHumanAction != nil {
		prev := result.OnWaitForHumanAction
		result.OnWaitForHumanAction = func(id constants.AgentIdentity, reason, prompt string) {
			sys.OnWaitForHumanAction(id, reason, prompt)
			if prev != nil {
				prev(id, reason, prompt)
			}
		}
	}
	if sys.OnPlanRollback != nil {
		prev := result.OnPlanRollback
		result.OnPlanRollback = func(id constants.AgentIdentity, planID string, targetVersion int, planName string) {
			sys.OnPlanRollback(id, planID, targetVersion, planName)
			if prev != nil {
				prev(id, planID, targetVersion, planName)
			}
		}
	}
	if sys.OnTaskUpdated != nil {
		prev := result.OnTaskUpdated
		result.OnTaskUpdated = func(id constants.AgentIdentity, taskID, goal, status, output string) {
			sys.OnTaskUpdated(id, taskID, goal, status, output)
			if prev != nil {
				prev(id, taskID, goal, status, output)
			}
		}
	}
	if sys.OnBeforeCompress != nil {
		prev := result.OnBeforeCompress
		result.OnBeforeCompress = func(id constants.AgentIdentity) {
			sys.OnBeforeCompress(id)
			if prev != nil {
				prev(id)
			}
		}
	}

	return result
}
