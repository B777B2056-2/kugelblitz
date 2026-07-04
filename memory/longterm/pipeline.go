package longterm

import (
	"context"
	"time"

	"github.com/B777B2056-2/kugelblitz/core"
)

// PipelineResult aggregates metrics from a write pipeline run.
type PipelineResult struct {
	ItemsExtracted  int // Raw fact candidates from LLM
	ItemsStored     int // All persisted to MEMORY.md
	ItemsConflicts  int // Conflicts detected during resolution
	ItemsRejected   int // All rejected by dedup
	NeedsHuman      int // Conflicts deferred for human review
	Duration        time.Duration
	ExtractionUsage *core.Usage
}

// WritePipeline orchestrates the 4-stage memory write process:
//  1. Extract – LLM extracts all memories as FactCandidates
//  2. Resolve – conflict resolution against existing MEMORY.md items
//  3. Dedup   – semantic dedup against existing items and batch peers
//  4. Store   – write to MEMORY.md, then trigger ChromaDB index rebuild
type WritePipeline struct {
	provider  core.ILMProvider
	extractor *Extractor
	resolver  *ConflictResolver
	dedup     *Deduplicator
	ltm       *LongTermMemory
	indexMgr  *IndexManager
}

// NewWritePipeline creates a configured pipeline.
func NewWritePipeline(
	provider core.ILMProvider,
	ltm *LongTermMemory,
	indexMgr *IndexManager,
	confidenceGap float64,
) *WritePipeline {
	return &WritePipeline{
		provider:  provider,
		extractor: NewExtractor(provider),
		resolver:  NewConflictResolver(ltm, confidenceGap),
		dedup:     NewDeduplicator(ltm),
		ltm:       ltm,
		indexMgr:  indexMgr,
	}
}

// Run executes the full pipeline synchronously. After storing to MEMORY.md,
// it asynchronously triggers a ChromaDB index rebuild.
func (p *WritePipeline) Run(ctx context.Context, ec *ExtractionContext) (*PipelineResult, error) {
	start := time.Now()
	result := &PipelineResult{}

	// Stage 1: Extract
	fullResult, usage, err := p.extractor.ExtractFull(ctx, ec)
	if err != nil {
		// Fallback to legacy Extract if full extraction is not supported
		candidates, usage2, err2 := p.extractor.Extract(ctx, ec)
		if err2 != nil {
			return result, err2
		}
		usage = usage2
		fullResult = &ExtractionFullResult{Items: candidates}
	}
	result.ExtractionUsage = usage
	result.ItemsExtracted = len(fullResult.Items)

	// Stage 2: Conflict Resolution
	resolvedFacts := p.resolver.Resolve(fullResult.Items)

	// Stage 3: Dedup
	dedupResult := p.dedup.DedupItems(resolvedFacts)
	result.ItemsRejected = dedupResult.Rejected

	// Stage 4: Store → MEMORY.md
	if len(dedupResult.Accepted) > 0 {
		if err := p.ltm.BulkStore(dedupResult.Accepted); err != nil {
			return result, err
		}
		result.ItemsStored = len(dedupResult.Accepted)
	}

	// Stage 5: Update entity-relationship graph
	if g := p.ltm.Graph(); g != nil && (len(fullResult.Entities) > 0 || len(fullResult.Relationships) > 0) {
		g.UpsertRelationships(ctx, fullResult.Entities, fullResult.Relationships)
	}

	// Stage 6: Rebuild ChromaDB index (async)
	if p.indexMgr != nil {
		go func() { _ = p.indexMgr.Rebuild(context.Background()) }()
	}

	result.Duration = time.Since(start)
	return result, nil
}

// ExtractionInput carries session data needed by ExtractFromSession.
type ExtractionInput struct {
	Conversation   []core.Message
	SessionSummary string
	Goal           string
}

// ExtractFromSession builds an ExtractionContext from session data and runs the pipeline.
func (p *WritePipeline) ExtractFromSession(ctx context.Context, input ExtractionInput) (*PipelineResult, error) {
	if p.ltm == nil {
		return nil, nil
	}
	ec := &ExtractionContext{
		Conversation:   input.Conversation,
		SessionSummary: input.SessionSummary,
		ExistingItems:  p.ltm.All(),
		UserMessage:    input.Goal,
	}
	return p.Run(ctx, ec)
}
