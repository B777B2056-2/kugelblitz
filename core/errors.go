package core

import "errors"

// ErrContextLengthExceeded is returned by providers when the input exceeds
// the model's maximum context length. Callers should compress history and retry.
var ErrContextLengthExceeded = errors.New("context length exceeded")

// ErrMaxStepsExceeded is returned when a ReAct loop reaches its configured
// iteration limit, preventing unbounded tool-call loops.
var ErrMaxStepsExceeded = errors.New("max ReAct steps exceeded")
