package cli

import (
	"testing"
	"time"

	"github.com/fgmacedo/buchecha/internal/loop"
)

// TestTeeLoopEvents_ForwardsToBothChannels covers the happy path: every
// event read from src appears on both transient and persistent.
func TestTeeLoopEvents_ForwardsToBothChannels(t *testing.T) {
	t.Parallel()

	src := make(chan loop.Event, 4)
	transient := make(chan loop.Event, 4)
	persistent := make(chan loop.Event, 4)

	go teeLoopEvents(src, transient, persistent)

	want := []loop.Event{
		loop.IterationStarted{Index: 1},
		loop.IterationFinished{Index: 1, DurationMS: 10},
		loop.LoopFinished{Reason: "ok", ExitCode: 0},
	}
	for _, ev := range want {
		src <- ev
	}
	close(src)

	deadline := time.After(time.Second)
	for i, ev := range want {
		select {
		case got := <-transient:
			if got != ev {
				t.Errorf("transient[%d]: got %#v, want %#v", i, got, ev)
			}
		case <-deadline:
			t.Fatalf("transient[%d]: timeout", i)
		}
		select {
		case got := <-persistent:
			if got != ev {
				t.Errorf("persistent[%d]: got %#v, want %#v", i, got, ev)
			}
		case <-deadline:
			t.Fatalf("persistent[%d]: timeout", i)
		}
	}

	// transient must close after src closes so per-run consumers exit.
	select {
	case _, ok := <-transient:
		if ok {
			t.Errorf("transient should be closed after src closes")
		}
	case <-deadline:
		t.Fatalf("transient close: timeout")
	}

	// persistent must NOT close: services subscribers outlive a single
	// l.Run, so the bcc run's defer owns the close.
	select {
	case _, ok := <-persistent:
		if !ok {
			t.Errorf("persistent must stay open across run boundaries")
		} else {
			t.Errorf("persistent received unexpected event after src close")
		}
	case <-time.After(50 * time.Millisecond):
		// Expected: nothing arrives, channel still open.
	}
}

// TestTeeLoopEvents_NilPersistent confirms a nil persistent channel
// disables services forwarding without affecting the transient leg.
func TestTeeLoopEvents_NilPersistent(t *testing.T) {
	t.Parallel()

	src := make(chan loop.Event, 1)
	transient := make(chan loop.Event, 1)

	go teeLoopEvents(src, transient, nil)

	src <- loop.LoopFinished{Reason: "done", ExitCode: 0}
	close(src)

	select {
	case got, ok := <-transient:
		if !ok {
			t.Fatal("transient closed before delivering the event")
		}
		if _, isFinal := got.(loop.LoopFinished); !isFinal {
			t.Errorf("got %#v, want LoopFinished", got)
		}
	case <-time.After(time.Second):
		t.Fatal("transient: timeout")
	}
	select {
	case _, ok := <-transient:
		if ok {
			t.Errorf("transient should be closed after src closes")
		}
	case <-time.After(time.Second):
		t.Fatal("transient close: timeout")
	}
}

// TestTeeLoopEvents_DropsWhenChannelFull asserts the non-blocking send
// contract: a full transient or persistent channel does not stall the
// tee, matching the EventService back-pressure behavior.
func TestTeeLoopEvents_DropsWhenChannelFull(t *testing.T) {
	t.Parallel()

	src := make(chan loop.Event, 4)
	// Capacity 1 so the second send drops.
	transient := make(chan loop.Event, 1)
	persistent := make(chan loop.Event, 1)

	done := make(chan struct{})
	go func() {
		teeLoopEvents(src, transient, persistent)
		close(done)
	}()

	src <- loop.IterationStarted{Index: 1}
	src <- loop.IterationStarted{Index: 2}
	src <- loop.IterationStarted{Index: 3}
	close(src)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("tee did not exit after src close; sends must be non-blocking")
	}

	// transient and persistent each captured exactly one event before
	// the buffer filled; the rest were dropped.
	if got := len(transient); got != 1 {
		t.Errorf("transient len: got %d, want 1", got)
	}
	if got := len(persistent); got != 1 {
		t.Errorf("persistent len: got %d, want 1", got)
	}
}
