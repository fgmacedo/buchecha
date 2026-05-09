package tui

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/fgmacedo/buchecha/internal/loop"
	"github.com/fgmacedo/buchecha/internal/services"
	"github.com/fgmacedo/buchecha/internal/supervision"
)

// testSvcResult groups the test services with its input channel and a
// function that closes the channel exactly once (safe to call from
// t.Cleanup and from the test body).
type testSvcResult struct {
	Svc       *services.Services
	SessionID string
	Events    chan loop.Event
	Close     func() // closes Events exactly once
}

// newTestSvc creates a *services.Services backed by a freshly created
// session in t.TempDir(). events is the raw loop events channel tests
// push into; Close shuts it down (idempotent). The Cleanup is not
// registered automatically so tests that close early can call it
// themselves without a double-close panic.
func newTestSvc(t *testing.T) testSvcResult {
	t.Helper()
	tmp := t.TempDir()
	store, _, err := supervision.CreateSession(
		filepath.Join(tmp, ".bcc"),
		"testspec.md",
		"testhash",
		time.Now(),
	)
	if err != nil {
		t.Fatalf("newTestSvc: create session: %v", err)
	}
	events := make(chan loop.Event, 16)
	var once sync.Once
	closeFn := func() { once.Do(func() { close(events) }) }
	svc := services.New(services.Deps{
		LoopEvents:   events,
		SessionStore: store,
	})
	return testSvcResult{
		Svc:       svc,
		SessionID: store.Session().ID,
		Events:    events,
		Close:     closeFn,
	}
}
