// Package working provides working memory — plan and task tracking for the
// Planner agent. Plans are versioned and persisted as JSONL via the persist
// package, with checkpoints at every mutation.
package working

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/B777B2056-2/kugelblitz/persist"
)

// ---- Types ----

// TaskStatus represents the lifecycle state of a subtask.
type TaskStatus string

const (
	TaskStatusPending TaskStatus = "pending"
	TaskStatusDoing   TaskStatus = "doing"
	TaskStatusDone    TaskStatus = "done"
	TaskStatusFailed  TaskStatus = "failed"
)

// Task is a single subtask within a plan.
type Task struct {
	ID            string      `json:"id"`
	ParentTaskID  string      `json:"parent_task_id,omitempty"`
	Goal          string      `json:"goal"`
	Status        TaskStatus  `json:"status"`
	FinishedReson string      `json:"finished_reason,omitempty"`
	Action        string      `json:"action,omitempty"`
	Usage         *core.Usage `json:"usage,omitempty"`
}

// PlanStatus represents the lifecycle state of a plan.
type PlanStatus string

const (
	PlanStatusInit     PlanStatus = "init"
	PlanStatusDoing    PlanStatus = "doing"
	PlanStatusUpdating PlanStatus = "update"
	PlanStatusDone     PlanStatus = "done"
	PlanStatusFailed   PlanStatus = "failed"
)

// Plan is a versioned plan with subtasks, persisted as JSONL.
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

// GetPlan returns the plan with the given ID, or false if not found.
func GetPlan(id string) (*Plan, bool) {
	planStoreMu.RLock()
	defer planStoreMu.RUnlock()
	p, ok := planStore[id]
	return p, ok
}

// PutPlan stores a plan in memory and persists it with a checkpoint.
func PutPlan(p *Plan) { putPlanWithReason(p, "") }

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

// ListPlans returns all plans in memory.
func ListPlans() []*Plan {
	planStoreMu.RLock()
	defer planStoreMu.RUnlock()
	result := make([]*Plan, 0, len(planStore))
	for _, p := range planStore {
		result = append(result, p)
	}
	return result
}

// FindTask locates a task by ID across all plans.
func FindTask(taskID string) (*Plan, *Task) {
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

// FindTaskIdx returns the index of a task within a plan, or -1.
func FindTaskIdx(plan *Plan, taskID string) int {
	for i, t := range plan.SubTasks {
		if t.ID == taskID {
			return i
		}
	}
	return -1
}

// ---- Plan methods ----

// IsIncomplete returns true if the plan needs resuming.
func (p *Plan) IsIncomplete() bool {
	return p.Status == PlanStatusInit || p.Status == PlanStatusDoing
}

// Persist saves the plan to disk via the persist package.
func (p *Plan) Persist() error {
	return persist.SavePlanJSON(p.ID, p)
}

// LoadPlan loads a plan from disk and restores it to in-memory store.
func LoadPlan(planID string) (*Plan, error) {
	var p Plan
	if err := persist.LoadPlanJSON(planID, &p); err != nil {
		return nil, err
	}
	planStoreMu.Lock()
	planStore[p.ID] = &p
	planStoreMu.Unlock()
	return &p, nil
}

// ---- Workers ----

// WorkerFactory creates workers. Set by runtime at startup to avoid
// circular imports between the tools and runtime packages.
type WorkerFactory func(goal, action string) (output string, usage *core.Usage, err error)

var workerFactory WorkerFactory

// RegisterWorkerFactory sets the factory used to create workers.
func RegisterWorkerFactory(fn WorkerFactory) {
	workerFactory = fn
}

// GetWorkerFactory returns the current worker factory.
func GetWorkerFactory() WorkerFactory { return workerFactory }

// ---- Helpers ----

// PlanToMap converts a Plan to a generic map for tool output.
func PlanToMap(p *Plan) map[string]any {
	data, _ := json.Marshal(p)
	var m map[string]any
	json.Unmarshal(data, &m)
	return m
}

// PlansToMaps converts a slice of Plans for list output.
func PlansToMaps(plans []*Plan) []map[string]any {
	result := make([]map[string]any, len(plans))
	for i, p := range plans {
		result[i] = PlanToMap(p)
	}
	return result
}

// TaskToMap converts a Task to a generic map for tool output.
func TaskToMap(t *Task) map[string]any {
	data, _ := json.Marshal(t)
	var m map[string]any
	json.Unmarshal(data, &m)
	return m
}

// RemoveFromSlice removes an item from a string slice.
func RemoveFromSlice(slice []string, item string) []string {
	for i, s := range slice {
		if s == item {
			return append(slice[:i], slice[i+1:]...)
		}
	}
	return slice
}

