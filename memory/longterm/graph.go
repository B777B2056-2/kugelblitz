package longterm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/B777B2056-2/kugelblitz/persist"
	"github.com/B777B2056-2/kugelblitz/utils"
)

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

// EntityCandidate is an entity extracted by the LLM before graph insertion.
type EntityCandidate struct {
	Name   string   `json:"name"`
	Type   string   `json:"type"`
	Labels []string `json:"labels"`
}

// RelCandidate is a relationship extracted by the LLM before graph insertion.
type RelCandidate struct {
	From   string  `json:"from"` // entity name (resolved to ID at insertion)
	To     string  `json:"to"`   // entity name
	Type   string  `json:"type"`
	Weight float64 `json:"weight"`
}

// GraphStore is a local in-memory knowledge graph persisted as JSONL.
// Entities and relationships are extracted from conversations and
// stored under {workspace}/memory/longterm/.
type GraphStore struct {
	entities      map[string]*Entity // ID → Entity
	relationships []*Relationship
	adjOut        map[string][]*Relationship // entity ID → outgoing edges
	adjIn         map[string][]*Relationship // entity ID → incoming edges
	mu            sync.RWMutex
	backend       persist.IPersist
	path          string
}

// NewGraphStore creates a GraphStore. Pass nil backend for in-memory-only (testing).
func NewGraphStore(backend persist.IPersist, path string) *GraphStore {
	return &GraphStore{
		entities: make(map[string]*Entity),
		adjOut:   make(map[string][]*Relationship),
		adjIn:    make(map[string][]*Relationship),
		backend:  backend,
		path:     path,
	}
}

// Load restores the graph from JSONL.
func (g *GraphStore) Load(ctx context.Context) error {
	if g.backend == nil || g.path == "" {
		return nil
	}
	data, err := g.backend.Load(ctx, g.path)
	if err != nil || len(data) == 0 {
		return err
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var evt struct {
			Type   string   `json:"type"`
			ID     string   `json:"id"`
			Name   string   `json:"name"`
			Type_  string   `json:"type_"`
			Labels []string `json:"labels"`
			From   string   `json:"from"`
			To     string   `json:"to"`
			Weight float64  `json:"weight"`
		}
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			continue
		}
		switch evt.Type {
		case "entity":
			g.entities[evt.ID] = &Entity{ID: evt.ID, Name: evt.Name, Type: evt.Type_, Labels: evt.Labels}
		case "relationship":
			rel := &Relationship{ID: evt.ID, From: evt.From, To: evt.To, Type: evt.Type_, Weight: evt.Weight}
			g.relationships = append(g.relationships, rel)
			g.adjOut[rel.From] = append(g.adjOut[rel.From], rel)
			g.adjIn[rel.To] = append(g.adjIn[rel.To], rel)
		}
	}
	// Regenerate Mermaid view on startup
	return g.writeMermaid(ctx)
}

// persist saves the graph to JSONL and regenerates the Mermaid markdown view.
func (g *GraphStore) persist(ctx context.Context) error {
	if g.backend == nil || g.path == "" {
		return nil
	}
	var sb strings.Builder
	for _, e := range g.entities {
		data, _ := json.Marshal(map[string]any{
			"type": "entity", "id": e.ID, "name": e.Name, "type_": e.Type, "labels": e.Labels,
		})
		sb.WriteString(string(data) + "\n")
	}
	for _, r := range g.relationships {
		data, _ := json.Marshal(map[string]any{
			"type": "relationship", "id": r.ID, "from": r.From, "to": r.To,
			"type_": r.Type, "weight": r.Weight,
		})
		sb.WriteString(string(data) + "\n")
	}
	if err := g.backend.Store(ctx, g.path, []byte(sb.String())); err != nil {
		return err
	}
	// Regenerate human-readable Mermaid view
	return g.writeMermaid(ctx)
}

// mermaidPath derives the .md path from the JSONL path.
func (g *GraphStore) mermaidPath() string {
	if strings.HasSuffix(g.path, ".jsonl") {
		return g.path[:len(g.path)-6] + ".md"
	}
	return g.path + ".md"
}

// writeMermaid regenerates the MEMORY_GRAPH.md file with a Mermaid graph.
func (g *GraphStore) writeMermaid(ctx context.Context) error {
	g.mu.RLock()
	defer g.mu.RUnlock()

	var sb strings.Builder
	sb.WriteString("# Knowledge Graph\n\n")
	sb.WriteString("> Auto-generated from conversations. Do not edit — overwritten on every graph update.\n\n")

	// Mermaid graph
	sb.WriteString("```mermaid\ngraph LR\n")

	// Entity ID → safe display name (Mermaid node IDs can't have dots or Chinese)
	safeID := func(e *Entity) string {
		return strings.NewReplacer(".", "_", " ", "_", "-", "_").Replace(e.ID)
	}
	colorByType := map[string]string{
		"project": "#4A90D9", "language": "#7BBA3B", "file": "#D4A84B",
		"concept": "#9B59B6", "bug": "#E74C3C", "tool": "#1ABC9C", "person": "#E67E22",
		"package": "#3498DB",
	}

	// Style definitions
	for t, c := range colorByType {
		fmt.Fprintf(&sb, "    classDef %s fill:%s,stroke:#333,color:#fff\n", t, c)
	}

	// Nodes: entity ID → [display label]
	nameOf := make(map[string]string, len(g.entities))
	for _, e := range g.entities {
		sid := safeID(e)
		nameOf[e.ID] = sid
		labels := strings.Join(e.Labels, ", ")
		if labels != "" {
			labels = "<br/>" + labels
		}
		fmt.Fprintf(&sb, "    %s[%s<br/>%s%s]\n", sid, e.Name, e.Type, labels)
		fmt.Fprintf(&sb, "    class %s %s\n", sid, e.Type)
	}

	// Edges
	for _, r := range g.relationships {
		from := nameOf[r.From]
		to := nameOf[r.To]
		if from == "" || to == "" {
			continue
		}
		weight := ""
		if r.Weight < 1.0 {
			weight = fmt.Sprintf(" (w=%.1f)", r.Weight)
		}
		fmt.Fprintf(&sb, "    %s -->|%s%s| %s\n", from, r.Type, weight, to)
	}

	sb.WriteString("```\n\n")

	// Entity legend
	sb.WriteString("## Legend\n\n")
	sb.WriteString("| Entity | Type | Labels |\n")
	sb.WriteString("|--------|------|--------|\n")
	for _, e := range g.entities {
		fmt.Fprintf(&sb, "| %s | %s | %s |\n", e.Name, e.Type, strings.Join(e.Labels, ", "))
	}
	sb.WriteString("\n")

	// Relationship list
	sb.WriteString("## Relationships\n\n")
	for _, r := range g.relationships {
		from := g.entityNameLocked(r.From)
		to := g.entityNameLocked(r.To)
		fmt.Fprintf(&sb, "- **%s** → _%s_ → **%s**  `w=%.1f`\n", from, r.Type, to, r.Weight)
	}

	return g.backend.Store(ctx, g.mermaidPath(), []byte(sb.String()))
}

// entityNameLocked is like entityName but must be called under RLock.
func (g *GraphStore) entityNameLocked(id string) string {
	if e, ok := g.entities[id]; ok {
		return e.Name
	}
	return id
}

// ---- CRUD ----

// UpsertEntity adds or updates an entity by name+type match.
func (g *GraphStore) UpsertEntity(e EntityCandidate) *Entity {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Check for existing entity with same name+type
	for _, existing := range g.entities {
		if strings.EqualFold(existing.Name, e.Name) && strings.EqualFold(existing.Type, e.Type) {
			// Merge labels
			labelSet := make(map[string]bool)
			for _, l := range existing.Labels {
				labelSet[l] = true
			}
			for _, l := range e.Labels {
				labelSet[l] = true
			}
			merged := make([]string, 0, len(labelSet))
			for l := range labelSet {
				merged = append(merged, l)
			}
			existing.Labels = merged
			return existing
		}
	}

	// New entity
	entity := &Entity{
		ID:     fmt.Sprintf("ent:%s", utils.GenerateShortID()),
		Name:   e.Name,
		Type:   e.Type,
		Labels: e.Labels,
	}
	g.entities[entity.ID] = entity
	return entity
}

// AddRelationship adds a relationship between two named entities.
func (g *GraphStore) AddRelationship(c RelCandidate) {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Resolve names to IDs
	fromID := g.resolveName(c.From)
	toID := g.resolveName(c.To)
	if fromID == "" || toID == "" {
		return
	}

	// Avoid exact duplicates
	for _, r := range g.relationships {
		if r.From == fromID && r.To == toID && r.Type == c.Type {
			// Update weight
			if c.Weight > r.Weight {
				r.Weight = c.Weight
			}
			return
		}
	}

	rel := &Relationship{
		ID:     fmt.Sprintf("rel:%s", utils.GenerateShortID()),
		From:   fromID,
		To:     toID,
		Type:   c.Type,
		Weight: c.Weight,
	}
	g.relationships = append(g.relationships, rel)
	g.adjOut[fromID] = append(g.adjOut[fromID], rel)
	g.adjIn[toID] = append(g.adjIn[toID], rel)
}

// resolveName finds an entity ID by name (case-insensitive).
func (g *GraphStore) resolveName(name string) string {
	for id, e := range g.entities {
		if strings.EqualFold(e.Name, name) {
			return id
		}
	}
	return ""
}

// UpsertRelationships persists changes after batch updates.
func (g *GraphStore) UpsertRelationships(ctx context.Context, entities []EntityCandidate, rels []RelCandidate) {
	for _, e := range entities {
		g.UpsertEntity(e)
	}
	for _, r := range rels {
		g.AddRelationship(r)
	}
	_ = g.persist(ctx)
}

// ---- Query ----

// SearchEntities finds entities matching a query by name, type, or label.
func (g *GraphStore) SearchEntities(query string, limit int) []Entity {
	g.mu.RLock()
	defer g.mu.RUnlock()

	q := strings.ToLower(query)
	var result []Entity
	for _, e := range g.entities {
		if strings.Contains(strings.ToLower(e.Name), q) ||
			strings.Contains(strings.ToLower(e.Type), q) {
			result = append(result, *e)
		} else {
			for _, l := range e.Labels {
				if strings.Contains(strings.ToLower(l), q) {
					result = append(result, *e)
					break
				}
			}
		}
		if len(result) >= limit {
			break
		}
	}
	return result
}

// Neighbors returns entities and relationships connected to the given entity.
func (g *GraphStore) Neighbors(entityID string) ([]Entity, []Relationship) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	seen := make(map[string]bool)
	var entities []Entity
	var rels []Relationship

	for _, r := range g.adjOut[entityID] {
		rels = append(rels, *r)
		if e, ok := g.entities[r.To]; ok && !seen[e.ID] {
			entities = append(entities, *e)
			seen[e.ID] = true
		}
	}
	for _, r := range g.adjIn[entityID] {
		rels = append(rels, *r)
		if e, ok := g.entities[r.From]; ok && !seen[e.ID] {
			entities = append(entities, *e)
			seen[e.ID] = true
		}
	}
	return entities, rels
}

// ShortestPath finds the shortest path between two entities (BFS).
// Returns alternating entity IDs and relationship types, e.g. ["e1", "uses", "e2", "depends_on", "e3"].
func (g *GraphStore) ShortestPath(fromID, toID string) []string {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if fromID == toID {
		return []string{fromID}
	}

	type bfsNode struct {
		entityID string
		path     []string // alternating entity, relType, entity, ...
	}

	visited := make(map[string]bool)
	queue := []bfsNode{{entityID: fromID, path: []string{fromID}}}
	visited[fromID] = true

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		// Explore outgoing edges
		for _, r := range g.adjOut[current.entityID] {
			if visited[r.To] {
				continue
			}
			newPath := make([]string, len(current.path)+2)
			copy(newPath, current.path)
			newPath[len(current.path)] = r.Type
			newPath[len(current.path)+1] = r.To

			if r.To == toID {
				return newPath
			}
			visited[r.To] = true
			queue = append(queue, bfsNode{entityID: r.To, path: newPath})
		}

		// Explore incoming edges (treat as undirected for path finding)
		for _, r := range g.adjIn[current.entityID] {
			if visited[r.From] {
				continue
			}
			newPath := make([]string, len(current.path)+2)
			copy(newPath, current.path)
			newPath[len(current.path)] = "<" + r.Type // reverse direction marker
			newPath[len(current.path)+1] = r.From

			if r.From == toID {
				return newPath
			}
			visited[r.From] = true
			queue = append(queue, bfsNode{entityID: r.From, path: newPath})
		}
	}
	return nil
}

// Subgraph returns entities and relationships within n hops of an entity.
func (g *GraphStore) Subgraph(entityID string, hops int) ([]Entity, []Relationship) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	visited := map[string]int{entityID: 0}
	queue := []string{entityID}
	seenRel := make(map[string]bool)
	var entities []Entity
	var rels []Relationship

	if e, ok := g.entities[entityID]; ok {
		entities = append(entities, *e)
	}

	addRel := func(r *Relationship) {
		if !seenRel[r.ID] {
			seenRel[r.ID] = true
			rels = append(rels, *r)
		}
	}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		depth := visited[current]
		if depth >= hops {
			continue
		}

		for _, r := range g.adjOut[current] {
			addRel(r)
			if _, seen := visited[r.To]; !seen {
				visited[r.To] = depth + 1
				queue = append(queue, r.To)
				if e, ok := g.entities[r.To]; ok {
					entities = append(entities, *e)
				}
			}
		}
		for _, r := range g.adjIn[current] {
			addRel(r)
			if _, seen := visited[r.From]; !seen {
				visited[r.From] = depth + 1
				queue = append(queue, r.From)
				if e, ok := g.entities[r.From]; ok {
					entities = append(entities, *e)
				}
			}
		}
	}
	return entities, rels
}

// Stats returns total entity and relationship counts.
func (g *GraphStore) Stats() (int, int) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return len(g.entities), len(g.relationships)
}

// AllEntities returns all entities (for graph visualization).
func (g *GraphStore) AllEntities() []Entity {
	g.mu.RLock()
	defer g.mu.RUnlock()
	result := make([]Entity, 0, len(g.entities))
	for _, e := range g.entities {
		result = append(result, *e)
	}
	return result
}
