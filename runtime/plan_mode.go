package runtime

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/B777B2056-2/kugelblitz/memory"
	"github.com/B777B2056-2/kugelblitz/memory/longterm"
	"github.com/B777B2056-2/kugelblitz/memory/working"
	"github.com/B777B2056-2/kugelblitz/observability"
	"github.com/B777B2056-2/kugelblitz/persist"
	"github.com/B777B2056-2/kugelblitz/runtime/prompts"
	"github.com/B777B2056-2/kugelblitz/skills"
	"github.com/B777B2056-2/kugelblitz/tools/internals"
	"github.com/B777B2056-2/kugelblitz/utils"
)

type ReviewConfig struct {
	FailuresBeforeReview int
	ReActStepInterval    int
	MaxToolResultChars   int // 0 = no limit; >0 = compress tool results exceeding this many UTF-8 chars
}

func DefaultReviewConfig() ReviewConfig {
	return ReviewConfig{FailuresBeforeReview: 5, ReActStepInterval: 12}
}

// DefaultMaxToolResultChars is the default threshold for tool result compression.
const DefaultMaxToolResultChars = 4000

type Planner struct {
	react              *ReactAgent
	mem                *memory.SessionMemory
	ltm                *longterm.LongTermMemory
	indexMgr           *longterm.IndexManager
	writePipeline      *longterm.WritePipeline
	dreamScheduler     *longterm.DreamScheduler
	compressor         *memory.Compressor
	reviewer           *Reviewer
	reviewCfg          ReviewConfig
	goal               string
	consecutiveFails   int
	enableThinking     *bool
	reasoningEffort    string
	skillTool          *internals.SkillUse
	activeSkill        *skills.Skill
	skillList          []*skills.Skill
	obs                core.Observer
	currentTrace       core.TraceSpan
	instr              *observability.PlannerInstrument
	eventHooks         core.AgentEventHooks // Planner-level callbacks (OnPlanRollback, etc.)
	onLLMUsage         func(core.LLMUsageReport)
	maxToolResultChars int
	sessionOverride *memory.SessionMemory // if set, reuse instead of creating new
	customTools     []string
	stateMachine    *PlannerStateMachine
	dagExecutor     *DAGTaskExecutor
}

// PlannerOption configures a Planner at creation time.
type PlannerOption func(*Planner)

// WithCustomTools adds additional tool names to Planner, StateMachine, and Workers.
func WithCustomTools(names ...string) PlannerOption {
	return func(p *Planner) {
		p.react.WithTools(names...)
		p.customTools = append(p.customTools, names...)
	}
}

// collectCustomTools extracts customTools from opts without creating a Planner.
func collectCustomTools(opts []PlannerOption) []string {
	var tools []string
	dummy := &Planner{}
	for _, opt := range opts {
		opt(dummy)
		tools = dummy.customTools
	}
	return tools
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

// WithExistingSession makes Planner reuse an existing session instead of
// creating a new one. Used by IntentRouter to share session across phases.
func WithExistingSession(mem *memory.SessionMemory) PlannerOption {
	return func(p *Planner) { p.sessionOverride = mem }
}

// WithExistingSessionID is like WithExistingSession but takes a session ID
// string and resolves or creates the SessionMemory internally. Preferred
// when the caller only has a session ID.
func WithExistingSessionID(sessionID string) PlannerOption {
	return func(p *Planner) {
		p.sessionOverride = memory.GetSessionMemoryManager().CreateSessionMemory(sessionID)
	}
}

// ltmSetup bundles the long-term memory subsystem components.
type ltmSetup struct {
	ltm            *longterm.LongTermMemory
	indexMgr       *longterm.IndexManager
	writePipeline  *longterm.WritePipeline
	dreamScheduler *longterm.DreamScheduler
}

func NewPlanner(provider core.ILMProvider, streamMode bool, opts ...PlannerOption) *Planner {
	react := buildReactAgent(provider, streamMode)
	ltmSetup := initLTM(provider)
	skillTool, activeSkill, skillList := initSkills()
	initSemanticJudge(provider)

	planner := &Planner{
		react:              react,
		mem:                nil,
		ltm:                ltmSetup.ltm,
		indexMgr:           ltmSetup.indexMgr,
		writePipeline:      ltmSetup.writePipeline,
		dreamScheduler:     ltmSetup.dreamScheduler,
		compressor:         memory.NewCompressor(provider),
		reviewer:           NewReviewer(provider),
		reviewCfg:          DefaultReviewConfig(),
		skillTool:          skillTool,
		activeSkill:        activeSkill,
		skillList:          skillList,
		obs:                core.NoopObserver{},
		maxToolResultChars: DefaultMaxToolResultChars,
	}

	// Apply opts to populate customTools, then create DAG+SM
	for _, opt := range opts {
		opt(planner)
	}
	planner.dagExecutor = buildDAGExecutor(provider, streamMode, planner.customTools...)
	planner.stateMachine = NewPlannerStateMachine(planner.dagExecutor, planner.customTools...)
	planner.stateMachine.SetReviewer(planner.reviewer, 5, 12)

	wireCallbacks(planner, planner.stateMachine)
	finishInit(planner, provider, ltmSetup.indexMgr)
	return planner
}

func buildDAGExecutor(provider core.ILMProvider, streamMode bool, customTools ...string) *DAGTaskExecutor {
	return NewDAGTaskExecutor(provider, streamMode, customTools...)
}

func buildReactAgent(provider core.ILMProvider, streamMode bool) *ReactAgent {
	react := NewReactAgent(provider, streamMode)
	react.WithTools(
		"plan_create", "plan_query", "confirm_plan", "plan_rollback",
		"task_insert", "task_delete", "task_query",
		"memory_store", "memory_search", "memory_get_section",
		"memory_remove", "memory_list_sections", "memory_stats",
		"memory_extract", "memory_resolve_conflict",
		"skill_use", "ask_human", "set_work_mode",
	)
	react.EnableHumanInTheLoop()
	return react
}

func initLTM(provider core.ILMProvider) ltmSetup {
	mgr := persist.GetManager()
	ltm, err := longterm.NewLongTermMemory(mgr.Markdown())
	if err != nil {
		core.Warn("plan_mode: failed to init long-term memory", "error", err)
		return ltmSetup{}
	}
	indexMgr := longterm.NewIndexManager(mgr.Vector(), ltm)
	writePipeline := longterm.NewWritePipeline(provider, ltm, indexMgr, 0.15)
	internals.RegisterMemoryTools(ltm, indexMgr, writePipeline)
	graphStore := longterm.NewGraphStore(mgr.JSONL(), "memory/longterm/memory_graph.jsonl")
	graphStore.Load(context.Background())
	ltm.SetGraph(graphStore)

	dreamer := &longterm.Dreamer{}
	dreamer.SetProvider(provider)
	dreamer.SetLTM(ltm)
	dreamer.SetGraph(graphStore)
	dreamer.SetIndexManager(indexMgr)
	dreamScheduler := longterm.NewDreamScheduler(dreamer)
	dreamScheduler.Start()

	return ltmSetup{ltm, indexMgr, writePipeline, dreamScheduler}
}

// lint:ignore U1000 — retained for type clarity
func initSkills() (*internals.SkillUse, *skills.Skill, []*skills.Skill) {
	activeSkill := &skills.Skill{}
	skillNames, _ := skills.List()
	skillList := make([]*skills.Skill, 0, len(skillNames))
	for _, name := range skillNames {
		if s, err := skills.Load(name); err == nil {
			skillList = append(skillList, s)
		}
	}
	skillTool := internals.RegisterSkillTool(skillList, activeSkill)
	return skillTool, activeSkill, skillList
}

func initSemanticJudge(provider core.ILMProvider) {
	longterm.SetSemanticJudge(func(oldVal, newVal string) bool {
		msg := core.NewUserMessage("judge", core.TextContent{
			Text: prompts.DefaultFactory.MustRender(prompts.TypeSemanticJudge, prompts.SemanticJudgeParams{
				OldVal: oldVal, NewVal: newVal,
			}),
		})
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
}

func wireCallbacks(planner *Planner, sm *PlannerStateMachine) {
	sm.OnDrift(func(plan *working.Plan, reason string) {
		if planner.eventHooks.OnPlanRollback != nil {
			planner.eventHooks.OnPlanRollback(plan.ID, plan.Version, plan.Name)
		}
	})

	internals.BuildExtractContext = func() *longterm.ExtractionContext {
		return planner.buildExtractionContext(
			core.NewUserMessage("planner", core.TextContent{Text: planner.goal}),
			planner.mem.GetHistoryMessages(),
		)
	}

	planner.react.SetOnToolResult(func(results []core.ToolCallResult, step int) bool {
		planner.compressToolResults(context.Background(), results)
		var stepUsage core.Usage
		if planner.instr != nil {
			stepUsage = planner.instr.LastUsage()
			sp := planner.instr.StepSpan(step, results)
			sp.End()
		}
		planner.fireLLMUsage(core.LLMUsageReport{
			Identity: fmt.Sprintf("planner.step-%d", step),
			Usage:    stepUsage,
		})
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
		return true
	})

	sm.OnContextExceeded(func() {
		planner.handleContextExceeded(context.Background(),
			core.NewUserMessage("planner", core.TextContent{Text: planner.goal}),
			planner.mem.GetHistoryMessages())
	})
}

func finishInit(planner *Planner, provider core.ILMProvider, indexMgr *longterm.IndexManager) {
	if planner.sessionOverride != nil {
		planner.mem = planner.sessionOverride
	} else {
		planner.mem = memory.GetSessionMemoryManager().CreateSessionMemory(utils.GenerateSessionID())
	}
	if indexMgr != nil && indexMgr.IsAvailable() {
		indexMgr.RebuildIfStale(context.Background())
	}
	if provider != nil && planner.sessionOverride == nil {
		planner.ResumeIncomplete(context.Background())
	}
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
	msg := core.NewUserMessage("tool-compressor", core.TextContent{
		Text: prompts.DefaultFactory.MustRender(prompts.TypeCompressTool, prompts.CompressToolParams{
			ToolName: toolName, FieldKey: fieldKey, OrigLen: utf8.RuneCountInString(raw), Raw: raw,
		}),
	})
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
	p.eventHooks = hooks
	p.react.RegisterEventHooks(hooks)
	if p.dagExecutor != nil {
		p.dagExecutor.hooks = hooks
	}
}

// SessionID returns the ID of the underlying session memory.
func (p *Planner) SessionID() string { return p.mem.SessionID() }

func (p *Planner) Execute(ctx context.Context, goal string) ([]core.Message, error) {
	p.goal = goal
	if p.dreamScheduler != nil {
		p.dreamScheduler.NotifyActivity()
	}
	traceName := p.mem.SessionID()
	ctx, p.currentTrace = p.obs.StartTrace(ctx, traceName, goal)
	defer func() {
		if p.instr != nil { p.instr.Flush() }
		p.currentTrace.End()
	}()
	if p.instr != nil {
		p.instr.SetTrace(p.currentTrace)
		p.react.eventHooks.ModelEventHandler = p.instr.EventHandler()
	}
	userMsg := core.NewUserMessage("planner", core.TextContent{Text: goal})
	result, err := p.stateMachine.Run(ctx, p.react, p.mem, goal, p.ltm)
	if err != nil {
		return result, err
	}
	p.mem.AppendMessage(userMsg)
	p.mem.AppendMessages(result)
	_ = p.mem.Persist()
	return result, nil
}

// handleContextExceeded compresses session memory and updates history.
func (p *Planner) handleContextExceeded(ctx context.Context, userMsg core.Message, history []core.Message) {
	p.extractBeforeCompress(ctx, userMsg, history)
	compressUsage, _ := p.mem.Compress(ctx, p.compressor,
		memory.CompressConfig{KeepLastN: 4, MinMessagesToCompress: 1})
	if compressUsage != nil {
		p.fireLLMUsage(core.LLMUsageReport{Identity: "compressor", Usage: *compressUsage})
	}
	p.maybeReview(ctx, "context exceeded - aggressive compress", p.goal)
}

func (p *Planner) maybeReview(ctx context.Context, trigger, goalOverride string) {
	if goalOverride == "" {
		goalOverride = p.goal
	}
	planIDs, _ := persist.ListPlans()
	for _, id := range planIDs {
		plan, err := working.LoadPlan(id)
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

func (p *Planner) replan(plan *working.Plan) {
	if plan.Version <= 1 {
		return
	}
	targetVersion := plan.Version - 1
	var cp working.Checkpoint
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
		Text: fmt.Sprintf("⚠️ 自动审查检测到执行可能偏离目标，计划已回滚至版本 %d。请在下一轮回复中向用户解释回滚原因，列出回滚后计划的任务，并请用户确认是否继续。", targetVersion),
	})
	alert.Role = "system"
	p.mem.AppendMessage(alert)

	// Notify frontend via Planner-level callback so the UI can show a visible
	// rollback banner (instead of silently reverting the plan).
	if p.eventHooks.OnPlanRollback != nil {
		p.eventHooks.OnPlanRollback(plan.ID, targetVersion, plan.Name)
	}
}

// injectPendingConflicts adds unresolved memory conflicts to the system prompt.
func injectPendingConflicts(sb *strings.Builder, ltm *longterm.LongTermMemory) {
	if ltm == nil {
		return
	}
	pending := ltm.PendingConflicts()
	if len(pending) == 0 {
		return
	}

	sb.WriteString("## Pending Memory Conflicts\n")
	sb.WriteString("The following long-term memory entries have conflicting values. ")
	sb.WriteString("Use memory_resolve_conflict to resolve them after asking the human via ask_human.\n\n")
	for _, pc := range pending {
		sb.WriteString(fmt.Sprintf(
			"- [%s] %s: OLD=%q (c%.2f) vs NEW=%q (c%.2f) — reason: %s\n",
			pc.Section, pc.Key, pc.OldValue, pc.OldConfidence,
			pc.NewValue, pc.NewConfidence, pc.Reason,
		))
	}
	sb.WriteString("\n")
}

// injectPlanState tells the model about the current session's latest plan
// so it doesn't re-confirm plans that have already been approved.
func (p *Planner) injectPlanState(sb *strings.Builder) {
	plan := working.LatestIncompletePlan(p.mem.SessionID())
	if plan == nil {
		return
	}

	done := 0
	for _, t := range plan.SubTasks {
		if t.Status == "done" {
			done++
		}
	}
	sb.WriteString("## Current Plan\n")
	sb.WriteString(fmt.Sprintf("- %q (status: %s, %d/%d tasks done)\n\n",
		plan.Name, plan.Status, done, len(plan.SubTasks)))
	sb.WriteString("Continue from the current state above. ")
	sb.WriteString("If confirmed, spawn workers for pending tasks. ")
	sb.WriteString("If executing, check results via task_query.\n\n")
}

// extractBeforeCompress runs the write pipeline on old messages before they are
// compressed away. This is the last chance to capture detailed episodic memories.
func (p *Planner) extractBeforeCompress(ctx context.Context, userMsg core.Message, history []core.Message) {
	if p.writePipeline == nil || p.ltm == nil {
		return
	}
	ec := p.buildExtractionContext(userMsg, history)
	if result, err := p.writePipeline.Run(ctx, ec); err == nil && p.currentTrace != nil {
		span := p.currentTrace.StartSpan("memory.extract_before_compress", map[string]any{
			"facts_stored": result.ItemsStored,
			"needs_human":  result.NeedsHuman,
		})
		span.End()
	}
}

// buildExtractionContext constructs an ExtractionContext from the current planner state.
func (p *Planner) buildExtractionContext(userMsg core.Message, history []core.Message) *longterm.ExtractionContext {
	if p.ltm == nil {
		return &longterm.ExtractionContext{Conversation: history}
	}
	ec := &longterm.ExtractionContext{
		Conversation:   history,
		SessionSummary: p.mem.Summary(),
		ExistingItems:  p.ltm.All(),
	}

	if tc, ok := userMsg.Content.(core.TextContent); ok {
		ec.UserMessage = tc.Text
	}

	// Load active plan goals from checkpoints
	planIDs, _ := persist.ListPlans()
	for _, id := range planIDs {
		plan, err := working.LoadPlan(id)
		if err == nil && plan.IsIncomplete() {
			ec.CheckpointGoals = append(ec.CheckpointGoals, plan.Name)
		}
	}

	return ec
}

func (p *Planner) ResumeIncomplete(ctx context.Context) {
	planIDs, _ := persist.ListPlans()
	for _, id := range planIDs {
		plan, err := working.LoadPlan(id)
		if err != nil || !plan.IsIncomplete() {
			continue
		}
		p.resume(ctx, id)
	}
}

func (p *Planner) resume(ctx context.Context, planID string) {
	plan, err := working.LoadPlan(planID)
	if err != nil {
		return
	}
	prompt := fmt.Sprintf(
		`Resume the plan %q (id: %s). It is currently in status %q.
Use plan_query to see the full state with all subtasks.
Continue from where it left off — spawn workers for remaining pending tasks.
When all tasks are done, call confirm_plan "done" and summarize.`,
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
	if p.react.HumanLoopWaiting() {
		return p.react.ResumeWithHumanResponse(ctx, response)
	}
	if p.dagExecutor != nil {
		for id, gate := range p.dagExecutor.hitlAgents {
			if gate.HumanLoopWaiting() {
				defer func() {
					delete(p.dagExecutor.hitlAgents, id)
					p.dagExecutor.Resume()
				}()
				return gate.ResumeWithHumanResponse(ctx, response)
			}
		}
	}
	return fmt.Errorf("no agent waiting for human input")
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
