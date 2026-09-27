// Package types holds the pure value types shared across the memory subsystem.
// Keeping them in a leaf package lets consumers (tools, runtime) reference
// memory value types without importing the longterm implementation, mirroring
// the core/types and runtime/engine/types split.
package types

import (
	"time"

	coretypes "github.com/B777B2056-2/kugelblitz/core/types"
)

// MemoryItem is a single versioned entry in long-term memory.
// Confidence decays exponentially over time; new items start at 1.0.
// When a conflict occurs (same section+key, different value),
// the version with higher confidence wins.
type MemoryItem struct {
	Section    string
	Key        string
	Value      string
	Version    int       // starts at 1
	Confidence float64   // 0.0–1.0, decays over time
	UpdatedAt  time.Time // last update timestamp
}

// MemoryItemCandidate is a raw fact produced by the LLM before conflict
// resolution and dedup. It carries extraction metadata (source evidence,
// suggested confidence) that the resolved MemoryItem drops.
type MemoryItemCandidate struct {
	Section             string  `json:"section"`
	Key                 string  `json:"key"`
	Value               string  `json:"value"`
	SourceEvidence      string  `json:"source_evidence"`
	SuggestedConfidence float64 `json:"suggested_confidence"`
}

// PipelineResult aggregates metrics from a write pipeline run.
type PipelineResult struct {
	ItemsExtracted  int // Raw fact candidates from LLM
	ItemsStored     int // All persisted to MEMORY.md
	ItemsConflicts  int // Conflicts detected during resolution
	ItemsRejected   int // All rejected by dedup
	NeedsHuman      int // Conflicts deferred for human review
	Duration        time.Duration
	ExtractionUsage *coretypes.Usage
}

// ExtractionInput carries session data needed by ExtractFromSession.
type ExtractionInput struct {
	Conversation   []coretypes.Message
	SessionSummary string
	Goal           string
}

// Entity is a node in the long-term memory knowledge graph.
type Entity struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Type   string   `json:"type"`   // "language", "file", "concept", "person", "project", "bug", ...
	Labels []string `json:"labels"` // tags for filtering
}

// Relationship is a directed edge between two entities.
type Relationship struct {
	ID     string  `json:"id"`
	From   string  `json:"from"`   // entity ID
	To     string  `json:"to"`     // entity ID
	Type   string  `json:"type"`   // "uses", "depends_on", "mentions", "causes", "contains", ...
	Weight float64 `json:"weight"` // 1.0 = explicit, < 1.0 = inferred
}
