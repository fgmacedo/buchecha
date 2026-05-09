package handlers

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/fgmacedo/buchecha/internal/services"
	"github.com/fgmacedo/buchecha/internal/services/events"
)

// defaultHeartbeatInterval is the cadence at which the SSE handler
// emits :heartbeat comments to defeat idle-timeout cuts on
// reverse-proxy hops. Tests override it via Deps.HeartbeatInterval.
const defaultHeartbeatInterval = 15 * time.Second

// reconnectMS is the EventSource reconnect backoff the server
// advertises once on connect (5 seconds, the spec default).
const reconnectMS = 5000

// SSEEmitter renders an event-stream framing onto an
// http.ResponseWriter. The api package supplies the production
// implementation through Deps; tests can swap a fake to inspect the
// rendered bytes.
type SSEEmitter interface {
	WriteRetry(ms int) error
	WriteEvent(seq int64, kind string, data []byte) error
	WriteSeqEvent(se events.SeqEvent) error
	WriteHeartbeat() error
}

// registerEvents wires GET /sessions/{id}/events as a plain
// http.HandlerFunc on the chi sub-router. The route does not flow
// through huma: SSE is fundamentally an open-ended chunked response,
// not a request/response pair, so the huma operation registry would
// have to model it as an exotic special case. Mounting on chi keeps
// the wire shape exact and the integration tests honest.
func registerEvents(router chi.Router, svc *services.Services, deps Deps) {
	router.Get("/sessions/{id}/events", eventsHandler(svc, deps))
}

func eventsHandler(svc *services.Services, deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if id == "" {
			deps.WriteError(w, r, services.ErrInvalidRequest.WithMessage("events: empty session id"))
			return
		}
		if svc == nil {
			deps.WriteError(w, r, services.ErrInternal.WithMessage("events: services not configured"))
			return
		}

		fromSeq := parseLastEventID(r.Header.Get("Last-Event-ID"))
		stream, replay, err := pickEventSource(r.Context(), svc, id, fromSeq+1)
		if err != nil {
			deps.WriteError(w, r, err)
			return
		}

		emitter := deps.NewSSEEmitter(w)
		if emitter == nil {
			deps.WriteError(w, r, services.ErrInternal.WithMessage("events: response writer cannot stream"))
			return
		}

		deps.SetSSEHeaders(w.Header())
		w.WriteHeader(http.StatusOK)
		if err := emitter.WriteRetry(reconnectMS); err != nil {
			return
		}

		interval := deps.HeartbeatInterval
		if interval <= 0 {
			interval = defaultHeartbeatInterval
		}
		streamLoop(r.Context(), emitter, stream, replay, interval)
	}
}

// streamLoop forwards events from the active source to the emitter,
// firing heartbeat comments on the configured interval. It returns
// when ctx is cancelled, when the source channel closes, or when a
// LoopFinished envelope flushes the response.
func streamLoop(ctx context.Context, emitter SSEEmitter, stream <-chan events.SeqEvent, replay bool, heartbeat time.Duration) {
	ticker := time.NewTicker(heartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := emitter.WriteHeartbeat(); err != nil {
				return
			}
		case se, ok := <-stream:
			if !ok {
				return
			}
			if err := emitter.WriteSeqEvent(se); err != nil {
				return
			}
			if !replay && events.IsFinalEvent(se) {
				return
			}
		}
	}
}

// pickEventSource chooses between the live Subscribe and the
// archived Replay branches. The handler tries Subscribe first
// because it is the cheap path for the common case (a live session
// the EventService is bound to); if Subscribe rejects with
// session_not_found, we fall back to Replay against the on-disk log.
// Returns the event channel, a flag that is true for replay (so the
// streamer skips the LoopFinished short-circuit), and any error
// from the underlying service that should surface to the client.
func pickEventSource(ctx context.Context, svc *services.Services, id string, fromSeq int64) (<-chan events.SeqEvent, bool, error) {
	stream, err := svc.Events.Subscribe(ctx, id, fromSeq)
	if err == nil {
		return stream, false, nil
	}
	if errors.Is(err, services.ErrSessionNotFound) {
		stream, replayErr := svc.Events.Replay(ctx, id, fromSeq)
		if replayErr != nil {
			return nil, false, replayErr
		}
		return stream, true, nil
	}
	return nil, false, err
}

// parseLastEventID parses the SSE Last-Event-ID header into an
// int64. Empty, malformed, or negative inputs collapse to 0 so the
// caller's `fromSeq+1` arithmetic surfaces 1, the smallest valid
// seq the EventService assigns.
func parseLastEventID(raw string) int64 {
	if raw == "" {
		return 0
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
}
