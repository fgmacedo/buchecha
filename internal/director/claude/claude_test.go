package claude

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fgmacedo/buchecha/internal/director"
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
		Attempt:     1,
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
		Attempt:  1,
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
		MCPURL:       "http://127.0.0.1:1/mcp",
		MCPToken:     "secret",
	})
	_, err := a.Brief(context.Background(), director.BrieferInput{
		AgentID:  "briefer-001",
		Plan:     &director.Plan{Goal: "g", Phases: []director.Phase{{ID: "p1", Title: "t", Intent: "i", Tasks: []director.Task{{ID: "t1", Title: "tt", Intent: "ii", Acceptance: []director.AcceptanceItem{{ID: "a1", Description: "d", Evidence: director.EvidenceTest}}, Status: director.TaskPending}}}}},
		SpecPath: "/tmp/spec.md",
		PhaseID:  "p1",
		Attempt:  1,
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
		Attempt:    1,
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
		Attempt:  1,
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
		{"plan", director.PlanPromptTemplate(), planView{AgentID: "planner-001", SpecPath: "/tmp/spec.md"}},
		{"brief", director.BriefPromptTemplate(), briefView{AgentID: "briefer-001", SpecPath: "/tmp/spec.md", IterationID: "p1-1", PhaseID: "p1", Attempt: 1}},
		{"review", director.ReviewPromptTemplate(), reviewView{AgentID: "reviewer-001", SpecPath: "/tmp/spec.md"}},
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
