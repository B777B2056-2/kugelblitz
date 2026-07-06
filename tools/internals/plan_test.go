package internals

import (
	"context"
	"testing"

	"github.com/B777B2056-2/kugelblitz/constants"
	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/B777B2056-2/kugelblitz/memory/working"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resetStore points the workspace to a temp dir for isolated test storage.
func resetStore(t *testing.T) {
	t.Helper()
	core.GetWorkspace().SetDir(t.TempDir())
}

func TestPlanCreate(t *testing.T) {
	resetStore(t)
	tool := &PlanCreate{}
	result := tool.Execute(context.Background(), core.ToolCallDetail{
		ID: "c1", ToolName: "plan_create",
		Args: map[string]any{"name": "Test Plan"},
	})
	assert.Nil(t, result.Outputs["error"])
	assert.Equal(t, "Test Plan", result.Outputs["name"])
	assert.Equal(t, string(constants.PlanStateInit), result.Outputs["status"])
	assert.NotEmpty(t, result.Outputs["id"])
}

func TestPlanCreate_MissingName(t *testing.T) {
	resetStore(t)
	tool := &PlanCreate{}
	result := tool.Execute(context.Background(), core.ToolCallDetail{
		ID: "c1", ToolName: "plan_create", Args: map[string]any{},
	})
	assert.NotNil(t, result.Outputs["error"])
}

func TestTaskInsert(t *testing.T) {
	resetStore(t)
	pc := &PlanCreate{}
	pres := pc.Execute(context.Background(), core.ToolCallDetail{ID: "c1", ToolName: "plan_create", Args: map[string]any{"name": "P"}})
	planID := pres.Outputs["id"].(string)

	ti := &TaskInsert{}
	result := ti.Execute(context.Background(), core.ToolCallDetail{
		ID: "i1", ToolName: "task_insert",
		Args: map[string]any{"plan_id": planID, "goal": "do something"},
	})
	assert.Nil(t, result.Outputs["error"])
	assert.NotEmpty(t, result.Outputs["task_id"])
	assert.Equal(t, "do something", result.Outputs["goal"])
}

func TestTaskInsert_AfterID(t *testing.T) {
	resetStore(t)
	pc := &PlanCreate{}
	pres := pc.Execute(context.Background(), core.ToolCallDetail{ID: "c1", ToolName: "plan_create", Args: map[string]any{"name": "P"}})
	planID := pres.Outputs["id"].(string)

	ti := &TaskInsert{}
	r1 := ti.Execute(context.Background(), core.ToolCallDetail{ID: "i1", ToolName: "task_insert", Args: map[string]any{"plan_id": planID, "goal": "first"}})
	r2 := ti.Execute(context.Background(), core.ToolCallDetail{ID: "i2", ToolName: "task_insert", Args: map[string]any{"plan_id": planID, "goal": "second", "after_id": r1.Outputs["task_id"]}})

	plan, _ := working.GetPlan(planID)
	require.Len(t, plan.SubTasks, 2)
	assert.Equal(t, "first", plan.SubTasks[0].Goal)
	assert.Equal(t, "second", plan.SubTasks[1].Goal)
	_ = r2
}

func TestTaskQuery(t *testing.T) {
	resetStore(t)
	pc := &PlanCreate{}
	pres := pc.Execute(context.Background(), core.ToolCallDetail{ID: "c1", ToolName: "plan_create", Args: map[string]any{"name": "P"}})
	planID := pres.Outputs["id"].(string)

	ti := &TaskInsert{}
	ires := ti.Execute(context.Background(), core.ToolCallDetail{ID: "i1", ToolName: "task_insert", Args: map[string]any{"plan_id": planID, "goal": "test"}})
	taskID := ires.Outputs["task_id"].(string)

	tq := &TaskQuery{}
	result := tq.Execute(context.Background(), core.ToolCallDetail{ID: "q1", ToolName: "task_query", Args: map[string]any{"task_id": taskID}})
	assert.Nil(t, result.Outputs["error"])
	assert.Equal(t, "test", result.Outputs["goal"])
}

func TestTaskDelete(t *testing.T) {
	resetStore(t)
	pc := &PlanCreate{}
	pres := pc.Execute(context.Background(), core.ToolCallDetail{ID: "c1", Args: map[string]any{"name": "P"}})
	planID := pres.Outputs["id"].(string)

	ti := &TaskInsert{}
	ires := ti.Execute(context.Background(), core.ToolCallDetail{ID: "i1", Args: map[string]any{"plan_id": planID, "goal": "test"}})
	taskID := ires.Outputs["task_id"].(string)

	td := &TaskDelete{}
	result := td.Execute(context.Background(), core.ToolCallDetail{ID: "d1", Args: map[string]any{"task_id": taskID}})
	assert.Nil(t, result.Outputs["error"])

	tq := &TaskQuery{}
	qres := tq.Execute(context.Background(), core.ToolCallDetail{ID: "q1", Args: map[string]any{"task_id": taskID}})
	assert.NotNil(t, qres.Outputs["error"])
}

func TestPlanQuery_ByID(t *testing.T) {
	resetStore(t)
	pc := &PlanCreate{}
	pres := pc.Execute(context.Background(), core.ToolCallDetail{ID: "c1", Args: map[string]any{"name": "P"}})
	planID := pres.Outputs["id"].(string)

	pq := &PlanQuery{}
	result := pq.Execute(context.Background(), core.ToolCallDetail{ID: "q1", Args: map[string]any{"plan_id": planID}})
	assert.Nil(t, result.Outputs["error"])
	assert.Equal(t, "P", result.Outputs["name"])
}

func TestConfirmPlan(t *testing.T) {
	resetStore(t)
	pc := &PlanCreate{}
	pres := pc.Execute(context.Background(), core.ToolCallDetail{ID: "c1", Args: map[string]any{"name": "P"}})
	planID := pres.Outputs["id"].(string)

	ps := &ConfirmPlan{}
	result := ps.Execute(context.Background(), core.ToolCallDetail{ID: "s1", Args: map[string]any{"plan_id": planID, "status": "doing"}})
	assert.Nil(t, result.Outputs["error"])
	assert.Equal(t, "doing", result.Outputs["status"])
}

func TestTaskStatusUpdate(t *testing.T) {
	resetStore(t)
	pc := &PlanCreate{}
	pres := pc.Execute(context.Background(), core.ToolCallDetail{ID: "c1", Args: map[string]any{"name": "P"}})
	planID := pres.Outputs["id"].(string)

	ti := &TaskInsert{}
	ires := ti.Execute(context.Background(), core.ToolCallDetail{ID: "i1", Args: map[string]any{"plan_id": planID, "goal": "test"}})
	taskID := ires.Outputs["task_id"].(string)

	ts := &TaskStatusUpdate{}
	result := ts.Execute(context.Background(), core.ToolCallDetail{ID: "s1", Args: map[string]any{"task_id": taskID, "status": "doing"}})
	assert.Nil(t, result.Outputs["error"])
	assert.Equal(t, "doing", result.Outputs["status"])
}

func TestPlanPersistAndLoad(t *testing.T) {
	resetStore(t)
	pc := &PlanCreate{}
	pres := pc.Execute(context.Background(), core.ToolCallDetail{ID: "c1", Args: map[string]any{"name": "Test"}})
	planID := pres.Outputs["id"].(string)

	ti := &TaskInsert{}
	ires := ti.Execute(context.Background(), core.ToolCallDetail{ID: "i1", Args: map[string]any{"plan_id": planID, "goal": "do it"}})
	taskID := ires.Outputs["task_id"].(string)

	ts := &TaskStatusUpdate{}
	ts.Execute(context.Background(), core.ToolCallDetail{ID: "s1", Args: map[string]any{"task_id": taskID, "status": "done", "reason": "completed"}})

	// Load from disk
	loaded, err := working.LoadPlan(planID)
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Equal(t, "Test", loaded.Name)
	assert.Len(t, loaded.SubTasks, 1)
	assert.Equal(t, working.TaskStatusDone, loaded.SubTasks[0].Status)
}
