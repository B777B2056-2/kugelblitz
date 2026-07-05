package internals

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/B777B2056-2/kugelblitz/constants"
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
		JSONSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{
					"type":        "string",
					"description": "Short, descriptive name for the plan (e.g. 'Deploy Iris SVM model').",
				},
			},
			"required": []string{"name"},
		},
		OutputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":       map[string]any{"type": "string", "description": "Unique plan ID (e.g. 'plan-a1b2c3d4')."},
				"name":     map[string]any{"type": "string", "description": "Plan name as provided."},
				"status":   map[string]any{"type": "string", "description": "Initial status: 'init'."},
				"subtasks": map[string]any{"type": "array", "description": "Empty array — add tasks via task_insert."},
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
		ID:        utils.GeneratePlanID(),
		SessionID: core.SessionIDFromContext(ctx),
		Name:      name,
		State:     constants.PlanStateInit,
	}
	working.PutPlan(plan)
	return tools.SuccessResult(detail.ID, "plan_create", working.PlanToMap(plan))
}

// ---- PlanQuery ----

type PlanQuery struct{}

func (t *PlanQuery) Definition() core.ToolDefinition {
	return core.ToolDefinition{
		Name:        "plan_query",
		Description: "Query a plan by ID to get all subtasks with status, or omit plan_id to list all plans.",
		JSONSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"plan_id": map[string]any{
					"type":        "string",
					"description": "Plan ID to fetch. Omit to list all plans with their IDs and statuses.",
				},
			},
		},
		OutputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"plans": map[string]any{"type": "array", "description": "List of plan objects (when no plan_id given)."},
				"count": map[string]any{"type": "integer", "description": "Number of plans returned."},
				// When plan_id is given: single plan object with id/name/status/subtasks
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
	_ = json.Unmarshal(plansJSON, &plansList)
	return tools.SuccessResult(detail.ID, "plan_query", map[string]any{
		"plans": plansList, "count": len(plans),
	})
}

// ---- ConfirmPlan ----

type ConfirmPlan struct{}

func (t *ConfirmPlan) Definition() core.ToolDefinition {
	return core.ToolDefinition{
		Name: "confirm_plan",
		Description: "Confirm the plan after user review. Call after ask_human to finalize. " +
			"Set status to 'doing' (approved), 'rejected', or 'update' (needs changes).",
		JSONSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"plan_id": map[string]any{
					"type":        "string",
					"description": "The plan ID to confirm.",
				},
				"status": map[string]any{
					"type": "string",
					"description": "Target status after user review:\n" +
						"- 'doing':    user approved — start executing tasks\n" +
						"- 'rejected': user rejected the plan entirely\n" +
						"- 'update':   user requested changes — return to planning",
				},
				"reason": map[string]any{
					"type":        "string",
					"description": "Optional reason or user feedback for the status change.",
				},
			},
			"required": []string{"plan_id", "status"},
		},
		OutputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":     map[string]any{"type": "string", "description": "Plan ID that was confirmed."},
				"status": map[string]any{"type": "string", "description": "New plan status after confirmation."},
			},
		},
	}
}

func (t *ConfirmPlan) Execute(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
	planID, err := tools.RequiredString(detail, "plan_id")
	if err != nil {
		return tools.ErrorResult(detail.ID, "confirm_plan", err)
	}
	statusStr, err := tools.RequiredString(detail, "status")
	if err != nil {
		return tools.ErrorResult(detail.ID, "confirm_plan", err)
	}
	if err := tools.In(statusStr, "doing", "rejected", "update"); err != nil {
		return tools.ErrorResult(detail.ID, "confirm_plan", err)
	}
	reason := tools.OptionalString(detail, "reason")

	plan, found := working.GetPlan(planID)
	if !found {
		return tools.ErrorResult(detail.ID, "confirm_plan", fmt.Errorf("plan not found: %s", planID))
	}

	newStatus := constants.PlanState(statusStr)
	plan.State = newStatus
	if reason != "" {
		plan.FinishedReason = reason
	}
	working.PutPlan(plan)

	return tools.SuccessResult(detail.ID, "confirm_plan", map[string]any{
		"id": plan.ID, "status": string(plan.State),
	})
}

// ---- TaskInsert ----

type TaskInsert struct{}

func (t *TaskInsert) Definition() core.ToolDefinition {
	return core.ToolDefinition{
		Name:        "task_insert",
		Description: "Insert a new subtask into a plan. Use parent_task_id to define execution order — independent tasks run concurrently.",
		JSONSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"plan_id": map[string]any{
					"type":        "string",
					"description": "The plan ID to add this task to.",
				},
				"goal": map[string]any{
					"type":        "string",
					"description": "What this task should accomplish — clear, actionable description.",
				},
				"action": map[string]any{
					"type": "string",
					"description": "Specific command or operation for the worker. " +
						"Examples: 'Run: pip install -r requirements.txt', " +
						"'Read config.yaml and extract database credentials'.",
				},
				"parent_task_id": map[string]any{
					"type":        "string",
					"description": "Comma-separated task IDs that must complete before this task starts. Omit if no dependencies.",
				},
			},
			"required": []string{"plan_id", "goal"},
		},
		OutputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task_id": map[string]any{"type": "string", "description": "Generated unique task ID."},
				"goal":    map[string]any{"type": "string", "description": "Task goal as provided."},
			},
		},
	}
}

func (t *TaskInsert) Execute(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
	planID, err := tools.RequiredString(detail, "plan_id")
	if err != nil {
		return tools.ErrorResult(detail.ID, "task_insert", err)
	}
	goal, err := tools.RequiredString(detail, "goal")
	if err != nil {
		return tools.ErrorResult(detail.ID, "task_insert", err)
	}
	action := tools.OptionalString(detail, "action")
	parentTaskID := tools.OptionalString(detail, "parent_task_id")

	plan, found := working.GetPlan(planID)
	if !found {
		return tools.ErrorResult(detail.ID, "task_insert", fmt.Errorf("plan not found: %s", planID))
	}

	task := working.Task{
		ID:           utils.GenerateTaskID(),
		ParentTaskID: parentTaskID,
		Goal:         goal,
		Action:       action,
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
		Description: "Delete a task from its plan. Use to remove failed or unnecessary tasks during replanning.",
		JSONSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task_id": map[string]any{
					"type":        "string",
					"description": "The task ID to delete.",
				},
			},
			"required": []string{"task_id"},
		},
		OutputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"deleted": map[string]any{"type": "boolean", "description": "true if the task was successfully deleted."},
			},
		},
	}
}

func (t *TaskDelete) Execute(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
	taskID, err := tools.RequiredString(detail, "task_id")
	if err != nil {
		return tools.ErrorResult(detail.ID, "task_delete", err)
	}
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
		Description: "Query a task by ID to get full details, or list all tasks in a plan by plan_id. Provide exactly one of task_id or plan_id.",
		JSONSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task_id": map[string]any{
					"type":        "string",
					"description": "Task ID to fetch. Mutually exclusive with plan_id.",
				},
				"plan_id": map[string]any{
					"type":        "string",
					"description": "Plan ID to list all tasks for. Mutually exclusive with task_id.",
				},
			},
		},
		OutputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				// Single-task query
				"id":              map[string]any{"type": "string", "description": "Task ID."},
				"goal":            map[string]any{"type": "string", "description": "Task goal."},
				"action":          map[string]any{"type": "string", "description": "Action to execute."},
				"status":          map[string]any{"type": "string", "description": "Status: pending, doing, done, or failed."},
				"finished_reason": map[string]any{"type": "string", "description": "Output or error message after completion."},
				"parent_task_id":  map[string]any{"type": "string", "description": "Dependency task IDs (comma-separated)."},
				// Plan-level query
				"tasks": map[string]any{"type": "array", "description": "List of task objects (when plan_id given)."},
				"count": map[string]any{"type": "integer", "description": "Number of tasks returned."},
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
		Description: "Update a task's status manually. Use during execution to mark tasks as done or failed after reviewing their output.",
		JSONSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task_id": map[string]any{
					"type":        "string",
					"description": "The task ID to update.",
				},
				"status": map[string]any{
					"type": "string",
					"description": "New status:\n" +
						"- 'pending': reset to waiting state\n" +
						"- 'doing':   mark as in-progress\n" +
						"- 'done':    mark as completed successfully\n" +
						"- 'failed':  mark as failed (provide reason)",
				},
				"reason": map[string]any{
					"type":        "string",
					"description": "Optional reason — required for 'failed', useful for 'done'.",
				},
			},
			"required": []string{"task_id", "status"},
		},
		OutputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":     map[string]any{"type": "string", "description": "Task ID that was updated."},
				"status": map[string]any{"type": "string", "description": "New task status after update."},
			},
		},
	}
}

func (t *TaskStatusUpdate) Execute(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
	taskID, err := tools.RequiredString(detail, "task_id")
	if err != nil {
		return tools.ErrorResult(detail.ID, "task_status_update", err)
	}
	statusStr, err := tools.RequiredString(detail, "status")
	if err != nil {
		return tools.ErrorResult(detail.ID, "task_status_update", err)
	}
	if err := tools.In(statusStr, "pending", "doing", "done", "failed"); err != nil {
		return tools.ErrorResult(detail.ID, "task_status_update", err)
	}
	reason := tools.OptionalString(detail, "reason")

	plan, task := working.FindTask(taskID)
	if task == nil {
		return tools.ErrorResult(detail.ID, "task_status_update", fmt.Errorf("task not found: %s", taskID))
	}

	task.Status = working.TaskStatus(statusStr)
	if reason != "" {
		task.FinishedReason = reason
	}
	working.PutPlan(plan)

	return tools.SuccessResult(detail.ID, "task_status_update", map[string]any{
		"id": task.ID, "status": string(task.Status),
	})
}

// ---- PlanRollback ----

type PlanRollback struct{}

func (t *PlanRollback) Definition() core.ToolDefinition {
	return core.ToolDefinition{
		Name:        "plan_rollback",
		Description: "Rollback a plan to a previous checkpoint. Use when execution has drifted or produced incorrect results. A new checkpoint is created on rollback.",
		JSONSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"plan_id": map[string]any{
					"type":        "string",
					"description": "Plan ID to rollback.",
				},
				"version": map[string]any{
					"type":        "integer",
					"description": "Target checkpoint version to restore. Defaults to current-1 if omitted. Must be between 1 and current-1.",
				},
			},
			"required": []string{"plan_id", "version"},
		},
		OutputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"plan_id":      map[string]any{"type": "string", "description": "Plan ID that was rolled back."},
				"from_version": map[string]any{"type": "integer", "description": "Version before rollback."},
				"to_version":   map[string]any{"type": "integer", "description": "Version after rollback."},
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
	plan.State = cp.Plan.State
	plan.FinishedReason = cp.Plan.FinishedReason
	working.PutPlan(plan)

	return tools.SuccessResult(detail.ID, "plan_rollback", map[string]any{
		"plan_id": planID, "from_version": fromVersion, "to_version": plan.Version,
	})
}
