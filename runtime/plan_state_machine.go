package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/B777B2056-2/kugelblitz/constants"
	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/B777B2056-2/kugelblitz/memory"
	"github.com/B777B2056-2/kugelblitz/memory/longterm"
	"github.com/B777B2056-2/kugelblitz/memory/working"
	"github.com/B777B2056-2/kugelblitz/persist"
	"github.com/B777B2056-2/kugelblitz/runtime/prompts"
)

// StateDef defines a state's behavior.
type StateDef struct {
	Tools []string // nil = terminal
}

// PlannerStateMachine manages state definitions and the main loop.
type PlannerStateMachine struct {
	dag               *DAGTaskExecutor
	defs              map[constants.PlanStatus]StateDef
	maxCycles         int
	customTools       []string
	onContextExceeded func()                                          // called when LLM context exceeds limit (compress + retry)
	onDrift           func(plan *working.Plan, reviewerResult string) // called when review detects drift

	reviewer             *Reviewer
	failuresBeforeReview int
	reviewInterval       int
	stepCount            int
	taskFailsCounter     int

	planID        string
	prevStatus    constants.PlanStatus
	currentStatus constants.PlanStatus

	workMode string // "plan" or "simple", set by Intent phase
}

// SetReviewer enables goal-drift detection.
func (sm *PlannerStateMachine) SetReviewer(r *Reviewer, failuresBeforeReview, reviewInterval int) {
	sm.reviewer = r
	sm.failuresBeforeReview = failuresBeforeReview
	sm.reviewInterval = reviewInterval
}

// OnDrift sets a callback invoked when drift is detected.
func (sm *PlannerStateMachine) OnDrift(fn func(plan *working.Plan, reviewerResult string)) {
	sm.onDrift = fn
}

// OnContextExceeded sets a callback invoked when the LLM context exceeds limit.
// The callback should compress the session memory so the next call can proceed.
func (sm *PlannerStateMachine) OnContextExceeded(fn func()) {
	sm.onContextExceeded = fn
}

// NewPlannerStateMachine creates the state machine with all state definitions.
func NewPlannerStateMachine(dag *DAGTaskExecutor, customTools ...string) *PlannerStateMachine {
	return &PlannerStateMachine{
		dag:           dag,
		customTools:   customTools,
		maxCycles:     30,
		prevStatus:    constants.PlanStatusNone,
		currentStatus: constants.PlanStatusIntent,
		defs: map[constants.PlanStatus]StateDef{
			constants.PlanStatusIntent: {Tools: []string{"set_work_mode"}},
			constants.PlanStatusDirect: {Tools: []string{
				"shell_exec", "web_fetch", "web_search",
				"file_read", "file_write", "file_copy", "file_delete",
				"dir_create", "dir_copy",
				"memory_store", "memory_search", "memory_get_section",
				"memory_remove", "memory_list_sections", "memory_stats",
				"skill_use", "ask_human",
			}},
			constants.PlanStatusInit: {Tools: []string{
				"plan_create", "task_insert",
				"memory_store", "memory_search", "memory_get_section",
				"memory_remove", "memory_list_sections", "memory_stats",
				"memory_extract", "memory_resolve_conflict",
				"skill_use",
			}},
			constants.PlanStatusConfirmed: {Tools: []string{"ask_human", "confirm_plan"}},
			constants.PlanStatusDoing:     {Tools: []string{"task_query", "task_status_update"}},
			constants.PlanStatusUpdating: {Tools: []string{
				"memory_store", "memory_search", "memory_get_section",
				"memory_remove", "memory_list_sections", "memory_stats",
				"memory_extract", "memory_resolve_conflict",
				"skill_use",
				"task_insert", "task_delete", "task_query", "plan_query",
			}},
			constants.PlanStatusDone:     {Tools: []string{"task_query", "plan_query"}},
			constants.PlanStatusFailed:   {Tools: []string{"task_query", "plan_query"}},
			constants.PlanStatusRejected: {},
		},
	}
}

func (sm *PlannerStateMachine) allTools(def *StateDef) []string {
	if len(sm.customTools) == 0 {
		return def.Tools
	}
	return append(def.Tools, sm.customTools...)
}

func (sm *PlannerStateMachine) Def(status constants.PlanStatus) *StateDef {
	if d, ok := sm.defs[status]; ok {
		return &d
	}
	return nil
}

func extractPlanID(stepResult []core.Message) string {
	a, _ := json.Marshal(stepResult)
	core.Info("stepResult", a)
	for _, msg := range stepResult {
		for _, r := range extractToolResults(msg.Content) {
			if r.ToolName == "plan_create" {
				if id, ok := r.Outputs["id"].(string); ok && id != "" {
					return id
				}
			}
		}
	}
	return ""
}

func extractWorkMode(stepResult []core.Message) string {
	for _, msg := range stepResult {
		for _, r := range extractToolResults(msg.Content) {
			if r.ToolName == "set_work_mode" {
				if mode, ok := r.Outputs["mode"].(string); ok {
					return mode
				}
			}
		}
	}
	return ""
}

func (sm *PlannerStateMachine) runOncePlanner(ctx context.Context, userMsg core.Message, plan *working.Plan,
	mem *memory.SessionMemory, plannerReactInst *ReactAgent, ltm *longterm.LongTermMemory) ([]core.Message, error) {
	def := sm.Def(sm.currentStatus)
	sysMsg := core.NewSystemMessage("planner", core.TextContent{Text: sm.buildPrompt(sm.currentStatus, ltm, plan)})
	sessionCtx := core.WithSessionID(ctx, mem.SessionID())
	history := mem.GetHistoryMessages()
	if len(history) == 0 {
		history = []core.Message{userMsg}
	}

	stepResult, err := plannerReactInst.ExecuteWithTools(sessionCtx, sysMsg, history, sm.allTools(def))
	if errors.Is(err, core.ErrContextLengthExceeded) {
		if sm.onContextExceeded != nil {
			sm.onContextExceeded()
		}
		history = mem.GetHistoryMessages()
		stepResult, err = plannerReactInst.ExecuteWithTools(sessionCtx, sysMsg, history, sm.allTools(def))
	}
	return stepResult, err
}

// reset returns the state machine to its initial state for a fresh execution.
func (sm *PlannerStateMachine) reset() {
	sm.prevStatus = constants.PlanStatusNone
	sm.currentStatus = constants.PlanStatusIntent
	sm.stepCount = 0
	sm.planID = ""
	sm.taskFailsCounter = 0
	sm.workMode = ""
}

// updatePlanStatus persists the status change and appends a system message.
func (sm *PlannerStateMachine) updatePlanStatus(plan *working.Plan, targetStatus constants.PlanStatus, mem *memory.SessionMemory) {
	sm.prevStatus = sm.currentStatus
	sm.currentStatus = targetStatus
	if plan != nil {
		plan.Status = targetStatus
		working.PutPlan(plan)
	}
	core.Info("planner state machine", "status update", fmt.Sprintf("%s -> %s", string(sm.prevStatus), string(sm.currentStatus)))
	if plan != nil && mem != nil {
		mem.AppendMessage(core.NewSystemMessage("planner", core.TextContent{
			Text: fmt.Sprintf("[System] Plan %q status: %s → %s.", plan.Name, string(sm.prevStatus), string(sm.currentStatus)),
		}))
	}
}

// Run executes the state machine main loop.
func (sm *PlannerStateMachine) Run(
	ctx context.Context,
	plannerReactInst *ReactAgent,
	mem *memory.SessionMemory,
	goal string,
	ltm *longterm.LongTermMemory,
) ([]core.Message, error) {
	sm.reset()

	var plan *working.Plan
	var result []core.Message

	for cycle := 0; cycle < sm.maxCycles; cycle++ {
		core.Info("planner state machine", "status", string(sm.currentStatus), "step", sm.stepCount, "planID", sm.planID)

		var stepResult []core.Message
		var err error

		if sm.prevStatus == sm.currentStatus {
			continue
		}

		if sm.currentStatus == constants.PlanStatusIntent {
			userMsg := core.NewUserMessage("planner", core.TextContent{Text: goal})
			stepResult, err = sm.runOncePlanner(ctx, userMsg, plan, mem, plannerReactInst, ltm)
			if err == nil {
				sm.workMode = extractWorkMode(stepResult)
				if sm.workMode == "plan" {
					sm.updatePlanStatus(plan, constants.PlanStatusInit, mem)
				} else {
					sm.updatePlanStatus(plan, constants.PlanStatusDirect, mem)
				}
			}
		} else if sm.currentStatus == constants.PlanStatusDirect {
			userMsg := core.NewUserMessage("planner", core.TextContent{Text: goal})
			stepResult, err = sm.runOncePlanner(ctx, userMsg, nil, mem, plannerReactInst, ltm)
			result = append(result, stepResult...)
			break
		} else if sm.currentStatus == constants.PlanStatusInit {
			userMsg := core.NewUserMessage("planner", core.TextContent{Text: goal})
			stepResult, err = sm.runOncePlanner(ctx, userMsg, nil, mem, plannerReactInst, ltm)
			if err == nil {
				sm.planID = extractPlanID(stepResult)
				if sm.planID != "" {
					plan, _ = working.GetPlan(sm.planID)
					if plan != nil && len(plan.SubTasks) != 0 {
						sm.updatePlanStatus(plan, constants.PlanStatusConfirmed, mem)
					}
				}
			}
		} else if sm.currentStatus == constants.PlanStatusConfirmed {
			userMsg := core.NewUserMessage("planner", core.TextContent{
				Text: "The plan has been created. Present it to the user for approval via ask_human. After the user responds, call confirm_plan with the appropriate status.",
			})
			stepResult, err = sm.runOncePlanner(ctx, userMsg, plan, mem, plannerReactInst, ltm)
			if err == nil {
				if sm.planID != "" {
					plan, _ = working.GetPlan(sm.planID)
					if plan != nil && plan.Status != sm.currentStatus {
						sm.updatePlanStatus(plan, plan.Status, mem)
					}
				}
			}
		} else if sm.currentStatus == constants.PlanStatusDoing {
			r := sm.dag.ExecuteBatch(plan, ctx, func(taskID, goal, reason string) {
				sm.taskFailsCounter++
				if sm.shouldReview(sm.currentStatus) {
					plan2, _ := working.GetPlan(sm.planID)
					if plan2 != nil {
						summary := fmt.Sprintf("Plan %q (v%d, status=%s), %d tasks",
							plan2.Name, plan2.Version, plan2.Status, len(plan2.SubTasks))
						if sm.reviewer.Review(ctx, goal, summary, reason).Drift {
							sm.dag.Cancel()
						}
					}
				}
			})
			if r.HasFailed {
				sm.updatePlanStatus(plan, constants.PlanStatusUpdating, mem)
			} else {
				sm.updatePlanStatus(plan, constants.PlanStatusDone, mem)
			}
		} else if sm.currentStatus == constants.PlanStatusUpdating {
			userMsg := core.NewUserMessage("planner", core.TextContent{
				Text: "Some tasks have failed. Review the failed tasks and update the plan as needed.",
			})
			stepResult, err = sm.runOncePlanner(ctx, userMsg, plan, mem, plannerReactInst, ltm)
			if err == nil {
				if sm.planID != "" {
					plan, _ = working.GetPlan(sm.planID)
					if plan != nil && len(plan.SubTasks) != 0 {
						sm.updatePlanStatus(plan, constants.PlanStatusConfirmed, mem)
					}
				}
			}
		} else if sm.currentStatus == constants.PlanStatusDone {
			userMsg := core.NewUserMessage("planner", core.TextContent{
				Text: "All tasks have completed. Review the results and summarize what was accomplished.",
			})
			stepResult, err = sm.runOncePlanner(ctx, userMsg, plan, mem, plannerReactInst, ltm)
			result = append(result, stepResult...)
			if plan != nil {
				plan.Status = sm.currentStatus
				working.PutPlan(plan)
			}
			break
		} else if sm.currentStatus == constants.PlanStatusFailed {
			userMsg := core.NewUserMessage("planner", core.TextContent{
				Text: "The plan has failed. Review the failed tasks and summarize what went wrong.",
			})
			stepResult, err = sm.runOncePlanner(ctx, userMsg, plan, mem, plannerReactInst, ltm)
			result = append(result, stepResult...)
			if plan != nil {
				plan.Status = sm.currentStatus
				working.PutPlan(plan)
			}
			break
		} else if sm.currentStatus == constants.PlanStatusRejected {
			if plan != nil {
				plan.Status = sm.currentStatus
				working.PutPlan(plan)
			}
			break
		}

		if err != nil {
			if plan != nil {
				plan.Status = sm.currentStatus
				working.PutPlan(plan)
			}
			return result, err
		}
		result = append(result, stepResult...)
		sm.stepCount++
	}

	if plan != nil {
		working.PutPlan(plan)
	}
	return result, nil
}

func (sm *PlannerStateMachine) shouldReview(status constants.PlanStatus) bool {
	if sm.reviewer == nil || status != constants.PlanStatusDoing {
		return false
	}
	if sm.reviewInterval > 0 && sm.stepCount%sm.reviewInterval == 0 {
		return true
	}
	if sm.failuresBeforeReview > 0 && sm.taskFailsCounter >= sm.failuresBeforeReview {
		return true
	}
	return false
}

func (sm *PlannerStateMachine) handleDrift(mem *memory.SessionMemory, plan *working.Plan, reason string) {
	if plan.Version <= 1 {
		return
	}
	targetVersion := plan.Version - 1
	var cp working.Checkpoint
	if err := persist.LoadCheckpointJSON(plan.ID, targetVersion, &cp); err != nil {
		return
	}

	sm.dag.Cancel()

	plan.Name = cp.Plan.Name
	plan.SubTasks = cp.Plan.SubTasks
	plan.CurrentActivateSubTaskIDs = cp.Plan.CurrentActivateSubTaskIDs
	plan.Status = constants.PlanStatusUpdating
	plan.FinishedReson = fmt.Sprintf("drift: %s", reason)
	plan.Persist()

	mem.AppendMessage(core.NewSystemMessage(plan.ID, core.TextContent{
		Text: fmt.Sprintf("⚠️ 自动审查检测到执行可能偏离目标（%s），计划已回滚至版本 %d。请根据当前任务进度和目标偏差，调整任务计划，完成后系统将进入确认阶段。", reason, targetVersion),
	}))

	if sm.onDrift != nil {
		sm.onDrift(plan, reason)
	}
}

func (sm *PlannerStateMachine) isTerminal(status constants.PlanStatus) bool {
	return status == constants.PlanStatusDone ||
		status == constants.PlanStatusFailed ||
		status == constants.PlanStatusRejected
}

func (sm *PlannerStateMachine) buildPrompt(status constants.PlanStatus, ltm *longterm.LongTermMemory, plan *working.Plan) string {
	var sb strings.Builder

	if ctx := core.LoadAgentContext(); ctx != "" {
		sb.WriteString(ctx)
		sb.WriteString("\n\n")
	}

	if ltm != nil {
		pending := ltm.PendingConflicts()
		if len(pending) > 0 {
			sb.WriteString("## Pending Memory Conflicts\n")
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
	}

	if plan != nil {
		switch status {
		case constants.PlanStatusConfirmed:
			sb.WriteString("## Plan to Confirm\n")
			sb.WriteString(fmt.Sprintf("- Name: %q\n", plan.Name))
			sb.WriteString(fmt.Sprintf("- ID: %s\n", plan.ID))
			sb.WriteString(fmt.Sprintf("- Tasks: %d\n\n", len(plan.SubTasks)))
			for i, t := range plan.SubTasks {
				deps := t.ParentTaskID
				if deps == "" {
					deps = "none"
				}
				sb.WriteString(fmt.Sprintf("### Task %d: %s\n", i+1, t.ID))
				sb.WriteString(fmt.Sprintf("- Goal: %s\n", t.Goal))
				if t.Action != "" {
					sb.WriteString(fmt.Sprintf("- Action: %s\n", t.Action))
				}
				sb.WriteString(fmt.Sprintf("- Status: %s\n", t.Status))
				sb.WriteString(fmt.Sprintf("- Depends on: %s\n\n", deps))
			}
			sb.WriteString("Present this plan to the user via ask_human. Wait for their response before calling confirm_plan.\n\n")
		default:
			done, failed := 0, 0
			for _, t := range plan.SubTasks {
				if t.Status == working.TaskStatusDone {
					done++
				}
				if t.Status == working.TaskStatusFailed {
					failed++
				}
			}
			sb.WriteString("## Current Plan\n")
			sb.WriteString(fmt.Sprintf("- %q (status: %s, %d/%d done, %d failed)\n",
				plan.Name, plan.Status, done, len(plan.SubTasks), failed))
			if failed > 0 {
				sb.WriteString("\n### Failed Tasks\n")
				for _, t := range plan.SubTasks {
					if t.Status == working.TaskStatusFailed {
						reason := t.FinishedReson
						if reason == "" {
							reason = "(no reason)"
						}
						if len(reason) > 200 {
							reason = reason[:200] + "..."
						}
						sb.WriteString(fmt.Sprintf("- %s (goal: %s): %s\n", t.ID, t.Goal, reason))
					}
				}
			}
			sb.WriteString("\nContinue from the current state above.\n\n")
		}
	}

	sb.WriteString(prompts.PlannerPrompt(status))
	return sb.String()
}
