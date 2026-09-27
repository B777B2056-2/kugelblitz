package prompts

import (
	"bytes"
	"fmt"
	"text/template"

	"github.com/B777B2056-2/kugelblitz/constants"
)

// Type enumerates all available prompts.
type Type int

const (
	TypeReview        Type = iota // dynamic → ReviewParams
	TypeWorker                    // dynamic → WorkerParams
	TypeCompressTool              // dynamic → CompressToolParams
	TypeSemanticJudge             // dynamic → SemanticJudgeParams
	TypePlanConfirm               // dynamic → PlanConfirmParams
	TypePlanStatus                // dynamic → PlanStatusParams
	TypeSummarize                 // dynamic → SummarizeParams
	TypeMemoryScore               // dynamic → MemoryScoreParams
	TypeMemoryReflect             // dynamic → MemoryReflectParams
	TypeExtract                   // dynamic → ExtractParams
)

// Factory produces prompt strings from typed templates.
// text/template handles parameter escaping — % signs in user content
// (e.g. code snippets) are safe and never misinterpreted.
type Factory struct {
	tmpls map[Type]*template.Template
}

// DefaultFactory is the shared singleton. Callers can also create isolated
// instances via NewFactory for testing.
var DefaultFactory = NewFactory()

// NewFactory creates a Factory with all templates pre-parsed.
// Panics at init time if any template syntax is invalid.
func NewFactory() *Factory {
	f := &Factory{tmpls: make(map[Type]*template.Template)}
	f.mustRegister(TypeReview, reviewTmpl)
	f.mustRegister(TypeWorker, workerTmpl)
	f.mustRegister(TypeCompressTool, compressToolTmpl)
	f.mustRegister(TypeSemanticJudge, semanticJudgeTmpl)
	f.mustRegister(TypePlanConfirm, planConfirmTmpl)
	f.mustRegister(TypePlanStatus, planStatusTmpl)
	f.mustRegister(TypeSummarize, summarizeTmpl)
	f.mustRegister(TypeMemoryScore, memoryScoreTmpl)
	f.mustRegister(TypeMemoryReflect, memoryReflectTmpl)
	f.mustRegister(TypeExtract, extractTmpl)
	return f
}

// Render fills the template for pt with params and returns the result.
func (f *Factory) Render(pt Type, params any) (string, error) {
	tmpl, ok := f.tmpls[pt]
	if !ok {
		return "", fmt.Errorf("unknown prompt type: %s", pt.String())
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, params); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// MustRender is like Render but panics on error. Safe for static prompts
// and for dynamic prompts whose params have been validated.
func (f *Factory) MustRender(pt Type, params any) string {
	s, err := f.Render(pt, params)
	if err != nil {
		panic("prompt render: " + err.Error())
	}
	return s
}

func (f *Factory) mustRegister(pt Type, text string) {
	tmpl, err := template.New(pt.String()).Parse(text)
	if err != nil {
		panic("prompt template parse: " + err.Error())
	}
	f.tmpls[pt] = tmpl
}

// String returns a human-readable name for the prompt type.
func (pt Type) String() string {
	switch pt {
	case TypeReview:
		return "Review"
	case TypeWorker:
		return "Worker"
	case TypeCompressTool:
		return "CompressTool"
	case TypeSemanticJudge:
		return "SemanticJudge"
	case TypeSummarize:
		return "Summarize"
	case TypeMemoryScore:
		return "MemoryScore"
	case TypeMemoryReflect:
		return "MemoryReflect"
	case TypeExtract:
		return "Extract"
	default:
		return "Unknown"
	}
}

// plannerTemplates maps PlanState to raw prompt text (static, no params).
var plannerTemplates = map[constants.PlanState]string{
	constants.PlanStateIntent:    plannerIntentTmpl,
	constants.PlanStateInit:      plannerInitTmpl,
	constants.PlanStateDirect:    plannerDirectTmpl,
	constants.PlanStateConfirmed: plannerConfirmedTmpl,
	constants.PlanStateDoing:     plannerExecuteTmpl,
	constants.PlanStateUpdating:  plannerAdaptTmpl,
	constants.PlanStateDone:      plannerFinishTmpl,
	constants.PlanStateFailed:    plannerFinishTmpl,
}

// PlannerPrompt returns the prompt template for a given PlanState.
func PlannerPrompt(status constants.PlanState) string {
	if tmpl, ok := plannerTemplates[status]; ok {
		return tmpl
	}
	return plannerInitTmpl
}
