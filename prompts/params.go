package prompts

// ReviewParams fills the TypeReview template.
type ReviewParams struct {
	OriginalGoal   string
	PlanSummary    string
	RecentActivity string
}

// WorkerParams fills the TypeWorker template.
type WorkerParams struct {
	Goal   string
	Action string
}

// CompressToolParams fills the TypeCompressTool template.
type CompressToolParams struct {
	ToolName string
	FieldKey string
	OrigLen  int
	Raw      string
}

// SemanticJudgeParams fills the TypeSemanticJudge template.
type SemanticJudgeParams struct {
	OldVal string
	NewVal string
}

// PlanConfirmParams fills the TypePlanConfirm template.
type PlanConfirmParams struct {
	Name  string
	ID    string
	Tasks []PlanConfirmTaskParams
}

// PlanConfirmTaskParams describes a single task for the confirm template.
type PlanConfirmTaskParams struct {
	Index  int
	ID     string
	Goal   string
	Action string
	Deps   string
}

// PlanStatusParams fills the TypePlanStatus template.
type PlanStatusParams struct {
	Name        string
	Status      string
	Done        int
	Total       int
	Failed      int
	FailedTasks []PlanFailedTaskParams
}

// PlanFailedTaskParams describes a failed task for the status template.
type PlanFailedTaskParams struct {
	ID     string
	Goal   string
	Reason string
}
