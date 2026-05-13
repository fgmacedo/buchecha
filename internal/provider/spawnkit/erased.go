package spawnkit

import (
	"time"

	"github.com/fgmacedo/buchecha/internal/loop"
	"github.com/fgmacedo/buchecha/internal/provider"
	"github.com/fgmacedo/buchecha/internal/supervision"
	"github.com/fgmacedo/buchecha/internal/supervision/session"
)

// The provider port carries SessionStore and LoopEvents as `any` so the
// provider package itself stays free of dependencies on internal/loop and
// internal/supervision. The helpers below perform the runtime type
// assertions inside spawnkit so each adapter can stay strictly within its
// declared layer-rule imports (stdlib + internal/provider +
// internal/provider/spawnkit + internal/loop/agentcontract).

// NewSpawnID is a thin re-export of supervision.NewSpawnID so adapters
// can mint spawn ids without importing the supervision root themselves.
func NewSpawnID() string { return supervision.NewSpawnID() }

// PersistPromptFromAny persists prompt to <store.SpawnsDir()>/<spawnID>.md
// when storeAny is a non-nil *session.Store; otherwise it returns
// ("", false, nil) so the adapter can skip prompt persistence cleanly.
// The bool return reports whether persistence happened.
func PersistPromptFromAny(storeAny any, spawnID, prompt string) (path string, ok bool, err error) {
	store, isStore := storeAny.(*session.Store)
	if !isStore || store == nil {
		return "", false, nil
	}
	p, perr := PersistPrompt(store, spawnID, prompt)
	if perr != nil {
		return "", false, perr
	}
	return p, true, nil
}

// EmitSpawnStartedAny forwards to EmitSpawnStarted when eventsAny is a
// non-nil chan<- loop.Event; otherwise it is a no-op. Adapters keep
// internal/loop out of their own import list this way.
func EmitSpawnStartedAny(eventsAny any, info SpawnInfo, promptPath string, at time.Time) {
	events, ok := eventsAny.(chan<- loop.Event)
	if !ok || events == nil {
		return
	}
	EmitSpawnStarted(events, info, promptPath, at)
}

// EmitSpawnFinishedAny forwards to EmitSpawnFinished when eventsAny is a
// non-nil chan<- loop.Event; otherwise it is a no-op.
func EmitSpawnFinishedAny(eventsAny any, info SpawnInfo, result provider.SpawnResult, at time.Time) {
	events, ok := eventsAny.(chan<- loop.Event)
	if !ok || events == nil {
		return
	}
	EmitSpawnFinished(events, info, result, at)
}
