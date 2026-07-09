package core

import (
	"errors"
	"strings"
)

// AgentInput is the unified input to AgentLoop.Run.
// It carries both the user's text goal and optional media attachments.
type AgentInput struct {
	Text  string             // user's text goal / question
	Media []MultiModalDetail // optional media (image, audio, video)
}

// IsTextOnly reports whether this input has no media attachments.
func (ai AgentInput) IsTextOnly() bool { return len(ai.Media) == 0 }

// Validate checks that the input is well-formed.
// Rules:
//   - Text is required (even when media is present); whitespace-only is rejected.
//   - Media must be homogeneous — mixing image and audio in one input is rejected.
func (ai AgentInput) Validate() error {
	if strings.TrimSpace(ai.Text) == "" {
		return errors.New("agent input: text is required")
	}
	if len(ai.Media) > 1 {
		t := ai.Media[0].Type
		for _, m := range ai.Media[1:] {
			if m.Type != t {
				return errors.New("agent input: cannot mix image and audio media in one request")
			}
		}
	}
	return nil
}

// BuildUserMessage builds the initial user message from this input.
//   - TextOnly → TextContent
//   - With media → CompositeContent{TextContent, MultiModalContent...}
func (ai AgentInput) BuildUserMessage() Message {
	if ai.IsTextOnly() {
		return NewUserMessage(TextContent{Text: ai.Text})
	}
	parts := make([]Content, 0, len(ai.Media)+1)
	parts = append(parts, TextContent{Text: ai.Text})
	for _, m := range ai.Media {
		parts = append(parts, MultiModalContent{Detail: m})
	}
	return NewUserMessage(CompositeContent{Parts: parts})
}
