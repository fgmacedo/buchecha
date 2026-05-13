package spawnkit_test

import (
	"strings"
	"testing"

	"github.com/fgmacedo/buchecha/internal/provider/spawnkit"
)

func TestRingBuffer(t *testing.T) {
	t.Run("write-around past capacity", func(t *testing.T) {
		cases := []struct {
			name   string
			cap    int
			writes []string
			want   string
		}{
			{
				name:   "single write exceeds cap keeps tail",
				cap:    4,
				writes: []string{"abcdefgh"},
				want:   "efgh",
			},
			{
				name:   "multiple writes wrap around",
				cap:    5,
				writes: []string{"abc", "defgh"},
				// "abc" fills 3; "defgh" is 5 = cap → keep tail "defgh"
				want: "defgh",
			},
			{
				name:   "incremental overflow evicts oldest bytes",
				cap:    5,
				writes: []string{"abcde", "fg"},
				// after "abcde": buf="abcde" (full)
				// after "fg": need 5-2=3 keep from end of buf → "cde" + "fg" = "cdefg"
				want: "cdefg",
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				r := spawnkit.NewRingBuffer(tc.cap)
				for _, w := range tc.writes {
					n, err := r.Write([]byte(w))
					if err != nil {
						t.Fatalf("Write(%q) error: %v", w, err)
					}
					if n != len(w) {
						t.Fatalf("Write(%q) = %d; want %d", w, n, len(w))
					}
				}
				if got := r.Tail(); got != tc.want {
					t.Errorf("Tail() = %q; want %q", got, tc.want)
				}
			})
		}
	})

	t.Run("tail length capping", func(t *testing.T) {
		cap := 8
		r := spawnkit.NewRingBuffer(cap)
		// Write 3*cap bytes in one shot to ensure Tail never exceeds cap.
		_, _ = r.Write([]byte(strings.Repeat("x", cap*3)))
		tail := r.Tail()
		if len(tail) > cap {
			t.Errorf("Tail() len=%d; want <= %d", len(tail), cap)
		}
		if tail != strings.Repeat("x", cap) {
			t.Errorf("Tail() = %q; want %q", tail, strings.Repeat("x", cap))
		}
	})

	t.Run("tail before buffer has filled", func(t *testing.T) {
		r := spawnkit.NewRingBuffer(16)
		// Write less than capacity.
		_, _ = r.Write([]byte("hello"))
		if got := r.Tail(); got != "hello" {
			t.Errorf("Tail() = %q; want %q", got, "hello")
		}
		// Append more but still within cap.
		_, _ = r.Write([]byte(" world"))
		if got := r.Tail(); got != "hello world" {
			t.Errorf("Tail() = %q; want %q", got, "hello world")
		}
	})
}
