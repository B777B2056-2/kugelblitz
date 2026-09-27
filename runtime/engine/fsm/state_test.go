package fsm

import (
	"testing"

	"github.com/B777B2056-2/kugelblitz/constants"
	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/B777B2056-2/kugelblitz/memory/working"
	"github.com/B777B2056-2/kugelblitz/prompts"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testContext builds a Context with the injected funcs buildPrompt needs.
func testContext() *Context {
	return &Context{
		Deps: Dependencies{
			LoadAgentContext: core.LoadAgentContext,
			RenderPlanPrompt: prompts.DefaultFactory.Render,
		},
	}
}

func TestToolsForState_AllStatusesRegistered(t *testing.T) {
	assert.NotNil(t, ToolsForState(constants.PlanStateIntent, nil))
	assert.NotNil(t, ToolsForState(constants.PlanStateDirect, nil))
	assert.NotNil(t, ToolsForState(constants.PlanStateInit, nil))
	assert.NotNil(t, ToolsForState(constants.PlanStateConfirmed, nil))
	assert.NotNil(t, ToolsForState(constants.PlanStateDoing, nil))
	assert.NotNil(t, ToolsForState(constants.PlanStateUpdating, nil))
	assert.NotNil(t, ToolsForState(constants.PlanStateDone, nil))
	assert.NotNil(t, ToolsForState(constants.PlanStateFailed, nil))
	assert.Empty(t, ToolsForState(constants.PlanStateRejected, nil))
}

func TestToolsForState_InitTools(t *testing.T) {
	tools := ToolsForState(constants.PlanStateInit, nil)
	has := func(name string) bool {
		for _, t := range tools {
			if t == name {
				return true
			}
		}
		return false
	}
	assert.True(t, has("plan_create"))
	assert.True(t, has("task_insert"))
	assert.False(t, has("ask_human"), "ask_human should not be available in Init phase")
}

func TestToolsForState_ConfirmedTools(t *testing.T) {
	tools := ToolsForState(constants.PlanStateConfirmed, nil)
	has := func(name string) bool {
		for _, t := range tools {
			if t == name {
				return true
			}
		}
		return false
	}
	assert.True(t, has("ask_human"))
	assert.True(t, has("confirm_plan"))
}

func TestToolsForState_DoingTools(t *testing.T) {
	tools := ToolsForState(constants.PlanStateDoing, nil)
	has := func(name string) bool {
		for _, t := range tools {
			if t == name {
				return true
			}
		}
		return false
	}
	assert.True(t, has("task_query"))
	assert.True(t, has("task_status_update"))
}

func TestToolsForState_UpdateTools(t *testing.T) {
	tools := ToolsForState(constants.PlanStateUpdating, nil)
	has := func(name string) bool {
		for _, t := range tools {
			if t == name {
				return true
			}
		}
		return false
	}
	assert.True(t, has("task_insert"))
	assert.True(t, has("task_delete"))
	assert.False(t, has("ask_human"))
	assert.False(t, has("confirm_plan"))
}

func TestToolsForState_DoneFailedTools(t *testing.T) {
	for _, status := range []constants.PlanState{constants.PlanStateDone, constants.PlanStateFailed} {
		tools := ToolsForState(status, nil)
		assert.NotContains(t, tools, "confirm_plan", "status %s should not have confirm_plan", status)
	}
}

func TestToolsForState_MemoryExtractAvailableToAgents(t *testing.T) {
	// LTM writing is a first-class tool for the simple-mode agent (Direct) and
	// the plan-mode main agent (Init/Confirmed/Updating/Done/Failed).
	for _, status := range []constants.PlanState{
		constants.PlanStateDirect,
		constants.PlanStateInit,
		constants.PlanStateConfirmed,
		constants.PlanStateUpdating,
		constants.PlanStateDone,
		constants.PlanStateFailed,
	} {
		assert.Contains(t, ToolsForState(status, nil), "memory_extract",
			"memory_extract should be available in %s", status)
	}

	// Not available to the classification phase (Intent) or DAG workers (Doing).
	assert.NotContains(t, ToolsForState(constants.PlanStateIntent, nil), "memory_extract")
	assert.NotContains(t, ToolsForState(constants.PlanStateDoing, nil), "memory_extract")
	assert.NotContains(t, ToolsForState(constants.PlanStateRejected, nil), "memory_extract")
}

func TestToolsForState_ContextCompressInDirect(t *testing.T) {
	// Manual context compression is exposed in the single-turn (Direct) mode.
	assert.Contains(t, ToolsForState(constants.PlanStateDirect, nil), "context_compress")
}

func TestBuildPrompt_ConfirmedShowsFullPlan(t *testing.T) {
	plan := &working.Plan{
		ID:    "plan-001",
		Name:  "Test Plan",
		State: constants.PlanStateConfirmed,
		SubTasks: []working.Task{
			{ID: "task-1", Goal: "Install dependencies", Action: "pip install requests", Status: working.TaskStatusPending, ParentTaskID: ""},
			{ID: "task-2", Goal: "Run tests", Action: "go test ./...", Status: working.TaskStatusPending, ParentTaskID: "task-1"},
		},
	}
	prompt, err := buildPrompt(testContext(), constants.PlanStateConfirmed, plan)
	require.NoError(t, err)
	assert.Contains(t, prompt, "Plan to Confirm")
	assert.Contains(t, prompt, "Test Plan")
	assert.Contains(t, prompt, "plan-001")
	assert.Contains(t, prompt, "Install dependencies")
	assert.Contains(t, prompt, "Run tests")
	assert.Contains(t, prompt, "pip install requests")
	assert.Contains(t, prompt, "go test ./...")
	assert.Contains(t, prompt, "none")
	assert.Contains(t, prompt, "task-1")
	assert.Contains(t, prompt, "ask_human")
}

func TestBuildPrompt_DoingShowsSummary(t *testing.T) {
	plan := &working.Plan{
		ID:    "plan-002",
		Name:  "Exec Plan",
		State: constants.PlanStateDoing,
		SubTasks: []working.Task{
			{ID: "task-1", Goal: "Task 1", Status: working.TaskStatusDone},
			{ID: "task-2", Goal: "Task 2", Status: working.TaskStatusPending},
			{ID: "task-3", Goal: "Task 3", Status: working.TaskStatusFailed, FinishedReason: "timeout"},
		},
	}
	prompt, err := buildPrompt(testContext(), constants.PlanStateDoing, plan)
	require.NoError(t, err)
	assert.Contains(t, prompt, "Current Plan")
	assert.Contains(t, prompt, "1/3 done, 1 failed")
	assert.Contains(t, prompt, "Failed Tasks")
	assert.NotContains(t, prompt, "Plan to Confirm")
}
