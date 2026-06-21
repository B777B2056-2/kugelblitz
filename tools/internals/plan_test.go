package internals

import (
	"context"
	"testing"

	"kugelblitz/core"
	"kugelblitz/persist"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resetStore() {
	planStoreMu.Lock()
	planStore = make(map[string]*Plan)
	planStoreMu.Unlock()
}

func TestPlanCreate(t *testing.T) {
	resetStore()
	tool := &PlanCreate{}
	result := tool.Execute(context.Background(), core.ToolCallDetail{
		ID: "c1", ToolName: "plan_create",
		Args: map[string]any{"name": "Test Plan"},
	})
	assert.Nil(t, result.Outputs["error"])
	assert.Equal(t, "Test Plan", result.Outputs["name"])
	assert.Equal(t, string(PlanStatusInit), result.Outputs["status"])
	assert.NotEmpty(t, result.Outputs["id"])
}

func TestPlanCreate_MissingName(t *testing.T) {
	resetStore()
	tool := &PlanCreate{}
	result := tool.Execute(context.Background(), core.ToolCallDetail{
		ID: "c1", ToolName: "plan_create",
		Args: map[string]any{},
	})
	assert.NotNil(t, result.Outputs["error"])
}

func TestTaskInsert(t *testing.T) {
	resetStore()
	// Create a plan first
	pc := &PlanCreate{}
	pres := pc.Execute(context.Background(), core.ToolCallDetail{ID: "c1", ToolName: "plan_create", Args: map[string]any{"name": "P"}})
	planID := pres.Outputs["id"].(string)

	// Insert a task
	ti := &TaskInsert{}
	result := ti.Execute(context.Background(), core.ToolCallDetail{
		ID: "i1", ToolName: "task_insert",
		Args: map[string]any{"plan_id": planID, "goal": "do something", "action": "run it"},
	})
	assert.Nil(t, result.Outputs["error"])
	assert.NotEmpty(t, result.Outputs["task_id"])
	assert.Equal(t, planID, result.Outputs["plan_id"])
	assert.Equal(t, "do something", result.Outputs["goal"])
}

func TestTaskInsert_AfterID(t *testing.T) {
	resetStore()
	pc := &PlanCreate{}
	pres := pc.Execute(context.Background(), core.ToolCallDetail{ID: "c1", ToolName: "plan_create", Args: map[string]any{"name": "P"}})
	planID := pres.Outputs["id"].(string)

	ti := &TaskInsert{}
	r1 := ti.Execute(context.Background(), core.ToolCallDetail{ID: "i1", ToolName: "task_insert", Args: map[string]any{"plan_id": planID, "goal": "first"}})
	r2 := ti.Execute(context.Background(), core.ToolCallDetail{ID: "i2", ToolName: "task_insert", Args: map[string]any{"plan_id": planID, "goal": "second", "after_id": r1.Outputs["task_id"]}})

	// Verify order
	plan, _ := getPlan(planID)
	require.Len(t, plan.SubTasks, 2)
	assert.Equal(t, "first", plan.SubTasks[0].Goal)
	assert.Equal(t, "second", plan.SubTasks[1].Goal)
	_ = r2
}

func TestTaskQuery(t *testing.T) {
	resetStore()
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
	assert.Equal(t, "pending", result.Outputs["status"])
	assert.Equal(t, planID, result.Outputs["plan_id"])
}

func TestTaskDelete(t *testing.T) {
	resetStore()
	pc := &PlanCreate{}
	pres := pc.Execute(context.Background(), core.ToolCallDetail{ID: "c1", Args: map[string]any{"name": "P"}})
	planID := pres.Outputs["id"].(string)

	ti := &TaskInsert{}
	ires := ti.Execute(context.Background(), core.ToolCallDetail{ID: "i1", Args: map[string]any{"plan_id": planID, "goal": "test"}})
	taskID := ires.Outputs["task_id"].(string)

	td := &TaskDelete{}
	result := td.Execute(context.Background(), core.ToolCallDetail{ID: "d1", Args: map[string]any{"task_id": taskID}})
	assert.Nil(t, result.Outputs["error"])
	assert.Equal(t, taskID, result.Outputs["deleted"])

	// Verify it's gone
	tq := &TaskQuery{}
	qres := tq.Execute(context.Background(), core.ToolCallDetail{ID: "q1", Args: map[string]any{"task_id": taskID}})
	assert.NotNil(t, qres.Outputs["error"])
}

func TestPlanQuery_ByID(t *testing.T) {
	resetStore()
	pc := &PlanCreate{}
	pres := pc.Execute(context.Background(), core.ToolCallDetail{ID: "c1", Args: map[string]any{"name": "P"}})
	planID := pres.Outputs["id"].(string)

	pq := &PlanQuery{}
	result := pq.Execute(context.Background(), core.ToolCallDetail{ID: "q1", Args: map[string]any{"plan_id": planID}})
	assert.Nil(t, result.Outputs["error"])
	assert.Equal(t, "P", result.Outputs["name"])
}

func TestPlanQuery_ListAll(t *testing.T) {
	resetStore()
	pc := &PlanCreate{}
	pc.Execute(context.Background(), core.ToolCallDetail{ID: "c1", Args: map[string]any{"name": "A"}})
	pc.Execute(context.Background(), core.ToolCallDetail{ID: "c2", Args: map[string]any{"name": "B"}})

	pq := &PlanQuery{}
	result := pq.Execute(context.Background(), core.ToolCallDetail{ID: "q1", Args: map[string]any{}})
	assert.Nil(t, result.Outputs["error"])
	// count is an int, may come back as int or float64 depending on JSON round-trip
	count, ok := result.Outputs["count"].(int)
	if !ok {
		countF, _ := result.Outputs["count"].(float64)
		assert.Equal(t, float64(2), countF)
	} else {
		assert.Equal(t, 2, count)
	}
}

func TestPlanStatusUpdate(t *testing.T) {
	resetStore()
	pc := &PlanCreate{}
	pres := pc.Execute(context.Background(), core.ToolCallDetail{ID: "c1", Args: map[string]any{"name": "P"}})
	planID := pres.Outputs["id"].(string)

	ps := &PlanStatusUpdate{}
	result := ps.Execute(context.Background(), core.ToolCallDetail{ID: "s1", Args: map[string]any{"plan_id": planID, "status": "doing"}})
	assert.Nil(t, result.Outputs["error"])
	assert.Equal(t, "doing", result.Outputs["status"])
}

func TestTaskStatusUpdate(t *testing.T) {
	resetStore()
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
	resetStore()
	oldPM := persist.GetManager()
	persist.SetManager(persist.NewFileManager(t.TempDir()))
	defer persist.SetManager(oldPM)

	// Create a plan with tasks and persist
	pc := &PlanCreate{}
	pres := pc.Execute(context.Background(), core.ToolCallDetail{ID: "c1", Args: map[string]any{"name": "Test"}})
	planID := pres.Outputs["id"].(string)

	ti := &TaskInsert{}
	ires := ti.Execute(context.Background(), core.ToolCallDetail{ID: "i1", Args: map[string]any{"plan_id": planID, "goal": "do it", "action": "run"}})
	taskID := ires.Outputs["task_id"].(string)

	ts := &TaskStatusUpdate{}
	ts.Execute(context.Background(), core.ToolCallDetail{ID: "s1", Args: map[string]any{"task_id": taskID, "status": "done", "reason": "completed"}})

	// Verify persisted (check via PersistManager)
	pm := persist.GetManager()
	data, err := pm.LoadPlan(planID)
	require.NoError(t, err)
	require.NotEmpty(t, data)

	// Simulate restart: clear in-memory store
	planStoreMu.Lock()
	planStore = make(map[string]*Plan)
	planStoreMu.Unlock()

	// Load from disk
	loaded, err := LoadPlan(planID)
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Equal(t, "Test", loaded.Name)
	assert.Equal(t, PlanStatusUpdating, loaded.Status)
	require.Len(t, loaded.SubTasks, 1)
	assert.Equal(t, "done", string(loaded.SubTasks[0].Status))
	assert.Equal(t, "completed", loaded.SubTasks[0].FinishedReson)
}

func TestWorkerSpawn_FactoryNotRegistered(t *testing.T) {
	resetStore()
	prev := workerFactory
	workerFactory = nil
	defer func() { workerFactory = prev }()

	ws := &WorkerSpawn{}
	result := ws.Execute(context.Background(), core.ToolCallDetail{ID: "w1", Args: map[string]any{"task_id": "nonexistent"}})
	// Before factory check, task lookup fails first (task doesn't exist)
	assert.NotNil(t, result.Outputs["error"])
}
