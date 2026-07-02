package runtime

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/B777B2056-2/kugelblitz/memory/working"
	"golang.org/x/sync/errgroup"
)

// DAGTaskExecutor executes plan tasks in topological order.
// ExecuteBatch runs all batches automatically until the DAG reaches a terminal
// state (all done, any failed, or context cancelled).
type DAGTaskExecutor struct {
	provider    core.ILMProvider
	streamMode  bool
	customTools []string
	cancel      context.CancelFunc
	hooks       core.AgentEventHooks // set by Planner.RegisterEventHooks
	pauseMu     sync.RWMutex                       // shared pause gate for all workers
	hitlAgents  map[string]*ReactAgent             // taskID → waiting worker (HITL)
}

// NewDAGTaskExecutor creates an executor that spawns WorkerAgents internally.
func NewDAGTaskExecutor(provider core.ILMProvider, streamMode bool, customTools ...string) *DAGTaskExecutor {
	return &DAGTaskExecutor{
		provider:    provider,
		streamMode:  streamMode,
		customTools: customTools,
		hitlAgents:  make(map[string]*ReactAgent),
	}
}

// Cancel stops all pending worker tasks. Already-running workers complete normally.
func (d *DAGTaskExecutor) Cancel() {
	if d.cancel != nil {
		d.cancel()
		d.cancel = nil
	}
}

// BatchResult reports the outcome of one ExecuteBatch call.
type BatchResult struct {
	Batched   bool // at least one task was executed
	HasFailed bool // at least one task in this batch failed
	AllDone   bool // all tasks are terminal (done or failed)
}

// Pause blocks all worker tool calls until Resume is called. Used when a
// worker enters HITL — other workers must wait for the human response.
func (d *DAGTaskExecutor) Pause() { d.pauseMu.Lock() }

// Resume unblocks all worker tool calls previously paused by Pause.
func (d *DAGTaskExecutor) Resume() { d.pauseMu.Unlock() }

// ExecuteBatch finds all pending tasks whose parents are done, marks them doing,
// and spawns them concurrently. It repeats batch-by-batch until the DAG reaches
// a terminal state — all tasks done, any task failed, or context cancelled.
// The caller does NOT need to re-invoke it from an outer loop.
//
// onTaskFailed is called synchronously from the worker goroutine whenever a
// task fails. The caller can call d.Cancel() from this callback to abort
// remaining batches (already-running tasks complete normally).
func (d *DAGTaskExecutor) ExecuteBatch(plan *working.Plan, ctx context.Context,
	onTaskFailed func(taskID, goal, reason string)) BatchResult {

	// Create a child context so Cancel() stops only this invocation.
	ctx, cancel := context.WithCancel(ctx)
	d.cancel = cancel
	defer func() { d.cancel = nil; cancel() }()

	hasFailed := false
	anyBatch := false

	for {
		ready := working.ReadyTasks(plan)
		if len(ready) == 0 {
			return BatchResult{
				Batched:   anyBatch,
				HasFailed: hasFailed,
				AllDone:   d.isDAGDone(plan),
			}
		}

		if ctx.Err() != nil {
			for _, t := range ready {
				t.Status = working.TaskStatusFailed
				t.FinishedReson = "cancelled"
			}
			working.PutPlan(plan)
			return BatchResult{Batched: anyBatch, HasFailed: true, AllDone: d.isDAGDone(plan)}
		}

		for _, t := range ready {
			t.Status = working.TaskStatusDoing
		}
		working.PutPlan(plan)

		g, gctx := errgroup.WithContext(ctx)
		failCount := int32(0)
		for _, t := range ready {
			task := t
			g.Go(func() error {
				if gctx.Err() != nil {
					planMu, _ := working.GetPlan(plan.ID)
					if planMu != nil {
						if _, taskMu := working.FindTask(task.ID); taskMu != nil {
							taskMu.Status = working.TaskStatusFailed
							taskMu.FinishedReson = "cancelled"
							working.PutPlan(planMu)
						}
					}
					return nil
				}
				worker := NewWorkerAgent(d.provider, d.streamMode, d.customTools...)
				worker.hooks = d.hooks
				worker.pauseGate = &d.pauseMu
				worker.onHITL = func(agent *ReactAgent, reason, prompt string) {
					d.hitlAgents[task.ID] = agent
					d.Pause()
				}
				output, usage, err := worker.ExecuteTask(gctx, task.Goal, task.Action)
				planMu, _ := working.GetPlan(plan.ID)
				if planMu == nil {
					return nil
				}
				_, taskMu := working.FindTask(task.ID)
				if taskMu == nil {
					return nil
				}
				if err != nil {
					taskMu.Status = working.TaskStatusFailed
					taskMu.FinishedReson = err.Error()
					atomic.AddInt32(&failCount, 1)
					if onTaskFailed != nil {
						onTaskFailed(task.ID, task.Goal, taskMu.FinishedReson)
					}
				} else {
					taskMu.Status = working.TaskStatusDone
					taskMu.FinishedReson = output
				}
				taskMu.Usage = usage
				working.PutPlan(planMu)

				if d.hooks.OnTaskUpdated != nil {
					d.hooks.OnTaskUpdated(task.ID, task.Goal, string(taskMu.Status), output)
				}
				return nil
			})
		}
		g.Wait()

		if atomic.LoadInt32(&failCount) > 0 {
			return BatchResult{Batched: true, HasFailed: true, AllDone: d.isDAGDone(plan)}
		}

		anyBatch = true
	}
}

func (d *DAGTaskExecutor) isDAGDone(plan *working.Plan) bool {
	for _, t := range plan.SubTasks {
		if t.Status == working.TaskStatusPending || t.Status == working.TaskStatusDoing {
			return false
		}
	}
	return true
}
