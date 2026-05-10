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

	"github.com/fgmacedo/buchecha/internal/loop"
	"github.com/fgmacedo/buchecha/internal/supervision"
	"github.com/fgmacedo/buchecha/internal/supervision/session"
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
	plan, stats, err := a.Plan(context.Background(), supervision.PlannerInput{
		AgentID:    "planner-001",
		SpecPath:   "spec.md",
		SpecHash:   "abc123",
		Assignment: supervision.RoleAssignment{Provider: "claude", Model: "claude-opus-4-7", Effort: "high"},
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
	stats, err := a.Brief(context.Background(), supervision.BrieferInput{
		AgentID:     "briefer-001",
		Plan:        &supervision.Plan{Goal: "g", Phases: []supervision.Phase{{ID: "p1", Title: "t", Intent: "i", Tasks: []supervision.Task{{ID: "t1", Title: "tt", Intent: "ii", Acceptance: []supervision.AcceptanceItem{{ID: "a1", Description: "d", Evidence: supervision.EvidenceTest}}, Status: supervision.TaskPending}}}}, SpecHash: "h"},
		SpecPath:    "/tmp/spec.md",
		IterationID: "p1-1",
		PhaseID:     "p1",
		Assignment:  &supervision.RoleAssignment{Provider: "claude", Model: "claude-sonnet-4-6", Effort: "medium"},
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
	stats, err := a.Review(context.Background(), supervision.ReviewerInput{
		AgentID:     "reviewer-001",
		IterationID: "p1-1",
		PhaseID:     "p1",
		SubDAG:      []string{"t1"},
		Assignment:  &supervision.RoleAssignment{Provider: "claude", Model: "claude-sonnet-4-6", Effort: "medium"},
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
	_, _, err := a.Plan(context.Background(), supervision.PlannerInput{
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
	stats, err := a.Brief(context.Background(), supervision.BrieferInput{
		AgentID:    "briefer-001",
		Plan:       &supervision.Plan{Goal: "g", Phases: []supervision.Phase{{ID: "p1", Title: "t", Intent: "i", Tasks: []supervision.Task{{ID: "t1", Title: "tt", Intent: "ii", Acceptance: []supervision.AcceptanceItem{{ID: "a1", Description: "d", Evidence: supervision.EvidenceTest}}, Status: supervision.TaskPending}}}}},
		SpecPath:   "/tmp/spec.md",
		PhaseID:    "p1",
		Assignment: &supervision.RoleAssignment{Provider: "claude", Model: "claude-sonnet-4-6", Effort: "medium"},
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
		MaxBudgetUSD: 0.5,
		ExtraArgs:    []string{"--foo"},
		MCPURL:       "http://127.0.0.1:1/mcp/",
		MCPToken:     "secret",
	})
	_, err := a.Brief(context.Background(), supervision.BrieferInput{
		AgentID:    "briefer-001",
		Plan:       &supervision.Plan{Goal: "g", Phases: []supervision.Phase{{ID: "p1", Title: "t", Intent: "i", Tasks: []supervision.Task{{ID: "t1", Title: "tt", Intent: "ii", Acceptance: []supervision.AcceptanceItem{{ID: "a1", Description: "d", Evidence: supervision.EvidenceTest}}, Status: supervision.TaskPending}}}}},
		SpecPath:   "/tmp/spec.md",
		PhaseID:    "p1",
		Assignment: &supervision.RoleAssignment{Provider: "claude", Model: "test-model"},
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

// TestBrief_AssignmentDrivesModelAndEffort pins the spawn argv: every
// Brief call must carry a (provider, model, effort) triple via
// Assignment, and the resolved values must reach --model and --effort
// on the claude command line.
func TestBrief_AssignmentDrivesModelAndEffort(t *testing.T) {
	argsPath := echoArgsOut(t)
	a := New(Config{Binary: fixture(t, "fake-claude-echo-args.sh")})
	_, err := a.Brief(context.Background(), supervision.BrieferInput{
		AgentID:    "briefer-001",
		Plan:       &supervision.Plan{Goal: "g", Phases: []supervision.Phase{{ID: "p1", Title: "t", Intent: "i", Tasks: []supervision.Task{{ID: "t1", Title: "tt", Intent: "ii", Acceptance: []supervision.AcceptanceItem{{ID: "a1", Description: "d", Evidence: supervision.EvidenceTest}}, Status: supervision.TaskPending}}}}},
		SpecPath:   "/tmp/spec.md",
		PhaseID:    "p1",
		Assignment: &supervision.RoleAssignment{Provider: "claude", Model: "override-model", Effort: "high"},
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
}

// TestBrief_RejectsMissingAssignment pins the contract that every
// Brief/Review call carries a complete RoleAssignment; a nil one is a
// programmer error.
func TestBrief_RejectsMissingAssignment(t *testing.T) {
	a := New(Config{Binary: fixture(t, "fake-claude-echo-args.sh")})
	_, err := a.Brief(context.Background(), supervision.BrieferInput{
		AgentID:  "briefer-001",
		Plan:     &supervision.Plan{Goal: "g", Phases: []supervision.Phase{{ID: "p1", Title: "t", Intent: "i", Tasks: []supervision.Task{{ID: "t1", Title: "tt", Intent: "ii", Acceptance: []supervision.AcceptanceItem{{ID: "a1", Description: "d", Evidence: supervision.EvidenceTest}}, Status: supervision.TaskPending}}}}},
		SpecPath: "/tmp/spec.md",
		PhaseID:  "p1",
	}, nil)
	if err == nil {
		t.Fatalf("expected error on missing assignment")
	}
}

// TestPlan_MenusRenderInPrompt confirms the planner prompt embeds the
// per-role option menus so the agent picks per-phase assignments only
// from the user's declared cardápio.
func TestPlan_MenusRenderInPrompt(t *testing.T) {
	argsPath := echoArgsOut(t)
	a := New(Config{Binary: fixture(t, "fake-claude-echo-args.sh")})
	menus := supervision.RoleMenus{
		Briefer: supervision.RoleMenu{Options: []supervision.MenuOption{
			{Provider: "claude", Model: "claude-haiku-4-5", Efforts: []string{"low"}, Tier: "fast", Summary: "cheapest"},
		}},
		Executor: supervision.RoleMenu{Options: []supervision.MenuOption{
			{Provider: "claude", Model: "claude-opus-4-7", Efforts: []string{"low", "high"}, Tier: "frontier", Summary: "deep reasoning"},
		}},
		Reviewer: supervision.RoleMenu{Options: []supervision.MenuOption{
			{Provider: "claude", Model: "claude-sonnet-4-6", Efforts: []string{"medium"}, Tier: "balanced"},
		}},
	}
	_, _, err := a.Plan(context.Background(), supervision.PlannerInput{
		AgentID:    "planner-001",
		SpecPath:   "/tmp/spec.md",
		SpecHash:   "h",
		Menus:      menus,
		Assignment: supervision.RoleAssignment{Provider: "claude", Model: "claude-opus-4-7", Effort: "high"},
	}, nil)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	out, _ := os.ReadFile(argsPath)
	body := string(out)
	for _, want := range []string{"claude-opus-4-7", "frontier", "deep reasoning", "claude-haiku-4-5", "fast", "low, high", "claude-sonnet-4-6"} {
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
		{"plan", supervision.PlanPromptTemplate(), planView{Role: "planner", AgentID: "planner-001", SpecPath: "/tmp/spec.md"}},
		{"brief", supervision.BriefPromptTemplate(), briefView{Role: "briefer", AgentID: "briefer-001", SpecPath: "/tmp/spec.md", IterationID: "p1-1", PhaseID: "p1"}},
		{"review", supervision.ReviewPromptTemplate(), reviewView{Role: "reviewer", AgentID: "reviewer-001"}},
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

// TestComposePrompt_RoleScoped verifies each per-role prompt frames
// the role on its own terms without leaking pipeline-wide context the
// role does not need. Specifically: no `## What bcc is` block (dropped
// in favor of role-scoped framing), and no mention of the other three
// role names that would distract the agent from its own contract.
func TestComposePrompt_RoleScoped(t *testing.T) {
	cases := []struct {
		name        string
		tpl         string
		view        any
		mustContain []string
		otherRoles  []string
	}{
		{
			"plan",
			supervision.PlanPromptTemplate(),
			planView{Role: "planner", AgentID: "planner-001", SpecPath: "/tmp/spec.md"},
			[]string{"plan_emit", "planner-001"},
			[]string{"## Your role: the Briefer", "## Your role: the Executor", "## Your role: the Reviewer"},
		},
		{
			"brief",
			supervision.BriefPromptTemplate(),
			briefView{Role: "briefer", AgentID: "briefer-001", SpecPath: "/tmp/spec.md", IterationID: "p1-1", PhaseID: "p1"},
			[]string{"briefing_emit", "briefer-001"},
			[]string{"## Your role: the Planner", "## Your role: the Executor", "## Your role: the Reviewer"},
		},
		{
			"review",
			supervision.ReviewPromptTemplate(),
			reviewView{Role: "reviewer", AgentID: "reviewer-001"},
			[]string{"review_finished", "reviewer-001"},
			[]string{"## Your role: the Planner", "## Your role: the Briefer", "## Your role: the Executor"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := composePrompt(tc.tpl, tc.view)
			if err != nil {
				t.Fatalf("compose: %v", err)
			}
			if strings.Contains(out, "## What bcc is") {
				t.Errorf("%s prompt should not carry the shared what_bcc_is block", tc.name)
			}
			for _, want := range tc.mustContain {
				if !strings.Contains(out, want) {
					t.Errorf("%s prompt missing required marker %q", tc.name, want)
				}
			}
			for _, other := range tc.otherRoles {
				if strings.Contains(out, other) {
					t.Errorf("%s prompt should not include %q; the role prompt must stay scoped to its own contract", tc.name, other)
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

// newTestStore creates a fresh session.Store in t.TempDir() for adapter
// tests that need per-spawn prompt persistence.
func newTestStore(t *testing.T) *session.Store {
	t.Helper()
	base := t.TempDir()
	store, _, err := session.CreateSession(base, "/tmp/spec.md", "deadbeef",
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
	_, err := a.Brief(context.Background(), supervision.BrieferInput{
		AgentID:     "briefer-001",
		Plan:        &supervision.Plan{Goal: "g", Phases: []supervision.Phase{{ID: "p1", Title: "t", Intent: "i", Tasks: []supervision.Task{{ID: "t1", Title: "tt", Intent: "ii", Acceptance: []supervision.AcceptanceItem{{ID: "a1", Description: "d", Evidence: supervision.EvidenceTest}}, Status: supervision.TaskPending}}}}},
		SpecPath:    "/tmp/spec.md",
		IterationID: "p1-01",
		PhaseID:     "p1",
		Attempt:     1,
		Assignment:  &supervision.RoleAssignment{Provider: "claude", Model: "claude-sonnet-4-6", Effort: "medium"},
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
	_, _, err := a.Plan(context.Background(), supervision.PlannerInput{
		AgentID:    "planner-001",
		SpecPath:   "/tmp/spec.md",
		SpecHash:   "abc123",
		Assignment: supervision.RoleAssignment{Provider: "claude", Model: "claude-opus-4-7", Effort: "high"},
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
	_, err := a.Review(context.Background(), supervision.ReviewerInput{
		AgentID:     "reviewer-001",
		IterationID: "p1-01",
		PhaseID:     "p1",
		SubDAG:      []string{"t1"},
		Attempt:     2,
		Assignment:  &supervision.RoleAssignment{Provider: "claude", Model: "claude-sonnet-4-6", Effort: "medium"},
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

// TestBrief_EmitsSpawnFinishedWithCost verifies that after Brief returns,
// a loop.SpawnFinished is emitted on the Events channel with the USD cost
// extracted from the result_summary in the fixture stdout and a SpawnID
// that matches the preceding SpawnStarted.
func TestBrief_EmitsSpawnFinishedWithCost(t *testing.T) {
	store := newTestStore(t)
	events := make(chan loop.Event, 16)
	a := New(Config{
		Binary:       fixture(t, "fake-claude-briefing.sh"),
		SessionStore: store,
		Events:       events,
	})
	_, err := a.Brief(context.Background(), supervision.BrieferInput{
		AgentID:     "briefer-001",
		Plan:        &supervision.Plan{Goal: "g", Phases: []supervision.Phase{{ID: "p1", Title: "t", Intent: "i", Tasks: []supervision.Task{{ID: "t1", Title: "tt", Intent: "ii", Acceptance: []supervision.AcceptanceItem{{ID: "a1", Description: "d", Evidence: supervision.EvidenceTest}}, Status: supervision.TaskPending}}}}},
		SpecPath:    "/tmp/spec.md",
		IterationID: "p1-01",
		PhaseID:     "p1",
		Assignment:  &supervision.RoleAssignment{Provider: "claude", Model: "claude-sonnet-4-6", Effort: "medium"},
	}, nil)
	if err != nil {
		t.Fatalf("Brief: %v", err)
	}

	evs := collectLoopEvents(events)
	var started *loop.SpawnStarted
	var finished *loop.SpawnFinished
	for i := range evs {
		if ss, ok := evs[i].(loop.SpawnStarted); ok {
			started = &ss
		}
		if sf, ok := evs[i].(loop.SpawnFinished); ok {
			finished = &sf
		}
	}
	if started == nil {
		t.Fatalf("no loop.SpawnStarted emitted")
	}
	if finished == nil {
		t.Fatalf("no loop.SpawnFinished emitted")
	}
	if finished.SpawnID != started.SpawnID {
		t.Errorf("SpawnFinished.SpawnID %q != SpawnStarted.SpawnID %q", finished.SpawnID, started.SpawnID)
	}
	if finished.Cost.USD != 0.002 {
		t.Errorf("Cost.USD = %v, want 0.002 (from fake-claude-briefing.sh result line)", finished.Cost.USD)
	}
	if finished.DurationMS <= 0 {
		t.Errorf("DurationMS = %d, want > 0", finished.DurationMS)
	}
	if finished.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", finished.ExitCode)
	}
}

// TestBrief_EmitsSpawnFinishedZeroCostWhenNoResult verifies that SpawnFinished
// is still emitted with Cost{} when the subprocess exits without emitting a
// result_summary line.
func TestBrief_EmitsSpawnFinishedZeroCostWhenNoResult(t *testing.T) {
	store := newTestStore(t)
	events := make(chan loop.Event, 16)
	a := New(Config{
		Binary:       fixture(t, "fake-claude-no-result.sh"),
		SessionStore: store,
		Events:       events,
	})
	// Brief will error because the fake emits no result event, but the
	// important thing is SpawnFinished is still emitted before return.
	a.Brief(context.Background(), supervision.BrieferInput{ //nolint:errcheck
		AgentID:     "briefer-001",
		Plan:        &supervision.Plan{Goal: "g", Phases: []supervision.Phase{{ID: "p1", Title: "t", Intent: "i", Tasks: []supervision.Task{{ID: "t1", Title: "tt", Intent: "ii", Acceptance: []supervision.AcceptanceItem{{ID: "a1", Description: "d", Evidence: supervision.EvidenceTest}}, Status: supervision.TaskPending}}}}},
		SpecPath:    "/tmp/spec.md",
		IterationID: "p1-01",
		PhaseID:     "p1",
		Assignment:  &supervision.RoleAssignment{Provider: "claude", Model: "claude-sonnet-4-6", Effort: "medium"},
	}, nil)

	evs := collectLoopEvents(events)
	var finished *loop.SpawnFinished
	for i := range evs {
		if sf, ok := evs[i].(loop.SpawnFinished); ok {
			finished = &sf
		}
	}
	if finished == nil {
		t.Fatalf("no loop.SpawnFinished emitted when no result_summary present")
	}
	if finished.Cost != (loop.SpawnCost{}) {
		t.Errorf("Cost = %+v, want zero Cost{} when no result_summary", finished.Cost)
	}
}

// TestBrief_EmitsSpawnFinishedOnNonZeroExit verifies that SpawnFinished is
// emitted even when the subprocess exits non-zero, carrying the actual exit
// code.
func TestBrief_EmitsSpawnFinishedOnNonZeroExit(t *testing.T) {
	store := newTestStore(t)
	events := make(chan loop.Event, 16)
	a := New(Config{
		Binary:       fixture(t, "fake-claude-fail.sh"),
		SessionStore: store,
		Events:       events,
	})
	// Non-zero exit returns an error; we only care about the event.
	a.Brief(context.Background(), supervision.BrieferInput{ //nolint:errcheck
		AgentID:     "briefer-001",
		Plan:        &supervision.Plan{Goal: "g", Phases: []supervision.Phase{{ID: "p1", Title: "t", Intent: "i", Tasks: []supervision.Task{{ID: "t1", Title: "tt", Intent: "ii", Acceptance: []supervision.AcceptanceItem{{ID: "a1", Description: "d", Evidence: supervision.EvidenceTest}}, Status: supervision.TaskPending}}}}},
		SpecPath:    "/tmp/spec.md",
		IterationID: "p1-01",
		PhaseID:     "p1",
		Assignment:  &supervision.RoleAssignment{Provider: "claude", Model: "claude-sonnet-4-6", Effort: "medium"},
	}, nil)

	evs := collectLoopEvents(events)
	var finished *loop.SpawnFinished
	for i := range evs {
		if sf, ok := evs[i].(loop.SpawnFinished); ok {
			finished = &sf
		}
	}
	if finished == nil {
		t.Fatalf("no loop.SpawnFinished emitted on non-zero exit")
	}
	if finished.ExitCode != 7 {
		t.Errorf("ExitCode = %d, want 7", finished.ExitCode)
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
	_, err := a.Brief(context.Background(), supervision.BrieferInput{
		AgentID:     "briefer-001",
		Plan:        &supervision.Plan{Goal: "g", Phases: []supervision.Phase{{ID: "p1", Title: "t", Intent: "i", Tasks: []supervision.Task{{ID: "t1", Title: "tt", Intent: "ii", Acceptance: []supervision.AcceptanceItem{{ID: "a1", Description: "d", Evidence: supervision.EvidenceTest}}, Status: supervision.TaskPending}}}}},
		SpecPath:    "/tmp/spec.md",
		IterationID: "p1-01",
		PhaseID:     "p1",
		Assignment:  &supervision.RoleAssignment{Provider: "claude", Model: "claude-sonnet-4-6", Effort: "medium"},
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

// TestComposePrompt_PlanUserDirective pins the plan.md conditional rendering
// for spec/prompt combinations: spec-only omits the User directive section,
// prompt-only shows it as source-of-truth, and both shows it as a lens.
func TestComposePrompt_PlanUserDirective(t *testing.T) {
	cases := []struct {
		name    string
		view    planView
		wantStr []string
		notStr  []string
	}{
		{
			name: "spec only",
			view: planView{
				Role:     "planner",
				AgentID:  "a",
				SpecPath: "/tmp/s.md",
				Prompt:   "",
			},
			wantStr: []string{"You read the spec at `/tmp/s.md`"},
			notStr:  []string{"## User directive"},
		},
		{
			name: "prompt only",
			view: planView{
				Role:     "planner",
				AgentID:  "a",
				SpecPath: "",
				Prompt:   "do X",
			},
			wantStr: []string{"This run has no spec file", "## User directive", "They are the source of truth", "```\ndo X\n```"},
			notStr:  []string{"You read the spec at"},
		},
		{
			name: "both",
			view: planView{
				Role:     "planner",
				AgentID:  "a",
				SpecPath: "/tmp/s.md",
				Prompt:   "do X",
			},
			wantStr: []string{"You read the spec at `/tmp/s.md`", "## User directive", "Treat them as a lens over the spec", "```\ndo X\n```"},
			notStr:  []string{"This run has no spec file", "They are the source of truth"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := composePrompt(supervision.PlanPromptTemplate(), tc.view)
			if err != nil {
				t.Fatalf("compose: %v", err)
			}
			for _, want := range tc.wantStr {
				if !strings.Contains(out, want) {
					t.Errorf("missing %q in:\n%s", want, out)
				}
			}
			for _, notWant := range tc.notStr {
				if strings.Contains(out, notWant) {
					t.Errorf("unexpected %q in:\n%s", notWant, out)
				}
			}
		})
	}
}

// TestComposePrompt_BriefSpecGuard pins the brief.md conditional rendering
// for SpecPath: when SpecPath is set, the spec-read sentence is present;
// when empty (prompt-only), it is omitted.
func TestComposePrompt_BriefSpecGuard(t *testing.T) {
	cases := []struct {
		name    string
		view    briefView
		wantStr []string
		notStr  []string
	}{
		{
			name: "with spec",
			view: briefView{
				Role:        "briefer",
				AgentID:     "a",
				SpecPath:    "/tmp/s.md",
				IterationID: "p1-1",
				PhaseID:     "p1",
			},
			wantStr: []string{"Read the spec at `/tmp/s.md`"},
			notStr:  []string{},
		},
		{
			name: "prompt only",
			view: briefView{
				Role:        "briefer",
				AgentID:     "a",
				SpecPath:    "",
				IterationID: "p1-1",
				PhaseID:     "p1",
			},
			wantStr: []string{},
			notStr:  []string{"Read the spec at"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := composePrompt(supervision.BriefPromptTemplate(), tc.view)
			if err != nil {
				t.Fatalf("compose: %v", err)
			}
			for _, want := range tc.wantStr {
				if !strings.Contains(out, want) {
					t.Errorf("missing %q in:\n%s", want, out)
				}
			}
			for _, notWant := range tc.notStr {
				if strings.Contains(out, notWant) {
					t.Errorf("unexpected %q in:\n%s", notWant, out)
				}
			}
		})
	}
}
