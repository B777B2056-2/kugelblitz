package dream

import (
	"context"
	"testing"
	"time"

	coretypes "github.com/B777B2056-2/kugelblitz/core/types"
	"github.com/B777B2056-2/kugelblitz/llm"
	"github.com/B777B2056-2/kugelblitz/memory/longterm"
	memorytypes "github.com/B777B2056-2/kugelblitz/memory/types"
	"github.com/B777B2056-2/kugelblitz/persist"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type dreamProvider struct {
	responses []string
	callCount int
}

func (m *dreamProvider) Generate(ctx context.Context, params coretypes.GenerateParams) (*coretypes.Message, error) {
	idx := m.callCount
	m.callCount++
	resp := "empty response"
	if idx < len(m.responses) {
		resp = m.responses[idx]
	}
	return &coretypes.Message{
		Content: coretypes.TextContent{Text: resp},
		Usage:   &coretypes.Usage{InputTokens: 100, OutputTokens: 50, TotalTokens: 150},
	}, nil
}

func setupDreamer(t *testing.T) (*longterm.LongTermMemory, *Dreamer) {
	t.Helper()

	graph := longterm.NewGraphStore(nil, "")
	ltm, _ := longterm.NewLongTermMemory(persist.NewMarkdownPersist(persist.NewFilePersist(t.TempDir())), longterm.WithGraph(graph))

	// Populate some memories
	_, _, _ = ltm.Store("user_preferences", "language", "Go")
	_, _, _ = ltm.Store("project_facts", "deploy", "production")
	_, _, _ = ltm.Store("episodic", "debug_nil_pointer", "Fixed nil pointer in plan_mode.go")
	_, _, _ = ltm.Store("lessons", "tdd_workflow", "Always write tests first")

	// Add graph data
	graph.UpsertEntity(longterm.EntityCandidate{Name: "Go", Type: "language"})
	graph.UpsertEntity(longterm.EntityCandidate{Name: "kugelblitz", Type: "project"})
	graph.UpsertEntity(longterm.EntityCandidate{Name: "plan_mode.go", Type: "file"})
	graph.AddRelationship(longterm.RelCandidate{From: "Go", To: "kugelblitz", Type: "implements", Weight: 1.0})
	graph.AddRelationship(longterm.RelCandidate{From: "kugelblitz", To: "plan_mode.go", Type: "contains", Weight: 1.0})

	return ltm, NewDreamer(llm.NewCaller(&dreamProvider{}, nil), ltm, graph)
}

func TestDreamer_LightSleep_CollectsCandidates(t *testing.T) {
	_, d := setupDreamer(t)

	candidates, err := d.lightSleep(context.Background())
	require.NoError(t, err)
	assert.NotEmpty(t, candidates)
	// All 4 items should be candidates
	assert.Len(t, candidates, 4)
}

func TestDreamer_LightSleep_IncludesGraphStats(t *testing.T) {
	_, d := setupDreamer(t)

	candidates, err := d.lightSleep(context.Background())
	require.NoError(t, err)

	// "Go" language item should have graph degree info
	for _, c := range candidates {
		if c.Item.Section == "user_preferences" && c.Item.Key == "language" {
			assert.Greater(t, c.GraphDegree, 0, "Go entity should have graph connections")
		}
	}
}

func TestDreamer_DeepSleep_ScoresAndFilters(t *testing.T) {
	ltm2, d := setupDreamer(t)

	// Provider returns JSON scores for each candidate
	d.caller.SetProvider(&dreamProvider{
		responses: []string{
			`{"scores":[
				{"section":"user_preferences","key":"language","score":9,"reason":"frequently used"},
				{"section":"project_facts","key":"deploy","score":8,"reason":"important"},
				{"section":"episodic","key":"debug_nil_pointer","score":3,"reason":"one-time event"},
				{"section":"lessons","key":"tdd_workflow","score":9,"reason":"recurring theme"}
			]}`,
		},
	})

	candidates, _ := d.lightSleep(context.Background())
	scored, err := d.deepSleep(context.Background(), candidates)
	require.NoError(t, err)
	assert.Len(t, scored, 4)

	// Check that scores are set
	hasHighScore := false
	hasLowScore := false
	for _, s := range scored {
		assert.NotZero(t, s.Score)
		assert.NotEmpty(t, s.Reason)
		if s.Score >= 8 {
			hasHighScore = true
		}
		if s.Score <= 3 {
			hasLowScore = true
		}
	}
	assert.True(t, hasHighScore, "should have high-scored items")
	assert.True(t, hasLowScore, "should have low-scored items")

	// Verify HIGH score items get confidence bump in LTM
	f, ok := ltm2.Get("lessons", "tdd_workflow")
	assert.True(t, ok)
	assert.Greater(t, f.Version, 1, "high-score item should be consolidated")
}

func TestDreamer_Run_DeprecatesLowScore(t *testing.T) {
	ltm, d := setupDreamer(t)

	d.caller.SetProvider(&dreamProvider{
		responses: []string{
			// Deep sleep: debug_nil_pointer scored 2 → low value, must be removed.
			`{"scores":[
				{"section":"user_preferences","key":"language","score":9,"reason":""},
				{"section":"project_facts","key":"deploy","score":8,"reason":""},
				{"section":"episodic","key":"debug_nil_pointer","score":2,"reason":"one-time"},
				{"section":"lessons","key":"tdd_workflow","score":9,"reason":""}
			]}`,
			`{"insights":[{"section":"insights","key":"p","value":"Go"}],"summary":"Go."}`,
		},
	})

	report, err := d.Run(context.Background())
	require.NoError(t, err)

	// Low-score item must be deprecated (removed from LTM).
	_, ok := ltm.Get("episodic", "debug_nil_pointer")
	assert.False(t, ok, "low-score item must be removed")

	// High-score items must be retained.
	_, ok = ltm.Get("lessons", "tdd_workflow")
	assert.True(t, ok, "high-score item must survive")

	assert.Equal(t, 1, report.Deprecated)
	assert.Equal(t, 1, report.ScoredLow)
}

func TestDreamer_REM_ExtractsInsights(t *testing.T) {
	_, d := setupDreamer(t)

	d.caller.SetProvider(&dreamProvider{
		responses: []string{
			// Deep sleep response
			`{"scores":[
				{"section":"user_preferences","key":"language","score":9,"reason":""},
				{"section":"project_facts","key":"deploy","score":7,"reason":""},
				{"section":"episodic","key":"debug_nil_pointer","score":2,"reason":""},
				{"section":"lessons","key":"tdd_workflow","score":9,"reason":""}
			]}`,
			// REM response
			`{"insights":[
				{"section":"insights","key":"go_agent_dev","value":"User is building a Go-based agent framework with TDD workflow"},
				{"section":"insights","key":"deploy_prod","value":"Project is deployed to production"}
			],"summary":"User is focused on Go agent development with strong emphasis on testing and production readiness."}`,
		},
	})

	candidates, _ := d.lightSleep(context.Background())
	scored, _ := d.deepSleep(context.Background(), candidates)
	var highForREM []memorytypes.MemoryItem
	for _, s := range scored {
		highForREM = append(highForREM, s.Item)
	}
	insights, _, err := d.rem(context.Background(), highForREM)
	require.NoError(t, err)
	assert.Len(t, insights, 2)
	assert.Equal(t, "insights", insights[0].Section)
	assert.Contains(t, insights[1].Value, "production")
}

func TestDreamer_Run_FullCycle(t *testing.T) {
	_, d := setupDreamer(t)

	// 4 items → needs 4 scores + 1 REM = 2 LLM calls total
	d.caller.SetProvider(&dreamProvider{
		responses: []string{
			`{"scores":[
				{"section":"user_preferences","key":"language","score":9,"reason":""},
				{"section":"project_facts","key":"deploy","score":8,"reason":""},
				{"section":"episodic","key":"debug_nil_pointer","score":2,"reason":""},
				{"section":"lessons","key":"tdd_workflow","score":9,"reason":""}
			]}`,
			`{"insights":[
				{"section":"insights","key":"pattern","value":"Go agent framework with TDD"}
			],"summary":"Go-focused development."}`,
		},
	})

	report, err := d.Run(context.Background())
	require.NoError(t, err)
	require.NotNil(t, report)
	// Duration may be 0 in fast tests
	assert.Equal(t, 2, report.LLMCalls)
	assert.Greater(t, report.Consolidated, 0)
	assert.Len(t, report.Insights, 1)
	assert.Contains(t, report.Summary, "Go")
}

func TestDreamReport_ToMarkdown(t *testing.T) {
	report := &DreamReport{
		Timestamp:    time.Date(2026, 6, 29, 3, 0, 0, 0, time.UTC),
		Consolidated: 2,
		Candidates:   4,
		ScoredHigh:   2,
		ScoredLow:    1,
		Deprecated:   1,
		Insights: []memorytypes.MemoryItem{
			{Section: "insights", Key: "pattern", Value: "Go agent framework with TDD"},
		},
		Summary:  "Focused on Go agent development.",
		LLMCalls: 2,
		Duration: time.Second * 15,
	}

	md := report.ToMarkdown()
	assert.Contains(t, md, "# Dream Report")
	assert.Contains(t, md, "2026-06-29")
	assert.Contains(t, md, "Consolidated")
	assert.Contains(t, md, "Go agent framework")
}

func TestDreamer_EmptyMemories_NoOp(t *testing.T) {
	ltm, _ := longterm.NewLongTermMemory(persist.NewMarkdownPersist(persist.NewFilePersist(t.TempDir())))
	d := NewDreamer(llm.NewCaller(&dreamProvider{}, nil), ltm, nil)

	report, err := d.Run(context.Background())
	require.NoError(t, err)
	assert.NotNil(t, report)
	assert.Equal(t, 0, report.Candidates)
}
