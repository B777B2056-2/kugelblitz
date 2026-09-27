package dag

import (
	"context"
	"testing"

	"github.com/B777B2056-2/kugelblitz/core"
	coretypes "github.com/B777B2056-2/kugelblitz/core/types"
	"github.com/B777B2056-2/kugelblitz/memory/working"
	"github.com/B777B2056-2/kugelblitz/observability"
	"github.com/B777B2056-2/kugelblitz/runtime/engine/worker"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeWorker implements worker.Worker without touching infra, proving the DAG
// executor is decoupled from the concrete execution layer.
type fakeWorker struct {
	goal   string
	action string
	calls  int
}

func (f *fakeWorker) SetHooks(core.AgentEventHooks)                    {}
func (f *fakeWorker) SetStepTracer(*observability.StepTracer)          {}
func (f *fakeWorker) SetPauseGate(worker.PauseGate)                    {}
func (f *fakeWorker) SetHumanToolFactory(worker.HumanToolFactory)      {}
func (f *fakeWorker) SetOnHITL(func(worker.HitlAgent, string, string)) {}
func (f *fakeWorker) ExecuteTask(_ context.Context, goal, action string) (string, *coretypes.Usage, error) {
	f.calls++
	f.goal = goal
	f.action = action
	return "done", &coretypes.Usage{TotalTokens: 1}, nil
}

// fakePauseGate implements worker.PauseGate.
type fakePauseGate struct{ paused bool }

func (g *fakePauseGate) Pause()        { g.paused = true }
func (g *fakePauseGate) Resume()       { g.paused = false }
func (g *fakePauseGate) WaitIfPaused() {}

// TestDAGTaskExecutor_AcceptsContractWorker verifies that the executor drives a
// task to completion using only the worker contract (no concrete infra types).
func TestDAGTaskExecutor_AcceptsContractWorker(t *testing.T) {
	fw := &fakeWorker{}
	pg := &fakePauseGate{}
	dag := NewDAGTaskExecutor(nil, false,
		func(_ coretypes.ILMProvider, _ bool) worker.Worker { return fw },
		pg)

	plan := &working.Plan{ID: "p1", SubTasks: []working.Task{
		{ID: "t1", Status: working.TaskStatusPending, Goal: "g", Action: "a"},
	}}
	r := dag.ExecuteBatch(context.Background(), plan, nil)

	assert.True(t, r.AllDone)
	assert.False(t, r.HasFailed)
	assert.Equal(t, 1, fw.calls, "worker factory should spawn exactly one worker")
	assert.Equal(t, "g", fw.goal)
	assert.Equal(t, "a", fw.action)
	require.Equal(t, working.TaskStatusDone, plan.SubTasks[0].Status)
}
