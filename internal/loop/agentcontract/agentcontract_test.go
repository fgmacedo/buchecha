package agentcontract

import (
	"bytes"
	"strings"
	"testing"
	"text/template"
)

func TestSignal_String(t *testing.T) {
	cases := []struct {
		s    Signal
		want string
	}{
		{SignalUnknown, "unknown"},
		{SignalContinue, "continue"},
		{SignalReview, "review"},
		{SignalDone, "done"},
		{SignalBlocked, "blocked"},
	}
	for _, tc := range cases {
		if got := tc.s.String(); got != tc.want {
			t.Errorf("Signal(%d).String() = %q, want %q", tc.s, got, tc.want)
		}
	}
}

func TestPartials_DefinesAllThreeBlocks(t *testing.T) {
	p := Partials()
	for _, name := range []string{"wire_protocol", "absolute_restrictions", "working_tree"} {
		if p.Lookup(name) == nil {
			t.Errorf("partial %q missing", name)
		}
	}
}

func TestPartials_WireProtocol_DescribesMCPMethods(t *testing.T) {
	p := Partials()
	var buf bytes.Buffer
	if err := p.ExecuteTemplate(&buf, "wire_protocol", nil); err != nil {
		t.Fatalf("execute wire_protocol: %v", err)
	}
	body := buf.String()
	for _, want := range []string{
		// Per-role surfaces the Director loop relies on.
		"Planner", "Briefer", "Executor", "Reviewer",
		"agent_id",
		"plan_emit",
		"briefing_emit",
		"get_dag_snapshot",
		"get_briefing",
		"get_pending_tasks",
		"get_baseline",
		"get_journal_delta",
		"task_started",
		"task_completed",
		"task_approved",
		"task_needs_fix",
		"iteration_finished",
		"review_finished",
		// Wire alphabets stay canonical English regardless of locale.
		"continue", "review", "done", "blocked",
		"approve", "revise", "escalate",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("wire_protocol partial missing %q", want)
		}
	}
}

// TestPartials_WireProtocol_NoLegacyEnvelope ensures the rewritten
// partial does not still describe the demoted stream-json envelope as
// the protocol of record.
func TestPartials_WireProtocol_NoLegacyEnvelope(t *testing.T) {
	p := Partials()
	var buf bytes.Buffer
	if err := p.ExecuteTemplate(&buf, "wire_protocol", nil); err != nil {
		t.Fatalf("execute wire_protocol: %v", err)
	}
	body := buf.String()
	for _, banned := range []string{
		"mcp__bcc__task_started",
		"mcp__bcc__task_completed",
		"mcp__bcc__iteration_result",
	} {
		if strings.Contains(body, banned) {
			t.Errorf("wire_protocol partial still references legacy envelope %q", banned)
		}
	}
}

// TestPartials_NoFrameworkPathLeaks ensures the embedded contract stays
// invariant to the user's working directory: no partial may name a path
// that lives inside the bcc binary's source tree. The agent must learn
// every framework artifact (schemas, prompts, packages) from the prompt
// itself or via MCP, never by reaching for a file in the cwd. A leak
// makes the contract silently dependent on dogfooding inside the bcc
// repo, since the file would only exist there.
func TestPartials_NoFrameworkPathLeaks(t *testing.T) {
	p := Partials()
	for _, name := range []string{"wire_protocol", "absolute_restrictions", "working_tree", "what_bcc_is"} {
		var buf bytes.Buffer
		view := struct{ Role string }{Role: "planner"}
		if err := p.ExecuteTemplate(&buf, name, view); err != nil {
			t.Fatalf("execute %s: %v", name, err)
		}
		body := buf.String()
		for _, banned := range []string{
			".schema.json",
			"internal/",
		} {
			if strings.Contains(body, banned) {
				t.Errorf("%s partial leaks framework path %q; describe the contract without naming files inside the bcc binary", name, banned)
			}
		}
	}
}

func TestPartials_ComposableWithChild(t *testing.T) {
	parent := Partials()
	child, err := parent.New("child").Parse("BEGIN\n{{template \"wire_protocol\" .}}\nEND\n")
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := child.Execute(&buf, nil); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.HasPrefix(out, "BEGIN\n") || !strings.HasSuffix(out, "\nEND\n") {
		t.Errorf("child template did not wrap partial correctly:\n%s", out)
	}
}

// Ensure the package's exported template name does not collide with
// a typical adapter's contract template.
func TestPartials_ParentNamePartials(t *testing.T) {
	p := Partials()
	if got := p.Name(); got != "partials" {
		t.Errorf("parent template name = %q, want %q", got, "partials")
	}
	// The Name() should not interfere with adapters parsing their own
	// "contract" template name on top.
	_ = template.Must(p.New("contract").Parse("hello"))
}
