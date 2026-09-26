package infra

import (
	"testing"
	"time"
)

// TestPauseGate_ResumeWithoutPause_NoPanic covers B2: a bare sync.RWMutex gate
// would panic on Resume() without a matching Pause() ("unlock of unlocked mutex").
func TestPauseGate_ResumeWithoutPause_NoPanic(t *testing.T) {
	g := NewPauseGate()
	g.Resume() // must not panic
	g.Resume() // idempotent
}

func TestPauseGate_BlocksWhilePaused(t *testing.T) {
	g := NewPauseGate()
	g.Pause()

	done := make(chan struct{})
	go func() {
		g.WaitIfPaused()
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("WaitIfPaused returned while paused")
	case <-time.After(50 * time.Millisecond):
	}

	g.Resume()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("WaitIfPaused did not return after Resume")
	}
}

func TestPauseGate_ReferenceCounted(t *testing.T) {
	g := NewPauseGate()
	g.Pause()
	g.Pause()

	done := make(chan struct{})
	go func() {
		g.WaitIfPaused()
		close(done)
	}()

	g.Resume()
	select {
	case <-done:
		t.Fatal("unblocked after only one Resume")
	case <-time.After(50 * time.Millisecond):
	}

	g.Resume()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("not unblocked after two Resumes")
	}
}
