package tui

import (
	"sync"
	"testing"
	"time"
)

// recv non-blockingly attempts to read one token from the gate. It
// returns true when a token was available, false otherwise (the gate
// is empty / paused).
func recv(g *Gate) bool {
	select {
	case <-g.Chan():
		return true
	default:
		return false
	}
}

func TestGate_InitiallyEmpty(t *testing.T) {
	g := NewGate()
	if recv(g) {
		t.Fatalf("new Gate must start empty (the first iteration is never gated)")
	}
}

func TestGate_IterDonePostsToken(t *testing.T) {
	g := NewGate()
	g.IterDone()
	if !recv(g) {
		t.Fatalf("IterDone should post one token")
	}
	if recv(g) {
		t.Fatalf("only one pending token should ever be queued")
	}
}

func TestGate_IterDoneCoalesces(t *testing.T) {
	g := NewGate()
	for i := 0; i < 5; i++ {
		g.IterDone()
	}
	got := 0
	for recv(g) {
		got++
	}
	if got != 1 {
		t.Fatalf("got %d tokens, want 1 (coalesced)", got)
	}
}

func TestGate_PausedDoesNotPost(t *testing.T) {
	g := NewGate()
	g.SetPaused(true)
	g.IterDone()
	if recv(g) {
		t.Fatalf("paused Gate must not post tokens on IterDone")
	}
}

func TestGate_ResumePostsToken(t *testing.T) {
	g := NewGate()
	g.SetPaused(true)
	g.IterDone() // queued, but suppressed by pause
	g.SetPaused(false)
	if !recv(g) {
		t.Fatalf("resuming the gate must release one token")
	}
}

func TestGate_PausedRoundTrip(t *testing.T) {
	g := NewGate()
	if g.Paused() {
		t.Fatalf("new Gate should report unpaused")
	}
	g.SetPaused(true)
	if !g.Paused() {
		t.Fatalf("Paused() should report true after SetPaused(true)")
	}
	g.SetPaused(false)
	if g.Paused() {
		t.Fatalf("Paused() should report false after SetPaused(false)")
	}
}

// TestGate_LoopReleaseHonoursPause models the loop's actual pattern:
// it blocks on Chan() before each iteration after the first; the TUI
// posts via IterDone after each iteration finished. Pause halts the
// loop; Resume releases it.
func TestGate_LoopReleaseHonoursPause(t *testing.T) {
	g := NewGate()
	released := make(chan struct{}, 1)
	go func() {
		<-g.Chan()
		released <- struct{}{}
	}()

	g.SetPaused(true)
	g.IterDone() // suppressed: loop must not advance while paused

	select {
	case <-released:
		t.Fatalf("loop advanced while paused")
	case <-time.After(50 * time.Millisecond):
	}

	g.SetPaused(false)
	select {
	case <-released:
	case <-time.After(time.Second):
		t.Fatalf("loop did not advance after resume")
	}
}

// TestGate_ConcurrentSafe asserts the gate survives the race detector
// under concurrent IterDone / SetPaused / receivers, modelling the TUI
// goroutines.
func TestGate_ConcurrentSafe(t *testing.T) {
	g := NewGate()
	var wg sync.WaitGroup
	stop := make(chan struct{})

	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					g.IterDone()
				}
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		paused := false
		for {
			select {
			case <-stop:
				return
			default:
				paused = !paused
				g.SetPaused(paused)
			}
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			case <-g.Chan():
			}
		}
	}()

	time.Sleep(20 * time.Millisecond)
	close(stop)
	wg.Wait()
}
