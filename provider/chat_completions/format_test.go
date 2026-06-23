package chat_completions

import (
	"errors"
	"testing"

	"github.com/B777B2056-2/kugelblitz/core"

	"github.com/stretchr/testify/assert"
)

func TestWrapContextError_ContextLengthExceeded(t *testing.T) {
	err := errors.New(`error: code=context_length_exceeded, message="this model's maximum context length is 8192 tokens"`)
	wrapped := wrapContextError(err)
	assert.True(t, errors.Is(wrapped, core.ErrContextLengthExceeded))
}

func TestWrapContextError_MaximumContextLength(t *testing.T) {
	err := errors.New("This model's maximum context length is 128000 tokens. However, your messages resulted in 150000 tokens. Please reduce the length of the messages.")
	wrapped := wrapContextError(err)
	assert.True(t, errors.Is(wrapped, core.ErrContextLengthExceeded))
}

func TestWrapContextError_ReduceLength(t *testing.T) {
	err := errors.New("Request too large. Please reduce the length of the messages and try again.")
	wrapped := wrapContextError(err)
	assert.True(t, errors.Is(wrapped, core.ErrContextLengthExceeded))
}

func TestWrapContextError_OtherError(t *testing.T) {
	err := errors.New("invalid API key")
	wrapped := wrapContextError(err)
	assert.False(t, errors.Is(wrapped, core.ErrContextLengthExceeded))
	assert.Equal(t, err, wrapped) // unwrapped
}

func TestWrapContextError_Nil(t *testing.T) {
	assert.Nil(t, wrapContextError(nil))
}
