package dag

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	coretypes "github.com/B777B2056-2/kugelblitz/core/types"
	"github.com/B777B2056-2/kugelblitz/memory/working"
	"github.com/B777B2056-2/kugelblitz/runtime/engine/infra"
	"github.com/B777B2056-2/kugelblitz/runtime/engine/worker"

	"github.com/stretchr/testify/assert"
)

// testWorkerFactory wraps infra.NewWorkerAgent into a worker.WorkerFactory so the
// DAG integration tests can still spawn the concrete worker.
func testWorkerFactory() worker.WorkerFactory {
	return func(p coretypes.ILMProvider, s bool) worker.Worker {
		return infra.NewWorkerAgent(p, s)
	}
}

// mockProvider implements coretypes.ILMProvider for testing.
type mockProvider struct {
	GenerateFn func(ctx context.Context, params coretypes.GenerateParams) (*coretypes.Message, error)
}

func (m *mockProvider) Generate(ctx context.Context, params coretypes.GenerateParams) (*coretypes.Message, error) {
	if m.GenerateFn != nil {
		return m.GenerateFn(ctx, params)
	}
	return nil, nil
}

func TestDAGTaskExecutor_ExecuteBatch_NoReadyTasks(t *testing.T) {
	dag := NewDAGTaskExecutor(nil, false, testWorkerFactory(), infra.NewPauseGate())
	plan := &working.Plan{ID: "p1", SubTasks: []working.Task{
		{ID: "t1", Status: working.TaskStatusDone},
	}}
	r := dag.ExecuteBatch(context.Background(), plan, nil)
	assert.False(t, r.Batched)
	assert.False(t, r.HasFailed)
	assert.True(t, r.AllDone)
}

func TestDAGTaskExecutor_ExecuteBatch_SingleTask(t *testing.T) {
	callCount := int32(0)
	prov := &mockProvider{
		GenerateFn: func(ctx context.Context, params coretypes.GenerateParams) (*coretypes.Message, error) {
			atomic.AddInt32(&callCount, 1)
			msg := coretypes.NewAssistantMessage(coretypes.TextContent{Text: "done"})
			msg.Usage = &coretypes.Usage{TotalTokens: 10}
			return &msg, nil
		},
	}
	dag := NewDAGTaskExecutor(prov, false, testWorkerFactory(), infra.NewPauseGate())

	plan := &working.Plan{ID: "p1", SubTasks: []working.Task{
		{ID: "t1", Status: working.TaskStatusPending, Goal: "test", Action: "echo hi"},
	}}
	r := dag.ExecuteBatch(context.Background(), plan, nil)
	assert.True(t, r.Batched)
	assert.False(t, r.HasFailed)
	assert.True(t, r.AllDone)
	assert.Equal(t, working.TaskStatusDone, plan.SubTasks[0].Status)
}

func TestDAGTaskExecutor_ExecuteBatch_TaskFailed(t *testing.T) {
	prov := &mockProvider{
		GenerateFn: func(ctx context.Context, params coretypes.GenerateParams) (*coretypes.Message, error) {
			return nil, errors.New("cmd not found")
		},
	}
	dag := NewDAGTaskExecutor(prov, false, testWorkerFactory(), infra.NewPauseGate())

	plan := &working.Plan{ID: "p1", SubTasks: []working.Task{
		{ID: "t1", Status: working.TaskStatusPending, Goal: "test", Action: "bad cmd"},
	}}
	r := dag.ExecuteBatch(context.Background(), plan, nil)
	assert.True(t, r.Batched)
	assert.True(t, r.HasFailed)
	assert.True(t, r.AllDone)
	assert.Equal(t, working.TaskStatusFailed, plan.SubTasks[0].Status)
}

func TestDAGTaskExecutor_ExecuteBatch_DAGOrder(t *testing.T) {
	prov := &mockProvider{
		GenerateFn: func(ctx context.Context, params coretypes.GenerateParams) (*coretypes.Message, error) {
			msg := coretypes.NewAssistantMessage(coretypes.TextContent{Text: "ok"})
			return &msg, nil
		},
	}
	dag := NewDAGTaskExecutor(prov, false, testWorkerFactory(), infra.NewPauseGate())

	plan := &working.Plan{ID: "p1", SubTasks: []working.Task{
		{ID: "A", Status: working.TaskStatusPending, Goal: "A"},
		{ID: "B", Status: working.TaskStatusPending, Goal: "B"},
		{ID: "C", Status: working.TaskStatusPending, Goal: "C", ParentTaskID: "A"},
		{ID: "D", Status: working.TaskStatusPending, Goal: "D", ParentTaskID: "A,B"},
	}}

	r := dag.ExecuteBatch(context.Background(), plan, nil)
	assert.True(t, r.Batched)
	assert.False(t, r.HasFailed)
	assert.True(t, r.AllDone)
	assert.Equal(t, working.TaskStatusDone, plan.SubTasks[0].Status) // A
	assert.Equal(t, working.TaskStatusDone, plan.SubTasks[1].Status) // B
	assert.Equal(t, working.TaskStatusDone, plan.SubTasks[2].Status) // C
	assert.Equal(t, working.TaskStatusDone, plan.SubTasks[3].Status) // D
}

func TestDAGTaskExecutor_ExecuteBatch_MultiBatchAutoLoop(t *testing.T) {
	batchOrder := make([]string, 0)
	var mu sync.Mutex

	prov := &mockProvider{
		GenerateFn: func(ctx context.Context, params coretypes.GenerateParams) (*coretypes.Message, error) {
			mu.Lock()
			batchOrder = append(batchOrder, params.Messages[0].Content.(coretypes.TextContent).Text)
			mu.Unlock()
			msg := coretypes.NewAssistantMessage(coretypes.TextContent{Text: "ok"})
			return &msg, nil
		},
	}
	dag := NewDAGTaskExecutor(prov, false, testWorkerFactory(), infra.NewPauseGate())

	plan := &working.Plan{ID: "p1", SubTasks: []working.Task{
		{ID: "A", Status: working.TaskStatusPending, Goal: "A", Action: "A-action"},
		{ID: "B", Status: working.TaskStatusPending, Goal: "B", Action: "B-action"},
		{ID: "C", Status: working.TaskStatusPending, Goal: "C", Action: "C-action", ParentTaskID: "A"},
		{ID: "D", Status: working.TaskStatusPending, Goal: "D", Action: "D-action", ParentTaskID: "A,B"},
	}}

	r := dag.ExecuteBatch(context.Background(), plan, nil)
	assert.True(t, r.AllDone)
	assert.False(t, r.HasFailed)
	assert.Len(t, batchOrder, 4)
}

func TestDAGTaskExecutor_Cancel(t *testing.T) {
	prov := &mockProvider{
		GenerateFn: func(ctx context.Context, params coretypes.GenerateParams) (*coretypes.Message, error) {
			msg := coretypes.NewAssistantMessage(coretypes.TextContent{Text: "ok"})
			return &msg, nil
		},
	}
	dag := NewDAGTaskExecutor(prov, false, testWorkerFactory(), infra.NewPauseGate())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	plan := &working.Plan{ID: "p1", SubTasks: []working.Task{
		{ID: "t1", Status: working.TaskStatusPending, Goal: "test", Action: "echo hi"},
	}}
	r := dag.ExecuteBatch(ctx, plan, nil)
	assert.True(t, r.HasFailed)
	assert.Equal(t, working.TaskStatusFailed, plan.SubTasks[0].Status)
}

func TestDAGTaskExecutor_AllDone(t *testing.T) {
	dag := NewDAGTaskExecutor(nil, false, testWorkerFactory(), infra.NewPauseGate())
	plan := &working.Plan{ID: "p1", SubTasks: []working.Task{
		{ID: "t1", Status: working.TaskStatusDone},
		{ID: "t2", Status: working.TaskStatusFailed},
	}}
	assert.True(t, dag.isDAGDone(plan))
}

func TestDAGTaskExecutor_NotDone(t *testing.T) {
	dag := NewDAGTaskExecutor(nil, false, testWorkerFactory(), infra.NewPauseGate())
	plan := &working.Plan{ID: "p1", SubTasks: []working.Task{
		{ID: "t1", Status: working.TaskStatusDone},
		{ID: "t2", Status: working.TaskStatusPending},
	}}
	assert.False(t, dag.isDAGDone(plan))
}

func TestDAGTaskExecutor_ContextCancelledDuringExecution(t *testing.T) {
	blocker := make(chan struct{})
	prov := &mockProvider{
		GenerateFn: func(ctx context.Context, params coretypes.GenerateParams) (*coretypes.Message, error) {
			select {
			case <-blocker:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			msg := coretypes.NewAssistantMessage(coretypes.TextContent{Text: "ok"})
			return &msg, nil
		},
	}
	dag := NewDAGTaskExecutor(prov, false, testWorkerFactory(), infra.NewPauseGate())
	ctx, cancel := context.WithCancel(context.Background())

	plan := &working.Plan{ID: "p1", SubTasks: []working.Task{
		{ID: "t1", Status: working.TaskStatusPending, Goal: "blocked", Action: "sleep 999"},
	}}

	go func() {
		cancel()
	}()
	dag.ExecuteBatch(ctx, plan, nil)
	close(blocker)
}

// TestDAGTaskExecutor_InjectsWorkerFactoryAndPauseGate verifies that the
// executor uses the injected worker factory and pause gate instead of
// self-constructing infra.NewWorkerAgent / infra.NewPauseGate.
func TestDAGTaskExecutor_InjectsWorkerFactoryAndPauseGate(t *testing.T) {
	prov := &mockProvider{
		GenerateFn: func(ctx context.Context, params coretypes.GenerateParams) (*coretypes.Message, error) {
			msg := coretypes.NewAssistantMessage(coretypes.TextContent{Text: "ok"})
			return &msg, nil
		},
	}
	var factoryCalls int32
	factory := func(p coretypes.ILMProvider, stream bool) worker.Worker {
		atomic.AddInt32(&factoryCalls, 1)
		return infra.NewWorkerAgent(p, stream)
	}
	pg := infra.NewPauseGate()
	dag := NewDAGTaskExecutor(prov, false, factory, pg)

	// Injected pause gate is shared, not self-constructed.
	assert.Same(t, pg, dag.PauseGate)

	plan := &working.Plan{ID: "p1", SubTasks: []working.Task{
		{ID: "t1", Status: working.TaskStatusPending, Goal: "g", Action: "a"},
	}}
	r := dag.ExecuteBatch(context.Background(), plan, nil)

	assert.True(t, r.AllDone)
	assert.Equal(t, int32(1), atomic.LoadInt32(&factoryCalls), "injected worker factory should be used to spawn workers")
}
