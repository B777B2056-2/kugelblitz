package dream

import (
	"context"
	"fmt"
	"strings"
	"time"

	"sync"

	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/B777B2056-2/kugelblitz/llm"
	"github.com/B777B2056-2/kugelblitz/memory/longterm"
	"github.com/B777B2056-2/kugelblitz/memory/pipeline"
	memorytypes "github.com/B777B2056-2/kugelblitz/memory/types"
	"github.com/B777B2056-2/kugelblitz/prompts"
)

// DreamReport captures the result of a dream cycle.
type DreamReport struct {
	Timestamp    time.Time
	Candidates   int // items examined
	Consolidated int // items whose confidence was bumped
	ScoredHigh   int // items scored >= 7
	ScoredLow    int // items scored <= 3
	Deprecated   int // items removed (below confidence floor)
	Insights     []memorytypes.MemoryItem
	Summary      string
	LLMCalls     int
	Duration     time.Duration
}

// ToMarkdown renders a human-readable dream diary entry.
func (r *DreamReport) ToMarkdown() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# Dream Report — %s\n\n", r.Timestamp.Format("2006-01-02 15:04"))
	fmt.Fprintf(&sb, "> Candidates: %d | Consolidated: %d | High-score: %d | Low-score: %d | Deprecated: %d\n",
		r.Candidates, r.Consolidated, r.ScoredHigh, r.ScoredLow, r.Deprecated)
	fmt.Fprintf(&sb, "> LLM calls: %d | Duration: %v\n\n", r.LLMCalls, r.Duration.Round(time.Millisecond))

	if r.Summary != "" {
		sb.WriteString("## Summary\n\n")
		sb.WriteString(r.Summary)
		sb.WriteString("\n\n")
	}

	if len(r.Insights) > 0 {
		sb.WriteString("## Insights\n\n")
		for _, ins := range r.Insights {
			fmt.Fprintf(&sb, "- **%s**: %s\n", ins.Key, ins.Value)
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// dreamCandidate is an item under consideration during light sleep.
type dreamCandidate struct {
	Item        memorytypes.MemoryItem
	GraphDegree int // number of relationships in the entity graph
}

// Dreamer runs background memory consolidation cycles.
type Dreamer struct {
	caller *llm.Caller
	ltm    *longterm.LongTermMemory
	graph  *longterm.GraphStore
}

// NewDreamer constructs a Dreamer with all dependencies injected. caller
// provides the LLM for deep-sleep scoring and REM insight extraction.
func NewDreamer(caller *llm.Caller, ltm *longterm.LongTermMemory, graph *longterm.GraphStore) *Dreamer {
	return &Dreamer{caller: caller, ltm: ltm, graph: graph}
}

// ---- Scheduler ----

// DreamScheduler runs dream cycles on a background goroutine.
// It checks for idle state and cooldown before each cycle.
type DreamScheduler struct {
	dreamer       *Dreamer
	checkInterval time.Duration // how often to poll (default 30 min)
	cooldown      time.Duration // min time between dreams (default 6 hours)
	idleThreshold time.Duration // min idle time before dreaming (default 5 min)
	lastDreamed   time.Time
	lastActivity  time.Time
	mu            sync.Mutex
	stopCh        chan struct{}
}

// NewDreamScheduler creates a scheduler with default intervals
// (30 min check / 6 hour cooldown / 5 min idle).
func NewDreamScheduler(dreamer *Dreamer) *DreamScheduler {
	return NewDreamSchedulerWithIntervals(dreamer, 30*time.Minute, 6*time.Hour, 5*time.Minute)
}

// NewDreamSchedulerWithIntervals creates a scheduler with explicit
// check/cooldown/idle intervals, for tests and non-default deployments.
func NewDreamSchedulerWithIntervals(dreamer *Dreamer, checkInterval, cooldown, idleThreshold time.Duration) *DreamScheduler {
	return &DreamScheduler{
		dreamer:       dreamer,
		checkInterval: checkInterval,
		cooldown:      cooldown,
		idleThreshold: idleThreshold,
		stopCh:        make(chan struct{}),
	}
}

// NotifyActivity marks that the agent just handled a request (resets idle timer).
func (ds *DreamScheduler) NotifyActivity() {
	ds.mu.Lock()
	ds.lastActivity = time.Now()
	ds.mu.Unlock()
}

// Start begins the background polling loop. Call once; runs until Stop().
func (ds *DreamScheduler) Start() {
	go ds.loop()
}

// Stop signals the background loop to exit.
func (ds *DreamScheduler) Stop() {
	close(ds.stopCh)
}

func (ds *DreamScheduler) loop() {
	ticker := time.NewTicker(ds.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ds.stopCh:
			return
		case <-ticker.C:
			ds.maybeDream()
		}
	}
}

func (ds *DreamScheduler) maybeDream() {
	ds.mu.Lock()
	now := time.Now()
	idle := now.Sub(ds.lastActivity)
	sinceLastDream := now.Sub(ds.lastDreamed)
	ds.mu.Unlock()

	// Only dream when idle AND cooldown elapsed
	if idle < ds.idleThreshold || sinceLastDream < ds.cooldown {
		return
	}

	ds.mu.Lock()
	ds.lastDreamed = time.Now()
	ds.mu.Unlock()

	report, err := ds.dreamer.Run(context.Background())
	if err != nil || report == nil || report.Candidates == 0 {
		return
	}

	// Persist dream report (background goroutine has no caller to propagate to,
	// so a write failure is logged rather than surfaced).
	if ds.dreamer.ltm != nil {
		if err := ds.dreamer.ltm.StoreMarkdown(context.Background(), "DREAMS.md", []byte(report.ToMarkdown())); err != nil {
			core.Warn("dream: persist report", "err", err)
		}
	}
}

// Run executes the full dream cycle as an ordered set of steps:
// Light Sleep → Deep Sleep → REM.
func (d *Dreamer) Run(ctx context.Context) (*DreamReport, error) {
	start := time.Now()
	report := &DreamReport{Timestamp: start}

	var (
		candidates []dreamCandidate
		scored     []deepSleepResult
	)

	steps := pipeline.New(
		pipeline.Step{Name: "light_sleep", Run: func(ctx context.Context) error {
			cs, err := d.lightSleep(ctx)
			if err != nil {
				return err
			}
			candidates = cs
			report.Candidates = len(cs)
			return nil
		}},
		pipeline.Step{Name: "deep_sleep", Run: func(ctx context.Context) error {
			if len(candidates) == 0 {
				return nil // nothing to consolidate
			}
			sc, err := d.deepSleep(ctx, candidates)
			report.LLMCalls++
			if err != nil {
				return err
			}
			scored = sc
			for _, s := range scored {
				if s.Score >= 7 {
					report.ScoredHigh++
					report.Consolidated++
				}
				if s.Score <= 3 {
					report.ScoredLow++
					report.Deprecated++
				}
			}
			return nil
		}},
		pipeline.Step{Name: "rem", Run: func(ctx context.Context) error {
			var highItems []memorytypes.MemoryItem
			for _, s := range scored {
				if s.Score >= 8 {
					highItems = append(highItems, s.Item)
				}
			}
			if len(highItems) == 0 {
				return nil
			}
			insights, summary, err := d.rem(ctx, highItems)
			report.LLMCalls++
			if err == nil {
				report.Insights = insights
				report.Summary = summary
			}
			return nil
		}},
	)

	if err := steps.Run(ctx); err != nil {
		return report, err
	}
	report.Duration = time.Since(start)
	return report, nil
}

// lightSleep collects all LTM items as candidates, enriched with graph degree.
func (d *Dreamer) lightSleep(_ context.Context) ([]dreamCandidate, error) {
	items := d.ltm.All()
	candidates := make([]dreamCandidate, len(items))
	for i, item := range items {
		c := dreamCandidate{Item: item}
		if d.graph != nil {
			// Map item section+key → potential entity match
			entities := d.graph.SearchEntities(item.Key, 3)
			for _, e := range entities {
				_, rels := d.graph.Neighbors(e.ID)
				c.GraphDegree += len(rels)
			}
		}
		candidates[i] = c
	}
	return candidates, nil
}

// deepSleepScore is the LLM's per-item scoring output.
type deepSleepScore struct {
	Section string `json:"section"`
	Key     string `json:"key"`
	Score   int    `json:"score"`
	Reason  string `json:"reason"`
}

type deepSleepResult struct {
	Item        memorytypes.MemoryItem
	Score       int
	Reason      string
	GraphDegree int
}

// deepSleep sends candidates to the LLM for scoring and consolidates high scores.
func (d *Dreamer) deepSleep(ctx context.Context, candidates []dreamCandidate) ([]deepSleepResult, error) {
	// Build scoring prompt
	var itemsDesc strings.Builder
	for i, c := range candidates {
		fmt.Fprintf(&itemsDesc, "%d. [%s] %s: %s (c%.2f, v%d, graph_degree=%d)\n",
			i+1, c.Item.Section, c.Item.Key, truncate(c.Item.Value, 100),
			c.Item.Confidence, c.Item.Version, c.GraphDegree)
	}

	prompt, err := prompts.DefaultFactory.Render(prompts.TypeMemoryScore, prompts.MemoryScoreParams{
		Items: itemsDesc.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("deep sleep: render: %w", err)
	}

	var result struct {
		Scores []deepSleepScore `json:"scores"`
	}
	if _, err := d.caller.Call(ctx, llm.Request{
		Prompt:   prompt,
		Mode:     llm.ModeJSON,
		Target:   &result,
		SpanName: "dream.deep_sleep",
	}); err != nil {
		return nil, fmt.Errorf("deep sleep: %w", err)
	}

	// Build results and consolidate high scores
	var scored []deepSleepResult
	for _, s := range result.Scores {
		for _, c := range candidates {
			if c.Item.Section == s.Section && c.Item.Key == s.Key {
				scored = append(scored, deepSleepResult{
					Item: c.Item, Score: s.Score, Reason: s.Reason, GraphDegree: c.GraphDegree,
				})
				// Consolidate: bump confidence on high-score items.
				if s.Score >= 7 {
					existing, _ := d.ltm.Get(s.Section, s.Key)
					existing.Confidence += 0.05
					if existing.Confidence > 1.0 {
						existing.Confidence = 1.0
					}
					existing.Version++
					_, _, _ = d.ltm.Store(s.Section, s.Key, existing.Value)
				} else if s.Score <= 3 {
					// Deprecate: drop low-value items (one-time/outdated/well-known).
					if err := d.ltm.Remove(s.Section, s.Key); err != nil {
						core.Warn("dream: deprecate item", "section", s.Section, "key", s.Key, "err", err)
					}
				}
				break
			}
		}
	}
	return scored, nil
}

// rem extracts cross-cutting insights from high-value items.
func (d *Dreamer) rem(ctx context.Context, highItems []memorytypes.MemoryItem) ([]memorytypes.MemoryItem, string, error) {
	var itemsDesc strings.Builder
	for i, item := range highItems {
		fmt.Fprintf(&itemsDesc, "%d. [%s] %s: %s (c%.2f)\n",
			i+1, item.Section, item.Key, truncate(item.Value, 200), item.Confidence)
	}

	prompt, err := prompts.DefaultFactory.Render(prompts.TypeMemoryReflect, prompts.MemoryReflectParams{
		Items: itemsDesc.String(),
	})
	if err != nil {
		return nil, "", fmt.Errorf("rem: render: %w", err)
	}

	var result struct {
		Insights []struct {
			Section string `json:"section"`
			Key     string `json:"key"`
			Value   string `json:"value"`
		} `json:"insights"`
		Summary string `json:"summary"`
	}
	if _, err := d.caller.Call(ctx, llm.Request{
		Prompt:   prompt,
		Mode:     llm.ModeJSON,
		Target:   &result,
		SpanName: "dream.rem",
	}); err != nil {
		return nil, "", fmt.Errorf("rem: %w", err)
	}

	var insights []memorytypes.MemoryItem
	for _, ins := range result.Insights {
		insights = append(insights, memorytypes.MemoryItem{
			Section:    ins.Section,
			Key:        ins.Key,
			Value:      ins.Value,
			Confidence: 1.0,
		})
	}

	return insights, result.Summary, nil
}

// truncate shortens s to maxLen bytes, appending an ellipsis when cut.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
