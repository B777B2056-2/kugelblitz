package runtime

import (
	"context"

	"kugelblitz/core"
	"kugelblitz/tools/internals"
)

// plannerSystemPrompt tells the LLM how to plan, execute, and adapt.
const plannerSystemPrompt = `You are a Planner agent. Follow this workflow:

1. PLAN – use plan_create to create an empty plan, then task_insert to add subtasks.
2. EXECUTE – set plan_status_update to "doing", then use worker_spawn with a task_id. The worker automatically reads the task's goal/action, executes it, and updates the task status to done/failed. After each worker finishes, use task_query to inspect the updated task.
3. ADAPT – if a task failed (check via task_query), adjust the plan:
   - task_insert to add a new subtask to fix the issue
   - task_delete to remove obsolete subtasks
   - worker_spawn the fixup task to execute it
4. FINISH – when all tasks are done/failed, call plan_status_update with "done" and summarize results.

Rules:
- Always create a plan first. Never execute without a plan.
- Only spawn ONE worker at a time via its task_id. Wait for it to finish.
- worker_spawn handles all task lifecycle automatically: pending → doing → done/failed. You do NOT need to update task status manually.
- Use plan_query or task_query to check current state before decisions.`

// Planner orchestrates complex goals using plan tools and worker_spawn.
// It wraps a ReactAgent pre-configured with plan/task/worker tools.
type Planner struct {
	react           *ReactAgent
	enableThinking  *bool
	reasoningEffort string
}

// NewPlanner creates a Planner with all plan/task/worker tools.
// WorkerSpawn and its factory are registered automatically.
func NewPlanner(provider core.ILMProvider, streamMode bool) *Planner {
	// Register worker_spawn — each worker gets file tools
	internals.RegisterWorkerSpawn(func(goal, action string) (string, *core.Usage, error) {
		worker := NewWorkerAgent(provider, streamMode, []string{
			"file_read", "file_write", "file_copy",
			"dir_create", "dir_copy",
		})
		return worker.ExecuteTask(context.Background(), goal, action)
	})

	react := NewReactAgent(provider, streamMode)
	react.WithTools(
		"plan_create", "plan_query", "plan_status_update",
		"task_insert", "task_delete", "task_query",
		"worker_spawn",
	)
	return &Planner{react: react}
}

// SetThinking configures thinking mode for the underlying ReactAgent.
func (p *Planner) SetThinking(enabled bool, effort string) {
	p.react.SetThinking(enabled, effort)
}

// RegisterEventHooks forwards event hooks to the underlying ReactAgent.
func (p *Planner) RegisterEventHooks(hooks core.AgentEventHooks) {
	p.react.RegisterEventHooks(hooks)
}

// Execute runs the Planner against a natural-language goal.
func (p *Planner) Execute(ctx context.Context, goal string) ([]core.Message, error) {
	sysMsg := core.NewUserMessage("root", core.TextContent{Text: plannerSystemPrompt})
	sysMsg.Role = "system"

	userMsg := core.NewUserMessage("root", core.TextContent{Text: goal})
	return p.react.Execute(ctx, sysMsg, []core.Message{userMsg})
}

// Interrupt signals the Planner to stop.
func (p *Planner) Interrupt(ctx context.Context) error {
	return p.react.Interrupt(ctx)
}
