package fsm

import (
	"context"
	"fmt"

	"github.com/B777B2056-2/kugelblitz/constants"
	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/B777B2056-2/kugelblitz/memory/working"
	"github.com/B777B2056-2/kugelblitz/persist"
)

// Machine orchestrates the finite state machine for plan lifecycle.
type Machine struct {
	states       map[constants.PlanState]State
	currentState constants.PlanState
	prevState    constants.PlanState
	deps         Dependencies
}

// NewMachine creates a new FSM Machine with all states registered.
func NewMachine(deps Dependencies) *Machine {
	m := &Machine{
		states:       make(map[constants.PlanState]State),
		currentState: constants.PlanStateIntent,
		prevState:    constants.PlanStateNone,
		deps:         deps,
	}
	m.registerStates()
	return m
}

func (m *Machine) registerStates() {
	m.states[constants.PlanStateIntent] = &IntentState{}
	m.states[constants.PlanStateDirect] = &DirectState{}
	m.states[constants.PlanStateInit] = &InitState{}
	m.states[constants.PlanStateConfirmed] = &ConfirmedState{}
	m.states[constants.PlanStateDoing] = &DoingState{}
	m.states[constants.PlanStateUpdating] = &UpdatingState{}
	m.states[constants.PlanStateDone] = &DoneState{}
	m.states[constants.PlanStateFailed] = &FailedState{}
	m.states[constants.PlanStateRejected] = &RejectedState{}
}

// Run executes the state machine main loop.
func (m *Machine) Run(ctx context.Context, goal string) ([]core.Message, error) {
	fsmCtx := &Context{
		Ctx:  ctx,
		Goal: goal,
		Deps: m.deps,
	}
	// Wire up drift handling
	fsmCtx.Deps.HandleDrift = func(c *Context, reason string) {
		m.handleDrift(c, reason)
	}
	m.reset()

	for cycle := 0; cycle < m.deps.Config.MaxCycles; cycle++ {
		core.Info("planner state machine", "status", string(m.currentState),
			"step", fsmCtx.StepCount, "planID", fsmCtx.PlanID)

		state, ok := m.states[m.currentState]
		if !ok {
			return fsmCtx.Results, fmt.Errorf("unknown state: %s", m.currentState)
		}

		nextState, err := state.Execute(fsmCtx)
		if err != nil {
			if fsmCtx.Plan != nil {
				fsmCtx.Plan.State = m.currentState
				working.PutPlan(fsmCtx.Plan)
			}
			return fsmCtx.Results, err
		}

		// A terminal state that returns itself has finished its work; break.
		if isTerminal(nextState) && nextState == m.currentState {
			if fsmCtx.Plan != nil {
				fsmCtx.Plan.State = nextState
				working.PutPlan(fsmCtx.Plan)
			}
			return fsmCtx.Results, nil
		}

		// Non-terminal transition: move to next state and continue loop.
		m.transition(fsmCtx, nextState)
		fsmCtx.StepCount++
	}

	if fsmCtx.Plan != nil {
		working.PutPlan(fsmCtx.Plan)
	}
	return fsmCtx.Results, nil
}

// reset returns the state machine to its initial state.
func (m *Machine) reset() {
	m.prevState = constants.PlanStateNone
	m.currentState = constants.PlanStateIntent
}

// transition updates the state machine to the next state, persists the plan,
// logs the change, and appends a system message.
func (m *Machine) transition(ctx *Context, next constants.PlanState) {
	m.prevState = m.currentState
	m.currentState = next
	if ctx.Plan != nil {
		ctx.Plan.State = next
		working.PutPlan(ctx.Plan)
	}
	core.Info("planner state machine", "status update",
		fmt.Sprintf("%s -> %s", string(m.prevState), string(m.currentState)))
	if ctx.Plan != nil {
		ctx.Deps.Session.AppendMessage(core.NewSystemMessage(core.TextContent{
			Text: fmt.Sprintf("[System] Plan %q status: %s → %s.",
				ctx.Plan.Name, string(m.prevState), string(m.currentState)),
		}))
	}
}

// isTerminal reports whether the given state is terminal (the loop should exit).
func isTerminal(state constants.PlanState) bool {
	switch state {
	case constants.PlanStateDirect, constants.PlanStateDone,
		constants.PlanStateFailed, constants.PlanStateRejected:
		return true
	default:
		return false
	}
}

// handleDrift performs a plan rollback when goal drift is detected.
func (m *Machine) handleDrift(ctx *Context, reason string) {
	plan := ctx.Plan
	if plan == nil || plan.Version <= 1 {
		return
	}
	targetVersion := plan.Version - 1
	var cp working.Checkpoint
	if err := persist.LoadCheckpointJSON(plan.ID, targetVersion, &cp); err != nil {
		return
	}

	ctx.Deps.DAG.Cancel()

	plan.Name = cp.Plan.Name
	plan.SubTasks = cp.Plan.SubTasks
	plan.CurrentActivateSubTaskIDs = cp.Plan.CurrentActivateSubTaskIDs
	plan.State = constants.PlanStateUpdating
	plan.FinishedReson = fmt.Sprintf("drift: %s", reason)
	_ = plan.Persist()

	ctx.Deps.Session.AppendMessage(core.NewSystemMessage(core.TextContent{
		Text: fmt.Sprintf("⚠️ 自动审查检测到执行可能偏离目标（%s），计划已回滚至版本 %d。请根据当前任务进度和目标偏差，调整任务计划，完成后系统将进入确认阶段。", reason, targetVersion),
	}))

	if ctx.Deps.React.EventHooks.OnPlanRollback != nil {
		ctx.Deps.React.EventHooks.OnPlanRollback(
			ctx.Deps.React.GetAgentIdentity(),
			plan.ID, targetVersion, plan.Name,
		)
	}
}
