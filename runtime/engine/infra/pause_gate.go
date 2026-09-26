package infra

import "sync"

// PauseGate coordinates pausing across a group of workers. When paused, all
// WaitIfPaused callers block until the gate is resumed. Pause/Resume are
// reference-counted: N Pause calls require N Resume calls to fully unblock.
//
// Resume is safe to call when not paused (a no-op), avoiding the
// unlock-of-unlocked-mutex panic of a bare sync.RWMutex gate.
type PauseGate struct {
	mu     sync.Mutex
	cond   *sync.Cond
	paused int
}

// NewPauseGate returns an unpaused gate.
func NewPauseGate() *PauseGate {
	g := &PauseGate{}
	g.cond = sync.NewCond(&g.mu)
	return g
}

// Pause increments the pause count, blocking subsequent WaitIfPaused callers.
func (g *PauseGate) Pause() {
	g.mu.Lock()
	g.paused++
	g.mu.Unlock()
}

// Resume decrements the pause count and unblocks waiters when it reaches zero.
func (g *PauseGate) Resume() {
	g.mu.Lock()
	if g.paused > 0 {
		g.paused--
	}
	g.cond.Broadcast()
	g.mu.Unlock()
}

// WaitIfPaused blocks while the gate is paused (pause count > 0).
func (g *PauseGate) WaitIfPaused() {
	g.mu.Lock()
	for g.paused > 0 {
		g.cond.Wait()
	}
	g.mu.Unlock()
}
