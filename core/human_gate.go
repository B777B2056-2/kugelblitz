package core

import "context"

// HumanGate is implemented by agent runtimes to support human-in-the-loop.
// Tools call WaitForHuman to pause the agent loop until a human responds
// via ResumeWithHumanResponse.
type HumanGate interface {
	WaitForHuman(ctx context.Context, reason, prompt string) (string, error)
}
