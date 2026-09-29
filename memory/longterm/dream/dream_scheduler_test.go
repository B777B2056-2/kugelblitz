package dream

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/B777B2056-2/kugelblitz/constants"
	coretypes "github.com/B777B2056-2/kugelblitz/core/types"
	"github.com/B777B2056-2/kugelblitz/events"
	"github.com/B777B2056-2/kugelblitz/llm"
	"github.com/B777B2056-2/kugelblitz/memory/longterm"
	"github.com/B777B2056-2/kugelblitz/persist"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newSchedulerTestDreamer builds a Dreamer backed by an isolated tmp dir with a
// single LTM item and a scripted provider that scores it high, so a full dream
// cycle produces a non-empty report that gets persisted as DREAMS.md.
func newSchedulerTestDreamer(t *testing.T) (*longterm.LongTermMemory, *Dreamer) {
	t.Helper()
	graph := longterm.NewGraphStore(nil, "")
	ltm, err := longterm.NewLongTermMemory(persist.NewMarkdownPersist(persist.NewFilePersist(t.TempDir())), longterm.WithGraph(graph))
	require.NoError(t, err)
	_, _, _ = ltm.Store("s", "k", "v")
	d := NewDreamer(llm.NewCaller(&dreamProvider{
		responses: []string{
			`{"scores":[{"section":"s","key":"k","score":9,"reason":"x"}]}`,
			`{"insights":[{"section":"insights","key":"i","value":"v"}],"summary":"s"}`,
		},
	}, nil), ltm, graph)
	return ltm, d
}

// flagProvider is a thread-safe ILMProvider that counts Generate calls and
// returns a valid score/insight pair on alternating calls.
type flagProvider struct {
	calls *atomic.Int32
}

func (p *flagProvider) Generate(_ context.Context, _ coretypes.GenerateParams) (*coretypes.Message, error) {
	n := p.calls.Add(1)
	var resp string
	if n%2 == 1 {
		resp = `{"scores":[{"section":"s","key":"k","score":9,"reason":"x"}]}`
	} else {
		resp = `{"insights":[{"section":"insights","key":"i","value":"v"}],"summary":"s"}`
	}
	return &coretypes.Message{Content: coretypes.TextContent{Text: resp}}, nil
}

func TestDreamScheduler_NoDreamWhenActive(t *testing.T) {
	_, d := newSchedulerTestDreamer(t)
	ds := NewDreamSchedulerWithIntervals(d, time.Hour, time.Hour, 5*time.Minute)
	ds.lastActivity = time.Now() // idle ≈ 0 < 5min
	ds.lastDreamed = time.Time{} // cooldown satisfied

	ds.maybeDream()

	assert.False(t, d.ltm.MarkdownExists(context.Background(), "DREAMS.md"))
}

func TestDreamScheduler_NoDreamBeforeCooldown(t *testing.T) {
	_, d := newSchedulerTestDreamer(t)
	ds := NewDreamSchedulerWithIntervals(d, time.Hour, 6*time.Hour, 5*time.Minute)
	ds.lastActivity = time.Now().Add(-time.Hour) // idle 1h ≥ 5min
	ds.lastDreamed = time.Now()                  // sinceLastDream ≈ 0 < 6h

	ds.maybeDream()

	assert.False(t, d.ltm.MarkdownExists(context.Background(), "DREAMS.md"))
}

func TestDreamScheduler_DreamsWhenIdleAndCooldownElapsed(t *testing.T) {
	_, d := newSchedulerTestDreamer(t)
	ds := NewDreamSchedulerWithIntervals(d, time.Hour, 6*time.Hour, 5*time.Minute)
	ds.lastActivity = time.Now().Add(-time.Hour)
	ds.lastDreamed = time.Time{}

	ds.maybeDream()

	data, err := d.ltm.LoadMarkdown(context.Background(), "DREAMS.md")
	require.NoError(t, err)
	assert.Contains(t, string(data), "# Dream Report")
}

func TestDreamScheduler_NoOpWhenNoCandidates(t *testing.T) {
	ltm, err := longterm.NewLongTermMemory(persist.NewMarkdownPersist(persist.NewFilePersist(t.TempDir())))
	require.NoError(t, err)
	d := NewDreamer(llm.NewCaller(&dreamProvider{}, nil), ltm, nil)
	ds := NewDreamSchedulerWithIntervals(d, time.Hour, 6*time.Hour, 5*time.Minute)
	ds.lastActivity = time.Now().Add(-time.Hour)
	ds.lastDreamed = time.Time{}

	ds.maybeDream()

	assert.False(t, ltm.MarkdownExists(context.Background(), "DREAMS.md"))
}

func TestDreamScheduler_AutoDream_Fires(t *testing.T) {
	ltm, d := newSchedulerTestDreamer(t)
	var calls atomic.Int32
	d.caller.SetProvider(&flagProvider{calls: &calls})
	ds := NewDreamSchedulerWithIntervals(d, 5*time.Millisecond, 0, 0)
	ds.Start()
	defer ds.Stop()

	require.Eventually(t, func() bool {
		return ltm.MarkdownExists(context.Background(), "DREAMS.md")
	}, 2*time.Second, 5*time.Millisecond)

	assert.GreaterOrEqual(t, calls.Load(), int32(2))
}

func TestDreamScheduler_SubscribeActivity_ResetsIdleTimer(t *testing.T) {
	_, d := newSchedulerTestDreamer(t)
	ds := NewDreamSchedulerWithIntervals(d, time.Hour, time.Hour, 5*time.Minute)
	ds.lastActivity = time.Now().Add(-time.Hour) // idle 1h ≥ 5min → would dream
	ds.lastDreamed = time.Time{}                 // cooldown satisfied

	bus := events.NewBus()
	unsub := ds.SubscribeActivity(bus)
	defer unsub()

	events.Emit(bus, events.AgentActivity{Identity: constants.AgentMain})

	ds.maybeDream()
	assert.False(t, d.ltm.MarkdownExists(context.Background(), "DREAMS.md"),
		"AgentActivity must reset the idle timer and prevent dreaming")
}
