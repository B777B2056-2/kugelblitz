package fsm

import (
	"errors"
	"fmt"

	"github.com/B777B2056-2/kugelblitz/constants"
	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/B777B2056-2/kugelblitz/memory/working"
	"github.com/B777B2056-2/kugelblitz/prompts"
)

// Action represents an executable operation within a state.
type Action interface {
	Execute(ctx *Context) (*ActionResult, error)
}

// ActionResult holds the output of an Action execution.
type ActionResult struct {
	Messages []core.Message
	Data     map[string]any
}

// ReactAction executes a ReAct agent cycle: build prompt → get history →
// call LLM → handle context exceeded → append messages.
type ReactAction struct {
	State      constants.PlanState
	UserPrompt string
	Plan       *working.Plan
}

func (a *ReactAction) Execute(ctx *Context) (*ActionResult, error) {
	deps := ctx.Deps

	sysMsg := core.NewSystemMessage(core.TextContent{
		Text: buildPrompt(a.State, a.Plan),
	})
	sessionCtx := core.WithSessionID(ctx.Ctx, deps.Session.SessionID())

	userMsg := core.NewUserMessage(core.TextContent{Text: a.UserPrompt})
	deps.Session.AppendMessage(userMsg)
	history := deps.Session.GetHistoryMessages()

	tools := ToolsForState(a.State)
	stepResult, err := deps.React.ExecuteWithTools(sessionCtx, sysMsg, history, tools)

	if errors.Is(err, core.ErrContextLengthExceeded) {
		stepResult, err = handleContextExceeded(ctx, sysMsg, tools)
	}

	return &ActionResult{
		Messages: stepResult,
	}, err
}

// handleContextExceeded compresses session memory and retries the ReAct call.
func handleContextExceeded(ctx *Context, sysMsg core.Message, tools []string) ([]core.Message, error) {
	deps := ctx.Deps
	sessionCtx := core.WithSessionID(ctx.Ctx, deps.Session.SessionID())

	for i := 0; i < deps.Config.CompressMaxAttempts; i++ {
		deps.Session.Compress(ctx.Ctx, deps.Compressor, 4, 1)

		history := deps.Session.GetHistoryMessages()
		result, err := deps.React.ExecuteWithTools(sessionCtx, sysMsg, history, tools)
		if err == nil || !errors.Is(err, core.ErrContextLengthExceeded) {
			return result, err
		}
	}
	return nil, core.ErrContextLengthExceeded
}

// DAGAction executes a DAG batch and handles drift review in the failure callback.
type DAGAction struct {
	Plan *working.Plan
}

func (a *DAGAction) Execute(ctx *Context) (*ActionResult, error) {
	deps := ctx.Deps

	r := deps.DAG.ExecuteBatch(a.Plan, ctx.Ctx, func(taskID, goal, reason string) {
		ctx.TaskFails++
		if shouldReview(ctx) {
			plan, _ := working.GetPlan(ctx.PlanID)
			if plan != nil {
				summary := fmtPlanSummary(plan)
				reviewResult := deps.Reviewer.Review(ctx.Ctx, ctx.Goal, summary, reason)
				if reviewResult.Drift && deps.HandleDrift != nil {
					deps.HandleDrift(ctx, reason)
				}
			}
		}
	})

	result := &ActionResult{
		Data: map[string]any{
			"hasFailed": r.HasFailed,
			"allDone":   r.AllDone,
		},
	}
	return result, nil
}

// NoOpAction performs no operation and returns an empty result.
type NoOpAction struct{}

func (a *NoOpAction) Execute(ctx *Context) (*ActionResult, error) {
	return &ActionResult{}, nil
}

// buildPrompt builds the system prompt for a given state and plan.
func buildPrompt(status constants.PlanState, plan *working.Plan) string {
	var sb string

	if agentCtx := core.LoadAgentContext(); agentCtx != "" {
		sb = agentCtx + "\n\n"
	}

	if plan != nil {
		if status == constants.PlanStateConfirmed {
			sb += prompts.DefaultFactory.MustRender(
				prompts.TypePlanConfirm, prompts.BuildPlanConfirmParams(plan))
		} else {
			sb += prompts.DefaultFactory.MustRender(
				prompts.TypePlanStatus, prompts.BuildPlanStatusParams(plan))
		}
		sb += "\n\n"
	}

	sb += prompts.PlannerPrompt(status)
	return sb
}

// shouldReview checks whether a drift review should be triggered.
func shouldReview(ctx *Context) bool {
	if ctx.Deps.Reviewer == nil {
		return false
	}
	if ctx.Deps.Config.ReviewInterval > 0 && ctx.StepCount%ctx.Deps.Config.ReviewInterval == 0 {
		return true
	}
	if ctx.Deps.Config.MaxFailuresBeforeReview > 0 && ctx.TaskFails >= ctx.Deps.Config.MaxFailuresBeforeReview {
		return true
	}
	return false
}

// fmtPlanSummary creates a summary string for a plan (used in drift review).
func fmtPlanSummary(plan *working.Plan) string {
	return fmt.Sprintf("Plan %q (v%d, status=%s), %d tasks",
		plan.Name, plan.Version, plan.State, len(plan.SubTasks))
}
