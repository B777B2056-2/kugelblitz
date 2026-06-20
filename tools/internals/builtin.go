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
	}
}

// RegisterAll registers all built-in tools with the global ToolRegistry.
// Called automatically via init(); you can also call it explicitly to re-register after a Reset().
func RegisterAll() {
	tools.RegisterAll(All()...)
}

func init() {
	RegisterAll()
}

// dirOf returns the directory portion of a path.
func dirOf(path string) string {
	return filepath.Dir(path)
}
