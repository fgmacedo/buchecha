package events

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/fgmacedo/buchecha/internal/loop"
	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
	"github.com/fgmacedo/buchecha/internal/supervision/session"
)

// writeManifest writes an archived session manifest under
// baseDir/sessions/<id>/manifest.json. Mirrors what session.CreateSession
// + session.Touch would produce in production.
func writeManifest(t *testing.T, baseDir string, sess session.Session) {
	t.Helper()
	dir := filepath.Join(baseDir, "sessions", sess.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), body, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

// liveDeps builds a Deps wired with a freshly created live session,
// the loop events channel, and the sessions base dir.
func liveDeps(t *testing.T, sessionID string) (Deps, chan loop.Event) {
	t.Helper()
	tmp := t.TempDir()
	baseDir := filepath.Join(tmp, ".bcc")
	now := time.Now().UTC().Truncate(time.Second)
	sess := session.Session{
		ID:        sessionID,
		SpecPath:  "/spec/x.md",
		SpecHash:  "h",
		CreatedAt: now,
		UpdatedAt: now,
		Status:    session.SessionRunning,
	}
	writeManifest(t, baseDir, sess)
	store, err := session.OpenSession(baseDir, sess.ID)
	if err != nil {
		t.Fatalf("open session: %v", err)
	}
	ch := make(chan loop.Event, 16)
	deps := Deps{
		LoopEvents:      ch,
		SessionStore:    store,
		SessionsBaseDir: baseDir,
	}
	return deps, ch
}

func TestEventService_Subscribe_LiveOrderingAndSeq(t *testing.T) {
	t.Parallel()
	deps, ch := liveDeps(t, "abcabcabc101")
	svc := New(deps)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sub, err := svc.Subscribe(ctx, "abcabcabc101", 0)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	for i := 1; i <= 5; i++ {
		ch <- loop.IterationStarted{Index: i, MaxIter: 10}
	}
	close(ch)

	var got []SeqEvent
	for se := range sub {
		got = append(got, se)
	}
	if len(got) != 5 {
		t.Fatalf("got %d events, want 5", len(got))
	}
	for i, se := range got {
		if se.Seq != int64(i+1) {
			t.Fatalf("got[%d].Seq = %d, want %d", i, se.Seq, i+1)
		}
		ev, ok := se.Event.(loop.IterationStarted)
		if !ok {
			t.Fatalf("got[%d].Event type = %T", i, se.Event)
		}
		if ev.Index != i+1 {
			t.Fatalf("got[%d].Index = %d, want %d", i, ev.Index, i+1)
		}
	}
}

// TestEventService_Fanout_EnrichesAgentEventWithActiveTask covers the
// active-task attribution: when a TaskStarted event opens a task for
// AgentID X, every AgentEventReceived bearing AgentID X that follows
// (until TaskCompleted/Approved/NeedsFix) inherits the task id on its
// embedded AgentEvent.
func TestEventService_Fanout_EnrichesAgentEventWithActiveTask(t *testing.T) {
	t.Parallel()
	deps, ch := liveDeps(t, "e1234500aaaa")
	svc := New(deps)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sub, err := svc.Subscribe(ctx, "e1234500aaaa", 0)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	ch <- loop.TaskStarted{AgentID: "exec-A", PhaseID: "P1", TaskID: "T1.1"}
	ch <- loop.TaskStarted{AgentID: "exec-B", PhaseID: "P2", TaskID: "T2.1"}
	ch <- loop.AgentEventReceived{Event: agentcontract.AgentEvent{
		Kind:    agentcontract.KindToolUse,
		AgentID: "exec-A",
		Role:    agentcontract.RoleExecutor,
	}}
	ch <- loop.AgentEventReceived{Event: agentcontract.AgentEvent{
		Kind:    agentcontract.KindAssistantText,
		AgentID: "exec-B",
		Role:    agentcontract.RoleExecutor,
	}}
	ch <- loop.AgentEventReceived{Event: agentcontract.AgentEvent{
		Kind: agentcontract.KindThinking,
	}}
	ch <- loop.TaskCompleted{AgentID: "exec-A", PhaseID: "P1", TaskID: "T1.1"}
	ch <- loop.AgentEventReceived{Event: agentcontract.AgentEvent{
		Kind:    agentcontract.KindToolUse,
		AgentID: "exec-A",
	}}
	close(ch)

	var got []SeqEvent
	for se := range sub {
		got = append(got, se)
	}
	if len(got) != 7 {
		t.Fatalf("got %d events, want 7", len(got))
	}

	if are, ok := got[2].Event.(loop.AgentEventReceived); !ok {
		t.Fatalf("got[2] type = %T", got[2].Event)
	} else if are.Event.TaskID != "T1.1" {
		t.Fatalf("got[2].TaskID = %q, want %q", are.Event.TaskID, "T1.1")
	}

	if are, ok := got[3].Event.(loop.AgentEventReceived); !ok {
		t.Fatalf("got[3] type = %T", got[3].Event)
	} else if are.Event.TaskID != "T2.1" {
		t.Fatalf("got[3].TaskID = %q, want %q", are.Event.TaskID, "T2.1")
	}

	if are, ok := got[4].Event.(loop.AgentEventReceived); !ok {
		t.Fatalf("got[4] type = %T", got[4].Event)
	} else if are.Event.TaskID != "" {
		t.Fatalf("got[4].TaskID = %q, want empty", are.Event.TaskID)
	}

	if are, ok := got[6].Event.(loop.AgentEventReceived); !ok {
		t.Fatalf("got[6] type = %T", got[6].Event)
	} else if are.Event.TaskID != "" {
		t.Fatalf("got[6].TaskID = %q, want empty", are.Event.TaskID)
	}
}

// TestEventService_Fanout_PersistsEventsLog verifies that when
// EventsLogPath is set, fanout writes one envelope per SeqEvent so the
// file round-trips through Replay.
func TestEventService_Fanout_PersistsEventsLog(t *testing.T) {
	t.Parallel()
	deps, ch := liveDeps(t, "e2222200aaaa")
	logPath := filepath.Join(deps.SessionStore.SessionDir(), "events.ndjson")
	deps.EventsLogPath = logPath
	svc := New(deps)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sub, err := svc.Subscribe(ctx, "e2222200aaaa", 0)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	ch <- loop.IterationStarted{Index: 1, MaxIter: 3}
	ch <- loop.TaskStarted{AgentID: "exec-A", PhaseID: "P1", TaskID: "T1.1"}
	ch <- loop.AgentEventReceived{Event: agentcontract.AgentEvent{
		Kind:    agentcontract.KindToolUse,
		AgentID: "exec-A",
		Role:    agentcontract.RoleExecutor,
		Tool:    &agentcontract.ToolCallInfo{ID: "t1", Name: "Read"},
	}}
	ch <- loop.LoopFinished{Reason: "ok", ExitCode: 0}

	for range sub {
		// drain
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read events.ndjson: %v", err)
	}
	lines := 0
	for _, b := range data {
		if b == '\n' {
			lines++
		}
	}
	if lines != 4 {
		t.Fatalf("events.ndjson has %d lines, want 4: %s", lines, string(data))
	}

	replayCtx, replayCancel := context.WithCancel(context.Background())
	defer replayCancel()
	replay, err := svc.Replay(replayCtx, "e2222200aaaa", 0)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	var got []SeqEvent
	for se := range replay {
		got = append(got, se)
	}
	if len(got) != 4 {
		t.Fatalf("replay got %d events, want 4", len(got))
	}
	if _, ok := got[0].Event.(loop.IterationStarted); !ok {
		t.Fatalf("got[0] type = %T", got[0].Event)
	}
	if are, ok := got[2].Event.(loop.AgentEventReceived); !ok {
		t.Fatalf("got[2] type = %T", got[2].Event)
	} else if are.Event.TaskID != "T1.1" {
		t.Fatalf("got[2] task_id = %q, want T1.1", are.Event.TaskID)
	}
}

// TestEventService_Subscribe_LiveAlias covers the SPA's bootstrap path:
// the dashboard opens an EventSource at /api/v1/sessions/live/events
// before it has a real session id.
func TestEventService_Subscribe_LiveAlias(t *testing.T) {
	t.Parallel()
	deps, ch := liveDeps(t, "abcabcabc104")
	svc := New(deps)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sub, err := svc.Subscribe(ctx, LiveSessionAlias, 0)
	if err != nil {
		t.Fatalf("Subscribe(live): %v", err)
	}
	ch <- loop.IterationStarted{Index: 1, MaxIter: 1}
	ch <- loop.LoopFinished{Reason: "done", ExitCode: 0}

	var got []SeqEvent
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case se, ok := <-sub:
			if !ok {
				if len(got) != 2 {
					t.Fatalf("got %d events, want 2", len(got))
				}
				return
			}
			got = append(got, se)
		case <-deadline.C:
			t.Fatalf("subscriber did not close after LoopFinished; got %d events", len(got))
		}
	}
}

func TestEventService_Subscribe_LoopFinishedClosesSubscriber(t *testing.T) {
	t.Parallel()
	deps, ch := liveDeps(t, "abcabcabc102")
	svc := New(deps)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sub, err := svc.Subscribe(ctx, "abcabcabc102", 0)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	ch <- loop.IterationStarted{Index: 1}
	ch <- loop.LoopFinished{Reason: "done", ExitCode: 0}

	var got []SeqEvent
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case se, ok := <-sub:
			if !ok {
				if len(got) != 2 {
					t.Fatalf("got %d events, want 2", len(got))
				}
				if _, ok := got[1].Event.(loop.LoopFinished); !ok {
					t.Fatalf("last event = %T, want LoopFinished", got[1].Event)
				}
				return
			}
			got = append(got, se)
		case <-deadline.C:
			t.Fatalf("subscriber did not close after LoopFinished; got %d events", len(got))
		}
	}
}

func TestEventService_Subscribe_RingOverflowReturnsSeqGone(t *testing.T) {
	t.Parallel()
	deps, ch := liveDeps(t, "abcabcabc103")
	svc := New(deps)
	svc.ensureFanout()

	const overflow = ringSize + 50
	done := make(chan struct{})
	go func() {
		for i := 1; i <= overflow; i++ {
			ch <- loop.IterationStarted{Index: i}
		}
		close(done)
	}()
	<-done
	deadline := time.Now().Add(2 * time.Second)
	for {
		svc.mu.Lock()
		ringLen := len(svc.ring)
		next := svc.nextSeq
		svc.mu.Unlock()
		if ringLen == ringSize && next >= int64(overflow+1) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("fan-out did not catch up; ring=%d nextSeq=%d", ringLen, next)
		}
		time.Sleep(2 * time.Millisecond)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err := svc.Subscribe(ctx, "abcabcabc103", 1)
	if !errors.Is(err, ErrSeqGone) {
		t.Fatalf("err = %v, want ErrSeqGone", err)
	}
	close(ch)
}

func TestEventService_Subscribe_CtxCancelClosesSubscriber(t *testing.T) {
	t.Parallel()
	deps, ch := liveDeps(t, "abcabcabc104")
	svc := New(deps)
	defer close(ch)

	ctx, cancel := context.WithCancel(context.Background())
	sub, err := svc.Subscribe(ctx, "abcabcabc104", 0)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	cancel()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case _, ok := <-sub:
			if !ok {
				return
			}
		case <-deadline.C:
			t.Fatal("subscriber did not close after ctx cancel")
		}
	}
}

func TestEventService_Subscribe_MultipleSubscribersSeeIdenticalSeqs(t *testing.T) {
	t.Parallel()
	deps, ch := liveDeps(t, "abcabcabc105")
	svc := New(deps)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const subs = 3
	const events = 8
	channels := make([]<-chan SeqEvent, subs)
	for i := range channels {
		c, err := svc.Subscribe(ctx, "abcabcabc105", 0)
		if err != nil {
			t.Fatalf("Subscribe: %v", err)
		}
		channels[i] = c
	}
	for i := 1; i <= events; i++ {
		ch <- loop.IterationStarted{Index: i}
	}
	close(ch)

	var wg sync.WaitGroup
	wg.Add(subs)
	results := make([][]int64, subs)
	for i := range channels {
		i := i
		go func() {
			defer wg.Done()
			for se := range channels[i] {
				results[i] = append(results[i], se.Seq)
			}
		}()
	}
	wg.Wait()
	for i := range results {
		if len(results[i]) != events {
			t.Fatalf("subscriber %d saw %d events, want %d", i, len(results[i]), events)
		}
		for j, seq := range results[i] {
			if seq != int64(j+1) {
				t.Fatalf("subscriber %d got seq[%d] = %d, want %d", i, j, seq, j+1)
			}
		}
	}
}

func TestEventService_Subscribe_BufferedReplayThenLive(t *testing.T) {
	t.Parallel()
	deps, ch := liveDeps(t, "abcabcabc106")
	svc := New(deps)
	svc.ensureFanout()

	ch <- loop.IterationStarted{Index: 1}
	ch <- loop.IterationStarted{Index: 2}
	ch <- loop.IterationStarted{Index: 3}
	deadline := time.Now().Add(2 * time.Second)
	for {
		svc.mu.Lock()
		next := svc.nextSeq
		svc.mu.Unlock()
		if next >= 4 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fan-out did not catch up before Subscribe")
		}
		time.Sleep(2 * time.Millisecond)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sub, err := svc.Subscribe(ctx, "abcabcabc106", 2)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	ch <- loop.IterationStarted{Index: 4}
	ch <- loop.LoopFinished{Reason: "done"}

	var got []int64
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for {
		select {
		case se, ok := <-sub:
			if !ok {
				close(ch)
				if want := []int64{2, 3, 4, 5}; !equalInts(got, want) {
					t.Fatalf("got %v, want %v", got, want)
				}
				return
			}
			got = append(got, se.Seq)
		case <-timer.C:
			t.Fatalf("timeout waiting for events; got %v", got)
		}
	}
}

func TestEventService_Subscribe_UnknownSession(t *testing.T) {
	t.Parallel()
	deps, ch := liveDeps(t, "abcabcabc107")
	defer close(ch)
	svc := New(deps)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err := svc.Subscribe(ctx, "doesntmatch01", 0)
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("err = %v, want ErrSessionNotFound", err)
	}
}

func TestEventService_Replay_OrderedThenCloses(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	baseDir := filepath.Join(tmp, ".bcc")
	now := time.Now().UTC().Truncate(time.Second)
	sess := session.Session{
		ID:        "abcabcabc108",
		SpecPath:  "/spec/r.md",
		SpecHash:  "h",
		CreatedAt: now,
		UpdatedAt: now,
		Status:    session.SessionDone,
	}
	writeManifest(t, baseDir, sess)
	sessionDir := filepath.Join(baseDir, "sessions", sess.ID)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeReplayLog(t, sessionDir, []replayEnvelope{
		{Seq: 1, Event: encodeEventJSON(t, "iter_started", map[string]any{"index": 1, "max_iter": 5})},
		{Seq: 2, Event: encodeEventJSON(t, "task_started", map[string]any{"phase_id": "P1", "task_id": "T1"})},
		{Seq: 3, Event: encodeEventJSON(t, "loop_finished", map[string]any{"reason": "done", "exit_code": 0})},
	})

	svc := New(Deps{SessionsBaseDir: baseDir})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out, err := svc.Replay(ctx, sess.ID, 1)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	var got []int64
	for se := range out {
		got = append(got, se.Seq)
	}
	if want := []int64{1, 2, 3}; !equalInts(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestEventService_Replay_FromSeqSkipsLowerSeqs(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	baseDir := filepath.Join(tmp, ".bcc")
	now := time.Now().UTC().Truncate(time.Second)
	sess := session.Session{
		ID:        "abcabcabc109",
		SpecPath:  "/spec/q.md",
		SpecHash:  "h",
		CreatedAt: now,
		UpdatedAt: now,
		Status:    session.SessionDone,
	}
	writeManifest(t, baseDir, sess)
	sessionDir := filepath.Join(baseDir, "sessions", sess.ID)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeReplayLog(t, sessionDir, []replayEnvelope{
		{Seq: 1, Event: encodeEventJSON(t, "iter_started", map[string]any{"index": 1, "max_iter": 5})},
		{Seq: 2, Event: encodeEventJSON(t, "iter_started", map[string]any{"index": 2, "max_iter": 5})},
		{Seq: 3, Event: encodeEventJSON(t, "iter_started", map[string]any{"index": 3, "max_iter": 5})},
	})

	svc := New(Deps{SessionsBaseDir: baseDir})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out, err := svc.Replay(ctx, sess.ID, 2)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	var got []int64
	for se := range out {
		got = append(got, se.Seq)
	}
	if want := []int64{2, 3}; !equalInts(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestEventService_Replay_MissingFileClosesCleanly(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	baseDir := filepath.Join(tmp, ".bcc")
	now := time.Now().UTC().Truncate(time.Second)
	sess := session.Session{
		ID:        "abcabcabc110",
		SpecPath:  "/spec/n.md",
		SpecHash:  "h",
		CreatedAt: now,
		UpdatedAt: now,
		Status:    session.SessionDone,
	}
	writeManifest(t, baseDir, sess)

	svc := New(Deps{SessionsBaseDir: baseDir})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out, err := svc.Replay(ctx, sess.ID, 0)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	for range out {
		t.Fatal("expected no events from missing file")
	}
}

func TestEventService_Replay_UnknownSession(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	svc := New(Deps{SessionsBaseDir: filepath.Join(tmp, ".bcc")})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err := svc.Replay(ctx, "000000000000", 0)
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("err = %v, want ErrSessionNotFound", err)
	}
}

func writeReplayLog(t *testing.T, sessionDir string, envelopes []replayEnvelope) {
	t.Helper()
	path := filepath.Join(sessionDir, "events.ndjson")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer f.Close()
	for _, env := range envelopes {
		body, err := json.Marshal(env)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		body = append(body, '\n')
		if _, err := f.Write(body); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
}

func encodeEventJSON(t *testing.T, kind string, fields map[string]any) json.RawMessage {
	t.Helper()
	fields["type"] = kind
	body, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	return body
}

func equalInts(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
