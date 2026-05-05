package api

import (
	"fmt"
	"io"
	"net/http"

	"github.com/fgmacedo/buchecha/internal/services"
)

// SSEWriter renders the canonical event-stream framing onto an
// http.ResponseWriter that supports flushing. The shape per event
// is exactly:
//
//	id: <seq>\n
//	event: <kind>\n
//	data: <json>\n
//	\n
//
// Heartbeat lines and the initial retry directive use the
// SSE-comment shape (`:heartbeat\n\n`) and the standalone retry
// field (`retry: <ms>\n\n`). Each public method flushes after the
// write so reverse proxies that buffer per chunk see the data
// immediately.
//
// SSEWriter is not safe for concurrent use; one writer serves one
// connection.
type SSEWriter struct {
	w       io.Writer
	flusher http.Flusher
}

// NewSSEWriter wraps w with the framing helpers. The underlying
// writer must satisfy http.Flusher (every modern net/http response
// writer does); callers that cannot supply one fall back to a
// buffered alternative outside this package.
func NewSSEWriter(w http.ResponseWriter) (*SSEWriter, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("api/sse: response writer is not http.Flusher")
	}
	return &SSEWriter{w: w, flusher: flusher}, nil
}

// WriteRetry emits a single `retry: <ms>` directive. The browser's
// EventSource implementation honors it as the reconnect backoff for
// any subsequent disconnect on this stream.
func (s *SSEWriter) WriteRetry(ms int) error {
	if _, err := fmt.Fprintf(s.w, "retry: %d\n\n", ms); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}

// WriteEvent emits one `id|event|data` triplet. The data argument is
// the marshaled JSON body to embed verbatim; callers ensure it
// contains no embedded newlines that would split the SSE record.
func (s *SSEWriter) WriteEvent(seq int64, kind string, data []byte) error {
	if _, err := fmt.Fprintf(s.w, "id: %d\nevent: %s\ndata: ", seq, kind); err != nil {
		return err
	}
	if _, err := s.w.Write(data); err != nil {
		return err
	}
	if _, err := s.w.Write([]byte("\n\n")); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}

// WriteSeqEvent renders one services.SeqEvent through the canonical
// framing, sourcing both the JSON body and the kind from the
// service-side helper. Errors at the marshal stage propagate to the
// caller, which usually closes the stream.
func (s *SSEWriter) WriteSeqEvent(se services.SeqEvent) error {
	data, kind, err := services.MarshalEvent(se)
	if err != nil {
		return fmt.Errorf("api/sse: marshal event: %w", err)
	}
	return s.WriteEvent(se.Seq, kind, data)
}

// WriteHeartbeat emits a `:heartbeat` SSE comment so reverse
// proxies that idle-time-out long-poll connections keep the stream
// open. Comments are silently ignored by EventSource consumers.
func (s *SSEWriter) WriteHeartbeat() error {
	if _, err := s.w.Write([]byte(":heartbeat\n\n")); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}

// SetSSEHeaders applies the canonical response headers an SSE
// endpoint must carry: text/event-stream content type, no caching,
// keep-alive, and X-Accel-Buffering off so nginx and similar proxies
// stop buffering the chunked body.
func SetSSEHeaders(h http.Header) {
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
}
