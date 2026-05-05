package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestSSEWriter_FramesEvent asserts WriteEvent renders the
// canonical id/event/data triplet terminated by a blank line. A
// regression here would corrupt every consumer's parser.
func TestSSEWriter_FramesEvent(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	w, err := NewSSEWriter(rec)
	if err != nil {
		t.Fatalf("NewSSEWriter: %v", err)
	}
	if err := w.WriteEvent(7, "iter_started", []byte(`{"type":"iter_started"}`)); err != nil {
		t.Fatalf("WriteEvent: %v", err)
	}
	want := "id: 7\nevent: iter_started\ndata: {\"type\":\"iter_started\"}\n\n"
	if got := rec.Body.String(); got != want {
		t.Fatalf("body: got %q, want %q", got, want)
	}
}

// TestSSEWriter_RetryAndHeartbeat covers the auxiliary records:
// retry: 5000 directives and :heartbeat comments. Each must end with
// the SSE record terminator (one blank line) so consumers do not
// stall waiting for more bytes.
func TestSSEWriter_RetryAndHeartbeat(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	w, err := NewSSEWriter(rec)
	if err != nil {
		t.Fatalf("NewSSEWriter: %v", err)
	}
	if err := w.WriteRetry(5000); err != nil {
		t.Fatalf("WriteRetry: %v", err)
	}
	if err := w.WriteHeartbeat(); err != nil {
		t.Fatalf("WriteHeartbeat: %v", err)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "retry: 5000\n\n") {
		t.Errorf("missing retry directive in %q", body)
	}
	if !strings.Contains(body, ":heartbeat\n\n") {
		t.Errorf("missing heartbeat comment in %q", body)
	}
}

// TestSSEWriter_RejectsNonFlusher confirms the constructor refuses a
// writer that cannot stream. Production never hits this branch
// (every net/http response writer satisfies http.Flusher) but
// callers handing us a buffered writer would silently break the
// flush-after-every-record contract without this guard.
func TestSSEWriter_RejectsNonFlusher(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	if _, err := NewSSEWriter(&nonFlusher{Writer: &buf}); err == nil {
		t.Fatal("expected error for non-flusher writer")
	}
}

// nonFlusher implements http.ResponseWriter without http.Flusher.
type nonFlusher struct {
	Writer *bytes.Buffer
}

func (n nonFlusher) Header() http.Header         { return http.Header{} }
func (n nonFlusher) Write(b []byte) (int, error) { return n.Writer.Write(b) }
func (n nonFlusher) WriteHeader(int)             {}
