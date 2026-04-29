package tui

import "github.com/fgmacedo/buchecha/internal/loop"

// loopSuspectWindow is the size of the rolling window the loop-suspect
// detector keeps. Spec: "last 10 KindToolUse events".
const loopSuspectWindow = 10

// loopSuspectThreshold is how many of the last loopSuspectWindow events
// must share the same key to flag the run. Spec: "≥ 7 share (name,
// primary_arg)".
const loopSuspectThreshold = 7

// loopSuspectKey is the dedup key for the heuristic: the tool name plus
// the most user-relevant string argument (file_path / path / command /
// pattern / url / query, picked by primaryArg). Two calls with the same
// key are "the same call" for the purpose of detecting a loop.
type loopSuspectKey struct {
	name string
	arg  string
}

// loopSuspect is a fixed-size circular ring of recent tool_use events.
// Filled lazily; once n == loopSuspectWindow, it overwrites the oldest
// entry on each push.
type loopSuspect struct {
	ring [loopSuspectWindow]loopSuspectKey
	n    int
	head int
}

// onAgentEvent records one tool_use event into the ring. Other event
// kinds are ignored; the heuristic measures repetitive tool calls only.
// Tool calls with no primary arg still produce a key (with arg=""), so
// repeated no-arg calls to the same tool count as a loop too.
func (l *loopSuspect) onAgentEvent(ev loop.AgentEvent) {
	if ev.Kind != loop.KindToolUse || ev.Tool == nil {
		return
	}
	l.ring[l.head] = loopSuspectKey{
		name: ev.Tool.Name,
		arg:  primaryArg(ev.Tool.Args),
	}
	l.head = (l.head + 1) % loopSuspectWindow
	if l.n < loopSuspectWindow {
		l.n++
	}
}

// triggered reports whether the rolling window is full AND the most
// frequent key reaches loopSuspectThreshold. Returns the dominant key
// and its count when triggered; the zero key and zero count otherwise.
//
// A partially filled window never triggers: the heuristic needs the
// full sample size to be honest.
func (l loopSuspect) triggered() (loopSuspectKey, int, bool) {
	if l.n < loopSuspectWindow {
		return loopSuspectKey{}, 0, false
	}
	counts := make(map[loopSuspectKey]int, loopSuspectWindow)
	var bestKey loopSuspectKey
	bestN := 0
	for i := 0; i < l.n; i++ {
		k := l.ring[i]
		counts[k]++
		if counts[k] > bestN {
			bestN = counts[k]
			bestKey = k
		}
	}
	if bestN >= loopSuspectThreshold {
		return bestKey, bestN, true
	}
	return loopSuspectKey{}, 0, false
}
