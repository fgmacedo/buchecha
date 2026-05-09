package director

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestSession_RoundTrip(t *testing.T) {
	in := Session{
		ID:        "abcdef012345",
		SpecPath:  "/tmp/spec.md",
		SpecHash:  "deadbeef",
		CreatedAt: time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 5, 2, 12, 30, 0, 0, time.UTC),
		Status:    SessionRunning,
		Prompt:    "hi",
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Session
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got != in {
		t.Fatalf("round-trip mismatch:\n got=%+v\nwant=%+v", got, in)
	}
}

func TestSession_RoundTrip_OmitsEmptyPrompt(t *testing.T) {
	in := Session{
		ID:        "abcdef012345",
		SpecPath:  "/tmp/spec.md",
		SpecHash:  "deadbeef",
		CreatedAt: time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 5, 2, 12, 30, 0, 0, time.UTC),
		Status:    SessionRunning,
		Prompt:    "",
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(data), "\"prompt\"") {
		t.Fatalf("omitempty failed: JSON contains 'prompt' key for empty Prompt field: %s", data)
	}
}

func TestSession_RoundTrip_IterationIndexAndMaxIter(t *testing.T) {
	t.Run("non-zero values round-trip", func(t *testing.T) {
		in := Session{
			ID:             "abcdef012345",
			SpecPath:       "/tmp/spec.md",
			SpecHash:       "deadbeef",
			CreatedAt:      time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC),
			UpdatedAt:      time.Date(2026, 5, 2, 12, 30, 0, 0, time.UTC),
			Status:         SessionRunning,
			Prompt:         "hi",
			IterationIndex: 3,
			MaxIter:        20,
		}
		data, err := json.Marshal(in)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var got Session
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got.IterationIndex != 3 || got.MaxIter != 20 {
			t.Fatalf("round-trip mismatch: got IterationIndex=%d MaxIter=%d, want 3 and 20", got.IterationIndex, got.MaxIter)
		}
	})

	t.Run("zero values are omitted from JSON", func(t *testing.T) {
		in := Session{
			ID:        "abcdef012345",
			SpecPath:  "/tmp/spec.md",
			SpecHash:  "deadbeef",
			CreatedAt: time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC),
			UpdatedAt: time.Date(2026, 5, 2, 12, 30, 0, 0, time.UTC),
			Status:    SessionRunning,
			Prompt:    "",
		}
		data, err := json.Marshal(in)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if strings.Contains(string(data), "\"iteration_index\"") {
			t.Fatalf("omitempty failed: JSON contains 'iteration_index' key for zero IterationIndex: %s", data)
		}
		if strings.Contains(string(data), "\"max_iter\"") {
			t.Fatalf("omitempty failed: JSON contains 'max_iter' key for zero MaxIter: %s", data)
		}
	})
}

func TestSessionStatus_RejectsUnknown(t *testing.T) {
	cases := []struct {
		name string
		data []byte
	}{
		{"empty string", []byte(`""`)},
		{"unknown literal", []byte(`"finished"`)},
		{"non-string", []byte(`42`)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var s SessionStatus
			if err := json.Unmarshal(tc.data, &s); err == nil {
				t.Fatalf("expected error, got nil (parsed %q)", s)
			}
		})
	}
}

func TestSessionStatus_MarshalRejectsZeroValue(t *testing.T) {
	var s SessionStatus
	if _, err := json.Marshal(s); err == nil {
		t.Fatalf("expected marshal error for zero SessionStatus")
	}
}

func TestNewSessionID_DeterministicWithFixedInputs(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	a := NewSessionID("/tmp/spec.md", now, bytes.NewReader(make([]byte, 32)))
	b := NewSessionID("/tmp/spec.md", now, bytes.NewReader(make([]byte, 32)))
	if a != b {
		t.Fatalf("same inputs produced different ids: a=%q b=%q", a, b)
	}
	if len(a) != 12 {
		t.Fatalf("len(id) = %d, want 12 (id=%q)", len(a), a)
	}
	if !validSessionID(a) {
		t.Fatalf("id %q does not match the 12-hex shape", a)
	}
}

func TestNewSessionID_VariesWithEntropy(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	mk := func(seed byte) string {
		buf := make([]byte, 16)
		for i := range buf {
			buf[i] = seed
		}
		return NewSessionID("/tmp/spec.md", now, bytes.NewReader(buf))
	}
	if mk(0x00) == mk(0xff) {
		t.Fatal("session ids should differ when entropy differs")
	}
}

func TestNewSessionID_VariesWithSpecPath(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	a := NewSessionID("/tmp/a.md", now, bytes.NewReader(make([]byte, 16)))
	b := NewSessionID("/tmp/b.md", now, bytes.NewReader(make([]byte, 16)))
	if a == b {
		t.Fatalf("different spec paths produced the same id: %q", a)
	}
}

func TestValidSessionID(t *testing.T) {
	cases := []struct {
		id   string
		want bool
	}{
		{"abcdef012345", true},
		{"ABCDEF012345", false},
		{"abcdef01234", false},
		{"abcdef0123456", false},
		{"abcdefxyz123", false},
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			if got := validSessionID(tc.id); got != tc.want {
				t.Fatalf("validSessionID(%q) = %v, want %v", tc.id, got, tc.want)
			}
		})
	}
}

func TestSessionStatus_AllValidValues(t *testing.T) {
	for _, s := range []SessionStatus{
		SessionRunning, SessionDone, SessionAborted, SessionEscalatedPending,
	} {
		data, err := json.Marshal(s)
		if err != nil {
			t.Fatalf("marshal %q: %v", s, err)
		}
		if !strings.Contains(string(data), string(s)) {
			t.Fatalf("marshalled %q missing literal: got %s", s, data)
		}
	}
}
