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

const plannerSystemPrompt = `You are a Planner agent. Follow this workflow:

1. PLAN – use plan_create to create an empty plan, then task_insert to add subtasks.
2. EXECUTE – set plan_status_update to "doing", then use worker_spawn with a task_id.
3. ADAPT – if a task failed (check via task_query), adjust the plan.
4. FINISH – when all tasks are done/failed, call plan_status_update with "done" and summarize.

Rules:
- Always create a plan first. Never execute without a plan.
- Only spawn ONE worker at a time via its task_id. Wait for it to finish.
- worker_spawn handles all task lifecycle automatically.`

// ReviewConfig controls goal-drift review triggers.
type ReviewConfig struct {
	FailuresBeforeReview int // consecutive failures before review (0 = disabled)
	ReActStepInterval    int // ReAct loop steps between reviews (0 = disabled)
}

func DefaultReviewConfig() ReviewConfig {
	return ReviewConfig{FailuresBeforeReview: 3, ReActStepInterval: 8}
}

type Planner struct {
	react            *ReactAgent
	mem              *memory.SessionMemory
	compressor       *memory.Compressor
	reviewer         *Reviewer
	reviewCfg        ReviewConfig
	goal             string
	consecutiveFails int
	enableThinking   *bool
	reasoningEffort  string
}

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
		"plan_create", "plan_query", "plan_status_update", "plan_rollback",
		"task_insert", "task_delete", "task_query",
		"worker_spawn",
	)

	sessionID := memory.GetSessionMemoryManager().CreateSessionMemory()
	mem, _ := memory.GetSessionMemoryManager().GetSessionMemory(sessionID)

	planner := &Planner{
		react:      react,
		mem:        mem,
		compressor: memory.NewCompressor(provider),
		reviewer:   NewReviewer(provider),
		reviewCfg:  DefaultReviewConfig(),
	}

	// Inject OnToolResult: Planner handles step counting + failure tracking + review
	planner.react.SetOnToolResult(func(results []core.ToolCallResult, step int) bool {
		// Track consecutive failures
		hasFailure := false
		for _, r := range results {
			if _, isErr := r.Outputs["error"]; isErr {
				hasFailure = true
				break
			}
		}
		if hasFailure {
			planner.consecutiveFails++
		} else {
			planner.consecutiveFails = 0
		}

		// Step interval review
		if planner.reviewCfg.ReActStepInterval > 0 && step%planner.reviewCfg.ReActStepInterval == 0 {
			planner.maybeReview(context.Background(), "ReAct step", "")
		}
		// Consecutive failure review
		if planner.reviewCfg.FailuresBeforeReview > 0 &&
			planner.consecutiveFails >= planner.reviewCfg.FailuresBeforeReview {
			planner.maybeReview(context.Background(), "consecutive failures", "")
		}
		return true // continue — replan is a data operation, LLM sees via alert
	})

	planner.ResumeIncomplete(context.Background())
	return planner
}

func (p *Planner) SetThinking(enabled bool, effort string) {
	p.react.SetThinking(enabled, effort)
}

func (p *Planner) SetReviewConfig(cfg ReviewConfig) {
	p.reviewCfg = cfg
}

func (p *Planner) RegisterEventHooks(hooks core.AgentEventHooks) {
	p.react.RegisterEventHooks(hooks)
}

func (p *Planner) Execute(ctx context.Context, goal string) ([]core.Message, error) {
	p.goal = goal
	history := p.mem.GetHistoryMessages()

	sysMsg := core.NewUserMessage("planner", core.TextContent{Text: plannerSystemPrompt})
	sysMsg.Role = "system"
	userMsg := core.NewUserMessage("planner", core.TextContent{Text: goal})

	result, err := p.react.Execute(ctx, sysMsg, append(history, userMsg))
	if errors.Is(err, core.ErrContextLengthExceeded) {
		p.mem.Compress(ctx, p.compressor, memory.CompressConfig{KeepLastN: 4, MinMessagesToCompress: 1})
		history = p.mem.GetHistoryMessages()
		// Review immediately after aggressive compress
		p.maybeReview(ctx, "context exceeded - aggressive compress", goal)
		result, err = p.react.Execute(ctx, sysMsg, append(history, userMsg))
	}
	if err != nil {
		return result, err
	}

	p.mem.AppendMessage(userMsg)
	p.mem.AppendMessages(result)

	_ = p.mem.Persist()
	return result, nil
}

func (p *Planner) maybeReview(ctx context.Context, trigger, goalOverride string) {
	if goalOverride == "" {
		goalOverride = p.goal
	}
	planIDs, _ := persist.GetManager().ListPlans()
	for _, id := range planIDs {
		plan, err := internals.LoadPlan(id)
		if err != nil || !plan.IsIncomplete() {
			continue
		}
		summary := fmt.Sprintf("Plan %q (v%d, status=%s), %d tasks, trigger: %s",
			plan.Name, plan.Version, plan.Status, len(plan.SubTasks), trigger)

		result := p.reviewer.Review(ctx, goalOverride, summary, trigger)
		if result.Drift {
			p.replan(plan)
		}
	}
}

// replan rolls back the plan by one version and injects a drift alert message
// into the session so the LLM sees it on the next ReAct iteration.
func (p *Planner) replan(plan *internals.Plan) {
	if plan.Version <= 1 {
		return
	}
	// Attempt rollback via plan_rollback tool logic (no LLM call)
	targetVersion := plan.Version - 1
	var cp internals.Checkpoint
	if err := persist.LoadCheckpointJSON(plan.ID, targetVersion, &cp); err != nil {
		return
	}
	plan.Name = cp.Plan.Name
	plan.SubTasks = cp.Plan.SubTasks
	plan.CurrentActivateSubTaskIDs = cp.Plan.CurrentActivateSubTaskIDs
	plan.Status = cp.Plan.Status
	plan.FinishedReson = cp.Plan.FinishedReson
	plan.Persist()

	// Inject a drift alert so the LLM notices on next iteration
	alert := core.NewUserMessage(plan.ID, core.TextContent{
		Text: fmt.Sprintf("[System] Goal drift was detected and the plan has been rolled back to version %d. The current plan state differs from what you last saw. Use plan_query to inspect the updated state and replan accordingly.", targetVersion),
	})
	alert.Role = "system"
	p.mem.AppendMessage(alert)
}

func (p *Planner) ResumeIncomplete(ctx context.Context) {
	planIDs, _ := persist.GetManager().ListPlans()
	for _, id := range planIDs {
		plan, err := internals.LoadPlan(id)
		if err != nil || !plan.IsIncomplete() {
			continue
		}
		p.resume(ctx, id)
	}
}

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

func (p *Planner) Interrupt(ctx context.Context) error {
	return p.react.Interrupt(ctx)
}
