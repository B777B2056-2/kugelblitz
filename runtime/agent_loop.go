package runtime

import (
	"context"
	"fmt"
	"time"

	"github.com/B777B2056-2/kugelblitz/config"
	"github.com/B777B2056-2/kugelblitz/constants"
	"github.com/B777B2056-2/kugelblitz/core"
	coretypes "github.com/B777B2056-2/kugelblitz/core/types"
	"github.com/B777B2056-2/kugelblitz/events"
	"github.com/B777B2056-2/kugelblitz/llm"
	"github.com/B777B2056-2/kugelblitz/memory"
	"github.com/B777B2056-2/kugelblitz/memory/longterm"
	"github.com/B777B2056-2/kugelblitz/memory/longterm/dream"
	"github.com/B777B2056-2/kugelblitz/memory/longterm/write"
	memorytypes "github.com/B777B2056-2/kugelblitz/memory/types"
	"github.com/B777B2056-2/kugelblitz/observability"
	"github.com/B777B2056-2/kugelblitz/persist"
	"github.com/B777B2056-2/kugelblitz/prompts"
	"github.com/B777B2056-2/kugelblitz/runtime/engine"
	"github.com/B777B2056-2/kugelblitz/skills"
	"github.com/B777B2056-2/kugelblitz/tools/internals"
	"github.com/B777B2056-2/kugelblitz/tools/mcp"
	"github.com/B777B2056-2/kugelblitz/utils"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

type AgentLoop struct {
	// LTM subsystem
	ltm            *longterm.LongTermMemory
	indexMgr       *longterm.IndexManager
	writePipeline  *write.WritePipeline
	dreamScheduler *dream.DreamScheduler

	// session
	sessionMem *memory.SessionMemory

	// execution engine
	planner *engine.Kernel

	// observability (OTel — zero-config: noop if InitTracer not called)
	stepTracer *observability.StepTracer
	rootCtx    context.Context // carries the root trace span for sub-operations

	// hooks & callbacks
	eventHooks core.AgentEventHooks
	bus        *events.Bus // shared per-loop event bus; injected into the planner

	// config
	cfg   config.Config
	input coretypes.AgentInput

	// lifecycle
	done     chan struct{}
	cancelFn context.CancelFunc
}

// AgentLoopOption configures an AgentLoop at creation time.
type AgentLoopOption func(*AgentLoop)

// WithExistingSessionID resolves or creates a SessionMemory for the given session ID.
func WithExistingSessionID(sessionID string) AgentLoopOption {
	return func(a *AgentLoop) {
		a.sessionMem = memory.GetSessionMemoryManager().CreateSessionMemory(sessionID)
	}
}

func NewAgentLoop(cfg config.Config, opts ...AgentLoopOption) (*AgentLoop, error) {
	al := &AgentLoop{
		cfg: cfg,
		bus: events.NewBus(),
	}

	// Apply opts (session reuse, observer)
	for _, opt := range opts {
		opt(al)
	}

	// LTM subsystem
	if err := initLTM(cfg.Model.Provider, al); err != nil {
		return nil, err
	}
	// The dream scheduler listens for agent activity on the shared bus (idle reset).
	if al.dreamScheduler != nil {
		al.dreamScheduler.SubscribeActivity(al.bus)
	}

	// Skills (registers globally)
	initSkills()
	// MCP (idempotent — only connects once per process)
	mcp.Init(context.Background(), cfg.MCP)

	// Session memory: opts may have set it (WithExistingSession/ID), else create.
	if al.sessionMem == nil {
		al.sessionMem = memory.GetSessionMemoryManager().CreateSessionMemory(utils.GenerateSessionID())
	}
	if al.indexMgr != nil && al.indexMgr.IsAvailable() {
		_ = al.indexMgr.RebuildIfStale(context.Background())
	}

	al.planner = engine.NewKernel(al.sessionMem, cfg, al.bus)
	return al, nil
}

func initLTM(provider coretypes.ILMProvider, al *AgentLoop) error {
	mgr := persist.GetManager()
	caller := llm.NewCaller(provider, otel.Tracer("kugelblitz"))

	graphStore := longterm.NewGraphStore(mgr.JSONL(), "memory/longterm/memory_graph.jsonl")
	_ = graphStore.Load(context.Background())

	ltm, err := longterm.NewLongTermMemory(mgr.Markdown(),
		longterm.WithGraph(graphStore),
		longterm.WithSemanticJudge(newSemanticJudge(caller)),
	)
	if err != nil {
		return fmt.Errorf("init long-term memory: %w", err)
	}
	al.ltm = ltm
	al.indexMgr = longterm.NewIndexManager(mgr.Vector(), ltm)
	al.writePipeline = write.NewWritePipeline(caller, ltm, al.indexMgr, 0.15)
	internals.RegisterMemoryTools(ltm, al.indexMgr, al.writePipeline)
	internals.RegisterContextCompressTool()

	if !al.cfg.AutoDream.Enabled {
		return nil
	}
	al.dreamScheduler = dream.NewDreamSchedulerWithIntervals(
		dream.NewDreamer(caller, ltm, graphStore),
		dreamInterval(al.cfg.AutoDream.CheckIntervalSec, 30*time.Minute),
		dreamInterval(al.cfg.AutoDream.CooldownSec, 6*time.Hour),
		dreamInterval(al.cfg.AutoDream.IdleThresholdSec, 5*time.Minute),
	)
	// Not Start()ed here: Start() is deferred to Run(), so the scheduler only
	// runs while the AgentLoop lifecycle is active (see Run).
	return nil
}

// newSemanticJudge builds the LLM-backed semantic-equivalence judge injected
// into long-term memory for Store's conflict resolution.
func newSemanticJudge(caller *llm.Caller) func(oldVal, newVal string) bool {
	return func(oldVal, newVal string) bool {
		text, err := prompts.DefaultFactory.Render(prompts.TypeSemanticJudge, prompts.SemanticJudgeParams{
			OldVal: oldVal, NewVal: newVal,
		})
		if err != nil {
			return false
		}
		res, err := caller.Call(context.Background(), llm.Request{
			Prompt:   text,
			Mode:     llm.ModeBool,
			SpanName: "semantic.judge",
		})
		if err != nil {
			return false
		}
		return res.Bool
	}
}

// dreamInterval converts a config interval in seconds to a time.Duration,
// falling back to def when the config value is unset (<= 0).
func dreamInterval(sec int, def time.Duration) time.Duration {
	if sec <= 0 {
		return def
	}
	return time.Duration(sec) * time.Second
}

func initSkills() {
	activeSkill := &skills.Skill{}
	skillNames, _ := skills.List()
	skillList := make([]*skills.Skill, 0, len(skillNames))
	for _, name := range skillNames {
		if s, err := skills.Load(name); err == nil {
			skillList = append(skillList, s)
		}
	}
	internals.RegisterSkillTool(skillList, activeSkill)
}

// ---- Public API ----

// SessionID returns the ID of the underlying session memory.
func (a *AgentLoop) SessionID() string { return a.sessionMem.SessionID() }

// CompressContext manually compresses the session history into a summary,
// freeing context-window space. It applies the configured compression policy
// and returns the LLM token usage of the summarization call.
func (a *AgentLoop) CompressContext(ctx context.Context) (*coretypes.Usage, error) {
	return a.planner.CompressContext(ctx)
}

// RegisterEventHooks saves hooks for the next Execute call.
func (a *AgentLoop) RegisterEventHooks(hooks core.AgentEventHooks) {
	a.eventHooks = hooks
}

// EventBus returns the shared per-loop event bus, for components (e.g. the dream
// scheduler) that subscribe directly rather than through AgentEventHooks.
func (a *AgentLoop) EventBus() *events.Bus { return a.bus }

// Run starts the agent loop in a background goroutine.
func (a *AgentLoop) Run(ctx context.Context, input coretypes.AgentInput) {
	ctx, a.cancelFn = context.WithCancel(ctx)
	a.done = make(chan struct{})
	go func() {
		defer close(a.done)
		defer a.Cancel()
		events.Emit(a.bus, events.RunStarted{SessionID: a.SessionID()})
		if a.dreamScheduler != nil {
			a.dreamScheduler.Start()
			defer a.dreamScheduler.Stop()
		}
		_, err := a.execute(ctx, input)
		events.Emit(a.bus, events.RunEnded{SessionID: a.SessionID(), Err: err})
		if err != nil && a.eventHooks.OnError != nil {
			a.eventHooks.OnError(constants.AgentMain, err)
		}
	}()
}

// Cancel stops the running execution: interrupts main ReAct loop, cancels all workers,
// and marks the current plan as cancelled.
func (a *AgentLoop) Cancel() {
	if a.cancelFn != nil {
		a.cancelFn()
	}
	a.planner.Cancel(context.Background())
}

// ResumeWithHumanResponse unblocks a pending HITL with the user response.
func (a *AgentLoop) ResumeWithHumanResponse(response string) error {
	return a.planner.ResumeWithHumanResponse(context.Background(), response)
}

// Done returns a channel that closes when execution completes.
func (a *AgentLoop) Done() <-chan struct{} { return a.done }

// resolveProvider selects the LLM provider based on the current input.
// If input has media and the matching multimodal model is configured, use it.
// Otherwise fall back to the main text model.
func (a *AgentLoop) resolveProvider() coretypes.ILMProvider {
	if a.input.IsTextOnly() {
		return a.cfg.Model.Provider
	}
	switch a.input.Media[0].Type {
	case constants.MultiModalTypeImage:
		if a.cfg.Multimodal.ImageModel != nil {
			return a.cfg.Multimodal.ImageModel.Provider
		}
	case constants.MultiModalTypeAudio:
		if a.cfg.Multimodal.AudioModel != nil {
			return a.cfg.Multimodal.AudioModel.Provider
		}
	}
	return a.cfg.Model.Provider
}

// HumanLoopWaiting reports whether the agent is waiting for human input.
func (a *AgentLoop) HumanLoopWaiting() bool {
	return a.planner.HumanLoopWaiting()
}

// Agent returns the underlying IAgent for external consumers (e.g. ACP server).
func (a *AgentLoop) Agent() core.IAgent { return a.planner.Agent() }

// ---- Execution ----

func (a *AgentLoop) execute(ctx context.Context, input coretypes.AgentInput) (messages []coretypes.Message, err error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}
	a.input = input
	events.Emit(a.bus, events.AgentActivity{Identity: constants.AgentMain})

	// observability — zero-config: noop if InitTracer not called
	tracer := otel.Tracer("kugelblitz")
	ctx, rootSpan := tracer.Start(ctx, "planner: "+a.input.Text)
	a.rootCtx = ctx
	defer func() {
		if a.stepTracer != nil {
			a.stepTracer.Flush()
		}
		if err != nil {
			rootSpan.SetStatus(codes.Error, err.Error())
			rootSpan.RecordError(err)
		}
		rootSpan.End()
	}()
	a.stepTracer = observability.NewStepTracer()
	ctx, _ = a.stepTracer.SetTrace(ctx, tracer, a.input.Text)
	a.planner.SetStepTracer(a.stepTracer)

	// System subscriptions (stepTracer / compress / extract) are registered first
	// so they fire before user hooks, preserving the prior sys-before-user order.
	unsubSystem := a.subscribeSystem(a.bus)
	defer unsubSystem()

	// User hooks subscribe onto the shared bus (overwrite semantics).
	a.planner.RegisterEventHooks(a.eventHooks)

	// wire memory_extract input
	internals.BindMemoryExtractInput(func() memorytypes.ExtractionInput {
		return memorytypes.ExtractionInput{
			Conversation:   a.sessionMem.GetHistoryMessages(),
			SessionSummary: a.sessionMem.Summary(),
			Goal:           a.input.Text,
		}
	})

	// wire context_compress to the planner's manual compression entry point
	internals.BindContextCompress(a.planner.CompressContext)

	// Resolve provider: if input has media, switch to configured multimodal model
	a.planner.SetProvider(a.resolveProvider())

	result, err := a.planner.Run(ctx, a.input)
	a.sessionMem.AppendMessages(result)
	_ = a.sessionMem.Persist()
	return result, err
}

// backgroundCtx returns the root execution context when available, falling back
// to context.Background(). It carries cancellation and the root trace span so
// that sub-operations (memory compress/extract) are cancelled and traced (B22).
func (a *AgentLoop) backgroundCtx() context.Context {
	if a.rootCtx != nil {
		return a.rootCtx
	}
	return context.Background()
}

// subscribeSystem registers the AgentLoop's internal consumers on the shared bus
// and returns a combined unsubscribe. Registered before user hooks so system
// consumers fire first. These subscriptions are per-run and torn down in execute's
// defer, so a loop reused across multiple Run calls does not accumulate them.
func (a *AgentLoop) subscribeSystem(bus *events.Bus) func() {
	var unsubs []func()

	// Tool-result compression: applied to every executed tool (main and workers).
	unsubs = append(unsubs, events.On(bus, func(ev events.ToolCallEnd) {
		a.sessionMem.CompressToolResult(a.backgroundCtx(),
			a.planner.Compressor(), a.cfg.ContextCompress.MaxToolResultChars, &ev.Result)
	}))

	// Pre-compress memory extraction: run the LTM write pipeline before session
	// history is summarized.
	unsubs = append(unsubs, events.On(bus, func(ev events.BeforeCompress) {
		a.extractMemories()
	}))

	// Instrumentation: forward model events to the StepTracer.
	if a.stepTracer != nil {
		instrH := a.stepTracer.EventHandler()
		unsubs = append(unsubs,
			events.On(bus, func(ev events.ReplyChunk) { instrH.OnReplyChunk(ev.Chunk) }),
			events.On(bus, func(ev events.ThinkingChunk) { instrH.OnThinkingChunk(ev.Chunk) }),
			events.On(bus, func(ev events.BlockReply) { instrH.OnBlockReply(ev.Text) }),
			events.On(bus, func(ev events.BlockThinking) { instrH.OnBlockThinking(ev.Reasoning) }),
			events.On(bus, func(ev events.FunctionCall) { instrH.OnFunctionCall(ev.Detail) }),
			events.On(bus, func(ev events.ModelFinished) { instrH.OnFinished(ev.Reason) }),
			events.On(bus, func(ev events.UsageUpdated) { instrH.OnUsageUpdated(ev.Usage) }),
			events.On(bus, func(ev events.AgentError) { instrH.OnError(ev.Err) }),
		)
	}

	return func() {
		for _, u := range unsubs {
			u()
		}
	}
}

// extractMemories runs the LTM write pipeline before session memory is
// compressed, so facts in soon-to-be-summarized messages are preserved.
func (a *AgentLoop) extractMemories() {
	input := memorytypes.ExtractionInput{
		Conversation:   a.sessionMem.GetHistoryMessages(),
		SessionSummary: a.sessionMem.Summary(),
		Goal:           a.input.Text,
	}
	result, _ := a.writePipeline.ExtractFromSession(a.backgroundCtx(), input)
	if result != nil {
		tracer := otel.Tracer("kugelblitz")
		_, span := tracer.Start(a.backgroundCtx(), "memory.extract_before_compress")
		span.SetAttributes(
			attribute.Int("facts_stored", result.ItemsStored),
			attribute.Int("needs_human", result.NeedsHuman),
		)
		span.End()
	}
}
