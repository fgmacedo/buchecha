// Package spawnkit provides helpers shared across all provider adapters:
// ringBuffer for stderr tail capture, WriteMCPConfig for per-spawn MCP
// config file creation, and PersistPrompt / EmitSpawnStarted /
// EmitSpawnFinished for prompt persistence and lifecycle event emission.
package spawnkit

// RingBuffer keeps the last N bytes written to it. Bytes older than the
// capacity are dropped silently as new data arrives. It satisfies
// io.Writer so it can be composed with io.MultiWriter as a stderr sink.
// The zero value is invalid; use NewRingBuffer.
type RingBuffer struct {
	cap int
	buf []byte
}

// NewRingBuffer returns a RingBuffer that retains up to cap bytes. cap
// must be > 0; passing 0 produces a buffer that discards all writes.
func NewRingBuffer(cap int) *RingBuffer { return &RingBuffer{cap: cap} }

// Write appends p to the buffer, evicting the oldest bytes when the
// combined length would exceed the capacity. Always returns len(p), nil.
func (r *RingBuffer) Write(p []byte) (int, error) {
	if r.cap <= 0 {
		return len(p), nil
	}
	if len(p) >= r.cap {
		// p alone fills or exceeds the buffer: keep only its tail.
		r.buf = append(r.buf[:0], p[len(p)-r.cap:]...)
		return len(p), nil
	}
	if len(r.buf)+len(p) <= r.cap {
		r.buf = append(r.buf, p...)
		return len(p), nil
	}
	// Evict the oldest bytes to make room for p.
	keep := r.cap - len(p)
	r.buf = append(r.buf[:0], r.buf[len(r.buf)-keep:]...)
	r.buf = append(r.buf, p...)
	return len(p), nil
}

// Tail returns the current contents of the buffer as a string. The
// result is at most cap bytes long.
func (r *RingBuffer) Tail() string { return string(r.buf) }

// String is an alias for Tail, provided so *RingBuffer satisfies the
// fmt.Stringer interface transparently.
func (r *RingBuffer) String() string { return r.Tail() }
