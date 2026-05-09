package stats

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestStatsLog_NilReceiverAppendIsNoop(t *testing.T) {
	var s *StatsLog
	if err := s.Append(StatsEntry{Role: "bcc-briefer", DurationMS: 1}); err != nil {
		t.Fatalf("nil StatsLog.Append should be a no-op; got %v", err)
	}
}

func TestStatsLog_RoundtripJSONL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stats.jsonl")
	s := NewStatsLog(path)
	entries := []StatsEntry{
		{
			At:           time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC),
			Role:         "bcc-planner",
			DurationMS:   1200,
			CostUSD:      0.04,
			InputTokens:  1500,
			OutputTokens: 600,
		},
		{
			At:           time.Date(2026, 5, 4, 12, 0, 5, 0, time.UTC),
			Role:         "bcc-briefer",
			PhaseID:      "p1",
			IterationID:  "p1-01",
			DurationMS:   800,
			CostUSD:      0.012,
			InputTokens:  900,
			OutputTokens: 400,
		},
		{
			At:           time.Date(2026, 5, 4, 12, 0, 10, 0, time.UTC),
			Role:         "bcc-executor",
			PhaseID:      "p1",
			IterationID:  "p1-01",
			Attempt:      2,
			DurationMS:   45_000,
			CostUSD:      0.32,
			InputTokens:  12_000,
			OutputTokens: 4_500,
		},
	}
	for _, e := range entries {
		if err := s.Append(e); err != nil {
			t.Fatalf("Append(%+v): %v", e, err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	var got []StatsEntry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var e StatsEntry
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			t.Fatalf("unmarshal %q: %v", scanner.Text(), err)
		}
		got = append(got, e)
	}
	if len(got) != len(entries) {
		t.Fatalf("got %d entries, want %d", len(got), len(entries))
	}
	for i := range entries {
		if !got[i].At.Equal(entries[i].At) {
			t.Errorf("entry %d At: got %v want %v", i, got[i].At, entries[i].At)
		}
		if got[i].Role != entries[i].Role || got[i].PhaseID != entries[i].PhaseID ||
			got[i].IterationID != entries[i].IterationID || got[i].Attempt != entries[i].Attempt ||
			got[i].DurationMS != entries[i].DurationMS || got[i].CostUSD != entries[i].CostUSD ||
			got[i].InputTokens != entries[i].InputTokens || got[i].OutputTokens != entries[i].OutputTokens {
			t.Errorf("entry %d mismatch:\n got=%+v\nwant=%+v", i, got[i], entries[i])
		}
	}
}

func TestStatsLog_RejectsEntryWithoutRole(t *testing.T) {
	dir := t.TempDir()
	s := NewStatsLog(filepath.Join(dir, "stats.jsonl"))
	if err := s.Append(StatsEntry{DurationMS: 100}); err == nil {
		t.Fatal("expected error when Role is empty")
	} else if !strings.Contains(err.Error(), "missing role") {
		t.Errorf("err = %v, want missing-role", err)
	}
}

func TestStatsLog_ConcurrentAppendDoesNotInterleave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stats.jsonl")
	s := NewStatsLog(path)
	const n = 50
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = s.Append(StatsEntry{
				At: time.Now(), Role: "bcc-executor",
				PhaseID: "p1", IterationID: "p1-01", Attempt: i,
				DurationMS: int64(i),
			})
		}(i)
	}
	wg.Wait()
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	var lines int
	for scanner.Scan() {
		lines++
		var e StatsEntry
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			t.Fatalf("line %d unmarshal failed (interleaving?): %v\nline: %q", lines, err, scanner.Text())
		}
	}
	if lines != n {
		t.Errorf("got %d lines, want %d", lines, n)
	}
}
