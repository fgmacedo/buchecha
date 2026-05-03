package tui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/fgmacedo/buchecha/internal/loop"
	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
)

// sessionKeyMap is the binding set the dashboard honors after the inner
// loop has terminated and the Model latches sessionMode. The Journal
// binding is intentionally absent: the journal viewer is carved out to
// the spec-vendor-neutrality spec because rendering the latest entry
// requires a format-aware adapter (today the journal lives in the spec
// markdown; other adapters expose it differently).
type sessionKeyMap struct {
	Resume key.Binding
	Edit   key.Binding
	Quit   key.Binding
}

func (k sessionKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Resume, k.Edit, k.Quit}
}

func (k sessionKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Resume, k.Edit, k.Quit}}
}

func defaultSessionKeyMap() sessionKeyMap {
	return sessionKeyMap{
		Resume: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "resume the loop"),
		),
		Edit: key.NewBinding(
			key.WithKeys("e"),
			key.WithHelp("e", "edit spec in $EDITOR"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q / Ctrl+C", "exit"),
		),
	}
}

// restartLoopMsg signals the host (cmd/run.go) that the user pressed
// [r] in the session menu. The host's factory closure builds a fresh
// loop.Loop, spawns its goroutine, and returns the new events channel
// via rebindEventsMsg so the Model can resume pumping.
type restartLoopMsg struct{}

// rebindEventsMsg carries a freshly built events channel produced by the
// host's NewEvents factory. The Model swaps its bridge channel and exits
// session mode so panels live-update against the new loop.
type rebindEventsMsg struct {
	events <-chan loop.Event
}

// editorFinishedMsg is the result of an [e] edit: the editor process has
// exited and the terminal is restored. The Model returns to session mode
// (post-edit re-parse is carved out to spec-vendor-neutrality).
type editorFinishedMsg struct {
	err error
}

// sessionStatus is the human-friendly form of a session signal,
// derived from the agent's last iteration_result (when known) or from
// the loop's terminal reason otherwise.
func sessionStatus(reason string, sig agentcontract.Signal) string {
	if sig != agentcontract.SignalUnknown {
		return sig.String()
	}
	switch reason {
	case "max_iterations":
		return "max iterations"
	case "head_stuck":
		return "head stuck"
	case "planner_failed":
		return "planner failed"
	case "":
		return "idle"
	default:
		return reason
	}
}

// renderSessionMenu produces the menu line the Model appends to the
// frozen dashboard while sessionMode is latched. The line lists only
// the bindings whose handlers are wired (Resume, Edit, Quit); the
// Journal binding lands together with the carved-out viewer.
func renderSessionMenu(h help.Model, k sessionKeyMap, status string) string {
	var b strings.Builder
	label := fmt.Sprintf("[ session: %s ]", status)
	b.WriteString(theme.title.Render(label))
	b.WriteByte('\n')
	b.WriteString("  ")
	b.WriteString(h.ShortHelpView(k.ShortHelp()))
	b.WriteByte('\n')
	return b.String()
}

// editSpecCmd suspends the program with ReleaseTerminal, runs $EDITOR
// (falling back to $VISUAL) on the spec path, and restores the terminal
// with RestoreTerminal. Returns an editorFinishedMsg with the editor's
// error (or a hint message wrapped as an error when neither $EDITOR nor
// $VISUAL is set). The post-edit spec re-parse is carved out to the
// spec-vendor-neutrality spec.
//
// The function takes a tea.Program rather than a tea.Cmd-shaped closure
// because ReleaseTerminal / RestoreTerminal are program-scoped methods
// in bubbletea v2.
func editSpecCmd(program *tea.Program, specPath string) tea.Cmd {
	return func() tea.Msg {
		editor := os.Getenv("EDITOR")
		if editor == "" {
			editor = os.Getenv("VISUAL")
		}
		if editor == "" {
			return editorFinishedMsg{
				err: fmt.Errorf("$EDITOR (or $VISUAL) not set; cannot edit spec"),
			}
		}
		if program == nil {
			return editorFinishedMsg{
				err: fmt.Errorf("editor invocation requires a program reference"),
			}
		}

		if err := program.ReleaseTerminal(); err != nil {
			return editorFinishedMsg{err: fmt.Errorf("release terminal: %w", err)}
		}
		// Defer restore so a panic inside exec.Run still hands the
		// terminal back to the alt-screen renderer.
		var runErr error
		func() {
			defer func() {
				if rerr := program.RestoreTerminal(); rerr != nil && runErr == nil {
					runErr = fmt.Errorf("restore terminal: %w", rerr)
				}
			}()
			cmd := exec.Command(editor, specPath)
			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			runErr = cmd.Run()
		}()
		return editorFinishedMsg{err: runErr}
	}
}
