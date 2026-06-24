package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/B777B2056-2/kugelblitz/memory"
	"github.com/B777B2056-2/kugelblitz/observability"
	"github.com/B777B2056-2/kugelblitz/persist"
	"github.com/B777B2056-2/kugelblitz/skills"
	"github.com/B777B2056-2/kugelblitz/tools/internals"
)

const plannerSystemPrompt = `You are a Planner agent. Follow this workflow:

1. PLAN – use plan_create to create an empty plan, then task_insert to add subtasks.
2. EXECUTE – set plan_status_update to "doing", then use worker_spawn with a task_id.
3. ADAPT – if a task failed (check via task_query), adjust the plan.
4. FINISH – when all tasks are done/failed, call plan_status_update with "done" and summarize.

Rules:
- Always create a plan first. Never execute without a plan.
- When tasks are independent (different files, no shared state), spawn them together in one response. They execute concurrently.
- When tasks depend on each other, execute sequentially -- wait for the first to finish before spawning the next.
- Use task_query to verify task independence before parallel spawn.
- worker_spawn handles all task lifecycle automatically.`

type ReviewConfig struct {
	FailuresBeforeReview  int
	ReActStepInterval     int
	MaxToolResultChars    int // 0 = no limit; >0 = compress tool results exceeding this many UTF-8 chars
}

func DefaultReviewConfig() ReviewConfig {
	return ReviewConfig{FailuresBeforeReview: 3, ReActStepInterval: 8}
}

// DefaultMaxToolResultChars is the default threshold for tool result compression.
const DefaultMaxToolResultChars = 4000

type Planner struct {
	react            *ReactAgent
	mem              *memory.SessionMemory
	ltm              *memory.LongTermMemory
	compressor       *memory.Compressor
	reviewer         *Reviewer
	reviewCfg        ReviewConfig
	goal             string
	consecutiveFails int
	enableThinking   *bool
	reasoningEffort  string
	skillTool        *internals.SkillUse
	activeSkill      *skills.Skill
	skillList        []*skills.Skill
	obs              core.Observer
	currentTrace     core.TraceSpan
	instr            *observability.PlannerInstrument
	onLLMUsage       func(core.LLMUsageReport)
	maxToolResultChars int
}

// PlannerOption configures a Planner at creation time.
type PlannerOption func(*Planner)

// WithCustomTools adds additional tool names to the Planner's ReAct agent.
func WithCustomTools(names ...string) PlannerOption {
	return func(p *Planner) { p.react.WithTools(names...) }
}

// WithObserver sets the observability observer for tracing Planner execution.
func WithObserver(obs core.Observer) PlannerOption {
	return func(p *Planner) {
		p.obs = obs
		p.instr = observability.NewPlannerInstrument(nil) // trace set in Execute
	}
}

// WithLLMUsageCallback registers a callback that fires for every LLM call
// made during planner execution (main loop, compressor, reviewer, workers).
// The report Identity field distinguishes the source.
func WithLLMUsageCallback(fn func(core.LLMUsageReport)) PlannerOption {
	return func(p *Planner) { p.onLLMUsage = fn }
}

// WithMaxToolResultChars sets the UTF-8 character threshold for tool result compression.
// When a tool result exceeds this length, the harness compresses it via the LLM before
// injecting it into the conversation context. Set to 0 to disable (default: 4000).
func WithMaxToolResultChars(n int) PlannerOption {
	return func(p *Planner) { p.maxToolResultChars = n }
}

func NewPlanner(provider core.ILMProvider, streamMode bool, opts ...PlannerOption) *Planner {
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
		"memory_store", "memory_search", "memory_get_section",
		"skill_use",
		"worker_spawn",
		"ask_human",
	)
	react.EnableHumanInTheLoop()

	sessionID := memory.GetSessionMemoryManager().CreateSessionMemory()
	mem, _ := memory.GetSessionMemoryManager().GetSessionMemory(sessionID)

	ltm, _ := memory.NewLongTermMemory()
	internals.RegisterMemoryTools(ltm)

	// Load skills and register skill_use tool
	activeSkill := &skills.Skill{}
	skillNames, _ := skills.List()
	skillList := make([]*skills.Skill, 0, len(skillNames))
	for _, name := range skillNames {
		if s, err := skills.Load(name); err == nil {
			skillList = append(skillList, s)
		}
	}
	skillTool := internals.RegisterSkillTool(skillList, activeSkill)

	// Configure LLM-based semantic judge for memory conflict resolution
	memory.SetSemanticJudge(func(oldVal, newVal string) bool {
		prompt := fmt.Sprintf(
			`Are these two statements semantically equivalent (same fact, different wording)?
A: %s
B: %s
Answer ONLY "YES" or "NO".`, oldVal, newVal)
		msg := core.NewUserMessage("judge", core.TextContent{Text: prompt})
		resp, err := provider.Generate(context.Background(), core.GenerateParams{
			Messages: []core.Message{msg}, Stream: false,
		})
		if err != nil {
			return false
		}
		if tc, ok := resp.Content.(core.TextContent); ok {
			return strings.Contains(strings.ToUpper(tc.Text), "YES")
		}
		return false
	})

	planner := &Planner{
		react:              react,
		mem:                mem,
		ltm:                ltm,
		compressor:         memory.NewCompressor(provider),
		reviewer:           NewReviewer(provider),
		reviewCfg:          DefaultReviewConfig(),
		skillTool:          skillTool,
		activeSkill:        activeSkill,
		skillList:          skillList,
		obs:                core.NoopObserver{},
		maxToolResultChars: DefaultMaxToolResultChars,
	}

	planner.react.SetOnToolResult(func(results []core.ToolCallResult, step int) bool {
		// Compress oversized tool results before they enter context
		planner.compressToolResults(context.Background(), results)

		// Capture LLM usage before StepSpan (it resets internal accumulators)
		var stepUsage core.Usage
		if planner.instr != nil {
			stepUsage = planner.instr.LastUsage()
			sp := planner.instr.StepSpan(step, results)
			sp.End()
		}
		// Fire usage callback for this planner step (always, even without instr)
		planner.fireLLMUsage(core.LLMUsageReport{
			Identity: fmt.Sprintf("planner.step-%d", step),
			Usage:    stepUsage,
		})

		// Fire usage callback for worker spawn results
		for _, r := range results {
			if task, ok := r.Outputs["task"].(map[string]any); ok {
				if um, ok := task["usage"].(map[string]any); ok {
					planner.fireLLMUsage(core.LLMUsageReport{
						Identity: fmt.Sprintf("worker.%v", task["id"]),
						Usage: core.Usage{
							InputTokens:  toInt64(um["input"]),
							OutputTokens: toInt64(um["output"]),
							TotalTokens:  toInt64(um["total"]),
						},
					})
				}
			}
		}

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
		if planner.reviewCfg.ReActStepInterval > 0 && step%planner.reviewCfg.ReActStepInterval == 0 {
			planner.maybeReview(context.Background(), "ReAct step", "")
		}
		if planner.reviewCfg.FailuresBeforeReview > 0 &&
			planner.consecutiveFails >= planner.reviewCfg.FailuresBeforeReview {
			planner.maybeReview(context.Background(), "consecutive failures", "")
		}
		return true
	})

	for _, opt := range opts {
		opt(planner)
	}
	planner.ResumeIncomplete(context.Background())
	return planner
}

func (p *Planner) SetThinking(enabled bool, effort string) {
	p.react.SetThinking(enabled, effort)
}

func (p *Planner) GetObserver() core.Observer { return p.obs }

func (p *Planner) fireLLMUsage(report core.LLMUsageReport) {
	if p.onLLMUsage != nil {
		p.onLLMUsage(report)
	}
}

// compressToolResults compresses individual string values in tool result Outputs
// that exceed maxToolResultChars. Non-string fields and shorter values are left as-is.
func (p *Planner) compressToolResults(ctx context.Context, results []core.ToolCallResult) {
	if p.maxToolResultChars <= 0 {
		return
	}
	for i := range results {
		r := &results[i]
		if _, isErr := r.Outputs["error"]; isErr {
			continue // don't compress error messages
		}
		for k, v := range r.Outputs {
			s, ok := v.(string)
			if !ok {
				continue // only compress string values
			}
			if utf8.RuneCountInString(s) <= p.maxToolResultChars {
				continue
			}
			summary, err := p.summarizeToolResult(ctx, r.ToolName, k, s)
			if err != nil {
				continue // leave original in place on error
			}
			r.Outputs[k] = summary
		}
	}
}

// summarizeToolResult asks the LLM to produce a concise summary of a large string field.
func (p *Planner) summarizeToolResult(ctx context.Context, toolName, fieldKey, raw string) (string, error) {
	prompt := fmt.Sprintf(
		`Summarize this tool result field. Keep key facts, IDs, file paths, error messages, and numbers.
Be concise but don't drop critical data. Output ONLY the summary, no preamble.

Tool: %s
Field: %s
Original length: %d chars

Content:
%s`, toolName, fieldKey, utf8.RuneCountInString(raw), raw)

	msg := core.NewUserMessage("tool-compressor", core.TextContent{Text: prompt})
	resp, err := p.reviewer.provider.Generate(ctx, core.GenerateParams{
		Messages: []core.Message{msg},
		Stream:   false,
	})
	if err != nil {
		return "", err
	}
	if tc, ok := resp.Content.(core.TextContent); ok {
		return strings.TrimSpace(tc.Text), nil
	}
	return "", fmt.Errorf("unexpected response type: %T", resp.Content)
}

func (p *Planner) SetReviewConfig(cfg ReviewConfig) {
	p.reviewCfg = cfg
}

func (p *Planner) RegisterEventHooks(hooks core.AgentEventHooks) {
	p.react.RegisterEventHooks(hooks)
}

func (p *Planner) Execute(ctx context.Context, goal string) ([]core.Message, error) {
	p.goal = goal

	traceName := p.mem.SessionID()

	var result []core.Message
	ctx, p.currentTrace = p.obs.StartTrace(ctx, traceName, goal)
	defer func() {
		if p.instr != nil {
			p.instr.Flush()
		}
		// Capture output from the last assistant message
		if len(result) > 0 {
			if tc, ok := result[len(result)-1].Content.(core.TextContent); ok && tc.Text != "" {
				output := tc.Text
				if len(output) > 200 {
					output = output[:200] + "..."
				}
				p.currentTrace.SetAttributes(map[string]any{"output": output})
			}
		}
		p.currentTrace.End()
	}()

	// Wire PlanckInstrument to this trace
	if p.instr != nil {
		p.instr.SetTrace(p.currentTrace)
		p.react.eventHooks.ModelEventHandler = p.instr.EventHandler()
	}

	history := p.mem.GetHistoryMessages()

	// Build system prompt: agent context + skills + active skill + planner instructions
	context := core.LoadAgentContext()
	var promptBuilder strings.Builder
	if context != "" {
		promptBuilder.WriteString(context)
		promptBuilder.WriteString("\n\n")
	}
	// Skill list
	if len(p.skillList) > 0 {
		var names []string
		for _, s := range p.skillList {
			desc := s.Description
			if desc == "" {
				desc = s.Name
			}
			names = append(names, fmt.Sprintf("- %s: %s", s.Name, desc))
		}
		promptBuilder.WriteString("Available skills (use skill_use to activate):\n")
		promptBuilder.WriteString(strings.Join(names, "\n"))
		promptBuilder.WriteString("\n\n")
	}
	// Active skill
	if p.activeSkill.Name != "" {
		promptBuilder.WriteString("Active skill: ")
		promptBuilder.WriteString(p.activeSkill.Name)
		promptBuilder.WriteString(" — ")
		promptBuilder.WriteString(p.activeSkill.Prompt)
		promptBuilder.WriteString("\n\n")
	}
	promptBuilder.WriteString(plannerSystemPrompt)
	sysMsg := core.NewUserMessage("planner", core.TextContent{Text: promptBuilder.String()})
	sysMsg.Role = "system"
	userMsg := core.NewUserMessage("planner", core.TextContent{Text: goal})

	var err error
	result, err = p.react.Execute(ctx, sysMsg, append(history, userMsg))
	if errors.Is(err, core.ErrContextLengthExceeded) {
		historyBefore := len(p.mem.GetHistoryMessages())
		oldSummary := p.mem.Summary()

		compSpan := p.currentTrace.StartSpan("context.compress", map[string]any{
			"messages_before": historyBefore,
			"prior_summary":   oldSummary,
		})
		compressUsage, _ := p.mem.Compress(ctx, p.compressor, memory.CompressConfig{KeepLastN: 4, MinMessagesToCompress: 1})
		history = p.mem.GetHistoryMessages()
		compSpan.SetAttributes(map[string]any{
			"output":          p.mem.Summary(),
			"messages_after":  len(history),
			"messages_before": historyBefore,
		})
		if compressUsage != nil {
			compSpan.SetAttributes(map[string]any{
				"tokens_in":    compressUsage.InputTokens,
				"tokens_out":   compressUsage.OutputTokens,
				"tokens_total": compressUsage.TotalTokens,
			})
			p.fireLLMUsage(core.LLMUsageReport{
				Identity: "compressor",
				Usage:    *compressUsage,
			})
		}
		compSpan.End()
		p.maybeReview(ctx, "context exceeded - aggressive compress", goal)
		result, err = p.react.Execute(ctx, sysMsg, append(history, userMsg))
	}
	if err != nil {
		return result, err
	}

	p.mem.AppendMessage(userMsg)
	p.mem.AppendMessages(result)
	_ = p.mem.Persist()

	go p.extractAndStoreFacts(userMsg, result)

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
		// Record review LLM call
		if p.currentTrace != nil {
			reviewSpan := p.currentTrace.StartSpan("reviewer.check", map[string]any{
				"input": summary,
			})
			if result.Usage != nil {
				reviewSpan.SetAttributes(map[string]any{
					"output":       result.Reason,
					"drift":        result.Drift,
					"tokens_in":    result.Usage.InputTokens,
					"tokens_out":   result.Usage.OutputTokens,
					"tokens_total": result.Usage.TotalTokens,
				})
				p.fireLLMUsage(core.LLMUsageReport{
					Identity: "reviewer",
					Usage:    *result.Usage,
				})
			}
			reviewSpan.End()
		}
		if result.Drift {
			p.replan(plan)
		}
	}
}

func (p *Planner) replan(plan *internals.Plan) {
	if plan.Version <= 1 {
		return
	}
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

	alert := core.NewUserMessage(plan.ID, core.TextContent{
		Text: fmt.Sprintf("[System] Goal drift was detected and the plan has been rolled back to version %d.", targetVersion),
	})
	alert.Role = "system"
	p.mem.AppendMessage(alert)
}

func (p *Planner) extractAndStoreFacts(userMsg core.Message, result []core.Message) {
	if p.ltm == nil {
		return
	}
	ctx := context.Background()

	var sb strings.Builder
	if tc, ok := userMsg.Content.(core.TextContent); ok {
		sb.WriteString(tc.Text)
	}
	sb.WriteString("\n\n---\n\n")
	for _, msg := range result {
		if tc, ok := msg.Content.(core.TextContent); ok && len(tc.Text) > 20 {
			sb.WriteString(tc.Text)
			sb.WriteString(" ")
		}
	}
	conversation := sb.String()
	if len(conversation) < 50 {
		return
	}

	prompt := fmt.Sprintf(
		`Extract key facts and lessons from this conversation. Output ONLY valid JSON array:
[{"section":"...","key":"...","value":"..."}]

Sections: user_preferences, project_facts, lessons_learned
Be concise. Only include facts that are clearly stated.

Conversation:
%s`, conversation[:min(3000, len(conversation))])

	userMsg2 := core.NewUserMessage("ltm-extractor", core.TextContent{Text: prompt})
	resp, err := p.reviewer.provider.Generate(ctx, core.GenerateParams{
		Messages: []core.Message{userMsg2},
		Stream:   false,
	})
	if err != nil {
		return
	}

	text := ""
	if tc, ok := resp.Content.(core.TextContent); ok {
		text = tc.Text
	}
	start := strings.Index(text, "[")
	end := strings.LastIndex(text, "]")
	if start < 0 || end <= start {
		return
	}
	jsonStr := text[start : end+1]

	type fact struct {
		Section string `json:"section"`
		Key     string `json:"key"`
		Value   string `json:"value"`
	}
	var facts []fact
	if err := json.Unmarshal([]byte(jsonStr), &facts); err != nil {
		return
	}
	for _, f := range facts {
		if f.Section != "" && f.Key != "" && f.Value != "" {
			p.ltm.Store(f.Section, f.Key, f.Value) // async — conflicts handled by confidence
		}
	}
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

// ResumeWithHumanResponse delegates to the inner ReactAgent to provide a
// human response that unblocks a pending ask_human tool call.
func (p *Planner) ResumeWithHumanResponse(ctx context.Context, response string) error {
	return p.react.ResumeWithHumanResponse(ctx, response)
}

// HumanLoopWaiting reports whether the agent is currently waiting for human input.
func (p *Planner) HumanLoopWaiting() bool {
	return p.react.HumanLoopWaiting()
}

func toInt64(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int:
		return int64(n)
	case int64:
		return n
	}
	return 0
}
