package supervision

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
	"github.com/fgmacedo/buchecha/internal/provider"
	"github.com/fgmacedo/buchecha/internal/provider/fake"
)

// newRoles wires a fake provider into a DirectorRoles with the given
// budget cap. The fake is returned so tests can configure its
// per-call SpawnResult / Err / Block before triggering the role.
func newRoles(t *testing.T, name string, cfg DirectorConfig) (*DirectorRoles, *fake.Provider) {
	t.Helper()
	f := &fake.Provider{ProviderName: name}
	r := NewDirectorRoles(provider.NewRegistry(f), cfg)
	return r, f
}

// TestNewDirectorRoles_DefaultAllowedTools pins the default toolbox
// every Director role spawn carries when DirectorConfig.AllowedTools
// is empty: the read-only quartet that lets the Planner, Briefer, and
// Reviewer inspect the repo without ever writing to it.
func TestNewDirectorRoles_DefaultAllowedTools(t *testing.T) {
	r := NewDirectorRoles(provider.NewRegistry(), DirectorConfig{})
	want := []string{"Read", "Bash", "Grep", "Glob"}
	if got := r.cfg.AllowedTools; !equalStrings(got, want) {
		t.Errorf("AllowedTools = %v, want %v", got, want)
	}
}

// TestDirectorRoles_PlanHappyPath confirms a clean SpawnResult flows
// through Plan: cost and tokens land on SpawnStats and the SpawnRequest
// the provider received carries the Planner role label, ReadOnly
// sandbox, the configured toolbox, and the assignment's model/effort.
func TestDirectorRoles_PlanHappyPath(t *testing.T) {
	r, f := newRoles(t, "claude", DirectorConfig{})
	f.Result = provider.SpawnResult{
		ExitCode:   0,
		DurationMS: 1234,
		CostUSD:    0.012,
		Tokens:     agentcontract.TokenUsage{InputFresh: 1000, Output: 300},
	}

	plan, stats, err := r.Plan(context.Background(), PlannerInput{
		AgentID:    "planner-001",
		SpecPath:   "/tmp/spec.md",
		Assignment: RoleAssignment{Provider: "claude", Model: "claude-opus-4-7", Effort: "high"},
	}, nil)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan != nil {
		t.Errorf("plan should be nil (handler is authoritative); got %+v", plan)
	}
	if stats == nil || stats.CostUSD != 0.012 || stats.Tokens.InputFresh != 1000 || stats.DurationMS != 1234 {
		t.Errorf("stats = %+v", stats)
	}

	req, ok := f.LastRequest()
	if !ok {
		t.Fatal("provider never spawned")
	}
	if req.Role != string(agentcontract.RolePlanner) {
		t.Errorf("Role = %q, want %q", req.Role, agentcontract.RolePlanner)
	}
	if req.Sandbox != provider.SandboxReadOnly {
		t.Errorf("Sandbox = %v, want SandboxReadOnly", req.Sandbox)
	}
	if !req.SkipPermissions {
		t.Error("SkipPermissions = false, want true")
	}
	if req.Model != "claude-opus-4-7" || req.Effort != "high" {
		t.Errorf("Model/Effort = %q/%q, want claude-opus-4-7/high", req.Model, req.Effort)
	}
	if req.AgentID != "planner-001" {
		t.Errorf("AgentID = %q, want planner-001", req.AgentID)
	}
	if !equalStrings(req.AllowedTools, []string{"Read", "Bash", "Grep", "Glob"}) {
		t.Errorf("AllowedTools = %v", req.AllowedTools)
	}
	if !strings.Contains(req.Prompt, "Your agent_id is `planner-001`") {
		t.Errorf("prompt should embed agent_id; got: %q", req.Prompt)
	}
}

// TestDirectorRoles_BriefHappyPath mirrors Plan for Briefer: the call
// must forward iteration / phase / attempt and use the Briefer role
// label so the handler's connection-name allow-list authorises the
// briefing_emit that follows.
func TestDirectorRoles_BriefHappyPath(t *testing.T) {
	r, f := newRoles(t, "claude", DirectorConfig{})
	f.Result = provider.SpawnResult{ExitCode: 0, CostUSD: 0.002}

	stats, err := r.Brief(context.Background(), BrieferInput{
		AgentID:     "briefer-001",
		SpecPath:    "/tmp/spec.md",
		IterationID: "p1-01",
		PhaseID:     "p1",
		Attempt:     2,
		Plan:        &Plan{Goal: "g"},
		Assignment:  &RoleAssignment{Provider: "claude", Model: "claude-sonnet-4-6", Effort: "medium"},
	}, nil)
	if err != nil {
		t.Fatalf("Brief: %v", err)
	}
	if stats == nil || stats.CostUSD != 0.002 {
		t.Errorf("stats = %+v", stats)
	}
	req, _ := f.LastRequest()
	if req.Role != string(agentcontract.RoleBriefer) {
		t.Errorf("Role = %q, want %q", req.Role, agentcontract.RoleBriefer)
	}
	if req.IterationID != "p1-01" || req.PhaseID != "p1" || req.Attempt != 2 {
		t.Errorf("origin fields = (%q,%q,%d)", req.IterationID, req.PhaseID, req.Attempt)
	}
}

// TestDirectorRoles_ReviewHappyPath confirms Reviewer carries the
// reviewer role label and the iteration metadata so the Reviewer's
// task_approved / task_needs_fix / review_finished calls scope cleanly.
func TestDirectorRoles_ReviewHappyPath(t *testing.T) {
	r, f := newRoles(t, "claude", DirectorConfig{})
	f.Result = provider.SpawnResult{ExitCode: 0, CostUSD: 0.004}

	stats, err := r.Review(context.Background(), ReviewerInput{
		AgentID:     "reviewer-001",
		IterationID: "p1-01",
		PhaseID:     "p1",
		SubDAG:      []string{"t1"},
		Attempt:     1,
		Assignment:  &RoleAssignment{Provider: "claude", Model: "claude-sonnet-4-6", Effort: "medium"},
	}, nil)
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if stats == nil || stats.CostUSD != 0.004 {
		t.Errorf("stats = %+v", stats)
	}
	req, _ := f.LastRequest()
	if req.Role != string(agentcontract.RoleReviewer) {
		t.Errorf("Role = %q, want %q", req.Role, agentcontract.RoleReviewer)
	}
}

// TestDirectorRoles_BudgetExceeded pins the fail-closed budget cap: a
// SpawnResult with CostUSD above MaxBudgetUSD yields ErrBudgetExceeded
// and the over-cap cost is preserved on the returned stats so the
// caller can include it in escalation logs.
func TestDirectorRoles_BudgetExceeded(t *testing.T) {
	r, f := newRoles(t, "claude", DirectorConfig{MaxBudgetUSD: 0.05})
	f.Result = provider.SpawnResult{ExitCode: 0, CostUSD: 0.12}

	stats, err := r.Brief(context.Background(), BrieferInput{
		AgentID:    "briefer-001",
		PhaseID:    "p1",
		Plan:       &Plan{Goal: "g"},
		Assignment: &RoleAssignment{Provider: "claude", Model: "claude-sonnet-4-6"},
	}, nil)
	if err == nil {
		t.Fatal("expected ErrBudgetExceeded")
	}
	if !errors.Is(err, ErrBudgetExceeded) {
		t.Errorf("err = %v, want wrapping ErrBudgetExceeded", err)
	}
	if stats == nil || stats.CostUSD != 0.12 {
		t.Errorf("stats should carry over-cap cost: %+v", stats)
	}
}

// TestDirectorRoles_ProviderError surfaces the provider error wrapped
// with the role and provider name so the loop can attribute the
// failure on the audit log without further plumbing. Stats remain
// populated with whatever the provider managed to report.
func TestDirectorRoles_ProviderError(t *testing.T) {
	r, f := newRoles(t, "claude", DirectorConfig{})
	f.Err = errors.New("boom")
	f.Result = provider.SpawnResult{ExitCode: 2, DurationMS: 42}

	stats, err := r.Review(context.Background(), ReviewerInput{
		AgentID:     "reviewer-001",
		IterationID: "p1-01",
		PhaseID:     "p1",
		Assignment:  &RoleAssignment{Provider: "claude", Model: "claude-sonnet-4-6"},
	}, nil)
	if err == nil {
		t.Fatal("expected provider error")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("err = %v, want wrap of \"boom\"", err)
	}
	if !strings.Contains(err.Error(), "bcc-reviewer") {
		t.Errorf("err should name the role; got: %v", err)
	}
	if stats == nil || stats.DurationMS != 42 {
		t.Errorf("stats lost provider metadata: %+v", stats)
	}
}

// TestDirectorRoles_ContextCancellation pins propagation: the
// orchestrator must not absorb ctx cancellation. The fake blocks until
// ctx.Done so DirectorRoles must return the same error the provider
// returns (ctx.Err()).
func TestDirectorRoles_ContextCancellation(t *testing.T) {
	r, f := newRoles(t, "claude", DirectorConfig{})
	f.BlockUntilCtxDone = true

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, _, err := r.Plan(ctx, PlannerInput{
			AgentID:    "planner-001",
			Assignment: RoleAssignment{Provider: "claude", Model: "claude-opus-4-7"},
		}, nil)
		done <- err
	}()

	// Give the goroutine a moment to enter the blocking spawn.
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected ctx cancellation error")
		}
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want wrap of context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Plan did not unblock after cancel")
	}
}

// TestDirectorRoles_UnknownProvider exercises the registry guard: if
// the Planner attributes a provider not present in the registry, the
// orchestrator returns a clear error instead of a confusing nil deref.
func TestDirectorRoles_UnknownProvider(t *testing.T) {
	r, _ := newRoles(t, "claude", DirectorConfig{})

	_, _, err := r.Plan(context.Background(), PlannerInput{
		AgentID:    "planner-001",
		Assignment: RoleAssignment{Provider: "codex", Model: "gpt-5"},
	}, nil)
	if err == nil {
		t.Fatal("expected unknown-provider error")
	}
	if !strings.Contains(err.Error(), "codex") {
		t.Errorf("err should name the missing provider; got: %v", err)
	}
}

// TestDirectorRoles_MissingAgentID guards the precondition: every role
// call requires an AgentID; without one the handler cannot scope MCP
// calls back to a registered role.
func TestDirectorRoles_MissingAgentID(t *testing.T) {
	r, _ := newRoles(t, "claude", DirectorConfig{})
	cases := []struct {
		name string
		fn   func() error
	}{
		{"plan", func() error {
			_, _, err := r.Plan(context.Background(), PlannerInput{
				Assignment: RoleAssignment{Provider: "claude", Model: "m"},
			}, nil)
			return err
		}},
		{"brief", func() error {
			_, err := r.Brief(context.Background(), BrieferInput{
				Plan:       &Plan{Goal: "g"},
				Assignment: &RoleAssignment{Provider: "claude", Model: "m"},
			}, nil)
			return err
		}},
		{"review", func() error {
			_, err := r.Review(context.Background(), ReviewerInput{
				Assignment: &RoleAssignment{Provider: "claude", Model: "m"},
			}, nil)
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.fn(); !errors.Is(err, ErrMissingAgentID) {
				t.Errorf("err = %v, want ErrMissingAgentID", err)
			}
		})
	}
}

// TestDirectorRoles_MissingAssignment guards the contract that every
// Brief/Review call carries a complete RoleAssignment; a nil one is a
// programmer error.
func TestDirectorRoles_MissingAssignment(t *testing.T) {
	r, _ := newRoles(t, "claude", DirectorConfig{})
	if _, err := r.Brief(context.Background(), BrieferInput{
		AgentID: "briefer-001",
		Plan:    &Plan{Goal: "g"},
	}, nil); err == nil {
		t.Error("Brief with nil Assignment should error")
	}
	if _, err := r.Review(context.Background(), ReviewerInput{
		AgentID: "reviewer-001",
	}, nil); err == nil {
		t.Error("Review with nil Assignment should error")
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
		{"plan", PlanPromptTemplate(), planView{Role: "planner", AgentID: "planner-001", SpecPath: "/tmp/spec.md"}},
		{"brief", BriefPromptTemplate(), briefView{Role: "briefer", AgentID: "briefer-001", SpecPath: "/tmp/spec.md", IterationID: "p1-1", PhaseID: "p1"}},
		{"review", ReviewPromptTemplate(), reviewView{Role: "reviewer", AgentID: "reviewer-001"}},
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
			PlanPromptTemplate(),
			planView{Role: "planner", AgentID: "planner-001", SpecPath: "/tmp/spec.md"},
			[]string{"plan_emit", "planner-001"},
			[]string{"## Your role: the Briefer", "## Your role: the Executor", "## Your role: the Reviewer"},
		},
		{
			"brief",
			BriefPromptTemplate(),
			briefView{Role: "briefer", AgentID: "briefer-001", SpecPath: "/tmp/spec.md", IterationID: "p1-1", PhaseID: "p1"},
			[]string{"briefing_emit", "briefer-001"},
			[]string{"## Your role: the Planner", "## Your role: the Executor", "## Your role: the Reviewer"},
		},
		{
			"review",
			ReviewPromptTemplate(),
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
			out, err := composePrompt(PlanPromptTemplate(), tc.view)
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
			out, err := composePrompt(BriefPromptTemplate(), tc.view)
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

// TestDirectorRoles_PlanMenusRender confirms the planner prompt embeds
// the per-role option menus so the agent picks per-phase assignments
// only from the user's declared cardápio.
func TestDirectorRoles_PlanMenusRender(t *testing.T) {
	r, f := newRoles(t, "claude", DirectorConfig{})
	f.Result = provider.SpawnResult{ExitCode: 0}

	menus := RoleMenus{
		Briefer: RoleMenu{Options: []MenuOption{
			{Provider: "claude", Model: "claude-haiku-4-5", Efforts: []string{"low"}, Tier: "fast", Summary: "cheapest"},
		}},
		Executor: RoleMenu{Options: []MenuOption{
			{Provider: "claude", Model: "claude-opus-4-7", Efforts: []string{"low", "high"}, Tier: "frontier", Summary: "deep reasoning"},
		}},
		Reviewer: RoleMenu{Options: []MenuOption{
			{Provider: "claude", Model: "claude-sonnet-4-6", Efforts: []string{"medium"}, Tier: "balanced"},
		}},
	}
	_, _, err := r.Plan(context.Background(), PlannerInput{
		AgentID:    "planner-001",
		SpecPath:   "/tmp/spec.md",
		Menus:      menus,
		Assignment: RoleAssignment{Provider: "claude", Model: "claude-opus-4-7", Effort: "high"},
	}, nil)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	req, _ := f.LastRequest()
	for _, want := range []string{"claude-opus-4-7", "frontier", "deep reasoning", "claude-haiku-4-5", "fast", "low, high", "claude-sonnet-4-6"} {
		if !strings.Contains(req.Prompt, want) {
			t.Errorf("planner prompt missing %q in:\n%s", want, req.Prompt)
		}
	}
}

// TestDirectorRoles_ForwardsSessionAndLoopEvents pins that SetSessionStore
// and SetLoopEvents values land on every SpawnRequest. The opaque any
// shape is intentional: provider adapters type-assert internally, and
// the orchestrator must not drop the values silently.
func TestDirectorRoles_ForwardsSessionAndLoopEvents(t *testing.T) {
	r, f := newRoles(t, "claude", DirectorConfig{})
	f.Result = provider.SpawnResult{ExitCode: 0}

	// Use sentinels distinct from any real type.
	type fakeStore struct{ id string }
	store := &fakeStore{id: "abc"}
	loopCh := make(chan struct{}, 1)
	r.SetSessionStore(store)
	r.SetLoopEvents(loopCh)

	_, _, err := r.Plan(context.Background(), PlannerInput{
		AgentID:    "planner-001",
		Assignment: RoleAssignment{Provider: "claude", Model: "m"},
	}, nil)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	req, _ := f.LastRequest()
	if got, ok := req.SessionStore.(*fakeStore); !ok || got != store {
		t.Errorf("SessionStore = %v; want %v", req.SessionStore, store)
	}
	if got, ok := req.LoopEvents.(chan struct{}); !ok || got != loopCh {
		t.Errorf("LoopEvents = %v; want the same channel", req.LoopEvents)
	}
}

// TestDirectorRoles_ForwardsMCPSpec confirms that SetMCPProvider's
// closure is called per spawn and its result lands on SpawnRequest.MCP
// with the role's canonical name in ConnectionName. Without this
// wiring the Director-role subprocesses run with no MCP endpoint and
// cannot emit plan_emit / briefing_emit / task verdicts.
func TestDirectorRoles_ForwardsMCPSpec(t *testing.T) {
	r, f := newRoles(t, "claude", DirectorConfig{})
	f.Result = provider.SpawnResult{ExitCode: 0}

	var observed []agentcontract.Role
	r.SetMCPProvider(func(role agentcontract.Role) provider.MCPSpec {
		observed = append(observed, role)
		return provider.MCPSpec{
			URL:            "http://127.0.0.1:4242/mcp/",
			Token:          "tok-abc",
			ConnectionName: string(role),
		}
	})

	_, _, err := r.Plan(context.Background(), PlannerInput{
		AgentID:    "planner-001",
		Assignment: RoleAssignment{Provider: "claude", Model: "m"},
	}, nil)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	req, _ := f.LastRequest()
	if req.MCP.URL != "http://127.0.0.1:4242/mcp/" {
		t.Errorf("MCP.URL = %q; want the resolver's value", req.MCP.URL)
	}
	if req.MCP.Token != "tok-abc" {
		t.Errorf("MCP.Token = %q; want the resolver's value", req.MCP.Token)
	}
	if req.MCP.ConnectionName != string(agentcontract.RolePlanner) {
		t.Errorf("MCP.ConnectionName = %q; want %q", req.MCP.ConnectionName, agentcontract.RolePlanner)
	}
	if len(observed) != 1 || observed[0] != agentcontract.RolePlanner {
		t.Errorf("resolver called with %v; want [bcc-planner]", observed)
	}
}

// TestDirectorRoles_NilMCPProvider keeps the legacy no-MCP path
// honest: when no resolver is installed, SpawnRequest.MCP stays at its
// zero value so the provider adapter omits the MCP wiring without
// failing.
func TestDirectorRoles_NilMCPProvider(t *testing.T) {
	r, f := newRoles(t, "claude", DirectorConfig{})
	f.Result = provider.SpawnResult{ExitCode: 0}

	_, _, err := r.Plan(context.Background(), PlannerInput{
		AgentID:    "planner-001",
		Assignment: RoleAssignment{Provider: "claude", Model: "m"},
	}, nil)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	req, _ := f.LastRequest()
	if (req.MCP != provider.MCPSpec{}) {
		t.Errorf("MCP = %+v; want zero value when no resolver is set", req.MCP)
	}
}

// equalStrings is a tiny helper for slice equality in tests; the
// stdlib does not ship a string-slice equality outside of slices.Equal
// which exists but reads less clearly inline.
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
