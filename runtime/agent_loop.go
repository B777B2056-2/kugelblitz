package runtime

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/B777B2056-2/kugelblitz/config"
	"github.com/B777B2056-2/kugelblitz/constants"
	"github.com/B777B2056-2/kugelblitz/core"
	coretypes "github.com/B777B2056-2/kugelblitz/core/types"
	"github.com/B777B2056-2/kugelblitz/memory"
	"github.com/B777B2056-2/kugelblitz/memory/longterm"
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
	writePipeline  *longterm.WritePipeline
	dreamScheduler *longterm.DreamScheduler

	// session
	sessionMem *memory.SessionMemory

	// execution engine
	planner *engine.Kernel

	// observability (OTel — zero-config: noop if InitTracer not called)
	stepTracer *observability.StepTracer
	rootCtx    context.Context // carries the root trace span for sub-operations

	// hooks & callbacks
	eventHooks core.AgentEventHooks

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
	}

	// Apply opts (session reuse, observer)
	for _, opt := range opts {
		opt(al)
	}

	// LTM subsystem
	if err := initLTM(cfg.Model.Provider, al); err != nil {
		return nil, err
	}

	// Skills (registers globally)
	initSkills()
	// MCP (idempotent — only connects once per process)
	mcp.Init(context.Background(), cfg.MCP)

	initSemanticJudge(cfg.Model.Provider)

	// Session memory: opts may have set it (WithExistingSession/ID), else create.
	if al.sessionMem == nil {
		al.sessionMem = memory.GetSessionMemoryManager().CreateSessionMemory(utils.GenerateSessionID())
	}
	if al.indexMgr != nil && al.indexMgr.IsAvailable() {
		_ = al.indexMgr.RebuildIfStale(context.Background())
	}

	al.planner = engine.NewKernel(al.sessionMem, cfg)
	return al, nil
}

func initLTM(provider coretypes.ILMProvider, al *AgentLoop) error {
	mgr := persist.GetManager()
	ltm, err := longterm.NewLongTermMemory(mgr.Markdown())
	if err != nil {
		return fmt.Errorf("init long-term memory: %w", err)
	}
	al.ltm = ltm
	al.indexMgr = longterm.NewIndexManager(mgr.Vector(), ltm)
	al.writePipeline = longterm.NewWritePipeline(provider, ltm, al.indexMgr, 0.15)
	internals.RegisterMemoryTools(ltm, al.indexMgr, al.writePipeline)
	internals.RegisterContextCompressTool()

	graphStore := longterm.NewGraphStore(mgr.JSONL(), "memory/longterm/memory_graph.jsonl")
	_ = graphStore.Load(context.Background())
	ltm.SetGraph(graphStore)

	if !al.cfg.AutoDream.Enabled {
		return nil
	}
	dreamer := &longterm.Dreamer{}
	dreamer.SetProvider(provider)
	dreamer.SetLTM(ltm)
	dreamer.SetGraph(graphStore)
	dreamer.SetIndexManager(al.indexMgr)
	al.dreamScheduler = longterm.NewDreamSchedulerWithIntervals(dreamer,
		dreamInterval(al.cfg.AutoDream.CheckIntervalSec, 30*time.Minute),
		dreamInterval(al.cfg.AutoDream.CooldownSec, 6*time.Hour),
		dreamInterval(al.cfg.AutoDream.IdleThresholdSec, 5*time.Minute),
	)
	// Not Start()ed here: Start() is deferred to Run(), so the scheduler only
	// runs while the AgentLoop lifecycle is active (see Run).
	return nil
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

func initSemanticJudge(provider coretypes.ILMProvider) {
	longterm.SetSemanticJudge(func(oldVal, newVal string) bool {
		text, err := prompts.DefaultFactory.Render(prompts.TypeSemanticJudge, prompts.SemanticJudgeParams{
			OldVal: oldVal, NewVal: newVal,
		})
		if err != nil {
			return false
		}
		msg := coretypes.NewUserMessage(coretypes.TextContent{Text: text})
		resp, err := provider.Generate(context.Background(), coretypes.GenerateParams{
			Messages: []coretypes.Message{msg}, Stream: false,
		})
		if err != nil {
			return false
		}
		if tc, ok := resp.Content.(coretypes.TextContent); ok {
			return strings.Contains(strings.ToUpper(tc.Text), "YES")
		}
		return false
	})
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

// Run starts the agent loop in a background goroutine.
func (a *AgentLoop) Run(ctx context.Context, input coretypes.AgentInput) {
	ctx, a.cancelFn = context.WithCancel(ctx)
	a.done = make(chan struct{})
	go func() {
		defer close(a.done)
		defer a.Cancel()
		if a.dreamScheduler != nil {
			a.dreamScheduler.Start()
			defer a.dreamScheduler.Stop()
		}
		_, err := a.execute(ctx, input)
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
	if a.dreamScheduler != nil {
		a.dreamScheduler.NotifyActivity()
	}

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

	a.planner.RegisterEventHooks(a.rewriteEventHooks(a.eventHooks))

	// wire memory_extract input
	internals.BindMemoryExtractInput(func() longterm.ExtractionInput {
		return longterm.ExtractionInput{
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

// rewriteEventHooks merges AgentLoop internal callbacks with user hooks.
// AgentLoop callbacks fire first, then user callbacks.
func (a *AgentLoop) rewriteEventHooks(userHooks core.AgentEventHooks) core.AgentEventHooks {
	// AgentLoop system wrappers: compress + extract + then user.
	sysHooks := core.AgentEventHooks{
		OnToolCallEnd: func(id constants.AgentIdentity, result coretypes.ToolCallResult) {
			a.sessionMem.CompressToolResult(a.backgroundCtx(),
				a.planner.Compressor(), a.cfg.ContextCompress.MaxToolResultChars, &result)
		},
		OnBeforeCompress: func(id constants.AgentIdentity) {
			a.extractMemories()
		},
	}

	// Instrumentation: wrap user callbacks so the observer receives events first.
	if a.stepTracer != nil {
		instrH := a.stepTracer.EventHandler()
		sysHooks.OnReplyChunk = func(id constants.AgentIdentity, chunk string) {
			instrH.OnReplyChunk(chunk)
		}
		sysHooks.OnThinkingChunk = func(id constants.AgentIdentity, chunk string) {
			instrH.OnThinkingChunk(chunk)
		}
		sysHooks.OnBlockReply = func(id constants.AgentIdentity, text string) {
			instrH.OnBlockReply(text)
		}
		sysHooks.OnBlockThinking = func(id constants.AgentIdentity, reasoning string) {
			instrH.OnBlockThinking(reasoning)
		}
		sysHooks.OnFunctionCall = func(id constants.AgentIdentity, detail coretypes.ToolCallDetail) {
			instrH.OnFunctionCall(detail)
		}
		sysHooks.OnModelFinished = func(id constants.AgentIdentity, reason string) {
			instrH.OnFinished(reason)
		}
		sysHooks.OnUsageUpdated = func(id constants.AgentIdentity, usage coretypes.Usage) {
			instrH.OnUsageUpdated(usage)
		}
		sysHooks.OnError = func(id constants.AgentIdentity, err error) {
			instrH.OnError(err)
		}
	}

	return core.Chain(userHooks, sysHooks)
}

// extractMemories runs the LTM write pipeline before session memory is
// compressed, so facts in soon-to-be-summarized messages are preserved.
func (a *AgentLoop) extractMemories() {
	input := longterm.ExtractionInput{
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
