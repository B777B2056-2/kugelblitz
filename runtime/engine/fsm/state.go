package fsm

import (
	"github.com/B777B2056-2/kugelblitz/constants"
	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/B777B2056-2/kugelblitz/memory/working"
)

// State represents a single state in the finite state machine.
type State interface {
	Name() constants.PlanState
	AvailableTools() []string
	Execute(ctx *Context) (constants.PlanState, error)
}

// stateToolsMap defines the available tools for each PlanState.
var stateToolsMap = map[constants.PlanState][]string{
	constants.PlanStateIntent: {"set_work_mode"},
	constants.PlanStateDirect: {
		"shell_exec", "web_fetch", "web_search",
		"file_read", "file_write", "file_copy", "file_delete",
		"dir_create", "dir_copy",
		"memory_store", "memory_search", "memory_get_section",
		"memory_remove", "memory_list_sections", "memory_stats",
		"skill_use", "ask_human",
	},
	constants.PlanStateInit: {
		"plan_create", "task_insert",
		"memory_store", "memory_search", "memory_get_section",
		"memory_remove", "memory_list_sections", "memory_stats",
		"memory_extract",
		"skill_use",
	},
	constants.PlanStateConfirmed: {"ask_human", "confirm_plan"},
	constants.PlanStateDoing:     {"task_query", "task_status_update"},
	constants.PlanStateUpdating: {
		"memory_store", "memory_search", "memory_get_section",
		"memory_remove", "memory_list_sections", "memory_stats",
		"memory_extract",
		"skill_use",
		"task_insert", "task_delete", "task_query", "plan_query",
	},
	constants.PlanStateDone:    {"task_query", "plan_query"},
	constants.PlanStateFailed:  {"task_query", "plan_query"},
	constants.PlanStateRejected: {},
}

// ToolsForState returns the available tools for a given state, merged with
// any custom tools registered in the global tool registry.
func ToolsForState(status constants.PlanState) []string {
	tools, ok := stateToolsMap[status]
	if !ok {
		return nil
	}

	customNames := core.GetToolRegistry().CustomToolNames()
	if len(customNames) == 0 {
		return tools
	}
	return append(tools, customNames...)
}

// ---- Concrete State Implementations ----

// IntentState handles intent recognition (phase 1).
type IntentState struct{}

func (s *IntentState) Name() constants.PlanState           { return constants.PlanStateIntent }
func (s *IntentState) AvailableTools() []string            { return ToolsForState(constants.PlanStateIntent) }
func (s *IntentState) Execute(ctx *Context) (constants.PlanState, error) {
	action := &ReactAction{
		State:      constants.PlanStateIntent,
		UserPrompt: ctx.Goal,
	}
	result, err := action.Execute(ctx)
	if err != nil {
		return constants.PlanStateIntent, err
	}

	ctx.WorkMode, _ = core.ExtractToolResult[string](result.Messages, "set_work_mode", "mode")
	if ctx.WorkMode == "plan" {
		return constants.PlanStateInit, nil
	}
	return constants.PlanStateDirect, nil
}

// DirectState executes a simple task directly (no plan).
type DirectState struct{}

func (s *DirectState) Name() constants.PlanState           { return constants.PlanStateDirect }
func (s *DirectState) AvailableTools() []string            { return ToolsForState(constants.PlanStateDirect) }
func (s *DirectState) Execute(ctx *Context) (constants.PlanState, error) {
	action := &ReactAction{
		State:      constants.PlanStateDirect,
		UserPrompt: ctx.Goal,
	}
	result, err := action.Execute(ctx)
	if err != nil {
		return constants.PlanStateDirect, err
	}
	ctx.Results = append(ctx.Results, result.Messages...)
	return constants.PlanStateDirect, nil // terminal (caller checks isTerminal)
}

// InitState handles plan creation.
type InitState struct{}

func (s *InitState) Name() constants.PlanState           { return constants.PlanStateInit }
func (s *InitState) AvailableTools() []string            { return ToolsForState(constants.PlanStateInit) }
func (s *InitState) Execute(ctx *Context) (constants.PlanState, error) {
	action := &ReactAction{
		State:      constants.PlanStateInit,
		UserPrompt: ctx.Goal,
	}
	result, err := action.Execute(ctx)
	if err != nil {
		return constants.PlanStateInit, err
	}

	ctx.PlanID, _ = core.ExtractToolResult[string](result.Messages, "plan_create", "id")
	if ctx.PlanID != "" {
		plan, ok := working.GetPlan(ctx.PlanID)
		if ok && plan != nil && plan.IsValid() {
			ctx.Plan = plan
			return constants.PlanStateConfirmed, nil
		}
	}

	// Fallback to DIRECT mode
	return constants.PlanStateDirect, nil
}

// ConfirmedState presents the plan to the user for approval.
type ConfirmedState struct{}

func (s *ConfirmedState) Name() constants.PlanState { return constants.PlanStateConfirmed }
func (s *ConfirmedState) AvailableTools() []string  { return ToolsForState(constants.PlanStateConfirmed) }
func (s *ConfirmedState) Execute(ctx *Context) (constants.PlanState, error) {
	action := &ReactAction{
		State: constants.PlanStateConfirmed,
		Plan:  ctx.Plan,
		UserPrompt: "The plan has been created. Present it to the user for approval via ask_human. " +
			"After the user responds, call confirm_plan with the appropriate status.",
	}
	_, err := action.Execute(ctx)
	if err != nil {
		return constants.PlanStateConfirmed, err
	}

	if ctx.PlanID != "" {
		plan, ok := working.GetPlan(ctx.PlanID)
		if ok && plan != nil && plan.State != constants.PlanStateConfirmed {
			return plan.State, nil
		}
	}
	return constants.PlanStateConfirmed, nil
}

// DoingState executes plan tasks via DAG.
type DoingState struct{}

func (s *DoingState) Name() constants.PlanState           { return constants.PlanStateDoing }
func (s *DoingState) AvailableTools() []string            { return ToolsForState(constants.PlanStateDoing) }
func (s *DoingState) Execute(ctx *Context) (constants.PlanState, error) {
	action := &DAGAction{
		Plan: ctx.Plan,
	}
	result, err := action.Execute(ctx)
	if err != nil {
		return constants.PlanStateDoing, err
	}

	hasFailed, _ := result.Data["hasFailed"].(bool)
	if hasFailed {
		return constants.PlanStateUpdating, nil
	}
	return constants.PlanStateDone, nil
}

// UpdatingState adapts the plan after task failures.
type UpdatingState struct{}

func (s *UpdatingState) Name() constants.PlanState { return constants.PlanStateUpdating }
func (s *UpdatingState) AvailableTools() []string  { return ToolsForState(constants.PlanStateUpdating) }
func (s *UpdatingState) Execute(ctx *Context) (constants.PlanState, error) {
	action := &ReactAction{
		State:      constants.PlanStateUpdating,
		Plan:       ctx.Plan,
		UserPrompt: "Some tasks have failed. Review the failed tasks and update the plan as needed.",
	}
	_, err := action.Execute(ctx)
	if err != nil {
		return constants.PlanStateUpdating, err
	}

	if ctx.PlanID != "" {
		plan, ok := working.GetPlan(ctx.PlanID)
		if ok && plan != nil && plan.IsValid() {
			ctx.Plan = plan
			return constants.PlanStateConfirmed, nil
		}
	}

	// Fallback to DIRECT mode
	return constants.PlanStateDirect, nil
}

// DoneState summarizes completed work.
type DoneState struct{}

func (s *DoneState) Name() constants.PlanState           { return constants.PlanStateDone }
func (s *DoneState) AvailableTools() []string            { return ToolsForState(constants.PlanStateDone) }
func (s *DoneState) Execute(ctx *Context) (constants.PlanState, error) {
	action := &ReactAction{
		State:      constants.PlanStateDone,
		Plan:       ctx.Plan,
		UserPrompt: "All tasks have completed. Review the results and summarize what was accomplished.",
	}
	result, err := action.Execute(ctx)
	if err != nil {
		return constants.PlanStateDone, err
	}
	ctx.Results = append(ctx.Results, result.Messages...)
	return constants.PlanStateDone, nil // terminal
}

// FailedState summarizes failure.
type FailedState struct{}

func (s *FailedState) Name() constants.PlanState           { return constants.PlanStateFailed }
func (s *FailedState) AvailableTools() []string            { return ToolsForState(constants.PlanStateFailed) }
func (s *FailedState) Execute(ctx *Context) (constants.PlanState, error) {
	action := &ReactAction{
		State:      constants.PlanStateFailed,
		Plan:       ctx.Plan,
		UserPrompt: "The plan has failed. Review the failed tasks and summarize what went wrong.",
	}
	result, err := action.Execute(ctx)
	if err != nil {
		return constants.PlanStateFailed, err
	}
	ctx.Results = append(ctx.Results, result.Messages...)
	return constants.PlanStateFailed, nil // terminal
}

// RejectedState marks the plan as rejected (no action, just terminal).
type RejectedState struct{}

func (s *RejectedState) Name() constants.PlanState           { return constants.PlanStateRejected }
func (s *RejectedState) AvailableTools() []string            { return ToolsForState(constants.PlanStateRejected) }
func (s *RejectedState) Execute(ctx *Context) (constants.PlanState, error) {
	return constants.PlanStateRejected, nil // terminal, no action
}
