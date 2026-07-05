package internals

import (
	"path/filepath"

	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/B777B2056-2/kugelblitz/tools"
)

// All returns all built-in tools. Call RegisterAll at startup to make them
// available to agents via the global ToolRegistry.
func All() []tools.Tool {
	return []tools.Tool{
		&FileRead{},
		&FileWrite{},
		&FileDelete{},
		&FileCopy{},
		&DirCreate{},
		&DirCopy{},
		&ShellExec{},
		&PlanCreate{},
		&PlanQuery{},
		&ConfirmPlan{},
		&TaskInsert{},
		&TaskDelete{},
		&TaskQuery{},
		&TaskStatusUpdate{},
		&PlanRollback{},
		&SetWorkMode{},
		// WorkerSpawn removed — Planner state machine spawns workers directly via code
	}
}

// RegisterAll registers all built-in tools with the global ToolRegistry.
// Called automatically via init(); you can also call it explicitly to re-register after a Reset().
func RegisterAll() {
	all := All()
	tools.RegisterAll(all...)
	for _, t := range all {
		core.GetToolRegistry().MarkAsInternal(t.Definition().Name)
	}
}

// WorkerSpawn tool is registered via internals.RegisterWorkerSpawn in plan.go.

func init() {
	RegisterAll()
}

// dirOf returns the directory portion of a path.
func dirOf(path string) string {
	return filepath.Dir(path)
}
