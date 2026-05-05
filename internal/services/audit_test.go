package services

import (
	"bufio"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAudit_RecordRoundTrip(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "audit.ndjson")
	a := newAudit(Deps{AuditPath: path})
	a.SetLogger(slog.New(slog.NewJSONHandler(os.Stderr, nil)))
	defer a.Close()

	entries := []AuditEntry{
		{
			Actor:  AuditActor{Role: "executor", AgentID: "bcc-executor-x"},
			Method: "task_complete",
			Target: AuditTarget{SessionID: "s1", PhaseID: "P1", TaskID: "T1"},
			Result: AuditResult{Status: AuditStatusSuccess},
		},
		{
			Actor:  AuditActor{Role: "user"},
			Method: "force_approve",
			Target: AuditTarget{SessionID: "s1", PhaseID: "P1"},
			Result: AuditResult{Code: CodeConflict},
		},
	}
	for _, e := range entries {
		if err := a.Record(context.Background(), e); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	scanner := bufio.NewScanner(strings.NewReader(string(body)))
	var read []AuditEntry
	for scanner.Scan() {
		var e AuditEntry
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			t.Fatalf("unmarshal %q: %v", scanner.Text(), err)
		}
		read = append(read, e)
	}
	if len(read) != 2 {
		t.Fatalf("len = %d, want 2", len(read))
	}
	if read[0].Method != "task_complete" {
		t.Fatalf("Method[0] = %q", read[0].Method)
	}
	if read[0].Result.Status != AuditStatusSuccess {
		t.Fatalf("Status[0] = %q", read[0].Result.Status)
	}
	if read[1].Result.Code != CodeConflict {
		t.Fatalf("Code[1] = %q", read[1].Result.Code)
	}
	if read[1].Result.Status != AuditStatusError {
		t.Fatalf("Status[1] = %q, want %q (auto-derived from Code)", read[1].Result.Status, AuditStatusError)
	}
	if read[0].At.IsZero() {
		t.Fatal("At should be auto-stamped")
	}
}

func TestAudit_ConcurrentWrites(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "audit.ndjson")
	a := newAudit(Deps{AuditPath: path})
	defer a.Close()

	const writers = 16
	const each = 64
	var wg sync.WaitGroup
	wg.Add(writers)
	start := make(chan struct{})
	for w := 0; w < writers; w++ {
		go func(id int) {
			defer wg.Done()
			<-start
			for i := 0; i < each; i++ {
				_ = a.Record(context.Background(), AuditEntry{
					Actor:  AuditActor{Role: "executor", AgentID: "agent"},
					Method: "task_complete",
					Target: AuditTarget{SessionID: "s1", TaskID: "t"},
					Result: AuditResult{Status: AuditStatusSuccess},
				})
			}
		}(w)
	}
	close(start)
	wg.Wait()
	if err := a.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	count := 0
	scanner := bufio.NewScanner(strings.NewReader(string(body)))
	for scanner.Scan() {
		var e AuditEntry
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			t.Fatalf("interleaved write produced invalid JSON: %v (line=%q)", err, scanner.Text())
		}
		if e.Method != "task_complete" {
			t.Fatalf("Method = %q", e.Method)
		}
		count++
	}
	if count != writers*each {
		t.Fatalf("count = %d, want %d", count, writers*each)
	}
}

func TestAudit_NoPathDoesNotWriteFile(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	// Different path; Audit.path is empty so the call should be a no-op
	// on disk but the slog mirror still fires.
	a := newAudit(Deps{})
	if err := a.Record(context.Background(), AuditEntry{
		Actor:  AuditActor{Role: "user"},
		Method: "noop",
		Result: AuditResult{Status: AuditStatusSuccess},
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	entries, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected no files in tmp, got %v", entries)
	}
}

func TestAudit_RespectsContextCancel(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	a := newAudit(Deps{AuditPath: filepath.Join(tmp, "audit.ndjson")})
	defer a.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := a.Record(ctx, AuditEntry{
		Actor:  AuditActor{Role: "user"},
		Method: "noop",
		Result: AuditResult{Status: AuditStatusSuccess},
	})
	if err == nil {
		t.Fatal("expected ctx.Err on cancelled ctx")
	}
}

func TestAudit_NilReceiverIsNoop(t *testing.T) {
	t.Parallel()
	var a *Audit
	if err := a.Record(context.Background(), AuditEntry{}); err != nil {
		t.Fatalf("nil Record: %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("nil Close: %v", err)
	}
}

func TestAudit_AutoTimestamp(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	a := newAudit(Deps{AuditPath: filepath.Join(tmp, "audit.ndjson")})
	defer a.Close()
	before := time.Now().UTC()
	if err := a.Record(context.Background(), AuditEntry{
		Actor:  AuditActor{Role: "user"},
		Method: "x",
		Result: AuditResult{Status: AuditStatusSuccess},
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(tmp, "audit.ndjson"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var e AuditEntry
	if err := json.Unmarshal(body[:len(body)-1], &e); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if e.At.Before(before) {
		t.Fatalf("At = %v, before = %v", e.At, before)
	}
}
