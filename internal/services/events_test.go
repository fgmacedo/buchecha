package services

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/fgmacedo/buchecha/internal/director"
	"github.com/fgmacedo/buchecha/internal/loop"
)

// liveDeps builds a Deps wired with a freshly created live session,
// the loop events channel, and the sessions base dir. The test owns
// the loop events channel and sends events into it so the fan-out
// drains predictably.
func liveDeps(t *testing.T, sessionID string) (Deps, chan loop.Event) {
	t.Helper()
	tmp := t.TempDir()
	baseDir := filepath.Join(tmp, ".bcc")
	now := time.Now().UTC().Truncate(time.Second)
	sess := director.Session{
		ID:        sessionID,
		SpecPath:  "/spec/x.md",
		SpecHash:  "h",
		CreatedAt: now,
		UpdatedAt: now,
		Status:    director.SessionRunning,
	}
	writeManifest(t, baseDir, sess)
	store, err := director.OpenSession(baseDir, sess.ID)
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
	svc := newEventService(deps)
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

func TestEventService_Subscribe_LoopFinishedClosesSubscriber(t *testing.T) {
	t.Parallel()
	deps, ch := liveDeps(t, "abcabcabc102")
	svc := newEventService(deps)
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
	svc := newEventService(deps)
	svc.ensureFanout()

	// Fill the ring beyond capacity. Use a synchronous emitter so we
	// know fan-out has consumed every event before we Subscribe.
	const overflow = ringSize + 50
	done := make(chan struct{})
	go func() {
		for i := 1; i <= overflow; i++ {
			ch <- loop.IterationStarted{Index: i}
		}
		close(done)
	}()
	<-done
	// Drain: wait until ring is at capacity AND nextSeq advanced past
	// overflow. This is best-effort; the fan-out runs on its own
	// goroutine.
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
	svc := newEventService(deps)
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
	svc := newEventService(deps)
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
	svc := newEventService(deps)
	svc.ensureFanout()

	// Push a few events first, then Subscribe(fromSeq=2). The
	// subscriber should see seq 2 and 3 from the ring, then seq 4
	// live.
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
	svc := newEventService(deps)
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
	sess := director.Session{
		ID:        "abcabcabc108",
		SpecPath:  "/spec/r.md",
		SpecHash:  "h",
		CreatedAt: now,
		UpdatedAt: now,
		Status:    director.SessionDone,
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

	svc := newEventService(Deps{SessionsBaseDir: baseDir})
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
	sess := director.Session{
		ID:        "abcabcabc109",
		SpecPath:  "/spec/q.md",
		SpecHash:  "h",
		CreatedAt: now,
		UpdatedAt: now,
		Status:    director.SessionDone,
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

	svc := newEventService(Deps{SessionsBaseDir: baseDir})
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
	sess := director.Session{
		ID:        "abcabcabc110",
		SpecPath:  "/spec/n.md",
		SpecHash:  "h",
		CreatedAt: now,
		UpdatedAt: now,
		Status:    director.SessionDone,
	}
	writeManifest(t, baseDir, sess)

	svc := newEventService(Deps{SessionsBaseDir: baseDir})
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
	svc := newEventService(Deps{SessionsBaseDir: filepath.Join(tmp, ".bcc")})
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
