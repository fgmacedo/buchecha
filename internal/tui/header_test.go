package tui

import (
	"strings"
	"testing"
	"time"
)

func TestHeader_titleText_ContainsBranchIterAndElapsed(t *testing.T) {
	now := time.Date(2026, 4, 29, 14, 32, 30, 0, time.UTC)
	h := header{
		branch:    "feat/x",
		iter:      3,
		maxIter:   5,
		startedAt: now.Add(-90 * time.Second),
	}
	got := h.titleText(now)
	for _, w := range []string{"feat/x", "iter 3/5", "1m30s"} {
		if !strings.Contains(got, w) {
			t.Errorf("titleText missing %q\n%s", w, got)
		}
	}
}

func TestHeader_titleText_BlankBranchRendersDash(t *testing.T) {
	now := time.Date(2026, 4, 29, 14, 0, 0, 0, time.UTC)
	h := header{iter: 1, maxIter: 1, startedAt: now}
	got := h.titleText(now)
	if !strings.Contains(got, "bcc -") {
		t.Errorf("blank branch should render dash; got %q", got)
	}
}

func TestHeader_view_ContainsSpecPathAndAliveDot(t *testing.T) {
	now := time.Date(2026, 4, 29, 14, 32, 30, 0, time.UTC)
	h := header{
		specPath:  "docs/specs/foo.md",
		lastEvent: now.Add(-10 * time.Second),
	}
	got := h.view(now, 80)
	if !strings.Contains(got, "docs/specs/foo.md") {
		t.Errorf("view missing spec path: %q", got)
	}
	if !strings.Contains(got, "●") {
		t.Errorf("view missing alive dot glyph: %q", got)
	}
}

func TestHeader_view_PausedTagOnlyWhenPaused(t *testing.T) {
	now := time.Date(2026, 4, 29, 14, 0, 0, 0, time.UTC)
	h := header{specPath: "x.md"}
	if strings.Contains(h.view(now, 80), "[paused]") {
		t.Errorf("unpaused header should not contain [paused]")
	}
	h.paused = true
	if !strings.Contains(h.view(now, 80), "[paused]") {
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
