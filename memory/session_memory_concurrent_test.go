package memory

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestSessionMemoryManager_CreateConcurrent_SameInstance verifies that
// concurrent creation of the same session ID converges on a single instance,
// rather than each caller receiving its own object (B7).
func TestSessionMemoryManager_CreateConcurrent_SameInstance(t *testing.T) {
	smm := &SessionMemoryManager{}
	sessionID := fmt.Sprintf("concurrent-%d", time.Now().UnixNano())

	const n = 50
	results := make([]*SessionMemory, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = smm.CreateSessionMemory(sessionID)
		}(i)
	}
	wg.Wait()

	for _, m := range results {
		require.Same(t, results[0], m, "concurrent CreateSessionMemory returned different instances")
	}
}
