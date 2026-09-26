package fsm

import (
	"context"
	"fmt"

	"github.com/B777B2056-2/kugelblitz/constants"
	"github.com/B777B2056-2/kugelblitz/core"
	coretypes "github.com/B777B2056-2/kugelblitz/core/types"
	"github.com/B777B2056-2/kugelblitz/memory/working"
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
		states:    make(map[constants.PlanState]State),
		prevState: constants.PlanStateNone,
		deps:      deps,
	}
	m.currentState = m.initialState()
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
func (m *Machine) Run(ctx context.Context, input coretypes.AgentInput) ([]coretypes.Message, error) {
	fsmCtx := &Context{
		Ctx:   ctx,
		Input: input,
		Deps:  m.deps,
	}
	// Wire up drift handling
	fsmCtx.Deps.HandleDrift = func(c *Context, reason string) {
		m.handleDrift(c, reason)
	}

	// Append user message once at entry, not on every state transition.
	fsmCtx.Deps.Session.AppendMessage(input.BuildUserMessage())

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
				_ = fsmCtx.Deps.PutPlan(fsmCtx.Plan)
			}
			return fsmCtx.Results, err
		}

		// A terminal state that returns itself has finished its work; break.
		if isTerminal(nextState) && nextState == m.currentState {
			if fsmCtx.Plan != nil {
				fsmCtx.Plan.State = nextState
				if err := fsmCtx.Deps.PutPlan(fsmCtx.Plan); err != nil {
					return fsmCtx.Results, err
				}
			}
			return fsmCtx.Results, nil
		}

		// Non-terminal transition: move to next state and continue loop.
		if err := m.transition(fsmCtx, nextState); err != nil {
			return fsmCtx.Results, err
		}
		fsmCtx.StepCount++
	}

	if fsmCtx.Plan != nil {
		if err := fsmCtx.Deps.PutPlan(fsmCtx.Plan); err != nil {
			return fsmCtx.Results, err
		}
	}
	return fsmCtx.Results, coretypes.ErrMaxCyclesExceeded
}

// reset returns the state machine to its initial state.
func (m *Machine) reset() {
	m.prevState = constants.PlanStateNone
	m.currentState = m.initialState()
}

// initialState returns the state the machine should start in. When ForceMode is
// "plan" or "simple", the Intent (set_work_mode) classification phase is skipped
// entirely and the machine enters the requested mode directly.
func (m *Machine) initialState() constants.PlanState {
	switch m.deps.Config.ForceMode {
	case "plan":
		return constants.PlanStateInit
	case "simple":
		return constants.PlanStateDirect
	default:
		return constants.PlanStateIntent
	}
}

// transition updates the state machine to the next state, persists the plan,
// logs the change, and appends a system message.
func (m *Machine) transition(ctx *Context, next constants.PlanState) error {
	m.prevState = m.currentState
	m.currentState = next
	if ctx.Plan != nil {
		ctx.Plan.State = next
		if err := ctx.Deps.PutPlan(ctx.Plan); err != nil {
			return err
		}
	}
	core.Info("planner state machine", "status update",
		fmt.Sprintf("%s -> %s", string(m.prevState), string(m.currentState)))
	if ctx.Plan != nil {
		ctx.Deps.Session.AppendMessage(coretypes.NewSystemMessage(coretypes.TextContent{
			Text: fmt.Sprintf("[System] Plan %q status: %s → %s.",
				ctx.Plan.Name, string(m.prevState), string(m.currentState)),
		}))
	}
	return nil
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
	if err := ctx.Deps.LoadCheckpoint(plan.ID, targetVersion, &cp); err != nil {
		return
	}

	ctx.Deps.DAG.Cancel()

	plan.Name = cp.Plan.Name
	plan.SubTasks = cp.Plan.SubTasks
	plan.CurrentActivateSubTaskIDs = cp.Plan.CurrentActivateSubTaskIDs
	plan.State = constants.PlanStateUpdating
	plan.FinishedReason = fmt.Sprintf("drift: %s", reason)
	_ = plan.Persist()

	ctx.Deps.Session.AppendMessage(coretypes.NewSystemMessage(coretypes.TextContent{
		Text: fmt.Sprintf("⚠️ 自动审查检测到执行可能偏离目标（%s），计划已回滚至版本 %d。请根据当前任务进度和目标偏差，调整任务计划，完成后系统将进入确认阶段。", reason, targetVersion),
	}))

	ctx.Deps.React.NotifyPlanRollback(
		ctx.Deps.React.GetAgentIdentity(),
		plan.ID, targetVersion, plan.Name,
	)
}
