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

// PlanStatus represents the lifecycle state of a plan.
type PlanStatus string

const (
	PlanStatusNone     PlanStatus = "none"     // empty status
	PlanStatusIntent   PlanStatus = "intent"   // intent recognition (phase 1)
	PlanStatusInit     PlanStatus = "init"     // no plan exists yet
	PlanStatusDirect   PlanStatus = "direct"   // simple task, execute directly
	PlanStatusConfirmed PlanStatus = "confirmed" // user approved, about to execute
	PlanStatusRejected PlanStatus = "rejected"  // user rejected
	PlanStatusDoing    PlanStatus = "doing"     // executing tasks
	PlanStatusUpdating PlanStatus = "update"    // adapting after failure
	PlanStatusDone     PlanStatus = "done"      // all tasks completed
	PlanStatusFailed   PlanStatus = "failed"    // unrecoverable
)
