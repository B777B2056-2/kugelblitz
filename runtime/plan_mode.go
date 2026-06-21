package runtime

import (
	"context"
	"errors"
	"fmt"

	"kugelblitz/core"
	"kugelblitz/memory"
	"kugelblitz/persist"
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
// It maintains session memory across calls and auto-compresses when the
// context window is exceeded.
type Planner struct {
	react           *ReactAgent
	mem             *memory.SessionMemory
	compressor      *memory.Compressor
	enableThinking  *bool
	reasoningEffort string
}

// NewPlanner creates a Planner with session memory and automatic compression.
// On creation, it scans for incomplete plans from a previous run and resumes them.
func NewPlanner(provider core.ILMProvider, streamMode bool) *Planner {
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

	sessionID := memory.GetSessionMemoryManager().CreateSessionMemory()
	mem, _ := memory.GetSessionMemoryManager().GetSessionMemory(sessionID)

	planner := &Planner{
		react:      react,
		mem:        mem,
		compressor: memory.NewCompressor(provider),
	}
	planner.ResumeIncomplete(context.Background())
	return planner
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
// If the context window is exceeded, it auto-compresses and retries.
func (p *Planner) Execute(ctx context.Context, goal string) ([]core.Message, error) {
	history := p.mem.GetHistoryMessages()

	sysMsg := core.NewUserMessage("planner", core.TextContent{Text: plannerSystemPrompt})
	sysMsg.Role = "system"

	userMsg := core.NewUserMessage("planner", core.TextContent{Text: goal})

	// Try with current history; compress + retry if context exceeded
	result, err := p.react.Execute(ctx, sysMsg, append(history, userMsg))
	if errors.Is(err, core.ErrContextLengthExceeded) {
		// Compress aggressively: keep only the most recent 4 messages
		p.mem.Compress(ctx, p.compressor, memory.CompressConfig{
			KeepLastN:             4,
			MinMessagesToCompress: 1,
		})
		history = p.mem.GetHistoryMessages()
		result, err = p.react.Execute(ctx, sysMsg, append(history, userMsg))
	}
	if err != nil {
		return result, err
	}

	p.mem.AppendMessage(userMsg)
	p.mem.AppendMessages(result)

	// Preventive: compress old messages before they cause context errors
	_ = p.mem.Compress(ctx, p.compressor, memory.CompressConfig{
		KeepLastN:             20,
		MinMessagesToCompress: 10,
	})
	_ = p.mem.Persist()

	return result, nil
}

// ResumeIncomplete scans all persisted plans and resumes any that are
// in "init" or "doing" status. Call once at startup after NewPlanner.
func (p *Planner) ResumeIncomplete(ctx context.Context) {
	planIDs, err := persist.GetManager().ListPlans()
	if err != nil {
		return
	}
	for _, id := range planIDs {
		plan, err := internals.LoadPlan(id)
		if err != nil || !plan.IsIncomplete() {
			continue
		}
		p.resume(ctx, id)
	}
}

// resume loads a persisted plan and continues execution.
func (p *Planner) resume(ctx context.Context, planID string) {
	plan, err := internals.LoadPlan(planID)
	if err != nil {
		return
	}
	prompt := fmt.Sprintf(
		`Resume the plan %q (id: %s). It is currently in status %q.
Use plan_query to see the full state with all subtasks.
Continue from where it left off — spawn workers for remaining pending tasks.
When all tasks are done, call plan_status_update "done" and summarize.`,
		plan.Name, plan.ID, plan.Status,
	)
	p.Execute(ctx, prompt)
}

// Interrupt signals the Planner to stop.
func (p *Planner) Interrupt(ctx context.Context) error {
	return p.react.Interrupt(ctx)
}
