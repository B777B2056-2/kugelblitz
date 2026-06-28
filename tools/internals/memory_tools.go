package internals

import (
	"context"
	"fmt"

	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/B777B2056-2/kugelblitz/memory/longterm"
	"github.com/B777B2056-2/kugelblitz/persist"
	"github.com/B777B2056-2/kugelblitz/tools"
)

// RegisterMemoryTools registers all memory-related tools.
func RegisterMemoryTools(ltm *longterm.LongTermMemory, indexMgr *longterm.IndexManager, pipeline *longterm.WritePipeline) {
	t := []tools.Tool{
		&MemoryStore{ltm: ltm},
		&MemorySearch{ltm: ltm, indexMgr: indexMgr},
		&MemoryGetSection{ltm: ltm},
		&MemoryRemove{ltm: ltm},
		&MemoryListSections{ltm: ltm},
		&MemoryStats{ltm: ltm, indexMgr: indexMgr},
		&MemoryResolveConflict{ltm: ltm},
	}
	if pipeline != nil {
		t = append(t, &MemoryExtract{pipeline: pipeline})
	}
	tools.RegisterAll(t...)
}

// BuildExtractContextFunc builds an ExtractionContext for the memory_extract tool.
var BuildExtractContext func() *longterm.ExtractionContext

// ---- MemoryStore ----

type MemoryStore struct{ ltm *longterm.LongTermMemory }

func (t *MemoryStore) Definition() core.ToolDefinition {
	return core.ToolDefinition{
		Name:        "memory_store",
		Description: "Store a fact in long-term memory (MEMORY.md).",
		JsonSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"section": map[string]any{"type": "string", "description": "Section: user_preferences, project_facts, episodic, lessons, patterns"},
				"key":     map[string]any{"type": "string", "description": "MemoryItem key"},
				"value":   map[string]any{"type": "string", "description": "MemoryItem value"},
			},
			"required": []string{"section", "key", "value"},
		},
		OutputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"accepted":   map[string]any{"type": "boolean"},
				"section":    map[string]any{"type": "string"},
				"key":        map[string]any{"type": "string"},
				"value":      map[string]any{"type": "string"},
				"confidence": map[string]any{"type": "number"},
				"version":    map[string]any{"type": "integer"},
				"conflict":   map[string]any{"type": "object"},
			},
		},
	}
}

func (t *MemoryStore) Execute(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
	section, _ := tools.Arg(detail, "section")
	key, _ := tools.Arg(detail, "key")
	value, _ := tools.Arg(detail, "value")
	if section == "" || key == "" || value == "" {
		return tools.ErrorResult(detail.ID, "memory_store", fmt.Errorf("section, key, and value are required"))
	}
	winner, conflict, err := t.ltm.Store(section, key, value)
	if err != nil {
		return tools.ErrorResult(detail.ID, "memory_store", err)
	}
	result := map[string]any{
		"accepted":   conflict == nil,
		"section":    winner.Section,
		"key":        winner.Key,
		"value":      winner.Value,
		"confidence": winner.Confidence,
		"version":    winner.Version,
	}
	if conflict != nil {
		result["conflict"] = map[string]any{
			"old_value": conflict.Value, "accepted": false, "rejected_value": value,
		}
	}
	return tools.SuccessResult(detail.ID, "memory_store", result)
}

// ---- MemorySearch ----

type MemorySearch struct {
	ltm      *longterm.LongTermMemory
	indexMgr *longterm.IndexManager
}

func (t *MemorySearch) Definition() core.ToolDefinition {
	return core.ToolDefinition{
		Name:        "memory_search",
		Description: "Search long-term memory. Uses ChromaDB for semantic search; falls back to keyword search on MEMORY.md. Modes: 'semantic', 'bm25', 'hybrid'.",
		JsonSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string", "description": "Search term"},
				"mode":  map[string]any{"type": "string", "description": "'semantic', 'bm25', or 'hybrid'"},
			},
		},
		OutputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"results": map[string]any{"type": "array", "description": "List of {section, key, value, confidence, version}"},
				"count":   map[string]any{"type": "integer"},
			},
		},
	}
}

func (t *MemorySearch) Execute(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
	query, _ := tools.Arg(detail, "query")
	modeStr, _ := tools.Arg(detail, "mode")
	mode := persist.SearchBM25
	switch modeStr {
	case "semantic":
		mode = persist.SearchSemantic
	case "hybrid":
		mode = persist.SearchHybrid
	}

	var items []longterm.MemoryItem
	if t.indexMgr != nil {
		items = t.indexMgr.Search(ctx, query, mode, 10)
	} else {
		items = t.ltm.SearchWithMode(query, mode)
	}

	results := make([]map[string]any, len(items))
	for i, f := range items {
		results[i] = map[string]any{
			"section": f.Section, "key": f.Key, "value": f.Value,
			"confidence": f.Confidence, "version": f.Version,
		}
	}
	output := map[string]any{"results": results, "count": len(results)}

	// Graph query when needGraph is set
	if ng, ok := detail.Args["needGraph"]; ok && ng != nil {
		if needGraph, ok2 := ng.(bool); ok2 && needGraph {
			if g := t.ltm.Graph(); g != nil {
				entities := g.SearchEntities(query, 10)
				entityMaps := make([]map[string]any, len(entities))
				entityIDs := make(map[string]bool)
				for i, e := range entities {
					entityMaps[i] = map[string]any{"id": e.ID, "name": e.Name, "type": e.Type, "labels": e.Labels}
					entityIDs[e.ID] = true
				}
				var graphRels []map[string]any
				seenRel := make(map[string]bool)
				for _, e := range entities {
					_, rels := g.Neighbors(e.ID)
					for _, r := range rels {
						if !seenRel[r.ID] && (entityIDs[r.From] || entityIDs[r.To]) {
							seenRel[r.ID] = true
							graphRels = append(graphRels, map[string]any{
								"id": r.ID, "from": r.From, "to": r.To, "type": r.Type, "weight": r.Weight,
							})
						}
					}
				}
				output["graph"] = map[string]any{"entities": entityMaps, "relationships": graphRels}
			}
		}
	}

	return tools.SuccessResult(detail.ID, "memory_search", output)
}

// ---- MemoryGetSection ----

type MemoryGetSection struct{ ltm *longterm.LongTermMemory }

func (t *MemoryGetSection) Definition() core.ToolDefinition {
	return core.ToolDefinition{
		Name:        "memory_get_section",
		Description: "Get all items in a memory section.",
		JsonSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"section": map[string]any{"type": "string"}},
			"required":   []string{"section"},
		},
		OutputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"entries": map[string]any{"type": "object"},
			},
		},
	}
}

func (t *MemoryGetSection) Execute(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
	section, _ := tools.Arg(detail, "section")
	if section == "" {
		return tools.ErrorResult(detail.ID, "memory_get_section", fmt.Errorf("section is required"))
	}
	items := t.ltm.GetSection(section)
	entries := make(map[string]any, len(items))
	for _, f := range items {
		entries[f.Key] = map[string]any{
			"value": f.Value, "confidence": f.Confidence, "version": f.Version,
		}
	}
	return tools.SuccessResult(detail.ID, "memory_get_section", map[string]any{"entries": entries})
}

// ---- MemoryRemove ----

type MemoryRemove struct{ ltm *longterm.LongTermMemory }

func (t *MemoryRemove) Definition() core.ToolDefinition {
	return core.ToolDefinition{
		Name:        "memory_remove",
		Description: "Permanently delete a fact from long-term memory.",
		JsonSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"section": map[string]any{"type": "string"}, "key": map[string]any{"type": "string"}},
			"required":   []string{"section", "key"},
		},
		OutputSchema: map[string]any{"type": "object", "properties": map[string]any{"removed": map[string]any{"type": "boolean"}}},
	}
}

func (t *MemoryRemove) Execute(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
	section, _ := tools.Arg(detail, "section")
	key, _ := tools.Arg(detail, "key")
	if err := t.ltm.Remove(section, key); err != nil {
		return tools.ErrorResult(detail.ID, "memory_remove", err)
	}
	return tools.SuccessResult(detail.ID, "memory_remove", map[string]any{"removed": true})
}

// ---- MemoryListSections ----

type MemoryListSections struct{ ltm *longterm.LongTermMemory }

func (t *MemoryListSections) Definition() core.ToolDefinition {
	return core.ToolDefinition{
		Name:        "memory_list_sections",
		Description: "List all memory sections with fact counts.",
		JsonSchema:  map[string]any{"type": "object", "properties": map[string]any{}},
		OutputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"sections": map[string]any{"type": "object"}},
		},
	}
}

func (t *MemoryListSections) Execute(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
	sections := t.ltm.ListSections()
	counts := make(map[string]any, len(sections))
	for k, v := range sections {
		counts[k] = v
	}
	return tools.SuccessResult(detail.ID, "memory_list_sections", map[string]any{"sections": counts})
}

// ---- MemoryStats ----

type MemoryStats struct {
	ltm      *longterm.LongTermMemory
	indexMgr *longterm.IndexManager
}

func (t *MemoryStats) Definition() core.ToolDefinition {
	return core.ToolDefinition{
		Name:        "memory_stats",
		Description: "Get aggregate statistics about long-term memory.",
		JsonSchema:  map[string]any{"type": "object", "properties": map[string]any{}},
		OutputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"total_facts":    map[string]any{"type": "integer"},
				"sections":       map[string]any{"type": "integer"},
				"avg_confidence": map[string]any{"type": "number"},
				"indexed":        map[string]any{"type": "boolean"},
			},
		},
	}
}

func (t *MemoryStats) Execute(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
	total, sections, avgConf := t.ltm.Stats()
	indexed := t.indexMgr != nil && t.indexMgr.IsAvailable()
	return tools.SuccessResult(detail.ID, "memory_stats", map[string]any{
		"total_facts": total, "sections": sections, "avg_confidence": avgConf, "indexed": indexed,
	})
}

// ---- MemoryResolveConflict ----

type MemoryResolveConflict struct{ ltm *longterm.LongTermMemory }

func (t *MemoryResolveConflict) Definition() core.ToolDefinition {
	return core.ToolDefinition{
		Name:        "memory_resolve_conflict",
		Description: "Resolve a pending memory conflict. Use after asking the human which value to keep.",
		JsonSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"section":  map[string]any{"type": "string"},
				"key":      map[string]any{"type": "string"},
				"decision": map[string]any{"type": "string", "description": "'keep_new' or 'keep_old'"},
			},
			"required": []string{"section", "key", "decision"},
		},
		OutputSchema: map[string]any{"type": "object", "properties": map[string]any{"resolved": map[string]any{"type": "boolean"}}},
	}
}

func (t *MemoryResolveConflict) Execute(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
	section, _ := tools.Arg(detail, "section")
	key, _ := tools.Arg(detail, "key")
	decision, _ := tools.Arg(detail, "decision")
	if err := t.ltm.ResolveConflict(section, key, decision); err != nil {
		return tools.ErrorResult(detail.ID, "memory_resolve_conflict", err)
	}
	return tools.SuccessResult(detail.ID, "memory_resolve_conflict", map[string]any{"resolved": true})
}

// ---- MemoryExtract ----

type MemoryExtract struct {
	pipeline *longterm.WritePipeline
}

func (t *MemoryExtract) Definition() core.ToolDefinition {
	return core.ToolDefinition{
		Name: "memory_extract",
		Description: "Extract and store long-term memories from the current session. " +
			"All memories are stored in MEMORY.md; ChromaDB index is rebuilt automatically.",
		JsonSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"focus": map[string]any{"type": "string", "description": "Optional extraction focus hint"},
			},
		},
		OutputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"facts_extracted": map[string]any{"type": "integer"},
				"facts_stored":    map[string]any{"type": "integer"},
				"facts_conflicts": map[string]any{"type": "integer"},
				"needs_human":     map[string]any{"type": "integer"},
			},
		},
	}
}

func (t *MemoryExtract) Execute(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
	if BuildExtractContext == nil {
		return tools.ErrorResult(detail.ID, "memory_extract", fmt.Errorf("extraction context not configured"))
	}
	ec := BuildExtractContext()
	if ec == nil {
		return tools.ErrorResult(detail.ID, "memory_extract", fmt.Errorf("no session context available"))
	}
	result, err := t.pipeline.Run(ctx, ec)
	if err != nil {
		return tools.ErrorResult(detail.ID, "memory_extract", err)
	}
	return tools.SuccessResult(detail.ID, "memory_extract", map[string]any{
		"facts_extracted": result.ItemsExtracted,
		"facts_stored":    result.ItemsStored,
		"facts_conflicts": result.ItemsConflicts,
		"needs_human":     result.NeedsHuman,
	})
}
