package tui

import (
	"strings"
	"testing"
	"time"
)

func TestHeader_view_RendersCoreFields(t *testing.T) {
	now := time.Date(2026, 4, 29, 14, 32, 30, 0, time.UTC)
	h := header{
		specPath:  "docs/specs/foo.md",
		branch:    "feat/x",
		iter:      3,
		maxIter:   5,
		startedAt: now.Add(-90 * time.Second),
		lastEvent: now.Add(-10 * time.Second),
	}
	out := h.view(now)
	want := []string{"feat/x", "iter 3/5", "1m30s", "docs/specs/foo.md"}
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("header missing %q\n%s", w, out)
		}
	}
}

func TestHeader_view_BlankBranchRendersDash(t *testing.T) {
	now := time.Date(2026, 4, 29, 14, 0, 0, 0, time.UTC)
	h := header{specPath: "x.md", iter: 1, maxIter: 1, startedAt: now}
	out := h.view(now)
	if !strings.Contains(out, "bcc -") {
		t.Errorf("blank branch should render dash; got %q", out)
	}
}

func TestHeader_view_PausedTagOnlyWhenPaused(t *testing.T) {
	now := time.Date(2026, 4, 29, 14, 0, 0, 0, time.UTC)
	h := header{branch: "x", iter: 1, maxIter: 1, startedAt: now}
	if strings.Contains(h.view(now), "[paused]") {
		t.Errorf("unpaused header should not contain [paused]")
	}
	h.paused = true
	if !strings.Contains(h.view(now), "[paused]") {
		t.Errorf("paused header should contain [paused]")
	}
}

func TestHeader_onIter_KeepsExistingMaxWhenSet(t *testing.T) {
	h := header{maxIter: 7}
	h.onIter(2, 99)
	if h.maxIter != 7 {
		t.Errorf("onIter should not overwrite an existing maxIter (got %d, want 7)", h.maxIter)
	}
	if h.iter != 2 {
		t.Errorf("iter not tracked: got %d", h.iter)
	}
}

func TestHeader_onIter_AdoptsMaxWhenZero(t *testing.T) {
	h := header{}
	h.onIter(2, 99)
	if h.maxIter != 99 {
		t.Errorf("onIter should adopt maxIter when starting at 0; got %d", h.maxIter)
	}
}

func TestAliveDot_Tiers(t *testing.T) {
	now := time.Date(2026, 4, 29, 14, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		last time.Time
	}{
		{"green-recent", now.Add(-5 * time.Second)},
		{"yellow-stale", now.Add(-90 * time.Second)},
		{"red-stuck", now.Add(-5 * time.Minute)},
	}
	for _, tc := range cases {
		got := aliveDot(tc.last, now)
		if !strings.Contains(got, "●") {
			t.Errorf("%s: missing dot glyph in %q", tc.name, got)
		}
	}
	if hollow := aliveDot(time.Time{}, now); !strings.Contains(hollow, "○") {
		t.Errorf("zero-time should render hollow ○; got %q", hollow)
	}
}

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, "0s"},
		{500 * time.Millisecond, "0s"},
		{12 * time.Second, "12s"},
		{60 * time.Second, "1m"},
		{92 * time.Second, "1m32s"},
		{60 * time.Minute, "1h"},
		{83 * time.Minute, "1h23m"},
	}
	for _, c := range cases {
		if got := formatDuration(c.d); got != c.want {
			t.Errorf("formatDuration(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}
