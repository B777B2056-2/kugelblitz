// Package working provides working memory — plan and task tracking for the
// Planner agent. Plans are versioned and persisted as JSONL via the persist
// package, with checkpoints at every mutation.
package working

import (
	"errors"
	"fmt"
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
	ID             string      `json:"id"`
	ParentTaskID   string      `json:"parent_task_id,omitempty"`
	Goal           string      `json:"goal"`
	Status         TaskStatus  `json:"status"`
	FinishedReason string      `json:"finished_reason,omitempty"`
	Action         string      `json:"action,omitempty"`
	Usage          *core.Usage `json:"usage,omitempty"`
}

// Plan is a versioned plan with subtasks, persisted as JSONL.
type Plan struct {
	ID                        string              `json:"id"`
	SessionID                 string              `json:"session_id,omitempty"`
	Name                      string              `json:"name"`
	SubTasks                  []Task              `json:"subtasks"`
	CurrentActivateSubTaskIDs []string            `json:"current_active_subtask_ids"`
	State                     constants.PlanState `json:"status"`
	FinishedReason            string              `json:"finished_reason,omitempty"`
	Version                   int                 `json:"version"`

	mu sync.Mutex `json:"-"` // serializes saveCheckpoint calls for this plan
}

// Lock acquires the plan's mutex for external mutation serialization.
func (p *Plan) Lock() { p.mu.Lock() }

// Unlock releases the plan's mutex.
func (p *Plan) Unlock() { p.mu.Unlock() }

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

// ResetPlans clears the in-memory plan store.
func ResetPlans() {
	planStoreMu.Lock()
	defer planStoreMu.Unlock()
	planStore = make(map[string]*Plan)
}

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
		FinishedReason:            p.FinishedReason,
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

	_ = p.Persist()
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

// allParentsDone returns true if every parent task ID has status Done.
func allParentsDone(parentIDs []string, statusMap map[string]TaskStatus) bool {
	for _, pid := range parentIDs {
		if statusMap[pid] != TaskStatusDone {
			return false
		}
	}
	return true
}

// ReadyTasks returns all pending tasks whose parents are done, for DAG execution.
func ReadyTasks(plan *Plan) []*Task {
	statusMap := make(map[string]TaskStatus, len(plan.SubTasks))
	for i := range plan.SubTasks {
		statusMap[plan.SubTasks[i].ID] = plan.SubTasks[i].Status
	}
	var ready []*Task
	for i := range plan.SubTasks {
		if plan.SubTasks[i].Status != TaskStatusPending {
			continue
		}
		if allParentsDone(SplitParentIDs(plan.SubTasks[i].ParentTaskID), statusMap) {
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

// Validate returns true if the plan has at least one subtask and all
// parent_task_id dependencies form a valid directed acyclic graph.
// Empty subtasks, self-loops, dangling references, and cycles all return false.
// Validate checks that the plan is well-formed: non-empty tasks, no dangling
// references, no self-loops, and a valid DAG. Returns nil on success.
func (p *Plan) Validate() error {
	if len(p.SubTasks) == 0 {
		return errors.New("plan has no tasks")
	}

	taskIDs := make(map[string]bool, len(p.SubTasks))
	for _, t := range p.SubTasks {
		taskIDs[t.ID] = true
	}

	graph := make(map[string][]string, len(p.SubTasks))
	for _, t := range p.SubTasks {
		for _, parentID := range SplitParentIDs(t.ParentTaskID) {
			if !taskIDs[parentID] {
				return fmt.Errorf("task %q depends on non-existent parent %q", t.ID, parentID)
			}
			if parentID == t.ID {
				return fmt.Errorf("task %q depends on itself", t.ID)
			}
			graph[t.ID] = append(graph[t.ID], parentID)
		}
	}

	const (
		white, gray, black = 0, 1, 2
	)
	color := make(map[string]int, len(taskIDs))

	var dfs func(id string) error
	dfs = func(id string) error {
		color[id] = gray
		for _, parent := range graph[id] {
			switch color[parent] {
			case gray:
				return fmt.Errorf("plan contains a dependency cycle involving %q", parent)
			case white:
				if err := dfs(parent); err != nil {
					return err
				}
			}
		}
		color[id] = black
		return nil
	}

	for id := range taskIDs {
		if color[id] == white {
			if err := dfs(id); err != nil {
				return err
			}
		}
	}
	return nil
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

// ---- Helpers ----

// PlanToMap converts a Plan to a generic map for tool output.
func PlanToMap(p *Plan) map[string]any {
	tasks := make([]map[string]any, len(p.SubTasks))
	for i := range p.SubTasks {
		tasks[i] = TaskToMap(&p.SubTasks[i])
	}
	return map[string]any{
		"id":                         p.ID,
		"session_id":                 p.SessionID,
		"name":                       p.Name,
		"subtasks":                   tasks,
		"current_active_subtask_ids": p.CurrentActivateSubTaskIDs,
		"status":                     string(p.State),
		"finished_reason":            p.FinishedReason,
		"version":                    p.Version,
	}
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
	m := map[string]any{
		"id":     t.ID,
		"goal":   t.Goal,
		"status": string(t.Status),
	}
	if t.ParentTaskID != "" {
		m["parent_task_id"] = t.ParentTaskID
	}
	if t.FinishedReason != "" {
		m["finished_reason"] = t.FinishedReason
	}
	if t.Action != "" {
		m["action"] = t.Action
	}
	if t.Usage != nil {
		m["usage"] = map[string]any{
			"total_tokens":     t.Usage.TotalTokens,
			"input_tokens":     t.Usage.InputTokens,
			"cached_tokens":    t.Usage.CachedTokens,
			"reasoning_tokens": t.Usage.ReasoningTokens,
			"output_tokens":    t.Usage.OutputTokens,
		}
	}
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
