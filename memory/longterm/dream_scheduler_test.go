package longterm

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/B777B2056-2/kugelblitz/persist"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newSchedulerTestDreamer builds a Dreamer backed by an isolated tmp dir with a
// single LTM item and a scripted provider that scores it high, so a full dream
// cycle produces a non-empty report that gets persisted as DREAMS.md.
func newSchedulerTestDreamer(t *testing.T) (*LongTermMemory, *Dreamer) {
	t.Helper()
	ltm, err := NewLongTermMemory(persist.NewMarkdownPersist(persist.NewFilePersist(t.TempDir())))
	require.NoError(t, err)
	graph := NewGraphStore(nil, "")
	ltm.SetGraph(graph)
	_, _, _ = ltm.Store("s", "k", "v")
	d := &Dreamer{
		ltm:   ltm,
		graph: graph,
		provider: &dreamProvider{
			responses: []string{
				`{"scores":[{"section":"s","key":"k","score":9,"reason":"x"}]}`,
				`{"insights":[{"section":"insights","key":"i","value":"v"}],"summary":"s"}`,
			},
		},
	}
	return ltm, d
}

// flagProvider is a thread-safe ILMProvider that counts Generate calls and
// returns a valid score/insight pair on alternating calls.
type flagProvider struct {
	calls *atomic.Int32
}

func (p *flagProvider) Generate(_ context.Context, _ core.GenerateParams) (*core.Message, error) {
	n := p.calls.Add(1)
	var resp string
	if n%2 == 1 {
		resp = `{"scores":[{"section":"s","key":"k","score":9,"reason":"x"}]}`
	} else {
		resp = `{"insights":[{"section":"insights","key":"i","value":"v"}],"summary":"s"}`
	}
	return &core.Message{Content: core.TextContent{Text: resp}}, nil
}

func TestDreamScheduler_NoDreamWhenActive(t *testing.T) {
	_, d := newSchedulerTestDreamer(t)
	ds := NewDreamSchedulerWithIntervals(d, time.Hour, time.Hour, 5*time.Minute)
	ds.lastActivity = time.Now()  // idle ≈ 0 < 5min
	ds.lastDreamed = time.Time{} // cooldown satisfied

	ds.maybeDream()

	assert.False(t, d.ltm.mdStore.Exists(context.Background(), "DREAMS.md"))
}

func TestDreamScheduler_NoDreamBeforeCooldown(t *testing.T) {
	_, d := newSchedulerTestDreamer(t)
	ds := NewDreamSchedulerWithIntervals(d, time.Hour, 6*time.Hour, 5*time.Minute)
	ds.lastActivity = time.Now().Add(-time.Hour) // idle 1h ≥ 5min
	ds.lastDreamed = time.Now()                  // sinceLastDream ≈ 0 < 6h

	ds.maybeDream()

	assert.False(t, d.ltm.mdStore.Exists(context.Background(), "DREAMS.md"))
}

func TestDreamScheduler_DreamsWhenIdleAndCooldownElapsed(t *testing.T) {
	_, d := newSchedulerTestDreamer(t)
	ds := NewDreamSchedulerWithIntervals(d, time.Hour, 6*time.Hour, 5*time.Minute)
	ds.lastActivity = time.Now().Add(-time.Hour)
	ds.lastDreamed = time.Time{}

	ds.maybeDream()

	data, err := d.ltm.mdStore.Load(context.Background(), "DREAMS.md")
	require.NoError(t, err)
	assert.Contains(t, string(data), "# Dream Report")
}

func TestDreamScheduler_NoOpWhenNoCandidates(t *testing.T) {
	ltm, err := NewLongTermMemory(persist.NewMarkdownPersist(persist.NewFilePersist(t.TempDir())))
	require.NoError(t, err)
	d := &Dreamer{ltm: ltm, provider: &dreamProvider{}}
	ds := NewDreamSchedulerWithIntervals(d, time.Hour, 6*time.Hour, 5*time.Minute)
	ds.lastActivity = time.Now().Add(-time.Hour)
	ds.lastDreamed = time.Time{}

	ds.maybeDream()

	assert.False(t, ltm.mdStore.Exists(context.Background(), "DREAMS.md"))
}

func TestDreamScheduler_AutoDream_Fires(t *testing.T) {
	ltm, d := newSchedulerTestDreamer(t)
	var calls atomic.Int32
	d.provider = &flagProvider{calls: &calls}
	ds := NewDreamSchedulerWithIntervals(d, 5*time.Millisecond, 0, 0)
	ds.Start()
	defer ds.Stop()

	require.Eventually(t, func() bool {
		return ltm.mdStore.Exists(context.Background(), "DREAMS.md")
	}, 2*time.Second, 5*time.Millisecond)

	assert.GreaterOrEqual(t, calls.Load(), int32(2))
}
