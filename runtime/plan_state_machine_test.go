package runtime

import (
	"testing"

	"github.com/B777B2056-2/kugelblitz/constants"
	"github.com/B777B2056-2/kugelblitz/memory/working"

	"github.com/stretchr/testify/assert"
)

func TestStateMachine_Def_AllStatusesRegistered(t *testing.T) {
	sm := NewPlannerStateMachine(nil)
	assert.NotNil(t, sm.Def(constants.PlanStatusIntent))
	assert.NotNil(t, sm.Def(constants.PlanStatusDirect))
	assert.NotNil(t, sm.Def(constants.PlanStatusInit))
	assert.NotNil(t, sm.Def(constants.PlanStatusConfirmed))
	assert.NotNil(t, sm.Def(constants.PlanStatusDoing))
	assert.NotNil(t, sm.Def(constants.PlanStatusUpdating))
	assert.NotNil(t, sm.Def(constants.PlanStatusDone))
	assert.NotNil(t, sm.Def(constants.PlanStatusFailed))
	// Rejected is terminal (nil tools)
	assert.Nil(t, sm.Def(constants.PlanStatusRejected).Tools)
}

func TestStateMachine_InitTools(t *testing.T) {
	sm := NewPlannerStateMachine(nil)
	def := sm.Def(constants.PlanStatusInit)
	assert.NotNil(t, def)
	has := func(name string) bool {
		for _, t := range def.Tools {
			if t == name {
				return true
			}
		}
		return false
	}
	assert.True(t, has("plan_create"))
	assert.True(t, has("task_insert"))
	// ask_human should NOT be in Init — human approval happens in Confirmed phase.
	assert.False(t, has("ask_human"), "ask_human should not be available in Init phase")
}

func TestStateMachine_ConfirmedTools(t *testing.T) {
	sm := NewPlannerStateMachine(nil)
	def := sm.Def(constants.PlanStatusConfirmed)
	assert.NotNil(t, def)
	has := func(name string) bool {
		for _, t := range def.Tools {
			if t == name {
				return true
			}
		}
		return false
	}
	assert.True(t, has("ask_human"), "ask_human should be available in Confirmed phase")
	assert.True(t, has("confirm_plan"), "confirm_plan should be available in Confirmed phase")
}

func TestStateMachine_DoingTools(t *testing.T) {
	sm := NewPlannerStateMachine(nil)
	def := sm.Def(constants.PlanStatusDoing)
	has := func(name string) bool {
		for _, t := range def.Tools {
			if t == name {
				return true
			}
		}
		return false
	}
	assert.True(t, has("task_query"))
	assert.True(t, has("task_status_update"))
}

func TestStateMachine_UpdateTools(t *testing.T) {
	sm := NewPlannerStateMachine(nil)
	def := sm.Def(constants.PlanStatusUpdating)
	has := func(name string) bool {
		for _, t := range def.Tools {
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

func TestStateMachine_DoneFailedTools(t *testing.T) {
	sm := NewPlannerStateMachine(nil)
	for _, status := range []constants.PlanStatus{constants.PlanStatusDone, constants.PlanStatusFailed} {
		def := sm.Def(status)
		assert.NotContains(t, def.Tools, "confirm_plan", "status %s should not have confirm_plan", status)
	}
}

func TestStateMachine_BuildPrompt_ConfirmedShowsFullPlan(t *testing.T) {
	sm := NewPlannerStateMachine(nil)
	plan := &working.Plan{
		ID:     "plan-001",
		Name:   "Test Plan",
		Status: constants.PlanStatusConfirmed,
		SubTasks: []working.Task{
			{ID: "task-1", Goal: "Install dependencies", Action: "pip install requests", Status: working.TaskStatusPending, ParentTaskID: ""},
			{ID: "task-2", Goal: "Run tests", Action: "go test ./...", Status: working.TaskStatusPending, ParentTaskID: "task-1"},
		},
	}

	prompt := sm.buildPrompt(constants.PlanStatusConfirmed, nil, plan)

	// Confirmed phase should show full plan details for presentation.
	assert.Contains(t, prompt, "Plan to Confirm")
	assert.Contains(t, prompt, "Test Plan")
	assert.Contains(t, prompt, "plan-001")
	assert.Contains(t, prompt, "Install dependencies")
	assert.Contains(t, prompt, "Run tests")
	assert.Contains(t, prompt, "pip install requests")
	assert.Contains(t, prompt, "go test ./...")
	assert.Contains(t, prompt, "none")    // first task has no deps
	assert.Contains(t, prompt, "task-1")  // second task depends on task-1
	assert.Contains(t, prompt, "ask_human")
}

func TestStateMachine_BuildPrompt_DoingShowsSummary(t *testing.T) {
	sm := NewPlannerStateMachine(nil)
	plan := &working.Plan{
		ID:     "plan-002",
		Name:   "Exec Plan",
		Status: constants.PlanStatusDoing,
		SubTasks: []working.Task{
			{ID: "task-1", Goal: "Task 1", Status: working.TaskStatusDone},
			{ID: "task-2", Goal: "Task 2", Status: working.TaskStatusPending},
			{ID: "task-3", Goal: "Task 3", Status: working.TaskStatusFailed, FinishedReson: "timeout"},
		},
	}

	prompt := sm.buildPrompt(constants.PlanStatusDoing, nil, plan)

	// Doing phase should show brief summary, not full task details.
	assert.Contains(t, prompt, "Current Plan")
	assert.Contains(t, prompt, "1/3 done, 1 failed")
	assert.Contains(t, prompt, "Failed Tasks")
	// Should NOT contain the full "Plan to Confirm" section.
	assert.NotContains(t, prompt, "Plan to Confirm")
}
