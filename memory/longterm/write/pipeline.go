package write

import (
	"context"
	"time"

	"github.com/B777B2056-2/kugelblitz/llm"
	"github.com/B777B2056-2/kugelblitz/memory/longterm"
	"github.com/B777B2056-2/kugelblitz/memory/pipeline"
	memorytypes "github.com/B777B2056-2/kugelblitz/memory/types"
)

// WritePipeline orchestrates the 4-stage memory write process:
//  1. Extract – LLM extracts all memories as FactCandidates
//  2. Resolve – conflict resolution against existing MEMORY.md items
//  3. Dedup   – semantic dedup against existing items and batch peers
//  4. Store   – write to MEMORY.md, then trigger ChromaDB index rebuild
type WritePipeline struct {
	extractor *Extractor
	resolver  *longterm.ConflictResolver
	dedup     *longterm.Deduplicator
	ltm       *longterm.LongTermMemory
	indexMgr  *longterm.IndexManager
}

// NewWritePipeline creates a configured pipeline.
func NewWritePipeline(
	caller *llm.Caller,
	ltm *longterm.LongTermMemory,
	indexMgr *longterm.IndexManager,
	confidenceGap float64,
) *WritePipeline {
	return &WritePipeline{
		extractor: NewExtractor(caller),
		resolver:  longterm.NewConflictResolver(ltm, confidenceGap),
		dedup:     longterm.NewDeduplicator(ltm),
		ltm:       ltm,
		indexMgr:  indexMgr,
	}
}

// Run executes the full pipeline synchronously as an ordered set of steps.
// After storing to MEMORY.md, it asynchronously triggers a ChromaDB index rebuild.
func (p *WritePipeline) Run(ctx context.Context, ec *ExtractionContext) (*memorytypes.PipelineResult, error) {
	start := time.Now()
	result := &memorytypes.PipelineResult{}

	var (
		fullResult    *ExtractionFullResult
		resolvedFacts []memorytypes.MemoryItem
		dedupResult   *longterm.DedupResult
	)

	steps := pipeline.New(
		pipeline.Step{Name: "extract", Run: func(ctx context.Context) error {
			fr, usage, err := p.extractor.ExtractFull(ctx, ec)
			if err != nil {
				// Fallback to legacy Extract if full extraction is not supported.
				candidates, usage2, err2 := p.extractor.Extract(ctx, ec)
				if err2 != nil {
					return err2
				}
				usage = usage2
				fr = &ExtractionFullResult{Items: candidates}
			}
			result.ExtractionUsage = usage
			result.ItemsExtracted = len(fr.Items)
			fullResult = fr
			return nil
		}},
		pipeline.Step{Name: "resolve", Run: func(ctx context.Context) error {
			resolvedFacts = p.resolver.Resolve(fullResult.Items)
			return nil
		}},
		pipeline.Step{Name: "dedup", Run: func(ctx context.Context) error {
			dedupResult = p.dedup.DedupItems(resolvedFacts)
			result.ItemsRejected = dedupResult.Rejected
			return nil
		}},
		pipeline.Step{Name: "store", Run: func(ctx context.Context) error {
			if len(dedupResult.Accepted) == 0 {
				return nil
			}
			if err := p.ltm.BulkStore(dedupResult.Accepted); err != nil {
				return err
			}
			result.ItemsStored = len(dedupResult.Accepted)
			return nil
		}},
		pipeline.Step{Name: "graph", Run: func(ctx context.Context) error {
			if g := p.ltm.Graph(); g != nil && (len(fullResult.Entities) > 0 || len(fullResult.Relationships) > 0) {
				g.UpsertRelationships(ctx, fullResult.Entities, fullResult.Relationships)
			}
			return nil
		}},
		pipeline.Step{Name: "index", Run: func(ctx context.Context) error {
			if p.indexMgr == nil {
				return nil
			}
			go func() {
				rebuildCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Minute)
				defer cancel()
				_ = p.indexMgr.Rebuild(rebuildCtx)
			}()
			return nil
		}},
	)

	if err := steps.Run(ctx); err != nil {
		return result, err
	}
	result.Duration = time.Since(start)
	return result, nil
}

// ExtractFromSession builds an ExtractionContext from session data and runs the pipeline.
func (p *WritePipeline) ExtractFromSession(ctx context.Context, input memorytypes.ExtractionInput) (*memorytypes.PipelineResult, error) {
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
