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

func TestParseLine_TaskStarted(t *testing.T) {
	in := []byte(`{"type":"bcc_event","event":"task_started","id":"P1.2","summary":"start it"}`)
	got, ok := ParseLine(in)
	if !ok {
		t.Fatalf("ok = false, want true")
	}
	if got.Kind != BccEventTaskStarted {
		t.Errorf("Kind = %v", got.Kind)
	}
	if got.ID != "P1.2" {
		t.Errorf("ID = %q", got.ID)
	}
	if got.Summary != "start it" {
		t.Errorf("Summary = %q", got.Summary)
	}
}

func TestParseLine_TaskCompleted(t *testing.T) {
	got, ok := ParseLine([]byte(`{"type":"bcc_event","event":"task_completed","id":"P1.2"}`))
	if !ok {
		t.Fatal("ok = false")
	}
	if got.Kind != BccEventTaskCompleted || got.ID != "P1.2" {
		t.Errorf("got %+v", got)
	}
}

func TestParseLine_IterationResult_MapsValuesToSignal(t *testing.T) {
	cases := []struct {
		value string
		want  Signal
	}{
		{"continue", SignalContinue},
		{"review", SignalReview},
		{"done", SignalDone},
		{"blocked", SignalBlocked},
		{"weird", SignalUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.value, func(t *testing.T) {
			line := []byte(`{"type":"bcc_event","event":"iteration_result","value":"` + tc.value + `","summary":"x"}`)
			got, ok := ParseLine(line)
			if !ok {
				t.Fatalf("ok = false")
			}
			if got.Kind != BccEventIterationResult {
				t.Errorf("Kind = %v", got.Kind)
			}
			if got.Signal != tc.want {
				t.Errorf("Signal = %v, want %v", got.Signal, tc.want)
			}
		})
	}
}

func TestParseLine_RejectsForeignLines(t *testing.T) {
	cases := [][]byte{
		[]byte(`not json`),
		[]byte(`{"type":"system"}`),
		[]byte(`{"type":"assistant","content":"hi"}`),
		[]byte(`{"type":"bcc_event","event":"unknown_kind"}`),
	}
	for i, line := range cases {
		_, ok := ParseLine(line)
		if ok {
			t.Errorf("case %d: ok=true, want false (line=%s)", i, line)
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

func TestPartials_WireProtocol_DescribesEventValues(t *testing.T) {
	p := Partials()
	var buf bytes.Buffer
	if err := p.ExecuteTemplate(&buf, "wire_protocol", nil); err != nil {
		t.Fatalf("execute wire_protocol: %v", err)
	}
	body := buf.String()
	for _, want := range []string{
		`"event":"task_started"`,
		`"event":"task_completed"`,
		`"event":"iteration_result"`,
		`continue`, `review`, `done`, `blocked`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("wire_protocol partial missing %q", want)
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
