package internals

import (
	"path/filepath"

	"kugelblitz/tools"
)

// All returns all built-in tools. Call RegisterAll at startup to make them
// available to agents via the global ToolRegistry.
func All() []tools.Tool {
	return []tools.Tool{
		&FileRead{},
		&FileWrite{},
		&FileCopy{},
		&DirCreate{},
		&DirCopy{},
		&PlanCreate{},
		&PlanQuery{},
		&PlanStatusUpdate{},
		&TaskInsert{},
		&TaskDelete{},
		&TaskQuery{},
		&TaskStatusUpdate{},
	}
}

// RegisterAll registers all built-in tools with the global ToolRegistry.
// Called automatically via init(); you can also call it explicitly to re-register after a Reset().
func RegisterAll() {
	tools.RegisterAll(All()...)
}

// RegisterWorkerSpawn registers the worker_spawn tool and sets the factory
// that creates WorkerAgents. WorkerSpawn is NOT auto-registered by init()
// because it requires a provider reference.
//
// Usage:
//
//	internals.RegisterWorkerSpawn(func(goal, action string) (string, error) {
//	    worker := runtime.NewWorkerAgent(provider, true, []string{"file_read", "file_write"})
//	    return worker.ExecuteTask(ctx, goal, action)
//	})
func RegisterWorkerSpawn(fn WorkerFactory) {
	RegisterWorkerFactory(fn)
	tools.Register(&WorkerSpawn{})
}

func init() {
	RegisterAll()
}

// dirOf returns the directory portion of a path.
func dirOf(path string) string {
	return filepath.Dir(path)
}
