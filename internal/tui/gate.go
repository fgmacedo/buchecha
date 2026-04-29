package tui

import "sync"

// Gate is the pause manager between the TUI and the Loop.
//
// The Loop receives one value on Chan() before starting each iteration
// after the first. The TUI calls IterDone() when an IterationFinished
// event arrives so the next iteration can proceed. SetPaused(true)
// stops the TUI from replenishing the channel; future iterations block
// on Chan() until SetPaused(false) is called, at which point one token
// is posted to release the loop.
//
// The buffered capacity-1 channel and the mutex together guarantee that
// at most one token is pending at any time: extra IterDone or Resume
// calls become no-ops, so the loop never advances by more than one iter
// per release event.
type Gate struct {
	ch chan struct{}

	mu     sync.Mutex
	paused bool
}

// NewGate returns an empty Gate. The first call to Chan() returns a
// channel with no token; the loop will block on it until IterDone()
// runs (or, in practice, the first iteration runs without consulting
// the gate, see Loop.Run).
func NewGate() *Gate {
	return &Gate{ch: make(chan struct{}, 1)}
}

// Chan returns the receive-only channel the loop reads from before
// starting each iteration > 1.
func (g *Gate) Chan() <-chan struct{} {
	return g.ch
}

// IterDone signals that an iteration has completed. When the TUI is
// not paused, a token is posted so the loop can proceed; when paused,
// the call is a no-op and the loop will block in Chan() until Resume.
func (g *Gate) IterDone() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.paused {
		return
	}
	g.post()
}

// SetPaused toggles the paused flag. Resuming after a pause posts a
// token immediately so the gated loop iteration runs without waiting
// for the next IterDone.
func (g *Gate) SetPaused(p bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.paused == p {
		return
	}
	g.paused = p
	if !p {
		g.post()
	}
}

// Paused reports the current paused state.
func (g *Gate) Paused() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.paused
}

// post tries to put a token on the channel without blocking. The
// buffered capacity-1 channel coalesces redundant posts, so the loop
// never sees a backlog of release events.
func (g *Gate) post() {
	select {
	case g.ch <- struct{}{}:
	default:
	}
}
