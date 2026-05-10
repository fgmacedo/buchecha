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

func TestPartials_DefinesAbsoluteRestrictions(t *testing.T) {
	p := Partials()
	if p.Lookup("absolute_restrictions") == nil {
		t.Fatalf("partial absolute_restrictions missing")
	}
	var buf bytes.Buffer
	if err := p.ExecuteTemplate(&buf, "absolute_restrictions", nil); err != nil {
		t.Fatalf("execute absolute_restrictions: %v", err)
	}
	body := buf.String()
	for _, want := range []string{
		"Work **only inside the project directory**",
		"Do not execute",
		"git push",
		"Do not run",
		"Do not touch",
		"Do not change",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("absolute_restrictions partial missing %q", want)
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
	var buf bytes.Buffer
	view := struct{ Role string }{Role: "planner"}
	if err := p.ExecuteTemplate(&buf, "absolute_restrictions", view); err != nil {
		t.Fatalf("execute absolute_restrictions: %v", err)
	}
	body := buf.String()
	for _, banned := range []string{
		".schema.json",
		"internal/",
	} {
		if strings.Contains(body, banned) {
			t.Errorf("absolute_restrictions partial leaks framework path %q; describe the contract without naming files inside the bcc binary", banned)
		}
	}
}

func TestPartials_ComposableWithChild(t *testing.T) {
	parent := Partials()
	child, err := parent.New("child").Parse("BEGIN\n{{template \"absolute_restrictions\" .}}\nEND\n")
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
