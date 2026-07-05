package dag

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/B777B2056-2/kugelblitz/constants"
	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/B777B2056-2/kugelblitz/memory/working"
	"github.com/B777B2056-2/kugelblitz/runtime/engine/infra"
	"golang.org/x/sync/errgroup"
)

// DAGTaskExecutor executes plan tasks in topological order.
// ExecuteBatch runs all batches automatically until the DAG reaches a terminal
// state (all done, any failed, or context cancelled).
type DAGTaskExecutor struct {
	provider            core.ILMProvider
	streamMode          bool
	cancel              context.CancelFunc
	workerHooks         core.AgentEventHooks         // set by Planner.RegisterEventHooks
	workerAgentIdentity constants.AgentIdentity      // set by Kernel
	PauseMu             sync.RWMutex                 // shared pause gate for all workers
	hitlAgents          map[string]*infra.ReactAgent // taskID → waiting worker (HITL)
}

// NewDAGTaskExecutor creates an executor that spawns WorkerAgents internally.
func NewDAGTaskExecutor(provider core.ILMProvider, streamMode bool) *DAGTaskExecutor {
	return &DAGTaskExecutor{
		provider:            provider,
		streamMode:          streamMode,
		workerAgentIdentity: constants.AgentWorker,
		hitlAgents:          make(map[string]*infra.ReactAgent),
	}
}

func (d *DAGTaskExecutor) SetWorkerHooks(hooks core.AgentEventHooks) {
	d.workerHooks = hooks
}

// AnyWorkerInHumanLoopWaiting returns true if any worker is waiting for human input.
func (d *DAGTaskExecutor) AnyWorkerInHumanLoopWaiting() bool {
	for _, gate := range d.hitlAgents {
		if gate.HumanLoopWaiting() {
			return true
		}
	}
	return false
}

// ResumeAnyWorkerWithHumanResponse delivers a human response to the first waiting worker.
func (d *DAGTaskExecutor) ResumeAnyWorkerWithHumanResponse(ctx context.Context, response string) error {
	for id, gate := range d.hitlAgents {
		if gate.HumanLoopWaiting() {
			defer func() {
				delete(d.hitlAgents, id)
				d.Resume()
			}()
			return gate.ResumeWithHumanResponse(ctx, response)
		}
	}
	return fmt.Errorf("no worker waiting for human input")
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
func (d *DAGTaskExecutor) Pause() { d.PauseMu.Lock() }

// Resume unblocks all worker tool calls previously paused by Pause.
func (d *DAGTaskExecutor) Resume() { d.PauseMu.Unlock() }

// ExecuteBatch finds all pending tasks whose parents are done, marks them doing,
// and spawns them concurrently. It repeats batch-by-batch until the DAG reaches
// a terminal state — all tasks done, any task failed, or context cancelled.
// The caller does NOT need to re-invoke it from an outer loop.
//
// onTaskFailed is called synchronously from the worker goroutine whenever a
// task fails. The caller can call d.Cancel() from this callback to abort
// remaining batches (already-running tasks complete normally).
func (d *DAGTaskExecutor) ExecuteBatch(ctx context.Context, plan *working.Plan,
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
				t.FinishedReason = "cancelled"
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
							taskMu.FinishedReason = "cancelled"
							working.PutPlan(planMu)
						}
					}
					return nil
				}
				worker := infra.NewWorkerAgent(d.provider, d.streamMode)
				worker.SetHooks(d.workerHooks)
				worker.SetPauseGate(&d.PauseMu)
				worker.SetOnHITL(func(agent *infra.ReactAgent, reason, prompt string) {
					d.hitlAgents[task.ID] = agent
					d.Pause()
				})
				output, usage, err := worker.ExecuteTask(gctx, task.Goal, task.Action)
				planMu, _ := working.GetPlan(plan.ID)
				if planMu == nil {
					return nil
				}
				_, taskMu := working.FindTask(task.ID)
				if taskMu == nil {
					return nil
				}
				// Serialize plan mutations: PutPlan → saveCheckpoint → json.Marshal
				// reads all SubTasks. Holding the plan mutex during mutation ensures
				// the marshal (which also acquires plan.mu) sees a consistent snapshot.
				planMu.Lock()
				if err != nil {
					taskMu.Status = working.TaskStatusFailed
					taskMu.FinishedReason = err.Error()
					atomic.AddInt32(&failCount, 1)
					if onTaskFailed != nil {
						onTaskFailed(task.ID, task.Goal, taskMu.FinishedReason)
					}
				} else {
					taskMu.Status = working.TaskStatusDone
					taskMu.FinishedReason = output
				}
				taskMu.Usage = usage
				planMu.Unlock()
				working.PutPlan(planMu)

				if d.workerHooks.OnTaskUpdated != nil {
					d.workerHooks.OnTaskUpdated(d.workerAgentIdentity, task.ID, task.Goal, string(taskMu.Status), output)
				}
				return nil
			})
		}
		_ = g.Wait()

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
