package constants

type MultiModalType string

const (
	MultiModalTypeUnknown MultiModalType = "unknown"
	MultiModalTypeText    MultiModalType = "text"
	MultiModalTypePDF     MultiModalType = "pdf"
	MultiModalTypeWord    MultiModalType = "word"
	MultiModalTypeExcel   MultiModalType = "excel"
	MultiModalTypeImage   MultiModalType = "image"
	MultiModalTypeVideo   MultiModalType = "video"
	MultiModalTypeAudio   MultiModalType = "audio"
)

type RoleType string

const (
	RoleSystem    RoleType = "system"
	RoleUser      RoleType = "user"
	RoleTool      RoleType = "tool"
	RoleAssistant RoleType = "assistant"
)

// PlanState represents the lifecycle state of a plan.
type PlanState string

const (
	PlanStateNone      PlanState = "none"      // empty status
	PlanStateIntent    PlanState = "intent"    // intent recognition (phase 1)
	PlanStateInit      PlanState = "init"      // no plan exists yet
	PlanStateDirect    PlanState = "direct"    // simple task, execute directly
	PlanStateConfirmed PlanState = "confirmed" // user approved, about to execute
	PlanStateRejected  PlanState = "rejected"  // user rejected
	PlanStateDoing     PlanState = "doing"     // executing tasks
	PlanStateUpdating  PlanState = "update"    // adapting after failure
	PlanStateDone      PlanState = "done"      // all tasks completed
	PlanStateFailed    PlanState = "failed"    // unrecoverable
)

// AgentIdentity identifies which agent made an LLM call.
// It is passed as the first argument to all AgentEventHooks model callbacks.
type AgentIdentity string

const (
	AgentMain     AgentIdentity = "main"     // main ReAct loop, all FSM states
	AgentWorker   AgentIdentity = "worker"   // DAG task execution
	AgentReviewer AgentIdentity = "reviewer" // goal drift detection
)
