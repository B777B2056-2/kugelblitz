package internals

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/B777B2056-2/kugelblitz/memory/working"
	"github.com/B777B2056-2/kugelblitz/persist"
	"github.com/B777B2056-2/kugelblitz/tools"
	"github.com/B777B2056-2/kugelblitz/utils"
)

// ---- PlanCreate ----

type PlanCreate struct{}

func (t *PlanCreate) Definition() core.ToolDefinition {
	return core.ToolDefinition{
		Name:        "plan_create",
		Description: "Create a new empty plan. Use task_insert afterwards to add subtasks.",
		JsonSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{"type": "string", "description": "Name of the plan"},
			},
			"required": []string{"name"},
		},
		OutputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":       map[string]any{"type": "string"},
				"name":     map[string]any{"type": "string"},
				"status":   map[string]any{"type": "string"},
				"subtasks": map[string]any{"type": "array"},
			},
		},
	}
}

func (t *PlanCreate) Execute(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
	name, err := tools.Arg(detail, "name")
	if err != nil {
		return tools.ErrorResult(detail.ID, "plan_create", err)
	}
	plan := &working.Plan{
		ID:     utils.GeneratePlanID(),
		Name:   name,
		Status: working.PlanStatusInit,
	}
	working.PutPlan(plan)
	return tools.SuccessResult(detail.ID, "plan_create", working.PlanToMap(plan))
}

// ---- PlanQuery ----

type PlanQuery struct{}

func (t *PlanQuery) Definition() core.ToolDefinition {
	return core.ToolDefinition{
		Name:        "plan_query",
		Description: "Query a plan by ID with all subtasks, or list all plans.",
		JsonSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"plan_id": map[string]any{"type": "string", "description": "Plan ID. Omit to list all."},
			},
		},
		OutputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"plans": map[string]any{"type": "array"},
				"count": map[string]any{"type": "integer"},
			},
		},
	}
}

func (t *PlanQuery) Execute(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
	if planID, ok := detail.Args["plan_id"].(string); ok && planID != "" {
		plan, found := working.GetPlan(planID)
		if !found {
			return tools.ErrorResult(detail.ID, "plan_query", fmt.Errorf("plan not found: %s", planID))
		}
		return tools.SuccessResult(detail.ID, "plan_query", working.PlanToMap(plan))
	}
	plans := working.ListPlans()
	plansJSON, _ := json.Marshal(working.PlansToMaps(plans))
	var plansList []any
	json.Unmarshal(plansJSON, &plansList)
	return tools.SuccessResult(detail.ID, "plan_query", map[string]any{
		"plans": plansList, "count": len(plans),
	})
}

// ---- PlanStatusUpdate ----

type PlanStatusUpdate struct{}

func (t *PlanStatusUpdate) Definition() core.ToolDefinition {
	return core.ToolDefinition{
		Name:        "plan_status_update",
		Description: "Update a plan's status: init → doing → done (or failed). Set status to 'update' to add tasks mid-plan.",
		JsonSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"plan_id": map[string]any{"type": "string"},
				"status":  map[string]any{"type": "string", "description": "init, doing, update, done, failed"},
				"reason":  map[string]any{"type": "string", "description": "Optional reason for status change"},
			},
			"required": []string{"plan_id", "status"},
		},
		OutputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":     map[string]any{"type": "string"},
				"status": map[string]any{"type": "string"},
			},
		},
	}
}

func (t *PlanStatusUpdate) Execute(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
	planID, _ := tools.Arg(detail, "plan_id")
	statusStr, _ := tools.Arg(detail, "status")
	reason, _ := tools.Arg(detail, "reason")

	plan, found := working.GetPlan(planID)
	if !found {
		return tools.ErrorResult(detail.ID, "plan_status_update", fmt.Errorf("plan not found: %s", planID))
	}

	newStatus := working.PlanStatus(statusStr)
	plan.Status = newStatus
	if reason != "" {
		plan.FinishedReson = reason
	}
	working.PutPlan(plan)

	return tools.SuccessResult(detail.ID, "plan_status_update", map[string]any{
		"id": plan.ID, "status": string(plan.Status),
	})
}

// ---- TaskInsert ----

type TaskInsert struct{}

func (t *TaskInsert) Definition() core.ToolDefinition {
	return core.ToolDefinition{
		Name:        "task_insert",
		Description: "Insert a new subtask into a plan.",
		JsonSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"plan_id":        map[string]any{"type": "string"},
				"goal":           map[string]any{"type": "string", "description": "What this task should accomplish"},
				"parent_task_id": map[string]any{"type": "string", "description": "Optional parent task ID for dependency tracking"},
			},
			"required": []string{"plan_id", "goal"},
		},
		OutputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task_id": map[string]any{"type": "string"},
				"goal":    map[string]any{"type": "string"},
			},
		},
	}
}

func (t *TaskInsert) Execute(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
	planID, _ := tools.Arg(detail, "plan_id")
	goal, _ := tools.Arg(detail, "goal")
	parentTaskID, _ := tools.Arg(detail, "parent_task_id")

	plan, found := working.GetPlan(planID)
	if !found {
		return tools.ErrorResult(detail.ID, "task_insert", fmt.Errorf("plan not found: %s", planID))
	}

	task := working.Task{
		ID:           utils.GenerateTaskID(),
		ParentTaskID: parentTaskID,
		Goal:         goal,
		Status:       working.TaskStatusPending,
	}
	plan.SubTasks = append(plan.SubTasks, task)
	working.PutPlan(plan)

	return tools.SuccessResult(detail.ID, "task_insert", map[string]any{
		"task_id": task.ID, "goal": task.Goal,
	})
}

// ---- TaskDelete ----

type TaskDelete struct{}

func (t *TaskDelete) Definition() core.ToolDefinition {
	return core.ToolDefinition{
		Name:        "task_delete",
		Description: "Delete a task from a plan.",
		JsonSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task_id": map[string]any{"type": "string"},
			},
			"required": []string{"task_id"},
		},
		OutputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"deleted": map[string]any{"type": "boolean"}},
		},
	}
}

func (t *TaskDelete) Execute(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
	taskID, _ := tools.Arg(detail, "task_id")
	plan, _ := working.FindTask(taskID)
	if plan == nil {
		return tools.ErrorResult(detail.ID, "task_delete", fmt.Errorf("task not found: %s", taskID))
	}
	idx := working.FindTaskIdx(plan, taskID)
	if idx < 0 {
		return tools.ErrorResult(detail.ID, "task_delete", fmt.Errorf("task not found in plan"))
	}
	plan.SubTasks = append(plan.SubTasks[:idx], plan.SubTasks[idx+1:]...)
	plan.CurrentActivateSubTaskIDs = working.RemoveFromSlice(plan.CurrentActivateSubTaskIDs, taskID)
	working.PutPlan(plan)
	return tools.SuccessResult(detail.ID, "task_delete", map[string]any{"deleted": true})
}

// ---- TaskQuery ----

type TaskQuery struct{}

func (t *TaskQuery) Definition() core.ToolDefinition {
	return core.ToolDefinition{
		Name:        "task_query",
		Description: "Query a task by ID, or list all tasks in a plan.",
		JsonSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task_id": map[string]any{"type": "string", "description": "Task ID. Omit plan_id to search all."},
				"plan_id": map[string]any{"type": "string", "description": "Plan ID to list tasks."},
			},
		},
		OutputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":     map[string]any{"type": "string"},
				"status": map[string]any{"type": "string"},
			},
		},
	}
}

func (t *TaskQuery) Execute(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
	if taskID, ok := detail.Args["task_id"].(string); ok && taskID != "" {
		_, task := working.FindTask(taskID)
		if task == nil {
			return tools.ErrorResult(detail.ID, "task_query", fmt.Errorf("task not found: %s", taskID))
		}
		return tools.SuccessResult(detail.ID, "task_query", working.TaskToMap(task))
	}
	if planID, ok := detail.Args["plan_id"].(string); ok && planID != "" {
		plan, found := working.GetPlan(planID)
		if !found {
			return tools.ErrorResult(detail.ID, "task_query", fmt.Errorf("plan not found: %s", planID))
		}
		tasks := make([]map[string]any, len(plan.SubTasks))
		for i := range plan.SubTasks {
			tasks[i] = working.TaskToMap(&plan.SubTasks[i])
		}
		return tools.SuccessResult(detail.ID, "task_query", map[string]any{
			"tasks": tasks, "count": len(tasks),
		})
	}
	return tools.ErrorResult(detail.ID, "task_query", fmt.Errorf("task_id or plan_id required"))
}

// ---- TaskStatusUpdate ----

type TaskStatusUpdate struct{}

func (t *TaskStatusUpdate) Definition() core.ToolDefinition {
	return core.ToolDefinition{
		Name:        "task_status_update",
		Description: "Update a task's status to pending, doing, done, or failed.",
		JsonSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task_id": map[string]any{"type": "string"},
				"status":  map[string]any{"type": "string", "description": "pending, doing, done, failed"},
				"reason":  map[string]any{"type": "string", "description": "Optional reason"},
			},
			"required": []string{"task_id", "status"},
		},
		OutputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":     map[string]any{"type": "string"},
				"status": map[string]any{"type": "string"},
			},
		},
	}
}

func (t *TaskStatusUpdate) Execute(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
	taskID, _ := tools.Arg(detail, "task_id")
	statusStr, _ := tools.Arg(detail, "status")
	reason, _ := tools.Arg(detail, "reason")

	plan, task := working.FindTask(taskID)
	if task == nil {
		return tools.ErrorResult(detail.ID, "task_status_update", fmt.Errorf("task not found: %s", taskID))
	}

	task.Status = working.TaskStatus(statusStr)
	if reason != "" {
		task.FinishedReson = reason
	}
	working.PutPlan(plan)

	return tools.SuccessResult(detail.ID, "task_status_update", map[string]any{
		"id": task.ID, "status": string(task.Status),
	})
}

// ---- WorkerSpawn ----

// RegisteredSpawnFactory is called to spawn workers. Set by runtime at startup.
var RegisteredSpawnFactory working.WorkerFactory

// RegisterWorkerSpawn sets the worker factory (delegates to working package).
func RegisterWorkerSpawn(fn working.WorkerFactory) {
	RegisteredSpawnFactory = fn
	working.RegisterWorkerFactory(fn)
}

type WorkerSpawn struct{}

func (t *WorkerSpawn) Definition() core.ToolDefinition {
	return core.ToolDefinition{
		Name: "worker_spawn",
		Description: "Spawn a WorkerAgent to execute a task. The worker runs independently — its status is updated automatically when done/failed. You only need to call this; there's no output to process.",
		JsonSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task_id": map[string]any{"type": "string"},
			},
			"required": []string{"task_id"},
		},
	}
}

func (t *WorkerSpawn) Execute(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
	taskID, _ := tools.Arg(detail, "task_id")
	fn := working.GetWorkerFactory()
	if fn == nil {
		return tools.ErrorResult(detail.ID, "worker_spawn", fmt.Errorf("worker factory not registered"))
	}

	plan, task := working.FindTask(taskID)
	if task == nil {
		return tools.ErrorResult(detail.ID, "worker_spawn", fmt.Errorf("task not found: %s", taskID))
	}

	task.Status = working.TaskStatusDoing
	working.PutPlan(plan)

	go func() {
		output, usage, err := fn(task.Goal, task.Action)
		planMu, _ := working.GetPlan(plan.ID)
		if planMu == nil {
			return
		}
		_, taskMu := working.FindTask(taskID)
		if taskMu == nil {
			return
		}
		if err != nil {
			taskMu.Status = working.TaskStatusFailed
			taskMu.FinishedReson = err.Error()
		} else {
			taskMu.Status = working.TaskStatusDone
			taskMu.FinishedReson = output
		}
		taskMu.Usage = usage
		working.PutPlan(planMu)
	}()

	return tools.SuccessResult(detail.ID, "worker_spawn", working.TaskToMap(task))
}

// ---- PlanRollback ----

type PlanRollback struct{}

func (t *PlanRollback) Definition() core.ToolDefinition {
	return core.ToolDefinition{
		Name:        "plan_rollback",
		Description: "Rollback a plan to a previous checkpoint. Each rollback creates a new checkpoint.",
		JsonSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"plan_id": map[string]any{"type": "string", "description": "Plan ID"},
				"version": map[string]any{"type": "integer", "description": "Target version (optional; defaults to current-1)"},
			},
			"required": []string{"plan_id", "version"},
		},
		OutputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"plan_id":      map[string]any{"type": "string"},
				"from_version": map[string]any{"type": "integer"},
				"to_version":   map[string]any{"type": "integer"},
			},
		},
	}
}

func (t *PlanRollback) Execute(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
	planID, err := tools.Arg(detail, "plan_id")
	if err != nil {
		return tools.ErrorResult(detail.ID, "plan_rollback", err)
	}
	plan, found := working.GetPlan(planID)
	if !found {
		return tools.ErrorResult(detail.ID, "plan_rollback", fmt.Errorf("plan not found: %s", planID))
	}

	fromVersion := plan.Version
	var targetVersion int
	if v, ok := detail.Args["version"].(float64); ok {
		targetVersion = int(v)
	} else if v, ok := detail.Args["version"].(int); ok {
		targetVersion = v
	} else {
		targetVersion = fromVersion - 1
	}
	if targetVersion < 1 || targetVersion >= fromVersion {
		return tools.ErrorResult(detail.ID, "plan_rollback",
			fmt.Errorf("version must be between 1 and %d", fromVersion-1))
	}

	var cp working.Checkpoint
	if err := persist.LoadCheckpointJSON(planID, targetVersion, &cp); err != nil {
		return tools.ErrorResult(detail.ID, "plan_rollback", err)
	}

	plan.Name = cp.Plan.Name
	plan.SubTasks = cp.Plan.SubTasks
	plan.CurrentActivateSubTaskIDs = cp.Plan.CurrentActivateSubTaskIDs
	plan.Status = cp.Plan.Status
	plan.FinishedReson = cp.Plan.FinishedReson
	working.PutPlan(plan)

	return tools.SuccessResult(detail.ID, "plan_rollback", map[string]any{
		"plan_id": planID, "from_version": fromVersion, "to_version": plan.Version,
	})
}
