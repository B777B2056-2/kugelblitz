package prompts

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFactory_DynamicPrompts(t *testing.T) {
	f := NewFactory()

	t.Run("Review", func(t *testing.T) {
		result, err := f.Render(TypeReview, ReviewParams{
			OriginalGoal:   "add auth",
			PlanSummary:    "3 tasks",
			RecentActivity: "task 1 done",
		})
		require.NoError(t, err)
		assert.Contains(t, result, "ORIGINAL GOAL: add auth")
		assert.Contains(t, result, "CURRENT PLAN STATE: 3 tasks")
		assert.Contains(t, result, "RECENT ACTIVITY: task 1 done")
		assert.Contains(t, result, "reviewer_report")
	})

	t.Run("Worker", func(t *testing.T) {
		result, err := f.Render(TypeWorker, WorkerParams{
			Goal:   "read README",
			Action: "use file_read",
		})
		require.NoError(t, err)
		assert.Contains(t, result, "Goal: read README")
		assert.Contains(t, result, "Action: use file_read")
	})

	t.Run("CompressTool", func(t *testing.T) {
		result, err := f.Render(TypeCompressTool, CompressToolParams{
			ToolName: "file_read",
			FieldKey: "content",
			OrigLen:  5000,
			Raw:      "some file content",
		})
		require.NoError(t, err)
		assert.Contains(t, result, "Tool: file_read")
		assert.Contains(t, result, "5000 chars")
		assert.Contains(t, result, "some file content")
	})

	t.Run("CompressTool_PercentSignsSafe", func(t *testing.T) {
		// Verify that % signs don't break anything (text/template, not fmt)
		result, err := f.Render(TypeCompressTool, CompressToolParams{
			ToolName: "shell_exec",
			FieldKey: "stdout",
			OrigLen:  100,
			Raw:      "100% complete — use 50%% margin",
		})
		require.NoError(t, err)
		assert.Contains(t, result, "100% complete — use 50%% margin")
	})

	t.Run("SemanticJudge", func(t *testing.T) {
		result, err := f.Render(TypeSemanticJudge, SemanticJudgeParams{
			OldVal: "Go is fast",
			NewVal: "Go has high performance",
		})
		require.NoError(t, err)
		assert.Contains(t, result, "A: Go is fast")
		assert.Contains(t, result, "B: Go has high performance")
		assert.Contains(t, result, "YES")
	})
}

func TestType_String(t *testing.T) {
	assert.Equal(t, "Review", TypeReview.String())
	assert.Equal(t, "Worker", TypeWorker.String())
	assert.Equal(t, "CompressTool", TypeCompressTool.String())
	assert.Equal(t, "SemanticJudge", TypeSemanticJudge.String())
	assert.Equal(t, "Unknown", Type(999).String())
}

func TestRender_UnknownType(t *testing.T) {
	f := NewFactory()
	_, err := f.Render(Type(999), nil)
	assert.Error(t, err)
}
