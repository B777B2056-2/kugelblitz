package main

import (
	"testing"

	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newDeriveTestSession() *ChatSession {
	return &ChatSession{
		ID:     "test",
		hitlCh: make(chan string, 1),
	}
}

func TestDerivePlanUpdate_PlanCreate(t *testing.T) {
	srv := &Server{}
	session := newDeriveTestSession()

	result := core.ToolCallResult{
		ToolName: "plan_create",
		Outputs: map[string]any{
			"id":   "plan-1",
			"name": "Test Plan",
			"subtasks": []any{
				map[string]any{"id": "t1", "goal": "step 1"},
				map[string]any{"id": "t2", "goal": "step 2"},
			},
		},
	}

	pu := srv.derivePlanUpdate(session, result)
	require.NotNil(t, pu)
	assert.Equal(t, "plan-1", pu.PlanID)
	assert.Equal(t, "Test Plan", pu.Name)
	assert.Equal(t, "init", pu.Status)
	require.Len(t, pu.Tasks, 2)
	// Map iteration order is non-deterministic
	taskMap := map[string]string{}
	for _, t := range pu.Tasks { taskMap[t.ID] = t.Goal }
	assert.Equal(t, "step 1", taskMap["t1"])
	assert.Equal(t, "step 2", taskMap["t2"])
}

func TestDerivePlanUpdate_ConfirmPlan(t *testing.T) {
	srv := &Server{}
	session := newDeriveTestSession()

	// First, create a plan
	srv.derivePlanUpdate(session, core.ToolCallResult{
		ToolName: "plan_create",
		Outputs:  map[string]any{"id": "plan-1", "name": "P"},
	})

	// Then confirm it
	pu := srv.derivePlanUpdate(session, core.ToolCallResult{
		ToolName: "confirm_plan",
		Outputs:  map[string]any{"id": "plan-1", "status": "doing"},
	})
	require.NotNil(t, pu)
	assert.Equal(t, "doing", pu.Status)
}

func TestDerivePlanUpdate_TaskInsert(t *testing.T) {
	srv := &Server{}
	session := newDeriveTestSession()

	srv.derivePlanUpdate(session, core.ToolCallResult{
		ToolName: "plan_create",
		Outputs:  map[string]any{"id": "plan-1", "name": "P"},
	})

	pu := srv.derivePlanUpdate(session, core.ToolCallResult{
		ToolName: "task_insert",
		Outputs:  map[string]any{"id": "t3", "goal": "new task"},
	})
	require.NotNil(t, pu)
	require.Len(t, pu.Tasks, 1)
	assert.Equal(t, "t3", pu.Tasks[0].ID)
	assert.Equal(t, "new task", pu.Tasks[0].Goal)
	assert.Equal(t, "pending", pu.Tasks[0].Status)
}

func TestDerivePlanUpdate_TaskStatusUpdate(t *testing.T) {
	srv := &Server{}
	session := newDeriveTestSession()

	srv.derivePlanUpdate(session, core.ToolCallResult{
		ToolName: "plan_create",
		Outputs: map[string]any{
			"id": "plan-1", "name": "P",
			"subtasks": []any{map[string]any{"id": "t1", "goal": "step 1"}},
		},
	})

	pu := srv.derivePlanUpdate(session, core.ToolCallResult{
		ToolName: "task_status_update",
		Outputs:  map[string]any{"id": "t1", "status": "done", "goal": "step 1 (completed)"},
	})
	require.NotNil(t, pu)
	require.Len(t, pu.Tasks, 1)
	assert.Equal(t, "done", pu.Tasks[0].Status)
	assert.Equal(t, "step 1 (completed)", pu.Tasks[0].Goal)
}

func TestDerivePlanUpdate_TaskDelete(t *testing.T) {
	srv := &Server{}
	session := newDeriveTestSession()

	srv.derivePlanUpdate(session, core.ToolCallResult{
		ToolName: "plan_create",
		Outputs: map[string]any{
			"id": "plan-1", "name": "P",
			"subtasks": []any{
				map[string]any{"id": "t1", "goal": "keep me"},
				map[string]any{"id": "t2", "goal": "delete me"},
			},
		},
	})

	pu := srv.derivePlanUpdate(session, core.ToolCallResult{
		ToolName: "task_delete",
		Outputs:  map[string]any{"id": "t2"},
	})
	require.NotNil(t, pu)
	require.Len(t, pu.Tasks, 1)
	assert.Equal(t, "t1", pu.Tasks[0].ID, "t2 should be deleted, t1 remaining")
}

func TestDerivePlanUpdate_UnknownTool_NoPlan(t *testing.T) {
	srv := &Server{}
	session := newDeriveTestSession()

	pu := srv.derivePlanUpdate(session, core.ToolCallResult{
		ToolName: "file_read",
		Outputs:  map[string]any{"path": "/tmp/test"},
	})
	assert.Nil(t, pu, "unknown tool without plan should return nil")
}

func TestDerivePlanUpdate_CurrentPlanUpdate_Nil(t *testing.T) {
	srv := &Server{}
	session := newDeriveTestSession()

	pu := srv.currentPlanUpdate(session)
	assert.Nil(t, pu, "no plan should return nil")
}

func TestDerivePlanUpdate_StoredPlanConversion(t *testing.T) {
	plan := &UIPlanState{
		PlanID: "p1",
		Name:   "Test",
		Status: "doing",
		Tasks: map[string]*UIPlanTask{
			"t1": {ID: "t1", Goal: "step 1", Status: "done"},
			"t2": {ID: "t2", Goal: "step 2", Status: "pending"},
		},
	}
	stored := plan.toStored()
	assert.Equal(t, "p1", stored.PlanID)
	assert.Equal(t, "Test", stored.Name)
	assert.Equal(t, "doing", stored.Status)
	assert.Len(t, stored.Tasks, 2)
}
