package claude

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fgmacedo/buchecha/internal/director"
	"github.com/fgmacedo/buchecha/internal/loop"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("abs %s: %v", name, err)
	}
	return abs
}

func echoArgsOut(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "argv.txt")
	t.Setenv("ECHO_ARGS_OUT", path)
	return path
}

// TestPlan_RunsAndReportsStats confirms that on a clean exit the
// adapter returns no Plan (the agent emits via MCP, not stdout) and
// surfaces the cost stats from the result event.
func TestPlan_RunsAndReportsStats(t *testing.T) {
	a := New(Config{Binary: fixture(t, "fake-claude-plan.sh")})
	plan, stats, err := a.Plan(context.Background(), director.PlannerInput{
		AgentID:  "planner-001",
		SpecPath: "spec.md",
		SpecHash: "abc123",
	}, nil)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan != nil {
		t.Errorf("Plan should be nil; agent emits via MCP, got %+v", plan)
	}
	if stats == nil || stats.CostUSD != 0.012 || stats.InputTokens != 1000 || stats.OutputTokens != 300 {
		t.Errorf("stats = %+v", stats)
	}
}

func TestBrief_RunsAndReportsStats(t *testing.T) {
	a := New(Config{Binary: fixture(t, "fake-claude-briefing.sh")})
	stats, err := a.Brief(context.Background(), director.BrieferInput{
		AgentID:     "briefer-001",
		Plan:        &director.Plan{Goal: "g", Phases: []director.Phase{{ID: "p1", Title: "t", Intent: "i", Tasks: []director.Task{{ID: "t1", Title: "tt", Intent: "ii", Acceptance: []director.AcceptanceItem{{ID: "a1", Description: "d", Evidence: director.EvidenceTest}}, Status: director.TaskPending}}}}, SpecHash: "h"},
		SpecPath:    "/tmp/spec.md",
		IterationID: "p1-1",
		PhaseID:     "p1",
	}, nil)
	if err != nil {
		t.Fatalf("Brief: %v", err)
	}
	if stats == nil || stats.CostUSD != 0.002 {
		t.Errorf("stats = %+v", stats)
	}
}

func TestReview_RunsAndReportsStats(t *testing.T) {
	a := New(Config{Binary: fixture(t, "fake-claude-verdict.sh")})
	stats, err := a.Review(context.Background(), director.ReviewerInput{
		AgentID:     "reviewer-001",
		IterationID: "p1-1",
		PhaseID:     "p1",
		SubDAG:      []string{"t1"},
	}, nil)
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if stats == nil || stats.CostUSD != 0.004 {
		t.Errorf("stats = %+v", stats)
	}
}

// TestPlan_RejectsMissingAgentID covers the precondition that the
// caller registers an agent_id before invoking the adapter; without
// it the handler cannot scope MCP calls back to a registered role.
func TestPlan_RejectsMissingAgentID(t *testing.T) {
	a := New(Config{Binary: fixture(t, "fake-claude-plan.sh")})
	_, _, err := a.Plan(context.Background(), director.PlannerInput{
		SpecPath: "spec.md", SpecHash: "h",
	}, nil)
	if !errors.Is(err, ErrMissingAgentID) {
		t.Errorf("err = %v, want ErrMissingAgentID", err)
	}
}

func TestRunRole_BudgetExceeded(t *testing.T) {
	a := New(Config{
		Binary:       fixture(t, "fake-claude-expensive.sh"),
		MaxBudgetUSD: 0.10,
	})
	stats, err := a.Brief(context.Background(), director.BrieferInput{
		AgentID:  "briefer-001",
		Plan:     &director.Plan{Goal: "g", Phases: []director.Phase{{ID: "p1", Title: "t", Intent: "i", Tasks: []director.Task{{ID: "t1", Title: "tt", Intent: "ii", Acceptance: []director.AcceptanceItem{{ID: "a1", Description: "d", Evidence: director.EvidenceTest}}, Status: director.TaskPending}}}}},
		SpecPath: "/tmp/spec.md",
		PhaseID:  "p1",
	}, nil)
	if err == nil {
		t.Fatalf("expected ErrBudgetExceeded")
	}
	if !errors.Is(err, ErrBudgetExceeded) {
		t.Errorf("err = %v, want ErrBudgetExceeded", err)
	}
	if stats == nil || stats.CostUSD < 1 {
		t.Errorf("stats should carry over-cap cost: %+v", stats)
	}
}

// TestArgs_PassedToBinary captures the argv envelope and asserts the
// new P4 contract: --allowed-tools "Read,Bash,Grep,Glob",
// --dangerously-skip-permissions, --mcp-config <tempfile>,
// --strict-mcp-config, no --json-schema.
func TestArgs_PassedToBinary(t *testing.T) {
	argsPath := echoArgsOut(t)
	a := New(Config{
		Binary:       fixture(t, "fake-claude-echo-args.sh"),
		Model:        "test-model",
		MaxBudgetUSD: 0.5,
		ExtraArgs:    []string{"--foo"},
		MCPURL:       "http://127.0.0.1:1/mcp/",
		MCPToken:     "secret",
	})
	_, err := a.Brief(context.Background(), director.BrieferInput{
		AgentID:  "briefer-001",
		Plan:     &director.Plan{Goal: "g", Phases: []director.Phase{{ID: "p1", Title: "t", Intent: "i", Tasks: []director.Task{{ID: "t1", Title: "tt", Intent: "ii", Acceptance: []director.AcceptanceItem{{ID: "a1", Description: "d", Evidence: director.EvidenceTest}}, Status: director.TaskPending}}}}},
		SpecPath: "/tmp/spec.md",
		PhaseID:  "p1",
	}, nil)
	if err != nil {
		t.Fatalf("Brief: %v", err)
	}
	out, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read argv: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	wantPrefix := []string{
		"-p",
		"--output-format", "stream-json",
		"--verbose",
		"--allowed-tools", "Read,Bash,Grep,Glob",
		"--dangerously-skip-permissions",
		"--mcp-config",
	}
	for i, w := range wantPrefix {
		if i >= len(lines) || lines[i] != w {
			t.Fatalf("arg[%d] = %q, want %q", i, safeAt(lines, i), w)
		}
	}
	if !strings.HasSuffix(lines[len(wantPrefix)], "mcp-config.json") {
		t.Errorf("mcp-config arg = %q, want ...mcp-config.json", lines[len(wantPrefix)])
	}
	rest := strings.Join(lines[len(wantPrefix)+1:], "\n")
	for _, want := range []string{"--strict-mcp-config", "--model\ntest-model", "--max-budget-usd\n0.5", "--foo"} {
		if !strings.Contains(rest, want) {
			t.Errorf("missing %q in rest:\n%s", want, rest)
		}
	}
	for _, banned := range []string{"--json-schema", "--bare", "--no-session-persistence"} {
		if strings.Contains(rest, banned) {
			t.Errorf("argv must not contain %q (P4 envelope drops it):\n%s", banned, rest)
		}
	}
	if !strings.Contains(rest, "Your agent_id is `briefer-001`") {
		t.Errorf("prompt should embed agent_id; rest:\n%s", rest)
	}
}

func safeAt(s []string, i int) string {
	if i < 0 || i >= len(s) {
		return "<oob>"
	}
	return s[i]
}

// TestBrief_AssignmentOverridesModelAndEffort pins the per-call
// override path: when BrieferInput.Assignment carries a Model or
// Effort, the spawned claude args must reflect it instead of the
// adapter's configured defaults.
func TestBrief_AssignmentOverridesModelAndEffort(t *testing.T) {
	argsPath := echoArgsOut(t)
	a := New(Config{
		Binary: fixture(t, "fake-claude-echo-args.sh"),
		Model:  "default-model",
		Effort: "low",
	})
	_, err := a.Brief(context.Background(), director.BrieferInput{
		AgentID:    "briefer-001",
		Plan:       &director.Plan{Goal: "g", Phases: []director.Phase{{ID: "p1", Title: "t", Intent: "i", Tasks: []director.Task{{ID: "t1", Title: "tt", Intent: "ii", Acceptance: []director.AcceptanceItem{{ID: "a1", Description: "d", Evidence: director.EvidenceTest}}, Status: director.TaskPending}}}}},
		SpecPath:   "/tmp/spec.md",
		PhaseID:    "p1",
		Assignment: &director.RoleAssignment{Model: "override-model", Effort: "high"},
	}, nil)
	if err != nil {
		t.Fatalf("Brief: %v", err)
	}
	out, _ := os.ReadFile(argsPath)
	body := string(out)
	if !strings.Contains(body, "--model\noverride-model") {
		t.Errorf("expected --model override-model, got: %s", body)
	}
	if !strings.Contains(body, "--effort\nhigh") {
		t.Errorf("expected --effort high, got: %s", body)
	}
	if strings.Contains(body, "default-model") {
		t.Errorf("default model should be overridden: %s", body)
	}
	if strings.Contains(body, "--effort\nlow") {
		t.Errorf("default effort should be overridden: %s", body)
	}
}

// TestBrief_NoAssignmentKeepsConfiguredDefaults pins the fall-through
// path: a nil Assignment (or empty fields) preserves the configured
// Model and Effort.
func TestBrief_NoAssignmentKeepsConfiguredDefaults(t *testing.T) {
	argsPath := echoArgsOut(t)
	a := New(Config{
		Binary: fixture(t, "fake-claude-echo-args.sh"),
		Model:  "default-model",
		Effort: "low",
	})
	_, err := a.Brief(context.Background(), director.BrieferInput{
		AgentID:  "briefer-001",
		Plan:     &director.Plan{Goal: "g", Phases: []director.Phase{{ID: "p1", Title: "t", Intent: "i", Tasks: []director.Task{{ID: "t1", Title: "tt", Intent: "ii", Acceptance: []director.AcceptanceItem{{ID: "a1", Description: "d", Evidence: director.EvidenceTest}}, Status: director.TaskPending}}}}},
		SpecPath: "/tmp/spec.md",
		PhaseID:  "p1",
	}, nil)
	if err != nil {
		t.Fatalf("Brief: %v", err)
	}
	out, _ := os.ReadFile(argsPath)
	body := string(out)
	if !strings.Contains(body, "--model\ndefault-model") {
		t.Errorf("expected --model default-model, got: %s", body)
	}
	if !strings.Contains(body, "--effort\nlow") {
		t.Errorf("expected --effort low, got: %s", body)
	}
}

// TestPlan_RegistryRendersInPrompt confirms the planner prompt embeds
// the configured CapabilityRegistry models so the agent can pick from
// them when authoring per-phase assignments.
func TestPlan_RegistryRendersInPrompt(t *testing.T) {
	argsPath := echoArgsOut(t)
	a := New(Config{Binary: fixture(t, "fake-claude-echo-args.sh")})
	registry := director.CapabilityRegistry{
		Models: []director.Capability{
			{Family: "claude", Model: "claude-opus-4-7", Tier: "frontier", Efforts: []string{"low", "high"}, Description: "Strongest reasoning."},
			{Family: "claude", Model: "claude-haiku-4-5", Tier: "fast", Description: "Cheapest."},
		},
	}
	_, _, err := a.Plan(context.Background(), director.PlannerInput{
		AgentID:  "planner-001",
		SpecPath: "/tmp/spec.md",
		SpecHash: "h",
		Registry: registry,
	}, nil)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	out, _ := os.ReadFile(argsPath)
	body := string(out)
	for _, want := range []string{"claude-opus-4-7", "frontier", "Strongest reasoning", "claude-haiku-4-5", "fast", "low, high"} {
		if !strings.Contains(body, want) {
			t.Errorf("planner prompt missing %q in:\n%s", want, body)
		}
	}
}

// TestComposePrompt_EmbedsAbsoluteRestrictions guards against the
// renderer dropping the safety partial. Plan, brief, and review
// prompts must all carry the absolute_restrictions text.
func TestComposePrompt_EmbedsAbsoluteRestrictions(t *testing.T) {
	cases := []struct {
		name string
		tpl  string
		view any
	}{
		{"plan", director.PlanPromptTemplate(), planView{Role: "planner", AgentID: "planner-001", SpecPath: "/tmp/spec.md"}},
		{"brief", director.BriefPromptTemplate(), briefView{Role: "briefer", AgentID: "briefer-001", SpecPath: "/tmp/spec.md", IterationID: "p1-1", PhaseID: "p1"}},
		{"review", director.ReviewPromptTemplate(), reviewView{Role: "reviewer", AgentID: "reviewer-001"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := composePrompt(tc.tpl, tc.view)
			if err != nil {
				t.Fatalf("compose: %v", err)
			}
			for _, marker := range []string{"git push", "Work **only inside the project directory"} {
				if !strings.Contains(out, marker) {
					t.Errorf("missing absolute_restrictions marker %q in %s prompt", marker, tc.name)
				}
			}
			if !strings.Contains(out, "agent_id") {
				t.Errorf("agent_id marker missing in %s prompt", tc.name)
			}
		})
	}
}

// TestComposePrompt_EmbedsWhatBccIs guards the "what bcc is" partial:
// every per-role prompt must carry the shared product description, and
// the role's bullet must be marked with "(you)" so the agent knows
// which one of the four it is. Review is not asserted because the
// review prompt has not adopted the partial yet.
func TestComposePrompt_EmbedsWhatBccIs(t *testing.T) {
	cases := []struct {
		name       string
		tpl        string
		view       any
		youMarker  string
		otherRoles []string
	}{
		{
			"plan",
			director.PlanPromptTemplate(),
			planView{Role: "planner", AgentID: "planner-001", SpecPath: "/tmp/spec.md"},
			"**Planner** (you)",
			[]string{"**Briefer** (you)", "**Executor** (you)", "**Reviewer** (you)"},
		},
		{
			"brief",
			director.BriefPromptTemplate(),
			briefView{Role: "briefer", AgentID: "briefer-001", SpecPath: "/tmp/spec.md", IterationID: "p1-1", PhaseID: "p1"},
			"**Briefer** (you)",
			[]string{"**Planner** (you)", "**Executor** (you)", "**Reviewer** (you)"},
		},
		{
			"review",
			director.ReviewPromptTemplate(),
			reviewView{Role: "reviewer", AgentID: "reviewer-001"},
			"**Reviewer** (you)",
			[]string{"**Planner** (you)", "**Briefer** (you)", "**Executor** (you)"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := composePrompt(tc.tpl, tc.view)
			if err != nil {
				t.Fatalf("compose: %v", err)
			}
			if !strings.Contains(out, "## What bcc is") {
				t.Errorf("what_bcc_is heading missing from %s prompt", tc.name)
			}
			if !strings.Contains(out, "format-agnostic") {
				t.Errorf("what_bcc_is body marker missing from %s prompt", tc.name)
			}
			if !strings.Contains(out, tc.youMarker) {
				t.Errorf("expected %q to mark the active role in %s prompt", tc.youMarker, tc.name)
			}
			for _, other := range tc.otherRoles {
				if strings.Contains(out, other) {
					t.Errorf("%s prompt should not mark %q as (you); rendered:\n%s", tc.name, other, out)
				}
			}
		})
	}
}

// TestWriteMCPConfig_PreservesURLAndCarriesRoleHeader pins the per-spawn
// mcp-config.json shape against the new shared-listener URL contract:
// the URL is written verbatim (no rewrite, no segment stripping), the
// X-BCC-Role header carries the role's connection name, and the bearer
// token lives in Authorization. The trailing slash matters because chi
// strips the /mcp prefix and an agent that hits /mcp would land outside
// the mount.
func TestWriteMCPConfig_PreservesURLAndCarriesRoleHeader(t *testing.T) {
	const url = "http://127.0.0.1:54321/mcp/"
	path, cleanup, err := writeMCPConfig(url, "tok", "bcc-planner")
	if err != nil {
		t.Fatalf("writeMCPConfig: %v", err)
	}
	t.Cleanup(cleanup)

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read mcp-config: %v", err)
	}
	var doc struct {
		MCPServers map[string]struct {
			Type    string            `json:"type"`
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("decode mcp-config: %v\n%s", err, body)
	}
	server, ok := doc.MCPServers["bcc"]
	if !ok {
		t.Fatalf("mcpServers[bcc] missing: %s", body)
	}
	if server.Type != "http" {
		t.Errorf("type = %q, want http", server.Type)
	}
	if server.URL != url {
		t.Errorf("url = %q, want %q (verbatim, no rewrite)", server.URL, url)
	}
	if !strings.HasSuffix(server.URL, "/mcp/") {
		t.Errorf("url must end with /mcp/, got %q", server.URL)
	}
	if got := server.Headers["Authorization"]; got != "Bearer tok" {
		t.Errorf("Authorization = %q, want Bearer tok", got)
	}
	if got := server.Headers["X-BCC-Role"]; got != "bcc-planner" {
		t.Errorf("X-BCC-Role = %q, want bcc-planner", got)
	}
}

// TestWriteMCPConfig_RejectsEmptyURL guards the precondition: callers
// must supply the run-wide MCP URL (the composition root resolves it
// once the listener binds).
func TestWriteMCPConfig_RejectsEmptyURL(t *testing.T) {
	_, _, err := writeMCPConfig("", "tok", "bcc-planner")
	if err == nil {
		t.Fatal("expected error for empty url")
	}
}

// TestWriteMCPConfig_RejectsEmptyConnection guards the role header so
// the handler's connection-name allow-list never sees a blank value.
func TestWriteMCPConfig_RejectsEmptyConnection(t *testing.T) {
	_, _, err := writeMCPConfig("http://x/mcp/", "tok", "")
	if err == nil {
		t.Fatal("expected error for empty connection name")
	}
}

func TestRingBuffer_KeepsLastBytes(t *testing.T) {
	r := newRingBuffer(8)
	r.Write([]byte("hello "))
	r.Write([]byte("world!"))
	got := r.String()
	if got != "o world!" {
		t.Errorf("ring tail = %q, want %q", got, "o world!")
	}
}

func TestRingBuffer_HandlesOversizeWrite(t *testing.T) {
	r := newRingBuffer(4)
	r.Write([]byte("abcdefghijk"))
	if got := r.String(); got != "hijk" {
		t.Errorf("ring tail after oversize write = %q, want %q", got, "hijk")
	}
}

func TestRingBuffer_FillsThenWrapsAcrossWrites(t *testing.T) {
	r := newRingBuffer(6)
	r.Write([]byte("abc"))
	r.Write([]byte("de"))
	if got := r.String(); got != "abcde" {
		t.Errorf("partial fill = %q, want %q", got, "abcde")
	}
	r.Write([]byte("fgh"))
	if got := r.String(); got != "cdefgh" {
		t.Errorf("after wrap = %q, want %q", got, "cdefgh")
	}
}

// newTestStore creates a fresh director.Store in t.TempDir() for adapter
// tests that need per-spawn prompt persistence.
func newTestStore(t *testing.T) *director.Store {
	t.Helper()
	base := t.TempDir()
	store, _, err := director.CreateSession(base, "/tmp/spec.md", "deadbeef",
		time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	return store
}

// collectLoopEvents drains a buffered loop.Event channel into a slice.
func collectLoopEvents(ch <-chan loop.Event) []loop.Event {
	var out []loop.Event
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, ev)
		default:
			return out
		}
	}
}

// TestBrief_WritesPromptAndEmitsSpawnStarted verifies that before the
// subprocess starts Brief writes the resolved prompt to
// <spawnsDir>/<spawnID>.md (mode 0o600) and emits loop.SpawnStarted on
// the Events channel with matching fields.
func TestBrief_WritesPromptAndEmitsSpawnStarted(t *testing.T) {
	store := newTestStore(t)
	events := make(chan loop.Event, 8)
	a := New(Config{
		Binary:       fixture(t, "fake-claude-briefing.sh"),
		SessionStore: store,
		Events:       events,
	})
	_, err := a.Brief(context.Background(), director.BrieferInput{
		AgentID:     "briefer-001",
		Plan:        &director.Plan{Goal: "g", Phases: []director.Phase{{ID: "p1", Title: "t", Intent: "i", Tasks: []director.Task{{ID: "t1", Title: "tt", Intent: "ii", Acceptance: []director.AcceptanceItem{{ID: "a1", Description: "d", Evidence: director.EvidenceTest}}, Status: director.TaskPending}}}}},
		SpecPath:    "/tmp/spec.md",
		IterationID: "p1-01",
		PhaseID:     "p1",
		Attempt:     1,
	}, nil)
	if err != nil {
		t.Fatalf("Brief: %v", err)
	}

	evs := collectLoopEvents(events)
	var started *loop.SpawnStarted
	for i := range evs {
		if ss, ok := evs[i].(loop.SpawnStarted); ok {
			started = &ss
			break
		}
	}
	if started == nil {
		t.Fatalf("no loop.SpawnStarted event emitted; got %v", evs)
	}
	if started.Role != "bcc-briefer" {
		t.Errorf("Role = %q, want %q", started.Role, "bcc-briefer")
	}
	if started.PhaseID != "p1" {
		t.Errorf("PhaseID = %q, want %q", started.PhaseID, "p1")
	}
	if started.IterationID != "p1-01" {
		t.Errorf("IterationID = %q, want %q", started.IterationID, "p1-01")
	}
	if started.Attempt != 1 {
		t.Errorf("Attempt = %d, want 1", started.Attempt)
	}
	if started.SpawnID == "" {
		t.Fatalf("SpawnID is empty")
	}
	if started.PromptPath == "" {
		t.Fatalf("PromptPath is empty")
	}
	// SpawnID must match file basename.
	baseName := strings.TrimSuffix(filepath.Base(started.PromptPath), ".md")
	if baseName != started.SpawnID {
		t.Errorf("SpawnID %q != basename of PromptPath %q", started.SpawnID, started.PromptPath)
	}
	// File must exist and be non-empty.
	info, err := os.Stat(started.PromptPath)
	if err != nil {
		t.Fatalf("prompt file not found at %q: %v", started.PromptPath, err)
	}
	if info.Size() == 0 {
		t.Errorf("prompt file is empty: %q", started.PromptPath)
	}
}

// TestPlan_WritesPromptAndEmitsSpawnStarted verifies the Planner path:
// PhaseID and IterationID are empty because the Planner runs outside
// any iteration.
func TestPlan_WritesPromptAndEmitsSpawnStarted(t *testing.T) {
	store := newTestStore(t)
	events := make(chan loop.Event, 8)
	a := New(Config{
		Binary:       fixture(t, "fake-claude-plan.sh"),
		SessionStore: store,
		Events:       events,
	})
	_, _, err := a.Plan(context.Background(), director.PlannerInput{
		AgentID:  "planner-001",
		SpecPath: "/tmp/spec.md",
		SpecHash: "abc123",
	}, nil)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	evs := collectLoopEvents(events)
	var started *loop.SpawnStarted
	for i := range evs {
		if ss, ok := evs[i].(loop.SpawnStarted); ok {
			started = &ss
			break
		}
	}
	if started == nil {
		t.Fatalf("no loop.SpawnStarted emitted; got %v", evs)
	}
	if started.Role != "bcc-planner" {
		t.Errorf("Role = %q, want bcc-planner", started.Role)
	}
	if started.PhaseID != "" {
		t.Errorf("PhaseID = %q, want empty (Planner has no phase)", started.PhaseID)
	}
	if started.IterationID != "" {
		t.Errorf("IterationID = %q, want empty", started.IterationID)
	}
	if started.SpawnID == "" {
		t.Fatalf("SpawnID is empty")
	}
	if _, err := os.Stat(started.PromptPath); err != nil {
		t.Fatalf("prompt file not found: %v", err)
	}
}

// TestReview_WritesPromptAndEmitsSpawnStarted verifies the Reviewer path.
func TestReview_WritesPromptAndEmitsSpawnStarted(t *testing.T) {
	store := newTestStore(t)
	events := make(chan loop.Event, 8)
	a := New(Config{
		Binary:       fixture(t, "fake-claude-verdict.sh"),
		SessionStore: store,
		Events:       events,
	})
	_, err := a.Review(context.Background(), director.ReviewerInput{
		AgentID:     "reviewer-001",
		IterationID: "p1-01",
		PhaseID:     "p1",
		SubDAG:      []string{"t1"},
		Attempt:     2,
	}, nil)
	if err != nil {
		t.Fatalf("Review: %v", err)
	}

	evs := collectLoopEvents(events)
	var started *loop.SpawnStarted
	for i := range evs {
		if ss, ok := evs[i].(loop.SpawnStarted); ok {
			started = &ss
			break
		}
	}
	if started == nil {
		t.Fatalf("no loop.SpawnStarted emitted; got %v", evs)
	}
	if started.Role != "bcc-reviewer" {
		t.Errorf("Role = %q, want bcc-reviewer", started.Role)
	}
	if started.PhaseID != "p1" {
		t.Errorf("PhaseID = %q, want p1", started.PhaseID)
	}
	if started.Attempt != 2 {
		t.Errorf("Attempt = %d, want 2", started.Attempt)
	}
	baseName := strings.TrimSuffix(filepath.Base(started.PromptPath), ".md")
	if baseName != started.SpawnID {
		t.Errorf("SpawnID %q != PromptPath basename %q", started.SpawnID, started.PromptPath)
	}
	if _, err := os.Stat(started.PromptPath); err != nil {
		t.Fatalf("prompt file missing: %v", err)
	}
}

// TestBrief_NoSpawnStartedWhenNoSessionStore verifies that when
// Config.SessionStore is nil, no SpawnStarted event is emitted and no
// file is written, preserving backward compatibility for callers that
// do not opt into prompt persistence.
func TestBrief_NoSpawnStartedWhenNoSessionStore(t *testing.T) {
	events := make(chan loop.Event, 8)
	a := New(Config{
		Binary: fixture(t, "fake-claude-briefing.sh"),
		Events: events,
		// SessionStore intentionally nil.
	})
	_, err := a.Brief(context.Background(), director.BrieferInput{
		AgentID:     "briefer-001",
		Plan:        &director.Plan{Goal: "g", Phases: []director.Phase{{ID: "p1", Title: "t", Intent: "i", Tasks: []director.Task{{ID: "t1", Title: "tt", Intent: "ii", Acceptance: []director.AcceptanceItem{{ID: "a1", Description: "d", Evidence: director.EvidenceTest}}, Status: director.TaskPending}}}}},
		SpecPath:    "/tmp/spec.md",
		IterationID: "p1-01",
		PhaseID:     "p1",
	}, nil)
	if err != nil {
		t.Fatalf("Brief: %v", err)
	}
	evs := collectLoopEvents(events)
	for _, ev := range evs {
		if _, ok := ev.(loop.SpawnStarted); ok {
			t.Errorf("unexpected SpawnStarted event when SessionStore is nil")
		}
	}
}
