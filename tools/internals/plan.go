package internals

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/B777B2056-2/kugelblitz/persist"
	"github.com/B777B2056-2/kugelblitz/tools"
	"github.com/B777B2056-2/kugelblitz/utils"
)

// ---- Types ----

type TaskStatus string

const (
	TaskStatusPending TaskStatus = "pending"
	TaskStatusDoing   TaskStatus = "doing"
	TaskStatusDone    TaskStatus = "done"
	TaskStatusFailed  TaskStatus = "failed"
)

type Task struct {
	ID            string      `json:"id"`
	ParentTaskID  string      `json:"parent_task_id,omitempty"`
	Goal          string      `json:"goal"`
	Status        TaskStatus  `json:"status"`
	FinishedReson string      `json:"finished_reason,omitempty"`
	Action        string      `json:"action,omitempty"`
	Usage         *core.Usage `json:"usage,omitempty"`
}

type PlanStatus string

const (
	PlanStatusInit     PlanStatus = "init"
	PlanStatusDoing    PlanStatus = "doing"
	PlanStatusUpdating PlanStatus = "update"
	PlanStatusDone     PlanStatus = "done"
	PlanStatusFailed   PlanStatus = "failed"
)

type Plan struct {
	ID                        string     `json:"id"`
	Name                      string     `json:"name"`
	SubTasks                  []Task     `json:"subtasks"`
	CurrentActivateSubTaskIDs []string   `json:"current_active_subtask_ids"`
	Status                    PlanStatus `json:"status"`
	FinishedReson             string     `json:"finished_reason,omitempty"`
	Version                   int        `json:"version"`
}

// Checkpoint is a versioned snapshot of a plan.
type Checkpoint struct {
	Version   int       `json:"version"`
	Timestamp time.Time `json:"timestamp"`
	Reason    string    `json:"reason"`
	Plan      Plan      `json:"plan"`
}

// ---- In-memory store ----

var (
	planStore   = make(map[string]*Plan)
	planStoreMu sync.RWMutex
)

func getPlan(id string) (*Plan, bool) {
	planStoreMu.RLock()
	defer planStoreMu.RUnlock()
	p, ok := planStore[id]
	return p, ok
}

func putPlan(p *Plan)                         { putPlanWithReason(p, "") }
func putPlanWithReason(p *Plan, reason string) { saveCheckpoint(p, reason) }

func saveCheckpoint(p *Plan, reason string) {
	p.Version++
	cp := Checkpoint{
		Version:   p.Version,
		Timestamp: time.Now(),
		Reason:    reason,
		Plan:      *p,
	}
	cp.Plan.CurrentActivateSubTaskIDs = append([]string{}, cp.Plan.CurrentActivateSubTaskIDs...)
	cp.Plan.SubTasks = append([]Task{}, cp.Plan.SubTasks...)

	planStoreMu.Lock()
	planStore[p.ID] = p
	planStoreMu.Unlock()

	p.Persist()
	_ = persist.SaveCheckpointJSON(p.ID, p.Version, cp)
}

func listPlans() []*Plan {
	planStoreMu.RLock()
	defer planStoreMu.RUnlock()
	result := make([]*Plan, 0, len(planStore))
	for _, p := range planStore {
		result = append(result, p)
	}
	return result
}

func findTask(taskID string) (*Plan, *Task) {
	planStoreMu.RLock()
	defer planStoreMu.RUnlock()
	for _, p := range planStore {
		for i := range p.SubTasks {
			if p.SubTasks[i].ID == taskID {
				return p, &p.SubTasks[i]
			}
		}
	}
	return nil, nil
}

func findTaskIdx(plan *Plan, taskID string) int {
	for i, t := range plan.SubTasks {
		if t.ID == taskID {
			return i
		}
	}
	return -1
}

// ============================================================
// Plan-level tools
// ============================================================

// ---- PlanCreate (empty plan only) ----

type PlanCreate struct{}

func (t *PlanCreate) Definition() core.ToolDefinition {
	return core.ToolDefinition{
		Name:        "plan_create",
		Description: "Create a new empty plan. Use task_insert afterwards to add subtasks one at a time.",
		JsonSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{
					"type":        "string",
					"description": "Name of the plan",
				},
			},
			"required": []string{"name"},
		},
		OutputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":       map[string]any{"type": "string", "description": "Plan ID"},
				"name":     map[string]any{"type": "string", "description": "Plan name"},
				"status":   map[string]any{"type": "string", "description": "init"},
				"subtasks": map[string]any{"type": "array", "description": "List of tasks (initially empty)"},
			},
		},
	}
}

func (t *PlanCreate) Execute(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
	name, err := tools.Arg(detail, "name")
	if err != nil {
		return tools.ErrorResult(detail.ID, "plan_create", err)
	}

	plan := &Plan{
		ID:     utils.GeneratePlanID(),
		Name:   name,
		Status: PlanStatusInit,
	}
	putPlanWithReason(plan, "plan_create")
	return tools.SuccessResult(detail.ID, "plan_create", planToMap(plan))
}

// ---- PlanQuery ----

type PlanQuery struct{}

func (t *PlanQuery) Definition() core.ToolDefinition {
	return core.ToolDefinition{
		Name:        "plan_query",
		Description: "Query a plan by ID (with all subtasks), or list all plans. Omit plan_id to list all.",
		JsonSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"plan_id": map[string]any{"type": "string", "description": "Plan ID to query. Omit to list all."},
			},
		},
		OutputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"plans": map[string]any{"type": "array", "description": "List of plans (when listing all)"},
				"count": map[string]any{"type": "integer", "description": "Number of plans"},
			},
		},
	}
}

func (t *PlanQuery) Execute(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
	if planID, ok := detail.Args["plan_id"].(string); ok && planID != "" {
		plan, found := getPlan(planID)
		if !found {
			return tools.ErrorResult(detail.ID, "plan_query", fmt.Errorf("plan not found: %s", planID))
		}
		return tools.SuccessResult(detail.ID, "plan_query", planToMap(plan))
	}

	plans := listPlans()
	plansJSON, _ := json.Marshal(plansToStrings(plans))
	var plansList []any
	json.Unmarshal(plansJSON, &plansList)
	return tools.SuccessResult(detail.ID, "plan_query", map[string]any{
		"plans": plansList,
		"count": len(plans),
	})
}

// ---- PlanStatusUpdate (plan only) ----

type PlanStatusUpdate struct{}

func (t *PlanStatusUpdate) Definition() core.ToolDefinition {
	return core.ToolDefinition{
		Name:        "plan_status_update",
		Description: "Update the status of a plan. Valid statuses: init, doing, update, done, failed. Provide a reason when marking done or failed.",
		JsonSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"plan_id": map[string]any{"type": "string", "description": "Plan ID"},
				"status":  map[string]any{"type": "string", "description": "New status"},
				"reason":  map[string]any{"type": "string", "description": "Reason (for done/failed)"},
			},
			"required": []string{"plan_id", "status"},
		},
		OutputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"plan_id": map[string]any{"type": "string", "description": "Plan ID"},
				"status":  map[string]any{"type": "string", "description": "Current status"},
			},
		},
	}
}

func (t *PlanStatusUpdate) Execute(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
	planID, err := tools.Arg(detail, "plan_id")
	if err != nil {
		return tools.ErrorResult(detail.ID, "plan_status_update", err)
	}
	status, err := tools.Arg(detail, "status")
	if err != nil {
		return tools.ErrorResult(detail.ID, "plan_status_update", err)
	}

	plan, found := getPlan(planID)
	if !found {
		return tools.ErrorResult(detail.ID, "plan_status_update", fmt.Errorf("plan not found: %s", planID))
	}

	plan.Status = PlanStatus(status)
	if s := PlanStatus(status); s == PlanStatusDone || s == PlanStatusFailed {
		reason, _ := tools.Arg(detail, "reason")
		plan.FinishedReson = reason
	}
	putPlanWithReason(plan, "plan_status_update")
	return tools.SuccessResult(detail.ID, "plan_status_update", map[string]any{
		"plan_id": planID,
		"status":  string(plan.Status),
	})
}

// ============================================================
// Task-level tools (fine-grained)
// ============================================================

// ---- TaskInsert ----

type TaskInsert struct{}

func (t *TaskInsert) Definition() core.ToolDefinition {
	return core.ToolDefinition{
		Name:        "task_insert",
		Description: "Insert a subtask into a plan. The new task is appended at the end, or inserted after a given task ID.",
		JsonSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"plan_id":  map[string]any{"type": "string", "description": "Plan ID to insert into"},
				"goal":     map[string]any{"type": "string", "description": "Goal of the new subtask"},
				"action":   map[string]any{"type": "string", "description": "Action to take (optional)"},
				"after_id": map[string]any{"type": "string", "description": "Insert after this task ID (optional, default: append to end)"},
			},
			"required": []string{"plan_id", "goal"},
		},
		OutputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task_id": map[string]any{"type": "string", "description": "Newly created task ID"},
				"plan_id": map[string]any{"type": "string", "description": "Parent plan ID"},
				"goal":    map[string]any{"type": "string", "description": "Goal of the new task"},
			},
		},
	}
}

func (t *TaskInsert) Execute(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
	planID, err := tools.Arg(detail, "plan_id")
	if err != nil {
		return tools.ErrorResult(detail.ID, "task_insert", err)
	}
	goal, err := tools.Arg(detail, "goal")
	if err != nil {
		return tools.ErrorResult(detail.ID, "task_insert", err)
	}

	plan, found := getPlan(planID)
	if !found {
		return tools.ErrorResult(detail.ID, "task_insert", fmt.Errorf("plan not found: %s", planID))
	}

	task := Task{
		ID:           utils.GenerateTaskID(),
		ParentTaskID: planID,
		Goal:         goal,
		Status:       TaskStatusPending,
	}
	if action, err := tools.Arg(detail, "action"); err == nil {
		task.Action = action
	}

	if afterID, ok := detail.Args["after_id"].(string); ok && afterID != "" {
		idx := findTaskIdx(plan, afterID)
		if idx >= 0 {
			plan.SubTasks = append(plan.SubTasks[:idx+1], append([]Task{task}, plan.SubTasks[idx+1:]...)...)
		} else {
			plan.SubTasks = append(plan.SubTasks, task)
		}
	} else {
		plan.SubTasks = append(plan.SubTasks, task)
	}

	plan.Status = PlanStatusUpdating
	putPlanWithReason(plan, "task_insert")
	return tools.SuccessResult(detail.ID, "task_insert", map[string]any{
		"task_id": task.ID,
		"plan_id": planID,
		"goal":    task.Goal,
	})
}

// ---- TaskDelete ----

type TaskDelete struct{}

func (t *TaskDelete) Definition() core.ToolDefinition {
	return core.ToolDefinition{
		Name:        "task_delete",
		Description: "Delete a subtask from its plan by task ID.",
		JsonSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task_id": map[string]any{"type": "string", "description": "Task ID to delete"},
			},
			"required": []string{"task_id"},
		},
		OutputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"deleted": map[string]any{"type": "string", "description": "ID of the deleted task"},
				"plan_id": map[string]any{"type": "string", "description": "Parent plan ID"},
			},
		},
	}
}

func (t *TaskDelete) Execute(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
	taskID, err := tools.Arg(detail, "task_id")
	if err != nil {
		return tools.ErrorResult(detail.ID, "task_delete", err)
	}

	plan, task := findTask(taskID)
	if task == nil {
		return tools.ErrorResult(detail.ID, "task_delete", fmt.Errorf("task not found: %s", taskID))
	}

	idx := findTaskIdx(plan, taskID)
	plan.SubTasks = append(plan.SubTasks[:idx], plan.SubTasks[idx+1:]...)
	plan.CurrentActivateSubTaskIDs = removeFromSlice(plan.CurrentActivateSubTaskIDs, taskID)
	plan.Status = PlanStatusUpdating
	putPlanWithReason(plan, "task_delete")
	return tools.SuccessResult(detail.ID, "task_delete", map[string]any{
		"deleted": taskID,
		"plan_id": plan.ID,
	})
}

// ---- TaskQuery ----

type TaskQuery struct{}

func (t *TaskQuery) Definition() core.ToolDefinition {
	return core.ToolDefinition{
		Name:        "task_query",
		Description: "Query a single subtask by its task ID. Returns the task and its parent plan ID.",
		JsonSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task_id": map[string]any{"type": "string", "description": "Task ID to query"},
			},
			"required": []string{"task_id"},
		},
		OutputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":              map[string]any{"type": "string", "description": "Task ID"},
				"goal":            map[string]any{"type": "string", "description": "Task goal"},
				"action":          map[string]any{"type": "string", "description": "Action to perform"},
				"status":          map[string]any{"type": "string", "description": "pending, doing, done, or failed"},
				"finished_reason": map[string]any{"type": "string", "description": "Reason when done/failed"},
				"plan_id":         map[string]any{"type": "string", "description": "Parent plan ID"},
			},
		},
	}
}

func (t *TaskQuery) Execute(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
	taskID, err := tools.Arg(detail, "task_id")
	if err != nil {
		return tools.ErrorResult(detail.ID, "task_query", err)
	}

	plan, task := findTask(taskID)
	if task == nil {
		return tools.ErrorResult(detail.ID, "task_query", fmt.Errorf("task not found: %s", taskID))
	}

	taskJSON, _ := json.Marshal(task)
	var taskMap map[string]any
	json.Unmarshal(taskJSON, &taskMap)
	taskMap["plan_id"] = plan.ID
	return tools.SuccessResult(detail.ID, "task_query", taskMap)
}

// ---- TaskStatusUpdate ----

type TaskStatusUpdate struct{}

func (t *TaskStatusUpdate) Definition() core.ToolDefinition {
	return core.ToolDefinition{
		Name:        "task_status_update",
		Description: "Update the status of a single subtask. Valid statuses: pending, doing, done, failed. The plan's active-subtask list is maintained automatically. Provide a reason when marking done or failed.",
		JsonSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task_id": map[string]any{"type": "string", "description": "Task ID to update"},
				"status":  map[string]any{"type": "string", "description": "New status"},
				"reason":  map[string]any{"type": "string", "description": "Reason (for done/failed)"},
			},
			"required": []string{"task_id", "status"},
		},
		OutputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task_id": map[string]any{"type": "string", "description": "Task ID"},
				"status":  map[string]any{"type": "string", "description": "Updated status"},
				"plan_id": map[string]any{"type": "string", "description": "Parent plan ID"},
			},
		},
	}
}

func (t *TaskStatusUpdate) Execute(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
	taskID, err := tools.Arg(detail, "task_id")
	if err != nil {
		return tools.ErrorResult(detail.ID, "task_status_update", err)
	}
	status, err := tools.Arg(detail, "status")
	if err != nil {
		return tools.ErrorResult(detail.ID, "task_status_update", err)
	}

	plan, task := findTask(taskID)
	if task == nil {
		return tools.ErrorResult(detail.ID, "task_status_update", fmt.Errorf("task not found: %s", taskID))
	}

	task.Status = TaskStatus(status)
	if s := TaskStatus(status); s == TaskStatusDone || s == TaskStatusFailed {
		reason, _ := tools.Arg(detail, "reason")
		task.FinishedReson = reason
	}

	// Maintain active-subtask list
	plan.CurrentActivateSubTaskIDs = removeFromSlice(plan.CurrentActivateSubTaskIDs, taskID)
	if status == string(TaskStatusDoing) {
		plan.CurrentActivateSubTaskIDs = append(plan.CurrentActivateSubTaskIDs, taskID)
	}
	putPlanWithReason(plan, "task_status_update")
	return tools.SuccessResult(detail.ID, "task_status_update", map[string]any{
		"task_id": taskID,
		"status":  string(task.Status),
		"plan_id": plan.ID,
	})
}

// ---- WorkerSpawn (dynamically spawns a WorkerAgent as a tool) ----

// WorkerFactory is the interface for spawning workers. It is set via
// RegisterWorkerSpawn and allows the tools package to create workers
// without importing runtime (avoiding circular imports).
type WorkerFactory func(goal, action string) (output string, usage *core.Usage, err error)

// workerFactory holds the injected worker factory function.
var workerFactory WorkerFactory

// RegisterWorkerFactory sets the factory used by WorkerSpawn to create workers.
// Call this once at startup, passing a function that creates and runs a WorkerAgent.
func RegisterWorkerFactory(fn WorkerFactory) {
	workerFactory = fn
}

// WorkerSpawn is a tool that dynamically spawns a WorkerAgent to execute
// a subtask. It reads the task from the plan store, executes it, and
// automatically updates the task status to done/failed with the result.
// The Planner only needs to observe via plan_query or task_query.
type WorkerSpawn struct{}

func (t *WorkerSpawn) Definition() core.ToolDefinition {
	return core.ToolDefinition{
		Name:        "worker_spawn",
		Description: "Execute a subtask by its task ID. The worker reads the task's goal and action, executes it autonomously, then auto-updates the task status to done or failed. Use task_query afterwards to see the updated state.",
		JsonSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task_id": map[string]any{
					"type":        "string",
					"description": "ID of the task to execute",
				},
			},
			"required": []string{"task_id"},
		},
		OutputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task": map[string]any{
					"type":        "object",
					"description": "The updated task after execution",
				},
				"output": map[string]any{
					"type":        "string",
					"description": "Worker's execution result text",
				},
			},
		},
	}
}

func (t *WorkerSpawn) Execute(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
	taskID, err := tools.Arg(detail, "task_id")
	if err != nil {
		return tools.ErrorResult(detail.ID, "worker_spawn", err)
	}

	plan, task := findTask(taskID)
	if task == nil {
		return tools.ErrorResult(detail.ID, "worker_spawn", fmt.Errorf("task not found: %s", taskID))
	}

	if workerFactory == nil {
		return tools.ErrorResult(detail.ID, "worker_spawn", fmt.Errorf("worker factory not registered"))
	}

	// Mark task as doing
	task.Status = TaskStatusDoing
	plan.CurrentActivateSubTaskIDs = append(plan.CurrentActivateSubTaskIDs, taskID)
	putPlanWithReason(plan, "worker_spawn_task_start")

	// Execute via the injected worker
	output, usage, err := workerFactory(task.Goal, task.Action)
	task.Usage = usage
	if err != nil {
		task.Status = TaskStatusFailed
		task.FinishedReson = fmt.Sprintf("%s: %s", err.Error(), output)
	} else {
		task.Status = TaskStatusDone
		task.FinishedReson = output
	}
	plan.CurrentActivateSubTaskIDs = removeFromSlice(plan.CurrentActivateSubTaskIDs, taskID)
	putPlanWithReason(plan, "worker_spawn_task_end")

	// Serialize the updated task for the return value
	taskMap := taskToMap(task)
	taskMap["plan_id"] = plan.ID
	if usage != nil {
		taskMap["usage"] = map[string]any{
			"input":     usage.InputTokens,
			"output":    usage.OutputTokens,
			"reasoning": usage.ReasoningTokens,
			"total":     usage.TotalTokens,
		}
	}

	return tools.SuccessResult(detail.ID, "worker_spawn", map[string]any{
		"task":   taskMap,
		"output": output,
	})
}

// ---- Plan persistence ----

// IsIncomplete returns true if the plan needs resuming.
func (p *Plan) IsIncomplete() bool {
	return p.Status == PlanStatusInit || p.Status == PlanStatusDoing
}

// Persist delegates to the persist package.
func (p *Plan) Persist() error {
	return persist.SavePlanJSON(p.ID, p)
}

// LoadPlan loads and restores a plan from the persist package.
func LoadPlan(planID string) (*Plan, error) {
	var p Plan
	if err := persist.LoadPlanJSON(planID, &p); err != nil {
		return nil, err
	}
	// Restore to memory without creating a new checkpoint
	planStoreMu.Lock()
	planStore[p.ID] = &p
	planStoreMu.Unlock()
	return &p, nil
}

// ---- PlanRollback ----

type PlanRollback struct{}

func (t *PlanRollback) Definition() core.ToolDefinition {
	return core.ToolDefinition{
		Name:        "plan_rollback",
		Description: "Rollback a plan to a previous checkpoint. Omit version to rollback one step; specify a version number to jump further back. Each rollback creates a new checkpoint.",
		JsonSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"plan_id": map[string]any{"type": "string", "description": "Plan ID to rollback"},
				"version": map[string]any{"type": "integer", "description": "Target version (optional; defaults to current-1)"},
			},
			"required": []string{"plan_id", "version"},
		},
		OutputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"plan_id":      map[string]any{"type": "string", "description": "Plan ID"},
				"from_version": map[string]any{"type": "integer", "description": "Version rolled back from"},
				"to_version":   map[string]any{"type": "integer", "description": "Version rolled back to"},
			},
		},
	}
}

func (t *PlanRollback) Execute(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
	planID, err := tools.Arg(detail, "plan_id")
	if err != nil {
		return tools.ErrorResult(detail.ID, "plan_rollback", err)
	}

	plan, found := getPlan(planID)
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
		targetVersion = fromVersion - 1 // default: rollback one step
	}

	if targetVersion < 1 || targetVersion >= fromVersion {
		return tools.ErrorResult(detail.ID, "plan_rollback",
			fmt.Errorf("version must be between 1 and %d", fromVersion-1))
	}

	// Load checkpoint and restore
	var cp Checkpoint
	if err := persist.LoadCheckpointJSON(planID, targetVersion, &cp); err != nil {
		return tools.ErrorResult(detail.ID, "plan_rollback", err)
	}

	plan.Name = cp.Plan.Name
	plan.SubTasks = cp.Plan.SubTasks
	plan.CurrentActivateSubTaskIDs = cp.Plan.CurrentActivateSubTaskIDs
	plan.Status = cp.Plan.Status
	plan.FinishedReson = cp.Plan.FinishedReson

	putPlanWithReason(plan, fmt.Sprintf("rollback from v%d to v%d", fromVersion, targetVersion))

	return tools.SuccessResult(detail.ID, "plan_rollback", map[string]any{
		"plan_id":      planID,
		"from_version": fromVersion,
		"to_version":   plan.Version,
	})
}

// ---- Helpers ----

func planToMap(p *Plan) map[string]any {
	data, _ := json.Marshal(p)
	var m map[string]any
	json.Unmarshal(data, &m)
	return m
}

func plansToStrings(plans []*Plan) []map[string]any {
	result := make([]map[string]any, len(plans))
	for i, p := range plans {
		result[i] = planToMap(p)
	}
	return result
}

func taskToMap(t *Task) map[string]any {
	data, _ := json.Marshal(t)
	var m map[string]any
	json.Unmarshal(data, &m)
	return m
}

func removeFromSlice(slice []string, item string) []string {
	for i, s := range slice {
		if s == item {
			return append(slice[:i], slice[i+1:]...)
		}
	}
	return slice
}
