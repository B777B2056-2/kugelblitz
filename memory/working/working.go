// Package working provides working memory — plan and task tracking for the
// Planner agent. Plans are versioned and persisted as JSONL via the persist
// package, with checkpoints at every mutation.
package working

import (
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/B777B2056-2/kugelblitz/constants"
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

// Plan is a versioned plan with subtasks, persisted as JSONL.
type Plan struct {
	ID                        string              `json:"id"`
	SessionID                 string              `json:"session_id,omitempty"`
	Name                      string              `json:"name"`
	SubTasks                  []Task              `json:"subtasks"`
	CurrentActivateSubTaskIDs []string            `json:"current_active_subtask_ids"`
	State                     constants.PlanState `json:"status"`
	FinishedReson             string              `json:"finished_reason,omitempty"`
	Version                   int                 `json:"version"`

	mu sync.Mutex `json:"-"` // serializes saveCheckpoint calls for this plan
}

// Checkpoint is a versioned snapshot of a plan.
type Checkpoint struct {
	Version   int       `json:"version"`
	Timestamp time.Time `json:"timestamp"`
	Reason    string    `json:"reason"`
	Plan      *Plan     `json:"plan"` // pointer avoids copying Plan.mu
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
	if p == nil {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	p.Version++

	// Build a deep copy for the checkpoint snapshot without copying the mutex.
	snapshot := Plan{
		ID:                        p.ID,
		SessionID:                 p.SessionID,
		Name:                      p.Name,
		SubTasks:                  append([]Task{}, p.SubTasks...),
		CurrentActivateSubTaskIDs: append([]string{}, p.CurrentActivateSubTaskIDs...),
		State:                     p.State,
		FinishedReson:             p.FinishedReson,
		Version:                   p.Version,
	}
	cp := Checkpoint{
		Version:   p.Version,
		Timestamp: time.Now(),
		Reason:    reason,
		Plan:      &snapshot,
	}

	planStoreMu.Lock()
	planStore[p.ID] = p
	planStoreMu.Unlock()

	p.Persist()
	_ = persist.SaveCheckpointJSON(p.ID, cp.Version, cp)
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

// ListPlansBySession returns all plans for the given session, newest first.
func ListPlansBySession(sessionID string) []*Plan {
	planStoreMu.RLock()
	defer planStoreMu.RUnlock()
	var result []*Plan
	for _, p := range planStore {
		if p.SessionID == sessionID {
			result = append(result, p)
		}
	}
	return result
}

// LatestIncompletePlan returns the most recent incomplete plan for the session,
// or nil if none found. "Most recent" = highest Version.
func LatestIncompletePlan(sessionID string) *Plan {
	planStoreMu.RLock()
	defer planStoreMu.RUnlock()
	var latest *Plan
	for _, p := range planStore {
		if p.SessionID == sessionID && p.IsIncomplete() {
			if latest == nil || p.Version > latest.Version {
				latest = p
			}
		}
	}
	return latest
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

// SplitParentIDs splits a comma-separated parent_task_id into individual IDs.
func SplitParentIDs(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// AllParentsDone returns true if every parent task ID is marked done in the plan.
func AllParentsDone(plan *Plan, parentIDs []string) bool {
	if len(parentIDs) == 0 {
		return true
	}
	for _, pid := range parentIDs {
		found := false
		for _, t := range plan.SubTasks {
			if t.ID == pid {
				if t.Status != TaskStatusDone {
					return false
				}
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// ReadyTasks returns all pending tasks whose parents are done, for DAG execution.
func ReadyTasks(plan *Plan) []*Task {
	var ready []*Task
	for i := range plan.SubTasks {
		if plan.SubTasks[i].Status != TaskStatusPending {
			continue
		}
		if AllParentsDone(plan, SplitParentIDs(plan.SubTasks[i].ParentTaskID)) {
			ready = append(ready, &plan.SubTasks[i])
		}
	}
	return ready
}

// ---- Plan methods ----

// IsIncomplete returns true if the plan needs resuming.
func (p *Plan) IsIncomplete() bool {
	return p.State == constants.PlanStateConfirmed || p.State == constants.PlanStateDoing || p.State == constants.PlanStateUpdating
}

// IsValid returns true if the plan has at least one subtask and all
// parent_task_id dependencies form a valid directed acyclic graph.
// Empty subtasks, self-loops, dangling references, and cycles all return false.
func (p *Plan) IsValid() bool {
	if len(p.SubTasks) == 0 {
		core.Warn("plan validation failed: empty subtasks", "plan_json", p.json())
		return false
	}

	// Build task ID set
	taskIDs := make(map[string]bool, len(p.SubTasks))
	for _, t := range p.SubTasks {
		taskIDs[t.ID] = true
	}

	// Build adjacency graph: taskID → list of IDs it depends on
	graph := make(map[string][]string, len(p.SubTasks))
	for _, t := range p.SubTasks {
		for _, parentID := range SplitParentIDs(t.ParentTaskID) {
			if !taskIDs[parentID] {
				core.Warn("plan validation failed: dangling reference",
					"task", t.ID, "parent", parentID, "plan_json", p.json())
				return false
			}
			if parentID == t.ID {
				core.Warn("plan validation failed: self-loop",
					"task", t.ID, "plan_json", p.json())
				return false
			}
			graph[t.ID] = append(graph[t.ID], parentID)
		}
	}

	// DFS cycle detection
	const (
		white, gray, black = 0, 1, 2
	)
	color := make(map[string]int, len(taskIDs))

	var dfs func(id string) bool
	dfs = func(id string) bool {
		color[id] = gray
		for _, parent := range graph[id] {
			switch color[parent] {
			case gray:
				core.Warn("plan validation failed: cycle detected",
					"task", id, "parent_in_cycle", parent, "plan_json", p.json())
				return false
			case white:
				if !dfs(parent) {
					return false
				}
			}
		}
		color[id] = black
		return true
	}

	for id := range taskIDs {
		if color[id] == white && !dfs(id) {
			return false
		}
	}
	return true
}

func (p *Plan) json() string {
	b, _ := json.Marshal(p)
	return string(b)
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
