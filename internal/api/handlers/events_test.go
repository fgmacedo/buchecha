package handlers_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fgmacedo/buchecha/internal/api"
	"github.com/fgmacedo/buchecha/internal/director"
	"github.com/fgmacedo/buchecha/internal/loop"
	"github.com/fgmacedo/buchecha/internal/services"
)

// liveEventsServer seeds a live session and returns the test server,
// the loop event channel the test pushes events through, the live
// session id, and the EventService for direct fan-out priming when
// needed.
func liveEventsServer(t *testing.T) (*httptest.Server, chan loop.Event, string, *services.Services) {
	t.Helper()
	tmp := t.TempDir()
	baseDir := filepath.Join(tmp, ".bcc")
	now := time.Now().UTC().Truncate(time.Second)
	live := director.Session{
		ID:        "abcdef000559",
		SpecPath:  "/spec/sse.md",
		SpecHash:  "h",
		CreatedAt: now,
		UpdatedAt: now,
		Status:    director.SessionRunning,
	}
	writeManifest(t, baseDir, live)
	store, err := director.OpenSession(baseDir, live.ID)
	if err != nil {
		t.Fatalf("open live: %v", err)
	}
	ch := make(chan loop.Event, 32)
	svc := services.New(services.Deps{
		LoopEvents:      ch,
		SessionStore:    store,
		SessionsBaseDir: baseDir,
	})
	srv := httptest.NewServer(api.New(svc).Routes())
	t.Cleanup(srv.Close)
	return srv, ch, live.ID, svc
}

// sseEvent is one parsed SSE record. Fields default to empty when
// the corresponding line is absent.
type sseEvent struct {
	ID    string
	Event string
	Data  string
	Note  string // first comment line in the record, if any
}

// parseSSEStream reads the response body line by line and emits one
// sseEvent per blank-line-terminated record. Blocks until the body
// closes; the caller must stop the connection from another goroutine
// (e.g. ctx cancel).
func parseSSEStream(r io.Reader, out chan<- sseEvent) {
	defer close(out)
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	cur := sseEvent{}
	flush := func() {
		if cur == (sseEvent{}) {
			return
		}
		out <- cur
		cur = sseEvent{}
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			flush()
			continue
		}
		switch {
		case strings.HasPrefix(line, ":"):
			cur.Note = strings.TrimPrefix(line, ":")
		case strings.HasPrefix(line, "id:"):
			cur.ID = strings.TrimSpace(strings.TrimPrefix(line, "id:"))
		case strings.HasPrefix(line, "event:"):
			cur.Event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			cur.Data = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		}
	}
}

func TestEvents_LiveOrdering(t *testing.T) {
	t.Parallel()
	srv, ch, id, _ := liveEventsServer(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/v1/sessions/"+id+"/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get events: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: got %d, want 200 (body=%s)", resp.StatusCode, body)
	}
	if got := resp.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Errorf("content-type: got %q, want text/event-stream", got)
	}

	out := make(chan sseEvent, 16)
	go parseSSEStream(resp.Body, out)

	// Push a few events through the live channel; LoopFinished
	// flushes the response and closes the stream.
	go func() {
		ch <- loop.IterationStarted{Index: 1}
		ch <- loop.IterationStarted{Index: 2}
		ch <- loop.LoopFinished{Reason: "done", ExitCode: 0}
	}()

	var got []sseEvent
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for {
		select {
		case ev, ok := <-out:
			if !ok {
				goto done
			}
			got = append(got, ev)
		case <-timer.C:
			t.Fatalf("timeout waiting for events; got %d", len(got))
		}
	}
done:
	// First record carries the retry directive, no id/event.
	// Subsequent records are the seq events in order. Heartbeats
	// may interleave but the test interval is 15s so none should
	// fire within the 2-second budget.
	var seqs []string
	for _, ev := range got {
		if ev.ID != "" {
			seqs = append(seqs, ev.ID)
		}
	}
	if len(seqs) != 3 {
		t.Fatalf("seq records: got %v, want 3", seqs)
	}
	if seqs[0] != "1" || seqs[1] != "2" || seqs[2] != "3" {
		t.Fatalf("seqs: got %v, want [1 2 3]", seqs)
	}
	// LoopFinished should be the last record; verify event field.
	last := got[len(got)-1]
	if last.Event != "loop_finished" {
		t.Errorf("last event: got %q, want loop_finished", last.Event)
	}
}

func TestEvents_ReconnectWithLastEventID(t *testing.T) {
	t.Parallel()
	srv, ch, id, svc := liveEventsServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Prime the fan-out by registering a discard subscriber. The
	// fan-out goroutine starts on the first Subscribe; without it
	// events sit unassigned in the channel buffer when the SSE
	// handler connects, and the ring stays empty.
	primeCtx, cancelPrime := context.WithCancel(context.Background())
	defer cancelPrime()
	prime, err := svc.Events.Subscribe(primeCtx, id, 0)
	if err != nil {
		t.Fatalf("prime: %v", err)
	}
	go func() {
		for range prime {
		}
	}()

	// Push three events first, then connect with Last-Event-ID:1
	// so the server resumes from seq=2.
	ch <- loop.IterationStarted{Index: 1}
	ch <- loop.IterationStarted{Index: 2}
	ch <- loop.IterationStarted{Index: 3}
	// Wait until the fan-out has assigned seq numbers up to 3.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_, err := svc.Events.Subscribe(context.Background(), id, 4)
		if err == nil {
			break
		}
		if errors.Is(err, services.ErrSeqGone) {
			t.Fatalf("ring overflowed prematurely: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/v1/sessions/"+id+"/events", nil)
	req.Header.Set("Last-Event-ID", "1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get events: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want 200", resp.StatusCode)
	}

	out := make(chan sseEvent, 16)
	go parseSSEStream(resp.Body, out)

	// Close the source so the stream terminates.
	go func() {
		ch <- loop.LoopFinished{Reason: "done", ExitCode: 0}
	}()

	var seqs []int64
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for {
		select {
		case ev, ok := <-out:
			if !ok {
				goto done
			}
			if ev.ID != "" {
				n, err := strconv.ParseInt(ev.ID, 10, 64)
				if err != nil {
					t.Fatalf("parse id %q: %v", ev.ID, err)
				}
				seqs = append(seqs, n)
			}
		case <-timer.C:
			t.Fatalf("timeout; seqs=%v", seqs)
		}
	}
done:
	if len(seqs) == 0 {
		t.Fatal("got no seq records")
	}
	if seqs[0] != 2 {
		t.Fatalf("first seq: got %d, want 2 (full=%v)", seqs[0], seqs)
	}
}

func TestEvents_SeqGoneReturnsGone(t *testing.T) {
	t.Parallel()
	srv, ch, id, svc := liveEventsServer(t)

	// Prime the fan-out and overflow the ring so seq=1 is gone.
	svc.Events.Subscribe(context.Background(), id, 0) // start fan-out
	const ringSize = 1024
	const overflow = ringSize + 50
	go func() {
		for i := 1; i <= overflow; i++ {
			ch <- loop.IterationStarted{Index: i}
		}
	}()
	// Wait until seq=1 has been evicted.
	deadline := time.Now().Add(3 * time.Second)
	var ready bool
	for time.Now().Before(deadline) {
		_, err := svc.Events.Subscribe(context.Background(), id, 1)
		if errors.Is(err, services.ErrSeqGone) {
			ready = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !ready {
		t.Fatal("ring did not fill up to evict seq=1 within deadline")
	}

	// Now hit the SSE endpoint with Last-Event-ID:0 (resume from
	// 1) and assert 410 Gone with envelope code seq_gone.
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/sessions/"+id+"/events", nil)
	req.Header.Set("Last-Event-ID", "0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get events: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	if resp.StatusCode != http.StatusGone {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: got %d, want 410 (body=%s)", resp.StatusCode, body)
	}
	var env struct {
		Code services.ErrorCode `json:"code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.Code != services.CodeSeqGone {
		t.Errorf("envelope code: got %q, want %q", env.Code, services.CodeSeqGone)
	}
}

func TestEvents_HeartbeatOnFastInterval(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	baseDir := filepath.Join(tmp, ".bcc")
	now := time.Now().UTC().Truncate(time.Second)
	live := director.Session{
		ID:        "abcdef00abc1",
		SpecPath:  "/spec/hb.md",
		SpecHash:  "h",
		CreatedAt: now,
		UpdatedAt: now,
		Status:    director.SessionRunning,
	}
	writeManifest(t, baseDir, live)
	store, err := director.OpenSession(baseDir, live.ID)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	ch := make(chan loop.Event, 4)
	svc := services.New(services.Deps{
		LoopEvents:      ch,
		SessionStore:    store,
		SessionsBaseDir: baseDir,
	})
	// Build the api server with a 25ms heartbeat so the test does
	// not have to wait the production 15-second cadence to observe
	// the comment lines.
	server := api.New(svc).WithSSEHeartbeat(25 * time.Millisecond)
	srv := httptest.NewServer(server.Routes())
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/v1/sessions/"+live.ID+"/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get events: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	out := make(chan sseEvent, 32)
	go parseSSEStream(resp.Body, out)

	// Run for ~150ms then close the stream and collect what came
	// out. Heartbeats are emitted every 25ms so the count should
	// be at least three.
	var beats int
	deadline := time.NewTimer(200 * time.Millisecond)
	defer deadline.Stop()
loop:
	for {
		select {
		case ev, ok := <-out:
			if !ok {
				break loop
			}
			if ev.Note == "heartbeat" {
				beats++
				if beats >= 3 {
					break loop
				}
			}
		case <-deadline.C:
			break loop
		}
	}
	cancel()
	close(ch)

	if beats < 3 {
		t.Fatalf("heartbeat count: got %d, want >= 3", beats)
	}
}

func TestEvents_ReplaysArchivedSession(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	baseDir := filepath.Join(tmp, ".bcc")
	now := time.Now().UTC().Truncate(time.Second)
	archived := director.Session{
		ID:        "abcdef00a4ce",
		SpecPath:  "/spec/a.md",
		SpecHash:  "h",
		CreatedAt: now.Add(-1 * time.Hour),
		UpdatedAt: now.Add(-30 * time.Minute),
		Status:    director.SessionDone,
	}
	writeManifest(t, baseDir, archived)
	sessionDir := filepath.Join(baseDir, "sessions", archived.ID)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeReplayLog(t, sessionDir, [][2]any{
		{int64(1), map[string]any{"type": "iter_started", "index": 1, "max_iter": 5}},
		{int64(2), map[string]any{"type": "task_started", "phase_id": "P1", "task_id": "T1"}},
		{int64(3), map[string]any{"type": "loop_finished", "reason": "done", "exit_code": 0}},
	})

	svc := services.New(services.Deps{SessionsBaseDir: baseDir})
	srv := httptest.NewServer(api.New(svc).Routes())
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/api/v1/sessions/" + archived.ID + "/events")
	if err != nil {
		t.Fatalf("get events: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: got %d, want 200 (body=%s)", resp.StatusCode, body)
	}

	out := make(chan sseEvent, 16)
	go parseSSEStream(resp.Body, out)

	var seqs []string
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
loop:
	for {
		select {
		case ev, ok := <-out:
			if !ok {
				break loop
			}
			if ev.ID != "" {
				seqs = append(seqs, ev.ID)
			}
		case <-timer.C:
			break loop
		}
	}
	if len(seqs) != 3 {
		t.Fatalf("replay seqs: got %v, want 3 records", seqs)
	}
}

// writeReplayLog persists each entry as a JSON line under
// <sessionDir>/events.ndjson in the canonical replayEnvelope shape:
// {"seq":<n>,"event":<map>}. Tests use this to seed the archived
// path of the SSE handler.
func writeReplayLog(t *testing.T, sessionDir string, entries [][2]any) {
	t.Helper()
	path := filepath.Join(sessionDir, "events.ndjson")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer f.Close()
	for _, e := range entries {
		seq := e[0]
		body := e[1]
		bodyJSON, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		line := fmt.Sprintf(`{"seq":%d,"event":%s}`+"\n", seq, bodyJSON)
		if _, err := f.WriteString(line); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
}
