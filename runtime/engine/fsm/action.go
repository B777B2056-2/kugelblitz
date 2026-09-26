package fsm

import (
	"errors"
	"fmt"
	"strings"

	"github.com/B777B2056-2/kugelblitz/constants"
	"github.com/B777B2056-2/kugelblitz/core"
	coretypes "github.com/B777B2056-2/kugelblitz/core/types"
	"github.com/B777B2056-2/kugelblitz/memory/working"
	"github.com/B777B2056-2/kugelblitz/prompts"
)

// Action represents an executable operation within a state.
type Action interface {
	Execute(ctx *Context) (*ActionResult, error)
}

// ActionResult holds the output of an Action execution.
type ActionResult struct {
	Messages []coretypes.Message
	Data     map[string]any
}

// ReactAction executes a ReAct agent cycle: build prompt → get history →
// call LLM → handle context exceeded → append messages.
type ReactAction struct {
	State constants.PlanState
	Input coretypes.AgentInput // user input (text + optional media); use BuildUserMessage()
	Plan  *working.Plan
}

func (a *ReactAction) Execute(ctx *Context) (*ActionResult, error) {
	deps := ctx.Deps

	prompt, err := buildPrompt(ctx, a.State, a.Plan)
	if err != nil {
		return nil, err
	}
	sysMsg := coretypes.NewSystemMessage(coretypes.TextContent{
		Text: prompt,
	})
	sessionCtx := core.WithSessionID(ctx.Ctx, deps.Session.SessionID())

	history := deps.Session.GetHistoryMessages()

	tools := ToolsForState(a.State, deps.CustomToolNames())
	stepResult, err := deps.React.ExecuteWithTools(sessionCtx, sysMsg, history, tools)

	if errors.Is(err, coretypes.ErrContextLengthExceeded) {
		stepResult, err = handleContextExceeded(ctx, sysMsg, tools)
	}

	return &ActionResult{
		Messages: stepResult,
	}, err
}

// handleContextExceeded compresses session memory and retries the ReAct call.
func handleContextExceeded(ctx *Context, sysMsg coretypes.Message, tools []string) ([]coretypes.Message, error) {
	deps := ctx.Deps
	sessionCtx := core.WithSessionID(ctx.Ctx, deps.Session.SessionID())

	for i := 0; i < deps.Config.CompressMaxAttempts; i++ {
		_, _ = deps.Session.Compress(ctx.Ctx, deps.Summarizer, 4, 1)

		history := deps.Session.GetHistoryMessages()
		result, err := deps.React.ExecuteWithTools(sessionCtx, sysMsg, history, tools)
		if err == nil || !errors.Is(err, coretypes.ErrContextLengthExceeded) {
			return result, err
		}
	}
	return nil, coretypes.ErrContextLengthExceeded
}

// DAGAction executes a DAG batch and handles drift review in the failure callback.
type DAGAction struct {
	Plan *working.Plan
}

func (a *DAGAction) Execute(ctx *Context) (*ActionResult, error) {
	deps := ctx.Deps

	r := deps.DAG.ExecuteBatch(ctx.Ctx, a.Plan, func(taskID, goal, reason string) {
		ctx.TaskFails++
		if shouldReview(ctx) {
			plan, _ := deps.GetPlan(ctx.PlanID)
			if plan != nil {
				summary := fmtPlanSummary(plan)
				reviewResult := deps.Reviewer.Review(ctx.Ctx, ctx.Input.Text, summary, reason)
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
func buildPrompt(ctx *Context, status constants.PlanState, plan *working.Plan) (string, error) {
	deps := ctx.Deps
	var sb strings.Builder

	if agentCtx := deps.LoadAgentContext(); agentCtx != "" {
		sb.WriteString(agentCtx)
		sb.WriteString("\n\n")
	}

	if plan != nil {
		var rendered string
		var err error
		if status == constants.PlanStateConfirmed {
			rendered, err = deps.RenderPlanPrompt(
				prompts.TypePlanConfirm, buildPlanConfirmParams(plan))
		} else {
			rendered, err = deps.RenderPlanPrompt(
				prompts.TypePlanStatus, buildPlanStatusParams(plan))
		}
		if err != nil {
			return "", fmt.Errorf("render plan prompt: %w", err)
		}
		sb.WriteString(rendered)
		sb.WriteString("\n\n")
	}

	sb.WriteString(prompts.PlannerPrompt(status))
	return sb.String(), nil
}

// buildPlanConfirmParams converts a Plan to prompts.PlanConfirmParams for rendering.
// Lives here (not in prompts) so the prompts package stays a leaf with no
// dependency on the working-memory domain model.
func buildPlanConfirmParams(plan *working.Plan) prompts.PlanConfirmParams {
	tasks := make([]prompts.PlanConfirmTaskParams, len(plan.SubTasks))
	for i, t := range plan.SubTasks {
		deps := t.ParentTaskID
		if deps == "" {
			deps = "none"
		}
		tasks[i] = prompts.PlanConfirmTaskParams{
			Index:  i + 1,
			ID:     t.ID,
			Goal:   t.Goal,
			Action: t.Action,
			Deps:   deps,
		}
	}
	return prompts.PlanConfirmParams{
		Name:  plan.Name,
		ID:    plan.ID,
		Tasks: tasks,
	}
}

// buildPlanStatusParams converts a Plan to prompts.PlanStatusParams for rendering.
func buildPlanStatusParams(plan *working.Plan) prompts.PlanStatusParams {
	done, failed := 0, 0
	var failedTasks []prompts.PlanFailedTaskParams
	for _, t := range plan.SubTasks {
		if t.Status == working.TaskStatusDone {
			done++
		}
		if t.Status == working.TaskStatusFailed {
			failed++
			reason := t.FinishedReason
			if reason == "" {
				reason = "(no reason)"
			}
			if len(reason) > 200 {
				reason = reason[:200] + "..."
			}
			failedTasks = append(failedTasks, prompts.PlanFailedTaskParams{
				ID: t.ID, Goal: t.Goal, Reason: reason,
			})
		}
	}
	return prompts.PlanStatusParams{
		Name:        plan.Name,
		Status:      string(plan.State),
		Done:        done,
		Total:       len(plan.SubTasks),
		Failed:      failed,
		FailedTasks: failedTasks,
	}
}

// shouldReview checks whether a drift review should be triggered.
func shouldReview(ctx *Context) bool {
	if ctx.Deps.Reviewer == nil {
		return false
	}
	if ctx.Deps.Config.ReviewInterval > 0 && ctx.TaskFails%ctx.Deps.Config.ReviewInterval == 0 {
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
