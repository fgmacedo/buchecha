package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/fgmacedo/buchecha/internal/loop"
)

// eventMsg wraps a single loop.Event delivered by the bridge.
//
// closed is true when the upstream events channel has been closed by
// the loop (final signal); ev is then the zero value and must not be
// inspected.
type eventMsg struct {
	ev     loop.Event
	closed bool
}

// readEventCmd returns a tea.Cmd that reads exactly one event from the
// loop's events channel and turns it into an eventMsg. After Update
// processes the message, it must return readEventCmd again to keep
// pumping; the cmd self-terminates when the channel closes by emitting
// eventMsg{closed: true}.
//
// One event per cmd keeps the read serial with respect to bubbletea's
// Update step, so panel state mutations stay race-free without locks.
func readEventCmd(events <-chan loop.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-events
		if !ok {
			return eventMsg{closed: true}
		}
		return eventMsg{ev: ev}
	}
}
