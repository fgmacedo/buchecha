package cli

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/fgmacedo/buchecha/internal/loop"
)

func recvWithin(t *testing.T, ch <-chan loop.EscalationReply, d time.Duration) (loop.EscalationReply, bool) {
	t.Helper()
	select {
	case r, ok := <-ch:
		return r, ok
	case <-time.After(d):
		return loop.EscalationReply{}, false
	}
}

func TestStdinEscalationGate_ResumeReadsTwoLines(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	in := strings.NewReader("r\ntighten the diff\n")
	ch := stdinEscalationGate(ctx, in)
	got, ok := recvWithin(t, ch, time.Second)
	if !ok {
		t.Fatal("no reply received")
	}
	want := loop.EscalationReply{Kind: loop.EscalationResume, Hint: "tighten the diff"}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestStdinEscalationGate_ResumeWithEmptyHintIsAllowed(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	in := strings.NewReader("r\n\n")
	ch := stdinEscalationGate(ctx, in)
	got, ok := recvWithin(t, ch, time.Second)
	if !ok {
		t.Fatal("no reply received")
	}
	want := loop.EscalationReply{Kind: loop.EscalationResume, Hint: ""}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestStdinEscalationGate_ForceApprove(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	in := strings.NewReader("f\n")
	ch := stdinEscalationGate(ctx, in)
	got, ok := recvWithin(t, ch, time.Second)
	if !ok {
		t.Fatal("no reply received")
	}
	if got.Kind != loop.EscalationForceApprove {
		t.Errorf("got %+v, want force approve", got)
	}
}

func TestStdinEscalationGate_SkipAndAbort(t *testing.T) {
	cases := []struct {
		line string
		want loop.EscalationKind
	}{
		{"s\n", loop.EscalationSkip},
		{"a\n", loop.EscalationAbort},
	}
	for _, tc := range cases {
		t.Run(strings.TrimSpace(tc.line), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			ch := stdinEscalationGate(ctx, strings.NewReader(tc.line))
			got, ok := recvWithin(t, ch, time.Second)
			if !ok {
				t.Fatal("no reply received")
			}
			if got.Kind != tc.want {
				t.Errorf("kind = %v, want %v", got.Kind, tc.want)
			}
		})
	}
}
