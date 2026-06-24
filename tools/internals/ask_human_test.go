package internals

import (
	"context"
	"errors"
	"testing"

	"github.com/B777B2056-2/kugelblitz/core"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockHumanGate implements core.HumanGate for testing.
type mockHumanGate struct {
	waitFn func(ctx context.Context, reason, prompt string) (string, error)
}

func (m *mockHumanGate) WaitForHuman(ctx context.Context, reason, prompt string) (string, error) {
	if m.waitFn != nil {
		return m.waitFn(ctx, reason, prompt)
	}
	return "", nil
}

func TestAskHumanTool_Execute_ReturnsHumanResponse(t *testing.T) {
	gate := &mockHumanGate{
		waitFn: func(ctx context.Context, reason, prompt string) (string, error) {
			assert.Equal(t, "need approval", reason)
			assert.Equal(t, "shall we continue?", prompt)
			return "yes, proceed", nil
		},
	}
	tool := &AskHumanTool{Gate: gate}

	result := tool.Execute(context.Background(), core.ToolCallDetail{
		ID:       "tc-1",
		ToolName: "ask_human",
		Args:     map[string]any{"question": "shall we continue?", "reason": "need approval"},
	})

	assert.Equal(t, "tc-1", result.ToolCallID)
	assert.Equal(t, "ask_human", result.ToolName)
	assert.Equal(t, map[string]any{"response": "yes, proceed"}, result.Outputs)
}

func TestAskHumanTool_Execute_WaitForHumanError(t *testing.T) {
	gate := &mockHumanGate{
		waitFn: func(ctx context.Context, reason, prompt string) (string, error) {
			return "", errors.New("connection lost")
		},
	}
	tool := &AskHumanTool{Gate: gate}

	result := tool.Execute(context.Background(), core.ToolCallDetail{
		ID:       "tc-1",
		ToolName: "ask_human",
		Args:     map[string]any{"question": "ok?"},
	})

	errMsg, ok := result.Outputs["error"]
	require.True(t, ok)
	assert.Contains(t, errMsg.(string), "connection lost")
}

func TestAskHumanTool_Execute_ContextCanceled(t *testing.T) {
	gate := &mockHumanGate{
		waitFn: func(ctx context.Context, reason, prompt string) (string, error) {
			return "", context.Canceled
		},
	}
	tool := &AskHumanTool{Gate: gate}

	result := tool.Execute(context.Background(), core.ToolCallDetail{
		ID:       "tc-1",
		ToolName: "ask_human",
		Args:     map[string]any{"question": "ok?"},
	})

	errMsg, ok := result.Outputs["error"]
	require.True(t, ok)
	assert.Contains(t, errMsg.(string), "canceled")
}

func TestAskHumanTool_Execute_MissingQuestion(t *testing.T) {
	gate := &mockHumanGate{}
	tool := &AskHumanTool{Gate: gate}

	result := tool.Execute(context.Background(), core.ToolCallDetail{
		ID:       "tc-1",
		ToolName: "ask_human",
		Args:     map[string]any{},
	})

	errMsg, ok := result.Outputs["error"]
	require.True(t, ok)
	assert.Contains(t, errMsg.(string), "missing required argument")
}

func TestAskHumanTool_Definition_HasCorrectName(t *testing.T) {
	tool := &AskHumanTool{}
	def := tool.Definition()
	assert.Equal(t, "ask_human", def.Name)
	assert.NotEmpty(t, def.Description)
	assert.Contains(t, def.JsonSchema["required"], "question")
}
