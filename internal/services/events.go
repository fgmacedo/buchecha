package services

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fgmacedo/buchecha/internal/director"
	"github.com/fgmacedo/buchecha/internal/loop"
	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
)

// SeqEvent is the value type subscribers receive: a monotonic
// sequence number (starting at 1) plus the loop event the EventService
// captured. Adapters render Seq as the SSE id so reconnects can
// resume from the last delivered seq.
type SeqEvent struct {
	Seq   int64      `json:"seq"`
	Event loop.Event `json:"event"`
}

// MarshalEvent returns the canonical JSON form of the SeqEvent's
// Event field plus the wire-level kind discriminator. Protocol
// adapters above services consume SeqEvent (it is part of the V1
// service contract) but the loop package owns the closed Event
// family and the matching wire schema, so adapters reach back here
// instead of importing loop directly. The kind is the same string
// the schemas/event.schema.json enum lists for the embedded "type"
// field, suitable as the SSE event-kind.
func MarshalEvent(se SeqEvent) ([]byte, string, error) {
	body, err := loop.MarshalJSONEvent(se.Event)
	if err != nil {
		return nil, "", err
	}
	var head struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(body, &head); err != nil {
		return body, "", err
	}
	return body, head.Type, nil
}

// IsFinalEvent reports whether se is the terminal LoopFinished
// event, the signal SSE handlers use to flush and close the
// response. Adapters cannot type-assert against loop.LoopFinished
// directly because they sit above services in the layer graph.
func IsFinalEvent(se SeqEvent) bool {
	_, ok := se.Event.(loop.LoopFinished)
	return ok
}

// ringSize is the in-memory ring buffer capacity per live session.
// Subscribers requesting a seq older than the oldest still in the
// ring receive ErrSeqGone and are expected to refresh state via
// Snapshot before reconnecting at the live tail.
const ringSize = 1024

// EventService multiplexes the loop event channel to N subscribers
// with monotonic Seq numbering and an in-memory ring buffer. Only
// one live session runs per bcc run; the service keys Subscribe off
// the SessionStore's bound session id.
//
// Replay reads the persisted .bcc/sessions/<id>/events.ndjson for
// archived sessions and emits the events back through the same
// SeqEvent envelope, then closes. Subscribe and Replay share the
// SeqEvent shape so a protocol adapter can pick the right method
// from the session status without changing the consumer code.
type EventService struct {
	deps Deps

	mu      sync.Mutex
	nextSeq int64
	ring    []SeqEvent
	subs    map[*subscriber]struct{}
	closed  bool

	fanoutOnce sync.Once
}

// subscriber is one in-flight Subscribe call. inbox carries live
// events from the fan-out; out is the consumer-facing channel; the
// relay goroutine merges the snapshot replay then forwards inbox to
// out. minSeq is the smallest seq the fan-out is allowed to deliver
// to inbox so events covered by the snapshot replay are not
// double-delivered.
type subscriber struct {
	inbox  chan SeqEvent
	out    chan SeqEvent
	minSeq int64
}

func newEventService(deps Deps) *EventService {
	s := &EventService{
		deps:    deps,
		nextSeq: 1,
		subs:    make(map[*subscriber]struct{}),
	}
	// Persistence to events.ndjson lives inside the fanout goroutine,
	// which Subscribe lazily starts. Without an SSE client (headless run,
	// TUI-only mode) the file would never be written, defeating bcc dev
	// replay. Start the fanout eagerly when persistence is configured.
	if deps.EventsLogPath != "" && deps.LoopEvents != nil {
		s.ensureFanout()
	}
	return s
}

// Subscribe registers a live subscriber for sessionID and returns a
// channel that emits buffered events with seq >= fromSeq followed by
// live events. fromSeq < 1 is normalised to 1. When fromSeq predates
// the oldest entry still in the ring, the call returns ErrSeqGone
// and no subscription is created. The returned channel closes on
// LoopFinished or when ctx is cancelled.
func (s *EventService) Subscribe(ctx context.Context, sessionID string, fromSeq int64) (<-chan SeqEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if sessionID == "" {
		return nil, ErrInvalidRequest.WithMessage("event service: empty session_id")
	}
	if !s.isLiveSession(sessionID) {
		return nil, ErrSessionNotFound.WithDetails(map[string]any{"id": sessionID})
	}
	if fromSeq < 1 {
		fromSeq = 1
	}
	s.ensureFanout()

	s.mu.Lock()
	if s.ringSizeLocked() > 0 {
		oldest := s.ring[0].Seq
		if fromSeq < oldest {
			s.mu.Unlock()
			return nil, ErrSeqGone.WithDetails(map[string]any{
				"requested": fromSeq,
				"oldest":    oldest,
			})
		}
	}
	snap := make([]SeqEvent, 0, len(s.ring))
	for _, se := range s.ring {
		if se.Seq >= fromSeq {
			snap = append(snap, se)
		}
	}
	sub := &subscriber{
		inbox:  make(chan SeqEvent, 256),
		out:    make(chan SeqEvent, 256),
		minSeq: s.nextSeq,
	}
	closed := s.closed
	if !closed {
		s.subs[sub] = struct{}{}
	}
	s.mu.Unlock()
	if closed {
		close(sub.inbox)
	}
	go s.relay(ctx, sub, snap)
	return sub.out, nil
}

// Replay reads .bcc/sessions/<id>/events.ndjson and emits each line's
// SeqEvent in order, skipping lines whose seq is less than fromSeq.
// The channel closes once the file is exhausted (or immediately
// when the file is absent so a session whose loop did not persist
// events is still queryable).
func (s *EventService) Replay(ctx context.Context, sessionID string, fromSeq int64) (<-chan SeqEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if sessionID == "" {
		return nil, ErrInvalidRequest.WithMessage("event service: empty session_id")
	}
	sessionDir, err := s.archivedSessionDir(sessionID)
	if err != nil {
		return nil, err
	}
	out := make(chan SeqEvent, 64)
	go s.replayLoop(ctx, sessionDir, fromSeq, out)
	return out, nil
}

// isLiveSession reports whether sessionID matches the SessionStore's
// bound session id, also accepting the reserved LiveSessionAlias so the
// SPA can subscribe before discovering the live session's real id. The
// fan-out is bound to a single live session; any other id routes
// through Replay.
func (s *EventService) isLiveSession(sessionID string) bool {
	if s.deps.SessionStore == nil {
		return false
	}
	live := s.deps.SessionStore.Session()
	if live == nil {
		return false
	}
	if sessionID == LiveSessionAlias {
		return true
	}
	return live.ID == sessionID
}

// archivedSessionDir resolves sessionID to its directory. The live
// session is acceptable here too: Replay against the live session
// reads its on-disk event log even while the loop is still running,
// which is how a freshly opened SPA tab can backfill before
// switching to Subscribe at the live tail. LiveSessionAlias resolves
// to whichever session is bound as live.
func (s *EventService) archivedSessionDir(sessionID string) (string, error) {
	if s.deps.SessionStore != nil {
		if live := s.deps.SessionStore.Session(); live != nil {
			if sessionID == LiveSessionAlias || live.ID == sessionID {
				return s.deps.SessionStore.SessionDir(), nil
			}
		}
	}
	if sessionID == LiveSessionAlias && s.deps.LiveAliasArchivedID != "" {
		sessionID = s.deps.LiveAliasArchivedID
	}
	if s.deps.SessionsBaseDir == "" {
		return "", ErrSessionNotFound.WithDetails(map[string]any{"id": sessionID})
	}
	store, err := director.OpenSession(s.deps.SessionsBaseDir, sessionID)
	if err != nil {
		if errors.Is(err, director.ErrSessionNotFound) || errors.Is(err, fs.ErrNotExist) {
			return "", ErrSessionNotFound.WithDetails(map[string]any{"id": sessionID})
		}
		return "", fmt.Errorf("services: open session %q: %w", sessionID, err)
	}
	return store.SessionDir(), nil
}

// ensureFanout starts the single fan-out goroutine on the first
// Subscribe. The goroutine drains LoopEvents, assigns Seq, fills the
// ring, and broadcasts to live subscribers until the channel closes.
// Re-invocations are no-ops thanks to fanoutOnce.
func (s *EventService) ensureFanout() {
	s.fanoutOnce.Do(func() {
		if s.deps.LoopEvents == nil {
			return
		}
		go s.fanout()
	})
}

// fanout consumes LoopEvents, stamps each event with the next seq,
// appends to the ring, and pushes to each live subscriber's inbox.
// On LoopFinished (or LoopEvents close) every subscriber inbox is
// closed and the relay goroutines drain.
//
// Before stamping seq the fan-out enriches AgentEventReceived events
// with the active task id of the agent that produced them. The active
// task is tracked locally via TaskStarted / TaskCompleted /
// TaskApproved / TaskNeedsFix events that pass through the same
// channel, all of which now carry an AgentID. The map lives in the
// goroutine's stack so no lock is needed.
//
// Window-of-race: TaskStarted (handler-side) and the subsequent
// AgentEventReceived (adapter-side) are produced by different
// publishers; under unlikely interleavings an event may arrive before
// its TaskStarted and miss attribution. Best-effort by design; if it
// becomes a problem we can buffer-and-reorder by timestamp here.
func (s *EventService) fanout() {
	activeTaskByAgent := make(map[string]string)
	var logFile *os.File
	if s.deps.EventsLogPath != "" {
		f, err := os.OpenFile(s.deps.EventsLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			slog.Error("services events: open events log failed",
				"path", s.deps.EventsLogPath,
				"err", err,
			)
		} else {
			logFile = f
			defer func() {
				if cerr := logFile.Close(); cerr != nil {
					slog.Warn("services events: close events log",
						"path", s.deps.EventsLogPath,
						"err", cerr,
					)
				}
			}()
		}
	}
	for ev := range s.deps.LoopEvents {
		switch e := ev.(type) {
		case loop.TaskStarted:
			if e.AgentID != "" {
				activeTaskByAgent[e.AgentID] = e.TaskID
			}
		case loop.TaskCompleted:
			if e.AgentID != "" {
				delete(activeTaskByAgent, e.AgentID)
			}
		case loop.TaskApproved:
			if e.AgentID != "" {
				delete(activeTaskByAgent, e.AgentID)
			}
		case loop.TaskNeedsFix:
			if e.AgentID != "" {
				delete(activeTaskByAgent, e.AgentID)
			}
		case loop.AgentEventReceived:
			if id := e.Event.AgentID; id != "" && e.Event.TaskID == "" {
				if t, ok := activeTaskByAgent[id]; ok {
					e.Event.TaskID = t
					ev = e
				}
			}
		}
		s.mu.Lock()
		seq := s.nextSeq
		s.nextSeq++
		se := SeqEvent{Seq: seq, Event: ev}
		s.appendRingLocked(se)
		if logFile != nil {
			if werr := writeEventLine(logFile, se); werr != nil {
				slog.Error("services events: write events log",
					"path", s.deps.EventsLogPath,
					"seq", seq,
					"err", werr,
				)
			}
		}
		recipients := make([]*subscriber, 0, len(s.subs))
		for sub := range s.subs {
			if seq >= sub.minSeq {
				recipients = append(recipients, sub)
			}
		}
		_, isFinal := ev.(loop.LoopFinished)
		s.mu.Unlock()
		for _, sub := range recipients {
			select {
			case sub.inbox <- se:
			default:
				slog.Warn("services events: subscriber inbox full; dropping event",
					"seq", se.Seq,
				)
			}
		}
		if isFinal {
			s.shutdownSubscribers()
			return
		}
	}
	s.shutdownSubscribers()
}

// shutdownSubscribers closes every live subscriber's inbox and marks
// the service closed so Subscribe calls after the loop has finished
// observe the terminal state instead of blocking on the fan-out.
func (s *EventService) shutdownSubscribers() {
	s.mu.Lock()
	for sub := range s.subs {
		close(sub.inbox)
		delete(s.subs, sub)
	}
	s.closed = true
	s.mu.Unlock()
}

// relay merges the snapshot replay with live events from sub.inbox
// and forwards them to sub.out. It owns sub.out: closing the channel
// is the relay's responsibility, never the fan-out's. ctx cancel
// returns immediately and unregisters the subscriber.
func (s *EventService) relay(ctx context.Context, sub *subscriber, snap []SeqEvent) {
	defer close(sub.out)
	defer s.removeSubscriber(sub)
	for _, se := range snap {
		select {
		case <-ctx.Done():
			return
		case sub.out <- se:
		}
	}
	for {
		select {
		case <-ctx.Done():
			return
		case se, ok := <-sub.inbox:
			if !ok {
				return
			}
			select {
			case <-ctx.Done():
				return
			case sub.out <- se:
			}
		}
	}
}

func (s *EventService) removeSubscriber(sub *subscriber) {
	s.mu.Lock()
	if _, ok := s.subs[sub]; ok {
		delete(s.subs, sub)
	}
	s.mu.Unlock()
}

// appendRingLocked appends se to the ring, evicting the oldest entry
// once ringSize is exceeded. mu must be held.
func (s *EventService) appendRingLocked(se SeqEvent) {
	if len(s.ring) < ringSize {
		s.ring = append(s.ring, se)
		return
	}
	copy(s.ring, s.ring[1:])
	s.ring[len(s.ring)-1] = se
}

func (s *EventService) ringSizeLocked() int { return len(s.ring) }

// replayLoop is the goroutine body for Replay. It opens the
// per-session events.ndjson, decodes one envelope per line, and
// emits SeqEvent values in order. ctx cancel and end-of-file both
// close the channel cleanly.
func (s *EventService) replayLoop(ctx context.Context, sessionDir string, fromSeq int64, out chan<- SeqEvent) {
	defer close(out)
	path := filepath.Join(sessionDir, "events.ndjson")
	f, err := os.Open(path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			slog.Warn("services events: replay open failed",
				"path", path,
				"err", err,
			)
		}
		return
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return
		}
		line := scanner.Bytes()
		se, ok := decodeReplayLine(line)
		if !ok {
			continue
		}
		if se.Seq < fromSeq {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case out <- se:
		}
		if d := s.deps.ReplayInterEventDelay; d > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(d):
			}
		}
	}
	if err := scanner.Err(); err != nil {
		slog.Warn("services events: replay scan failed",
			"path", path,
			"err", err,
		)
	}
}

// replayEnvelope is the on-disk wire shape of one persisted event.
// Seq is the monotonic counter assigned by the live fan-out; Event
// is the same map shape loop.MarshalJSONEvent emits so the JSON
// payload round-trips through the existing serializer.
type replayEnvelope struct {
	Seq   int64           `json:"seq"`
	Event json.RawMessage `json:"event"`
}

// writeEventLine serializes one SeqEvent as the canonical NDJSON
// envelope: {"seq": N, "event": <loop.MarshalJSONEvent body>}\n. The
// envelope shape mirrors replayEnvelope so the decoder round-trips
// what the writer emits. Errors propagate to the fanout caller for
// logging; a single write failure does not abort the run.
func writeEventLine(w io.Writer, se SeqEvent) error {
	body, err := loop.MarshalJSONEvent(se.Event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	env := struct {
		Seq   int64           `json:"seq"`
		Event json.RawMessage `json:"event"`
	}{
		Seq:   se.Seq,
		Event: body,
	}
	line, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("marshal envelope: %w", err)
	}
	line = append(line, '\n')
	if _, err := w.Write(line); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	return nil
}

// decodeReplayLine parses one JSON line into a SeqEvent. Lines whose
// event type is not yet handled by the V1 decoder are skipped so a
// log written by a newer bcc binary does not abort the replay.
func decodeReplayLine(line []byte) (SeqEvent, bool) {
	var env replayEnvelope
	if err := json.Unmarshal(line, &env); err != nil {
		slog.Warn("services events: replay parse failed",
			"err", err,
			"line", string(line),
		)
		return SeqEvent{}, false
	}
	ev, ok := decodeEvent(env.Event)
	if !ok {
		return SeqEvent{}, false
	}
	return SeqEvent{Seq: env.Seq, Event: ev}, true
}

// decodeEvent reverses loop.MarshalJSONEvent for the subset of types
// the V1 service replays. Unknown types return false so the replay
// loop can skip them. This keeps the decoder additive: new event
// types added to internal/loop need an entry here before they ride
// through Replay; until then they are skipped without crashing.
func decodeEvent(raw json.RawMessage) (loop.Event, bool) {
	var head struct {
		Type string `json:"type"`
		At   string `json:"at"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		slog.Warn("services events: replay event head parse failed",
			"err", err,
		)
		return nil, false
	}
	at := parseAt(head.At)
	switch head.Type {
	case "iter_started":
		var body struct {
			Index       int    `json:"index"`
			MaxIter     int    `json:"max_iter"`
			BaselineSHA string `json:"baseline_sha"`
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			return nil, false
		}
		return loop.IterationStarted{
			Index:       body.Index,
			MaxIter:     body.MaxIter,
			BaselineSHA: body.BaselineSHA,
			At:          at,
		}, true
	case "iter_finished":
		var body struct {
			Index        int    `json:"index"`
			Signal       string `json:"signal"`
			HEADAdvanced bool   `json:"head_advanced"`
			DurationMS   int64  `json:"duration_ms"`
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			return nil, false
		}
		return loop.IterationFinished{
			Index:        body.Index,
			HEADAdvanced: body.HEADAdvanced,
			DurationMS:   body.DurationMS,
			At:           at,
		}, true
	case "loop_finished":
		var body struct {
			Reason   string `json:"reason"`
			ExitCode int    `json:"exit_code"`
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			return nil, false
		}
		return loop.LoopFinished{
			Reason:   body.Reason,
			ExitCode: body.ExitCode,
			At:       at,
		}, true
	case "phase_briefed":
		var body struct {
			PhaseID   string `json:"phase_id"`
			Iteration int    `json:"iteration"`
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			return nil, false
		}
		return loop.PhaseBriefed{
			PhaseID:   body.PhaseID,
			Iteration: body.Iteration,
			At:        at,
		}, true
	case "phase_reviewed":
		var body struct {
			PhaseID   string `json:"phase_id"`
			Attempt   int    `json:"attempt"`
			Outcome   string `json:"outcome"`
			Reasoning string `json:"reasoning"`
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			return nil, false
		}
		return loop.PhaseReviewed{
			PhaseID:   body.PhaseID,
			Attempt:   body.Attempt,
			Outcome:   body.Outcome,
			Reasoning: body.Reasoning,
			At:        at,
		}, true
	case "task_started":
		var body struct {
			AgentID string `json:"agent_id"`
			PhaseID string `json:"phase_id"`
			TaskID  string `json:"task_id"`
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			return nil, false
		}
		return loop.TaskStarted{
			AgentID: body.AgentID,
			PhaseID: body.PhaseID,
			TaskID:  body.TaskID,
			At:      at,
		}, true
	case "task_completed":
		var body struct {
			AgentID string `json:"agent_id"`
			PhaseID string `json:"phase_id"`
			TaskID  string `json:"task_id"`
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			return nil, false
		}
		return loop.TaskCompleted{
			AgentID: body.AgentID,
			PhaseID: body.PhaseID,
			TaskID:  body.TaskID,
			At:      at,
		}, true
	case "task_approved":
		var body struct {
			AgentID string `json:"agent_id"`
			PhaseID string `json:"phase_id"`
			TaskID  string `json:"task_id"`
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			return nil, false
		}
		return loop.TaskApproved{
			AgentID: body.AgentID,
			PhaseID: body.PhaseID,
			TaskID:  body.TaskID,
			At:      at,
		}, true
	case "task_needs_fix":
		var body struct {
			AgentID string `json:"agent_id"`
			PhaseID string `json:"phase_id"`
			TaskID  string `json:"task_id"`
			Note    string `json:"note"`
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			return nil, false
		}
		return loop.TaskNeedsFix{
			AgentID: body.AgentID,
			PhaseID: body.PhaseID,
			TaskID:  body.TaskID,
			Note:    body.Note,
			At:      at,
		}, true
	case "agent_event":
		ev, ok := decodeAgentEvent(raw, at)
		if !ok {
			return nil, false
		}
		return loop.AgentEventReceived{Event: ev}, true
	default:
		return nil, false
	}
}

// decodeAgentEvent inverts the loop.MarshalJSONEvent shape for an
// AgentEventReceived envelope so Replay can rehydrate a recorded
// agent_event line back into the typed value the SSE handler expects.
// Mirrors agentEventJSON in internal/loop/eventjson.go; keep the two in
// sync when adding new AgentEvent kinds.
func decodeAgentEvent(raw json.RawMessage, at time.Time) (agentcontract.AgentEvent, bool) {
	var body struct {
		Kind        string `json:"kind"`
		AgentID     string `json:"agent_id"`
		Role        string `json:"role"`
		PhaseID     string `json:"phase_id"`
		IterationID string `json:"iteration_id"`
		Attempt     int    `json:"attempt"`
		TaskID      string `json:"task_id"`
		Init        *struct {
			SessionID string `json:"session_id"`
			Model     string `json:"model"`
			CWD       string `json:"cwd"`
		} `json:"init"`
		Tool *struct {
			ID      string         `json:"id"`
			Name    string         `json:"name"`
			Args    map[string]any `json:"args"`
			IsError bool           `json:"is_error"`
			Summary string         `json:"summary"`
		} `json:"tool"`
		Text  string `json:"text"`
		Usage *struct {
			InputTokens              int64 `json:"input_tokens"`
			OutputTokens             int64 `json:"output_tokens"`
			CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
			CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
		} `json:"usage"`
		Rate *struct {
			Status  string `json:"status"`
			ResetAt string `json:"reset_at"`
		} `json:"rate"`
		Done *struct {
			NumTurns                 int     `json:"num_turns"`
			TotalCostUSD             float64 `json:"total_cost_usd"`
			InputTokens              int64   `json:"input_tokens"`
			OutputTokens             int64   `json:"output_tokens"`
			CacheReadInputTokens     int64   `json:"cache_read_input_tokens"`
			CacheCreationInputTokens int64   `json:"cache_creation_input_tokens"`
			DurationMS               int64   `json:"duration_ms"`
		} `json:"done"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return agentcontract.AgentEvent{}, false
	}
	ev := agentcontract.AgentEvent{
		Kind:        agentcontract.AgentEventKind(body.Kind),
		At:          at,
		AgentID:     body.AgentID,
		Role:        agentcontract.Role(body.Role),
		PhaseID:     body.PhaseID,
		IterationID: body.IterationID,
		Attempt:     body.Attempt,
		TaskID:      body.TaskID,
		Text:        body.Text,
	}
	if body.Init != nil {
		ev.Init = &agentcontract.InitInfo{
			SessionID: body.Init.SessionID,
			Model:     body.Init.Model,
			CWD:       body.Init.CWD,
		}
	}
	if body.Tool != nil {
		ev.Tool = &agentcontract.ToolCallInfo{
			ID:      body.Tool.ID,
			Name:    body.Tool.Name,
			Args:    body.Tool.Args,
			IsError: body.Tool.IsError,
			Summary: body.Tool.Summary,
		}
	}
	if body.Usage != nil {
		ev.Usage = &agentcontract.UsageInfo{
			InputTokens:              body.Usage.InputTokens,
			OutputTokens:             body.Usage.OutputTokens,
			CacheReadInputTokens:     body.Usage.CacheReadInputTokens,
			CacheCreationInputTokens: body.Usage.CacheCreationInputTokens,
		}
	}
	if body.Rate != nil {
		ev.Rate = &agentcontract.RateLimitInfo{
			Status:  body.Rate.Status,
			ResetAt: parseAt(body.Rate.ResetAt),
		}
	}
	if body.Done != nil {
		ev.Done = &agentcontract.ResultSummaryInfo{
			NumTurns:                 body.Done.NumTurns,
			TotalCostUSD:             body.Done.TotalCostUSD,
			InputTokens:              body.Done.InputTokens,
			OutputTokens:             body.Done.OutputTokens,
			CacheReadInputTokens:     body.Done.CacheReadInputTokens,
			CacheCreationInputTokens: body.Done.CacheCreationInputTokens,
			DurationMS:               body.Done.DurationMS,
		}
	}
	return ev, true
}

func parseAt(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
