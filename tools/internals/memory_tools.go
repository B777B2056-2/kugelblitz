package internals

import (
	"context"
	"fmt"

	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/B777B2056-2/kugelblitz/memory"
	"github.com/B777B2056-2/kugelblitz/tools"
)

func RegisterMemoryTools(ltm *memory.LongTermMemory) {
	tools.RegisterAll(
		&MemoryStore{ltm: ltm},
		&MemorySearch{ltm: ltm},
		&MemoryGetSection{ltm: ltm},
	)
}

// ---- MemoryStore ----

type MemoryStore struct{ ltm *memory.LongTermMemory }

func (t *MemoryStore) Definition() core.ToolDefinition {
	return core.ToolDefinition{
		Name:        "memory_store",
		Description: "Store a fact in long-term memory. If the key already exists with a different value, confidence-based conflict resolution is applied: higher confidence wins. New facts start with confidence 1.0.",
		JsonSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"section": map[string]any{"type": "string", "description": "Section name (e.g. 'user_preferences', 'project_facts')"},
				"key":     map[string]any{"type": "string", "description": "Fact key"},
				"value":   map[string]any{"type": "string", "description": "Fact value"},
			},
			"required": []string{"section", "key", "value"},
		},
		OutputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"accepted":   map[string]any{"type": "boolean", "description": "Whether the new value was accepted"},
				"section":    map[string]any{"type": "string"},
				"key":        map[string]any{"type": "string"},
				"value":      map[string]any{"type": "string", "description": "Current winning value"},
				"confidence": map[string]any{"type": "number", "description": "Confidence of winning value (0-1)"},
				"version":    map[string]any{"type": "integer", "description": "Version of winning value"},
				"conflict":   map[string]any{"type": "object", "description": "If conflict: {old_value, old_confidence, rejected_value}"},
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
			"old_value":       conflict.Value,
			"accepted":        false,
			"rejected_value":  value,
		}
	}
	return tools.SuccessResult(detail.ID, "memory_store", result)
}

// ---- MemorySearch ----

type MemorySearch struct{ ltm *memory.LongTermMemory }

func (t *MemorySearch) Definition() core.ToolDefinition {
	return core.ToolDefinition{
		Name:        "memory_search",
		Description: "Search long-term memory. Modes: 'semantic' (ChromaDB), 'bm25' (keyword), 'hybrid'. Default: 'bm25'. Omit query for all.",
		JsonSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string", "description": "Search term (optional)"},
				"mode":  map[string]any{"type": "string", "description": "'semantic', 'bm25', or 'hybrid' (default: 'bm25')"},
			},
		},
		OutputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"results": map[string]any{"type": "array", "description": "List of {section, key, value, confidence, version} facts"},
				"count":   map[string]any{"type": "integer"},
			},
		},
	}
}

func (t *MemorySearch) Execute(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
	query, _ := tools.Arg(detail, "query")
	modeStr, _ := tools.Arg(detail, "mode")
	mode := memory.SearchBM25
	switch modeStr {
	case "semantic":
		mode = memory.SearchSemantic
	case "hybrid":
		mode = memory.SearchHybrid
	}
	facts := t.ltm.SearchWithMode(query, mode)

	results := make([]map[string]any, len(facts))
	for i, f := range facts {
		results[i] = map[string]any{
			"section": f.Section, "key": f.Key, "value": f.Value,
			"confidence": f.Confidence, "version": f.Version,
		}
	}
	return tools.SuccessResult(detail.ID, "memory_search", map[string]any{
		"results": results, "count": len(results),
	})
}

// ---- MemoryGetSection ----

type MemoryGetSection struct{ ltm *memory.LongTermMemory }

func (t *MemoryGetSection) Definition() core.ToolDefinition {
	return core.ToolDefinition{
		Name:        "memory_get_section",
		Description: "Get all facts in a memory section with their confidence scores.",
		JsonSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"section": map[string]any{"type": "string", "description": "Section name"},
			},
			"required": []string{"section"},
		},
		OutputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"entries": map[string]any{"type": "object", "description": "Map of key → {value, confidence, version}"},
			},
		},
	}
}

func (t *MemoryGetSection) Execute(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
	section, _ := tools.Arg(detail, "section")
	if section == "" {
		return tools.ErrorResult(detail.ID, "memory_get_section", fmt.Errorf("section is required"))
	}
	facts := t.ltm.GetSection(section)
	entries := make(map[string]any, len(facts))
	for _, f := range facts {
		entries[f.Key] = map[string]any{
			"value": f.Value, "confidence": f.Confidence, "version": f.Version,
		}
	}
	return tools.SuccessResult(detail.ID, "memory_get_section", map[string]any{
		"entries": entries,
	})
}
