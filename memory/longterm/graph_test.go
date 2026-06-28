package longterm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGraphStore_UpsertEntity(t *testing.T) {
	g := NewGraphStore(nil, "")

	e := g.UpsertEntity(EntityCandidate{Name: "Go", Type: "language", Labels: []string{"programming", "backend"}})
	assert.NotEmpty(t, e.ID)
	assert.True(t, len(e.ID) > 4 && e.ID[:4] == "ent:", "ID should start with ent:")
	assert.Equal(t, "Go", e.Name)
	assert.Contains(t, e.Labels, "programming")

	// Same name+type → merge
	e2 := g.UpsertEntity(EntityCandidate{Name: "Go", Type: "language", Labels: []string{"systems"}})
	assert.Equal(t, e.ID, e2.ID, "should merge into existing entity")
	assert.Contains(t, e2.Labels, "programming")
	assert.Contains(t, e2.Labels, "systems")
}

func TestGraphStore_AddRelationship(t *testing.T) {
	g := NewGraphStore(nil, "")

	g.UpsertEntity(EntityCandidate{Name: "Go", Type: "language"})
	g.UpsertEntity(EntityCandidate{Name: "kugelblitz", Type: "project"})

	g.AddRelationship(RelCandidate{From: "Go", To: "kugelblitz", Type: "implements", Weight: 1.0})

	assert.Len(t, g.relationships, 1)
	assert.Equal(t, "implements", g.relationships[0].Type)
	assert.Equal(t, 1.0, g.relationships[0].Weight)
}

func TestGraphStore_AddRelationship_Deduplicate(t *testing.T) {
	g := NewGraphStore(nil, "")

	g.UpsertEntity(EntityCandidate{Name: "Go", Type: "language"})
	g.UpsertEntity(EntityCandidate{Name: "kugelblitz", Type: "project"})

	g.AddRelationship(RelCandidate{From: "Go", To: "kugelblitz", Type: "implements", Weight: 0.8})
	g.AddRelationship(RelCandidate{From: "Go", To: "kugelblitz", Type: "implements", Weight: 1.0})

	// Should not duplicate; should update weight
	assert.Len(t, g.relationships, 1)
	assert.Equal(t, 1.0, g.relationships[0].Weight) // higher weight kept
}

func TestGraphStore_SearchEntities(t *testing.T) {
	g := NewGraphStore(nil, "")

	g.UpsertEntity(EntityCandidate{Name: "Go", Type: "language"})
	g.UpsertEntity(EntityCandidate{Name: "python", Type: "language"})
	g.UpsertEntity(EntityCandidate{Name: "plan_mode.go", Type: "file"})

	results := g.SearchEntities("Go", 5)
	require.GreaterOrEqual(t, len(results), 1)
	names := make([]string, len(results))
	for i, e := range results {
		names[i] = e.Name
	}
	assert.Contains(t, names, "Go")

	results = g.SearchEntities("language", 5)
	assert.Len(t, results, 2) // Go + python both type "language"
}

func TestGraphStore_Neighbors(t *testing.T) {
	g := NewGraphStore(nil, "")

	_ = g.UpsertEntity(EntityCandidate{Name: "Go", Type: "language"})
	projE := g.UpsertEntity(EntityCandidate{Name: "kugelblitz", Type: "project"})
	_ = g.UpsertEntity(EntityCandidate{Name: "main.go", Type: "file"})

	g.AddRelationship(RelCandidate{From: "Go", To: "kugelblitz", Type: "implements", Weight: 1.0})
	g.AddRelationship(RelCandidate{From: "kugelblitz", To: "main.go", Type: "contains", Weight: 1.0})

	entities, rels := g.Neighbors(projE.ID)
	assert.Len(t, entities, 2) // Go + main.go
	assert.Len(t, rels, 2)
}

func TestGraphStore_ShortestPath(t *testing.T) {
	g := NewGraphStore(nil, "")

	e1 := g.UpsertEntity(EntityCandidate{Name: "Go", Type: "language"})
	_ = g.UpsertEntity(EntityCandidate{Name: "kugelblitz", Type: "project"})
	e3 := g.UpsertEntity(EntityCandidate{Name: "nil pointer bug", Type: "bug"})

	g.AddRelationship(RelCandidate{From: "Go", To: "kugelblitz", Type: "implements", Weight: 1.0})
	g.AddRelationship(RelCandidate{From: "kugelblitz", To: "nil pointer bug", Type: "has", Weight: 1.0})

	path := g.ShortestPath(e1.ID, e3.ID)
	require.NotNil(t, path)
	// Expected: [Go, implements, kugelblitz, has, nil pointer bug]
	assert.Len(t, path, 5)
	assert.Equal(t, e1.ID, path[0])
	assert.Equal(t, e3.ID, path[4])
}

func TestGraphStore_ShortestPath_NoPath(t *testing.T) {
	g := NewGraphStore(nil, "")

	e1 := g.UpsertEntity(EntityCandidate{Name: "Go", Type: "language"})
	e2 := g.UpsertEntity(EntityCandidate{Name: "Python", Type: "language"})

	path := g.ShortestPath(e1.ID, e2.ID)
	assert.Nil(t, path, "no path between disconnected entities")
}

func TestGraphStore_Subgraph(t *testing.T) {
	g := NewGraphStore(nil, "")

	center := g.UpsertEntity(EntityCandidate{Name: "kugelblitz", Type: "project"})
	g.UpsertEntity(EntityCandidate{Name: "Go", Type: "language"})
	g.UpsertEntity(EntityCandidate{Name: "plan_mode.go", Type: "file"})
	g.UpsertEntity(EntityCandidate{Name: "remote", Type: "concept"})

	g.AddRelationship(RelCandidate{From: "Go", To: "kugelblitz", Type: "implements", Weight: 1.0})
	g.AddRelationship(RelCandidate{From: "kugelblitz", To: "plan_mode.go", Type: "contains", Weight: 1.0})
	// "remote" is disconnected — should not appear

	entities, rels := g.Subgraph(center.ID, 2)
	assert.Len(t, entities, 3) // center + Go + plan_mode.go
	assert.Len(t, rels, 2)
}

func TestGraphStore_Stats(t *testing.T) {
	g := NewGraphStore(nil, "")
	g.UpsertEntity(EntityCandidate{Name: "A", Type: "test"})
	g.UpsertEntity(EntityCandidate{Name: "B", Type: "test"})
	eCount, rCount := g.Stats()
	assert.Equal(t, 2, eCount)
	assert.Equal(t, 0, rCount)
}

func TestGraphStore_AllEntities(t *testing.T) {
	g := NewGraphStore(nil, "")
	g.UpsertEntity(EntityCandidate{Name: "A", Type: "test"})
	g.UpsertEntity(EntityCandidate{Name: "B", Type: "test"})
	assert.Len(t, g.AllEntities(), 2)
}

func TestUpsertRelationships(t *testing.T) {
	g := NewGraphStore(nil, "")

	entities := []EntityCandidate{
		{Name: "Go", Type: "language"},
		{Name: "kugelblitz", Type: "project"},
	}
	rels := []RelCandidate{
		{From: "Go", To: "kugelblitz", Type: "implements", Weight: 1.0},
	}
	g.UpsertRelationships(nil, entities, rels)

	assert.Len(t, g.entities, 2)
	assert.Len(t, g.relationships, 1)
}
