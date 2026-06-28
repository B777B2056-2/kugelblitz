package longterm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"sync"

	"github.com/B777B2056-2/kugelblitz/core"
)

// DreamReport captures the result of a dream cycle.
type DreamReport struct {
	Timestamp    time.Time
	Candidates   int // items examined
	Consolidated int // items whose confidence was bumped
	ScoredHigh   int // items scored >= 7
	ScoredLow    int // items scored <= 3
	Deprecated   int // items removed (below confidence floor)
	Insights     []MemoryItem
	Summary      string
	LLMCalls     int
	Duration     time.Duration
}

// ToMarkdown renders a human-readable dream diary entry.
func (r *DreamReport) ToMarkdown() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# Dream Report — %s\n\n", r.Timestamp.Format("2006-01-02 15:04")))
	sb.WriteString(fmt.Sprintf("> Candidates: %d | Consolidated: %d | High-score: %d | Low-score: %d | Deprecated: %d\n",
		r.Candidates, r.Consolidated, r.ScoredHigh, r.ScoredLow, r.Deprecated))
	sb.WriteString(fmt.Sprintf("> LLM calls: %d | Duration: %v\n\n", r.LLMCalls, r.Duration.Round(time.Millisecond)))

	if r.Summary != "" {
		sb.WriteString("## Summary\n\n")
		sb.WriteString(r.Summary)
		sb.WriteString("\n\n")
	}

	if len(r.Insights) > 0 {
		sb.WriteString("## Insights\n\n")
		for _, ins := range r.Insights {
			sb.WriteString(fmt.Sprintf("- **%s**: %s\n", ins.Key, ins.Value))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// dreamCandidate is an item under consideration during light sleep.
type dreamCandidate struct {
	Item        MemoryItem
	GraphDegree int // number of relationships in the entity graph
}

// Dreamer runs background memory consolidation cycles.
type Dreamer struct {
	ltm      *LongTermMemory
	graph    *GraphStore
	indexMgr *IndexManager
	provider core.ILMProvider
}

// SetIndexManager attaches an index manager for search-log awareness.
func (d *Dreamer) SetIndexManager(im *IndexManager) { d.indexMgr = im }

// SetProvider sets the LLM provider for dreaming.
func (d *Dreamer) SetProvider(p core.ILMProvider) { d.provider = p }

// SetLTM sets the long-term memory store.
func (d *Dreamer) SetLTM(ltm *LongTermMemory) { d.ltm = ltm }

// SetGraph sets the entity-relationship graph store.
func (d *Dreamer) SetGraph(g *GraphStore) { d.graph = g }

// ---- Scheduler ----

// DreamScheduler runs dream cycles on a background goroutine.
// It checks for idle state and cooldown before each cycle.
type DreamScheduler struct {
	dreamer       *Dreamer
	checkInterval time.Duration // how often to poll (default 30 min)
	cooldown      time.Duration // min time between dreams (default 6 hours)
	lastDreamed   time.Time
	lastActivity  time.Time
	mu            sync.Mutex
	stopCh        chan struct{}
}

// NewDreamScheduler creates a scheduler with default intervals.
func NewDreamScheduler(dreamer *Dreamer) *DreamScheduler {
	return &DreamScheduler{
		dreamer:       dreamer,
		checkInterval: 30 * time.Minute,
		cooldown:      6 * time.Hour,
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
	if idle < 5*time.Minute || sinceLastDream < ds.cooldown {
		return
	}

	ds.mu.Lock()
	ds.lastDreamed = time.Now()
	ds.mu.Unlock()

	report, err := ds.dreamer.Run(context.Background())
	if err != nil || report == nil || report.Candidates == 0 {
		return
	}

	// Persist dream report
	if ds.dreamer.ltm != nil {
		_ = ds.dreamer.ltm.mdStore.Store(context.Background(), "DREAMS.md", []byte(report.ToMarkdown()))
	}
}

// Run executes the full dream cycle: Light Sleep → Deep Sleep → REM.
func (d *Dreamer) Run(ctx context.Context) (*DreamReport, error) {
	start := time.Now()
	report := &DreamReport{Timestamp: start}

	// Phase 1: Light Sleep — collect candidates
	candidates, err := d.lightSleep(ctx)
	if err != nil {
		return report, err
	}
	report.Candidates = len(candidates)
	if len(candidates) == 0 {
		report.Duration = time.Since(start)
		return report, nil
	}

	// Phase 2: Deep Sleep — LLM scores and filters
	scored, err := d.deepSleep(ctx, candidates)
	report.LLMCalls++
	if err != nil {
		return report, err
	}
	for _, s := range scored {
		if s.Score >= 7 {
			report.ScoredHigh++
			report.Consolidated++
		}
		if s.Score <= 3 {
			report.ScoredLow++
		}
	}

	// Phase 3: REM — pattern extraction from high-value items
	var highItems []MemoryItem
	for _, s := range scored {
		if s.Score >= 8 {
			highItems = append(highItems, s.Item)
		}
	}
	if len(highItems) > 0 {
		insights, summary, err := d.rem(ctx, highItems)
		report.LLMCalls++
		if err == nil {
			report.Insights = insights
			report.Summary = summary
		}
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
	Item        MemoryItem
	Score       int
	Reason      string
	GraphDegree int
}

// deepSleep sends candidates to the LLM for scoring and consolidates high scores.
func (d *Dreamer) deepSleep(ctx context.Context, candidates []dreamCandidate) ([]deepSleepResult, error) {
	// Build scoring prompt
	var itemsDesc strings.Builder
	for i, c := range candidates {
		itemsDesc.WriteString(fmt.Sprintf("%d. [%s] %s: %s (c%.2f, v%d, graph_degree=%d)\n",
			i+1, c.Item.Section, c.Item.Key, truncate(c.Item.Value, 100),
			c.Item.Confidence, c.Item.Version, c.GraphDegree))
	}

	prompt := fmt.Sprintf(
		`You are a memory scoring system. Rate each memory item from 1-10:
- 1-3: low value (one-time event, outdated, already well-known)
- 4-6: moderate (useful but not critical)
- 7-10: high value (recurring theme, important preference, actionable insight)

Consider: recency (high confidence = recent), frequency (high version = updated often), graph connections (high degree = well-connected entity).

Output ONLY valid JSON:
{"scores": [{"section":"...","key":"...","score":N,"reason":"brief justification"}]}

Items:
%s`, itemsDesc.String())

	msg := core.NewUserMessage("dreamer-deep", core.TextContent{Text: prompt})
	resp, err := d.provider.Generate(ctx, core.GenerateParams{
		Messages: []core.Message{msg}, Stream: false,
	})
	if err != nil {
		return nil, fmt.Errorf("deep sleep: %w", err)
	}

	text := ""
	if tc, ok := resp.Content.(core.TextContent); ok {
		text = tc.Text
	}

	var result struct {
		Scores []deepSleepScore `json:"scores"`
	}
	if err := parseJSON(text, &result); err != nil {
		return nil, fmt.Errorf("deep sleep parse: %w", err)
	}

	// Build results and consolidate high scores
	var scored []deepSleepResult
	for _, s := range result.Scores {
		for _, c := range candidates {
			if c.Item.Section == s.Section && c.Item.Key == s.Key {
				scored = append(scored, deepSleepResult{
					Item: c.Item, Score: s.Score, Reason: s.Reason, GraphDegree: c.GraphDegree,
				})
				// Consolidate: bump confidence on high-score items
				if s.Score >= 7 {
					existing, _ := d.ltm.Get(s.Section, s.Key)
					existing.Confidence += 0.05
					if existing.Confidence > 1.0 {
						existing.Confidence = 1.0
					}
					existing.Version++
					d.ltm.Store(s.Section, s.Key, existing.Value)
				}
				break
			}
		}
	}
	return scored, nil
}

// rem extracts cross-cutting insights from high-value items.
func (d *Dreamer) rem(ctx context.Context, highItems []MemoryItem) ([]MemoryItem, string, error) {
	var itemsDesc strings.Builder
	for i, item := range highItems {
		itemsDesc.WriteString(fmt.Sprintf("%d. [%s] %s: %s (c%.2f)\n",
			i+1, item.Section, item.Key, truncate(item.Value, 200), item.Confidence))
	}

	prompt := fmt.Sprintf(
		`You are a memory reflection system. Analyze these high-value memories and extract cross-cutting insights.

Output ONLY valid JSON:
{"insights": [{"section":"insights","key":"short_label","value":"detailed insight"}], "summary":"one-sentence summary of what the user is focused on"}

High-value memories:
%s`, itemsDesc.String())

	msg := core.NewUserMessage("dreamer-rem", core.TextContent{Text: prompt})
	resp, err := d.provider.Generate(ctx, core.GenerateParams{
		Messages: []core.Message{msg}, Stream: false,
	})
	if err != nil {
		return nil, "", fmt.Errorf("rem: %w", err)
	}

	text := ""
	if tc, ok := resp.Content.(core.TextContent); ok {
		text = tc.Text
	}

	var result struct {
		Insights []struct {
			Section string `json:"section"`
			Key     string `json:"key"`
			Value   string `json:"value"`
		} `json:"insights"`
		Summary string `json:"summary"`
	}
	if err := parseJSON(text, &result); err != nil {
		return nil, "", fmt.Errorf("rem parse: %w", err)
	}

	var insights []MemoryItem
	for _, ins := range result.Insights {
		insights = append(insights, MemoryItem{
			Section:    ins.Section,
			Key:        ins.Key,
			Value:      ins.Value,
			Confidence: 1.0,
		})
	}

	return insights, result.Summary, nil
}

// parseJSON extracts the first JSON object from text and unmarshals into dst.
func parseJSON(text string, dst any) error {
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return fmt.Errorf("no JSON object found")
	}
	return json.Unmarshal([]byte(text[start:end+1]), dst)
}
