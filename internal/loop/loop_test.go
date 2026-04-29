package loop_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fgmacedo/buchecha/internal/config"
	"github.com/fgmacedo/buchecha/internal/executor/fake"
	"github.com/fgmacedo/buchecha/internal/loop"
)

// fakeGit returns scripted SHAs and never errors.
type fakeGit struct {
	heads []string
	idx   int
}

func (f *fakeGit) HeadSHA(_ context.Context) (string, error) {
	if f.idx >= len(f.heads) {
		return "", fmt.Errorf("fakeGit: out of HeadSHA calls (idx=%d)", f.idx)
	}
	s := f.heads[f.idx]
	f.idx++
	return s, nil
}

func (f *fakeGit) CurrentBranch(_ context.Context) (string, error) { return "main", nil }
func (f *fakeGit) IsClean(_ context.Context) (bool, error)         { return true, nil }

// stepfulSpecReader returns the n-th content on the n-th Read call.
type stepfulSpecReader struct {
	contents []string
	idx      int
}

func (s *stepfulSpecReader) Read(_ string) (string, error) {
	if s.idx >= len(s.contents) {
		return "", fmt.Errorf("specreader: out of contents (idx=%d)", s.idx)
	}
	c := s.contents[s.idx]
	s.idx++
	return c, nil
}

// errSpecReader always returns the configured error.
type errSpecReader struct{ err error }

func (e *errSpecReader) Read(_ string) (string, error) { return "", e.err }

// errGit always returns the configured error.
type errGit struct{ err error }

func (e *errGit) HeadSHA(_ context.Context) (string, error)       { return "", e.err }
func (e *errGit) CurrentBranch(_ context.Context) (string, error) { return "", e.err }
func (e *errGit) IsClean(_ context.Context) (bool, error)         { return false, e.err }

// specWith builds a minimal English spec with the given checkbox states
// in a single phase, and a single journal entry with the given result
// value. Helper for table-driven tests.
func specWith(states []string, result string) string {
	var items []string
	for i, s := range states {
		items = append(items, fmt.Sprintf("1. %s Item %d", s, i+1))
	}
	return fmt.Sprintf(`# spec

## Implementation Plan

### P1: phase

%s

## Execution Journal

### entry

- **Result**: %s
`, strings.Join(items, "\n"), result)
}

func newTestConfig() *config.Config {
	c := &config.Config{}
	config.ApplyDefaults(c)
	return c
}

func TestRun_OkThenDone(t *testing.T) {
	cfg := newTestConfig()
	exec := fake.New(
		fake.Step{JSONL: `{"type":"a"}` + "\n", ExitCode: 0},
		fake.Step{JSONL: `{"type":"b"}` + "\n", ExitCode: 0},
	)
	git := &fakeGit{heads: []string{"A", "B", "B", "C"}}
	reader := &stepfulSpecReader{contents: []string{
		specWith([]string{"[x]", "[ ]", "[ ]"}, "ok"),
		specWith([]string{"[x]", "[x]", "[x]"}, "done"),
	}}
	l := &loop.Loop{
		SpecPath:   "x.md",
		Config:     cfg,
		Executor:   exec,
		Git:        git,
		SpecReader: reader,
		JSONLDir:   t.TempDir(),
	}
	code, err := l.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != loop.ExitDone {
		t.Errorf("exit = %d, want %d (loop.ExitDone)", code, loop.ExitDone)
	}
	if exec.CallCount() != 2 {
		t.Errorf("executor called %d times, want 2", exec.CallCount())
	}
}

func TestRun_BlockedStops(t *testing.T) {
	cfg := newTestConfig()
	exec := fake.New(fake.Step{ExitCode: 0})
	git := &fakeGit{heads: []string{"A", "B"}}
	reader := &stepfulSpecReader{contents: []string{
		specWith([]string{"[x]", "[ ]", "[ ]"}, "blocked"),
	}}
	l := &loop.Loop{
		SpecPath: "x.md", Config: cfg, Executor: exec, Git: git,
		SpecReader: reader, JSONLDir: t.TempDir(),
	}
	code, err := l.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != loop.ExitBlocked {
		t.Errorf("exit = %d, want %d (loop.ExitBlocked)", code, loop.ExitBlocked)
	}
}

func TestRun_DoneWithLeftovers(t *testing.T) {
	cfg := newTestConfig()
	exec := fake.New(fake.Step{ExitCode: 0})
	git := &fakeGit{heads: []string{"A", "B"}}
	reader := &stepfulSpecReader{contents: []string{
		specWith([]string{"[x]", "[ ]"}, "done"),
	}}
	l := &loop.Loop{
		SpecPath: "x.md", Config: cfg, Executor: exec, Git: git,
		SpecReader: reader, JSONLDir: t.TempDir(),
	}
	code, err := l.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != loop.ExitDoneWithLeftovers {
		t.Errorf("exit = %d, want %d (loop.ExitDoneWithLeftovers)", code, loop.ExitDoneWithLeftovers)
	}
}

func TestRun_HEADStuck(t *testing.T) {
	cfg := newTestConfig()
	exec := fake.New(fake.Step{ExitCode: 0})
	git := &fakeGit{heads: []string{"A", "A"}}
	reader := &stepfulSpecReader{contents: []string{
		specWith([]string{"[x]", "[ ]"}, "ok"),
	}}
	l := &loop.Loop{
		SpecPath: "x.md", Config: cfg, Executor: exec, Git: git,
		SpecReader: reader, JSONLDir: t.TempDir(),
	}
	code, err := l.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != loop.ExitHEADStuck {
		t.Errorf("exit = %d, want %d (loop.ExitHEADStuck)", code, loop.ExitHEADStuck)
	}
}

func TestRun_UnknownResultIsInvalid(t *testing.T) {
	cfg := newTestConfig()
	exec := fake.New(fake.Step{ExitCode: 0})
	git := &fakeGit{heads: []string{"A", "B"}}
	reader := &stepfulSpecReader{contents: []string{
		specWith([]string{"[x]", "[ ]"}, "weird"),
	}}
	l := &loop.Loop{
		SpecPath: "x.md", Config: cfg, Executor: exec, Git: git,
		SpecReader: reader, JSONLDir: t.TempDir(),
	}
	code, err := l.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != loop.ExitInvalid {
		t.Errorf("exit = %d, want %d (loop.ExitInvalid)", code, loop.ExitInvalid)
	}
}

func TestRun_MaxIterationsReached(t *testing.T) {
	cfg := newTestConfig()
	cfg.Loop.MaxIterations = 2
	exec := fake.New(
		fake.Step{ExitCode: 0},
		fake.Step{ExitCode: 0},
	)
	git := &fakeGit{heads: []string{"A", "B", "B", "C"}}
	reader := &stepfulSpecReader{contents: []string{
		specWith([]string{"[x]", "[ ]"}, "ok"),
		specWith([]string{"[x]", "[ ]"}, "partial"), // still has [ ]
	}}
	l := &loop.Loop{
		SpecPath: "x.md", Config: cfg, Executor: exec, Git: git,
		SpecReader: reader, JSONLDir: t.TempDir(),
	}
	code, err := l.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != loop.ExitMaxIterations {
		t.Errorf("exit = %d, want %d (loop.ExitMaxIterations)", code, loop.ExitMaxIterations)
	}
}

func TestRun_ExecutorErrorPropagates(t *testing.T) {
	cfg := newTestConfig()
	wantErr := errors.New("boom")
	exec := fake.New(fake.Step{Err: wantErr})
	git := &fakeGit{heads: []string{"A", "B"}}
	reader := &stepfulSpecReader{} // never reached
	l := &loop.Loop{
		SpecPath: "x.md", Config: cfg, Executor: exec, Git: git,
		SpecReader: reader, JSONLDir: t.TempDir(),
	}
	code, err := l.Run(context.Background())
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want %v", err, wantErr)
	}
	if code != loop.ExitInvalid {
		t.Errorf("code = %d, want loop.ExitInvalid", code)
	}
}

func TestRun_GitErrorPropagates(t *testing.T) {
	cfg := newTestConfig()
	exec := fake.New(fake.Step{ExitCode: 0})
	git := &errGit{err: errors.New("git boom")}
	reader := &stepfulSpecReader{}
	l := &loop.Loop{
		SpecPath: "x.md", Config: cfg, Executor: exec, Git: git,
		SpecReader: reader, JSONLDir: t.TempDir(),
	}
	code, err := l.Run(context.Background())
	if err == nil {
		t.Fatalf("expected error")
	}
	if code != loop.ExitInvalid {
		t.Errorf("code = %d, want loop.ExitInvalid", code)
	}
}

func TestRun_SpecReaderErrorPropagates(t *testing.T) {
	cfg := newTestConfig()
	exec := fake.New(fake.Step{ExitCode: 0})
	git := &fakeGit{heads: []string{"A", "B"}}
	reader := &errSpecReader{err: errors.New("read boom")}
	l := &loop.Loop{
		SpecPath: "x.md", Config: cfg, Executor: exec, Git: git,
		SpecReader: reader, JSONLDir: t.TempDir(),
	}
	code, err := l.Run(context.Background())
	if err == nil {
		t.Fatalf("expected error")
	}
	if code != loop.ExitInvalid {
		t.Errorf("code = %d, want loop.ExitInvalid", code)
	}
}

func TestRun_SingleShotCapsAtOne(t *testing.T) {
	cfg := newTestConfig()
	cfg.Loop.MaxIterations = 99 // would normally allow many iterations
	exec := fake.New(fake.Step{ExitCode: 0})
	git := &fakeGit{heads: []string{"A", "B"}}
	reader := &stepfulSpecReader{contents: []string{
		// even though plan still has [ ], single-shot should cap at 1
		specWith([]string{"[x]", "[ ]"}, "ok"),
	}}
	l := &loop.Loop{
		SpecPath: "x.md", Config: cfg, Executor: exec, Git: git,
		SpecReader: reader, JSONLDir: t.TempDir(), SingleShot: true,
	}
	code, err := l.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != loop.ExitMaxIterations {
		t.Errorf("exit = %d, want %d", code, loop.ExitMaxIterations)
	}
	if exec.CallCount() != 1 {
		t.Errorf("executor called %d times, want 1 (single-shot)", exec.CallCount())
	}
}

func TestRun_PortugueseLocalized(t *testing.T) {
	cfg := &config.Config{Project: config.Project{Language: "pt-BR"}}
	config.ApplyDefaults(cfg)

	plan := strings.Join([]string{
		"1. [x] Item um",
		"1. [x] Item dois",
	}, "\n")
	specPt := fmt.Sprintf(`# spec

## Plano de implementação

### F1

%s

## Diário de execução

- **Resultado**: finalizado
`, plan)

	exec := fake.New(fake.Step{ExitCode: 0})
	git := &fakeGit{heads: []string{"A", "B"}}
	reader := &stepfulSpecReader{contents: []string{specPt}}
	l := &loop.Loop{
		SpecPath: "x.md", Config: cfg, Executor: exec, Git: git,
		SpecReader: reader, JSONLDir: t.TempDir(),
	}
	code, err := l.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != loop.ExitDone {
		t.Errorf("exit = %d, want %d (loop.ExitDone) for pt-BR done with no leftovers", code, loop.ExitDone)
	}
}

func TestRun_RejectsZeroMaxIterations(t *testing.T) {
	cfg := newTestConfig()
	cfg.Loop.MaxIterations = 0 // explicit override after defaults
	l := &loop.Loop{
		SpecPath:   "x.md",
		Config:     cfg,
		Executor:   fake.New(),
		Git:        &fakeGit{},
		SpecReader: &stepfulSpecReader{},
		JSONLDir:   t.TempDir(),
	}
	code, err := l.Run(context.Background())
	if err == nil {
		t.Errorf("expected error for max_iterations <= 0")
	}
	if code != loop.ExitInvalid {
		t.Errorf("code = %d, want loop.ExitInvalid", code)
	}
}

func TestRun_NilPortsRejected(t *testing.T) {
	cfg := newTestConfig()
	l := &loop.Loop{SpecPath: "x.md", Config: cfg}
	code, err := l.Run(context.Background())
	if err == nil {
		t.Errorf("expected error for nil ports")
	}
	if code != loop.ExitInvalid {
		t.Errorf("code = %d, want loop.ExitInvalid", code)
	}
}

func TestRun_JSONLFileWritten(t *testing.T) {
	cfg := newTestConfig()
	dir := t.TempDir()
	exec := fake.New(fake.Step{JSONL: `{"type":"hello"}` + "\n", ExitCode: 0})
	git := &fakeGit{heads: []string{"A", "B"}}
	reader := &stepfulSpecReader{contents: []string{
		specWith([]string{"[x]", "[x]"}, "done"),
	}}
	l := &loop.Loop{
		SpecPath: "/tmp/sample-spec.md", Config: cfg, Executor: exec, Git: git,
		SpecReader: reader, JSONLDir: dir,
	}
	if _, err := l.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Discover the file and check it has content.
	matches, err := filepath.Glob(filepath.Join(dir, "sample-spec-iter1.jsonl"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected 1 jsonl file, got %d", len(matches))
	}
	// Sanity smoke: file exists, non-empty.
	b, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(b) == 0 {
		t.Errorf("jsonl file is empty: %s", matches[0])
	}
	if !strings.Contains(string(b), `"hello"`) {
		t.Errorf("jsonl missing scripted content: %q", string(b))
	}
}
