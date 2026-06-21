package core

import "errors"

// ErrContextLengthExceeded is returned by providers when the input exceeds
// the model's maximum context length. Callers should compress history and retry.
var ErrContextLengthExceeded = errors.New("context length exceeded")
