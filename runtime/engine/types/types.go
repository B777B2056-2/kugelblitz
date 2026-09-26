// Package types holds pure value types shared across the engine packages.
// It is a leaf package (imports only core) so fsm can reference these result
// types without depending on the dag or infra implementations.
package types

import "github.com/B777B2056-2/kugelblitz/core"

// BatchResult reports the outcome of one DAG ExecuteBatch call.
type BatchResult struct {
	Batched   bool // at least one task was executed
	HasFailed bool // at least one task in this batch failed
	AllDone   bool // all tasks are terminal (done or failed)
}

// ReviewResult reports a goal-drift review outcome.
type ReviewResult struct {
	Drift      bool
	Reason     string
	Suggestion string
	Usage      *core.Usage
}
