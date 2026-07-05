package fsm

import (
	"testing"

	"github.com/B777B2056-2/kugelblitz/constants"
	"github.com/B777B2056-2/kugelblitz/memory/working"

	"github.com/stretchr/testify/assert"
)

func TestToolsForState_AllStatusesRegistered(t *testing.T) {
	assert.NotNil(t, ToolsForState(constants.PlanStateIntent))
	assert.NotNil(t, ToolsForState(constants.PlanStateDirect))
	assert.NotNil(t, ToolsForState(constants.PlanStateInit))
	assert.NotNil(t, ToolsForState(constants.PlanStateConfirmed))
	assert.NotNil(t, ToolsForState(constants.PlanStateDoing))
	assert.NotNil(t, ToolsForState(constants.PlanStateUpdating))
	assert.NotNil(t, ToolsForState(constants.PlanStateDone))
	assert.NotNil(t, ToolsForState(constants.PlanStateFailed))
	assert.Empty(t, ToolsForState(constants.PlanStateRejected))
}

func TestToolsForState_InitTools(t *testing.T) {
	tools := ToolsForState(constants.PlanStateInit)
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
	tools := ToolsForState(constants.PlanStateConfirmed)
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
	tools := ToolsForState(constants.PlanStateDoing)
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
	tools := ToolsForState(constants.PlanStateUpdating)
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
		tools := ToolsForState(status)
		assert.NotContains(t, tools, "confirm_plan", "status %s should not have confirm_plan", status)
	}
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
	prompt := buildPrompt(constants.PlanStateConfirmed, plan)
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
	prompt := buildPrompt(constants.PlanStateDoing, plan)
	assert.Contains(t, prompt, "Current Plan")
	assert.Contains(t, prompt, "1/3 done, 1 failed")
	assert.Contains(t, prompt, "Failed Tasks")
	assert.NotContains(t, prompt, "Plan to Confirm")
}
