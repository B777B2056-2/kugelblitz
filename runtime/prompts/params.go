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
